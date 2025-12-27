package golapis

/*
#include "lua_helpers.h"
*/
import "C"
import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// =============================================================================
// TCP Socket Implementation
// =============================================================================

// TCPSocket represents a TCP cosocket compatible with ngx.socket.tcp
type TCPSocket struct {
	conn           net.Conn      // *net.TCPConn or *net.UnixConn
	connectTimeout time.Duration // per-socket connect timeout (0 = no timeout)
	readTimeout    time.Duration // per-socket read timeout (0 = no timeout)
	writeTimeout   time.Duration // per-socket write timeout (0 = no timeout)
	connected      bool          // true after successful connect
	closed         bool          // true after close() called
	isUnix         bool          // true for unix:/ domain sockets
	gen            uint64        // increments to invalidate in-flight async operations
	ownerThread    *C.lua_State  // coroutine that created this socket (for request affinity)

	// TCP-specific: internal read buffer for exact byte reads
	readBuf    []byte
	readBufPos int

	// Connection pooling tracking (stub for now)
	reusedTimes int // number of times retrieved from pool (always 0 for now)

	// Busy state tracking (OpenResty-style)
	connecting bool
	reading    bool
	writing    bool
}

// TCP socket registry - maps socket ID to Go object
var (
	tcpSocketMap     = make(map[uint64]*TCPSocket)
	tcpSocketMu      sync.Mutex
	tcpSocketIDSeq   uint64
	cStrTCPMetatable = C.CString("golapis.socket.tcp") // allocated once, never freed
)

func registerTCPSocket(sock *TCPSocket) uint64 {
	tcpSocketMu.Lock()
	defer tcpSocketMu.Unlock()
	tcpSocketIDSeq++
	tcpSocketMap[tcpSocketIDSeq] = sock
	return tcpSocketIDSeq
}

func getTCPSocketByID(id uint64) *TCPSocket {
	tcpSocketMu.Lock()
	defer tcpSocketMu.Unlock()
	return tcpSocketMap[id]
}

func unregisterTCPSocket(id uint64) {
	tcpSocketMu.Lock()
	defer tcpSocketMu.Unlock()
	delete(tcpSocketMap, id)
}

// getTCPSocketFromUserdata extracts the TCPSocket from Lua userdata at stack index
func getTCPSocketFromUserdata(L *C.lua_State, idx C.int) (*TCPSocket, uint64) {
	ptr := C.lua_touserdata_wrapper(L, idx)
	if ptr == nil {
		return nil, 0
	}
	id := *(*uint64)(ptr)
	return getTCPSocketByID(id), id
}

// checkTCPSocketAffinity verifies the socket belongs to the current thread.
// Returns true if affinity check passes.
// Returns false and pushes (nil, "bad request") to Lua stack if it fails.
func checkTCPSocketAffinity(L *C.lua_State, sock *TCPSocket) bool {
	if sock.ownerThread != L {
		C.lua_pushnil(L)
		pushGoString(L, "bad request")
		return false
	}
	return true
}

func checkTCPSocketBusy(L *C.lua_State, sock *TCPSocket, checkConnect, checkRead, checkWrite bool) bool {
	if checkConnect && sock.connecting {
		C.lua_pushnil(L)
		pushGoString(L, "socket busy connecting")
		return false
	}
	if checkRead && sock.reading {
		C.lua_pushnil(L)
		pushGoString(L, "socket busy reading")
		return false
	}
	if checkWrite && sock.writing {
		C.lua_pushnil(L)
		pushGoString(L, "socket busy writing")
		return false
	}
	return true
}

func consumeLineFromBuffer(sock *TCPSocket) (string, bool) {
	// OpenResty line-mode semantics: stop at LF, strip any CR bytes in the line.
	if sock.readBufPos >= len(sock.readBuf) {
		sock.readBuf = nil
		sock.readBufPos = 0
		return "", false
	}

	data := sock.readBuf[sock.readBufPos:]
	lfIndex := bytes.IndexByte(data, '\n')
	if lfIndex == -1 {
		return "", false
	}

	line := data[:lfIndex]
	sock.readBufPos += lfIndex + 1
	if sock.readBufPos >= len(sock.readBuf) {
		sock.readBuf = nil
		sock.readBufPos = 0
	}

	if bytes.IndexByte(line, '\r') == -1 {
		return string(line), true
	}

	filtered := make([]byte, 0, len(line))
	for _, b := range line {
		if b != '\r' {
			filtered = append(filtered, b)
		}
	}
	return string(filtered), true
}

// =============================================================================
// Exported Functions (called from C wrappers)
// =============================================================================

//export golapis_socket_tcp_new
func golapis_socket_tcp_new(L *C.lua_State) C.int {
	sock := &TCPSocket{
		connectTimeout: 0,
		readTimeout:    0,
		writeTimeout:   0,
		ownerThread:    L,
	}
	id := registerTCPSocket(sock)

	// Create userdata containing the socket ID
	ptr := C.lua_newuserdata(L, C.size_t(unsafe.Sizeof(uint64(0))))
	*(*uint64)(ptr) = id

	// Apply the TCP socket metatable
	C.luaL_getmetatable_wrapper(L, cStrTCPMetatable)
	C.lua_setmetatable(L, -2)

	return 1
}

//export golapis_tcp_settimeout
func golapis_tcp_settimeout(L *C.lua_State) C.int {
	sock, _ := getTCPSocketFromUserdata(L, 1)
	if sock == nil {
		return 0
	}
	if !checkTCPSocketAffinity(L, sock) {
		return 2
	}

	n := int(C.lua_gettop(L))
	if n != 2 {
		pushGoString(L, fmt.Sprintf("golapis.socket settimeout: expecting 2 arguments (including the object) but seen %d", n))
		return -1
	}

	ms := float64(C.lua_tonumber(L, 2))
	timeout := time.Duration(ms) * time.Millisecond
	sock.connectTimeout = timeout
	sock.readTimeout = timeout
	sock.writeTimeout = timeout
	return 0 // settimeout returns nothing per OpenResty spec
}

//export golapis_tcp_settimeouts
func golapis_tcp_settimeouts(L *C.lua_State) C.int {
	sock, _ := getTCPSocketFromUserdata(L, 1)
	if sock == nil {
		return 0
	}
	if !checkTCPSocketAffinity(L, sock) {
		return 2
	}

	n := int(C.lua_gettop(L))
	if n != 4 {
		pushGoString(L, fmt.Sprintf("golapis.socket settimeouts: expecting 4 arguments (including the object) but seen %d", n))
		return -1
	}

	connectMs := float64(C.lua_tonumber(L, 2))
	sendMs := float64(C.lua_tonumber(L, 3))
	readMs := float64(C.lua_tonumber(L, 4))

	sock.connectTimeout = time.Duration(connectMs) * time.Millisecond
	sock.writeTimeout = time.Duration(sendMs) * time.Millisecond
	sock.readTimeout = time.Duration(readMs) * time.Millisecond
	return 0
}

//export golapis_tcp_connect
func golapis_tcp_connect(L *C.lua_State) C.int {
	sock, _ := getTCPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkTCPSocketAffinity(L, sock) {
		return 2
	}

	if sock.closed {
		C.lua_pushnil(L)
		pushGoString(L, "closed")
		return 2
	}

	if !checkTCPSocketBusy(L, sock, true, true, true) {
		return 2
	}

	// If already connected, close existing connection
	if sock.connected && sock.conn != nil {
		sock.conn.Close()
		sock.conn = nil
		sock.connected = false
		sock.isUnix = false
		sock.readBuf = nil
		sock.readBufPos = 0
		sock.gen++
	}

	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		C.lua_pushnil(L)
		pushGoString(L, "connect: could not find thread context")
		return 2
	}

	if C.lua_gettop(L) < 2 || C.lua_isstring(L, 2) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "connect requires host argument")
		return 2
	}

	arg1 := C.GoString(C.lua_tostring_wrapper(L, 2))

	// Unix domain socket: "unix:/path"
	if strings.HasPrefix(arg1, "unix:") {
		path := arg1[5:]
		timeout := sock.connectTimeout
		gen := sock.gen

		sock.connecting = true
		go func() {
			var conn net.Conn
			var err error

			if timeout > 0 {
				conn, err = net.DialTimeout("unix", path, timeout)
			} else {
				conn, err = net.Dial("unix", path)
			}

			if err != nil {
				thread.state.eventChan <- &StateEvent{
					Type:       EventResumeThread,
					Thread:     thread,
					ResumeValues: []interface{}{nil, normalizeNetError(err)},
					OnResume: func(event *StateEvent) {
						sock.connecting = false
					},
				}
				return
			}

			thread.state.eventChan <- &StateEvent{
				Type:       EventResumeThread,
				Thread:     thread,
				ResumeValues: []interface{}{1},
				OnResume: func(event *StateEvent) {
					sock.connecting = false
					if sock.closed || sock.gen != gen {
						if conn != nil {
							conn.Close()
						}
						event.ResumeValues = []interface{}{nil, "closed"}
						return
					}
					sock.conn = conn
					sock.connected = true
					sock.isUnix = true
				},
			}
		}()

		return C.lua_yield_wrapper(L, 0)
	}

	// TCP socket - requires port
	if C.lua_gettop(L) < 3 || C.lua_isnumber(L, 3) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "connect requires port argument")
		return 2
	}

	portNum := float64(C.lua_tonumber(L, 3))
	if portNum != float64(int(portNum)) || portNum < 1 || portNum > 65535 {
		C.lua_pushnil(L)
		pushGoString(L, "invalid port")
		return 2
	}

	port := int(portNum)
	host := arg1
	timeout := sock.connectTimeout
	gen := sock.gen

	sock.connecting = true
	go func() {
		var conn net.Conn
		var err error

		dialer := &net.Dialer{}
		if timeout > 0 {
			dialer.Timeout = timeout
		}

		conn, err = dialer.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))

		if err != nil {
			thread.state.eventChan <- &StateEvent{
				Type:       EventResumeThread,
				Thread:     thread,
				ResumeValues: []interface{}{nil, normalizeNetError(err)},
				OnResume: func(event *StateEvent) {
					sock.connecting = false
				},
			}
			return
		}

		thread.state.eventChan <- &StateEvent{
			Type:       EventResumeThread,
			Thread:     thread,
			ResumeValues: []interface{}{1},
			OnResume: func(event *StateEvent) {
				sock.connecting = false
				if sock.closed || sock.gen != gen {
					conn.Close()
					event.ResumeValues = []interface{}{nil, "closed"}
					return
				}
				sock.conn = conn
				sock.connected = true
			},
		}
	}()

	return C.lua_yield_wrapper(L, 0)
}

//export golapis_tcp_send
func golapis_tcp_send(L *C.lua_State) C.int {
	sock, _ := getTCPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkTCPSocketAffinity(L, sock) {
		return 2
	}

	if sock.closed {
		C.lua_pushnil(L)
		pushGoString(L, "closed")
		return 2
	}

	if !sock.connected {
		C.lua_pushnil(L)
		pushGoString(L, "not connected")
		return 2
	}

	if !checkTCPSocketBusy(L, sock, true, false, true) {
		return 2
	}

	if C.lua_gettop(L) < 2 {
		C.lua_pushnil(L)
		pushGoString(L, "send requires data argument")
		return 2
	}

	sock.writing = true
	defer func() {
		sock.writing = false
	}()

	var data []byte
	ok, errMsg := appendLuaValue(L, 2, &data, false)
	if !ok {
		C.lua_pushnil(L)
		pushGoString(L, errMsg)
		return 2
	}

	// Set write deadline if timeout is set
	if sock.writeTimeout > 0 {
		sock.conn.SetWriteDeadline(time.Now().Add(sock.writeTimeout))
	} else {
		sock.conn.SetWriteDeadline(time.Time{})
	}

	n, err := sock.conn.Write(data)
	if err != nil {
		C.lua_pushnil(L)
		pushGoString(L, normalizeNetError(err))
		return 2
	}

	C.lua_pushinteger(L, C.lua_Integer(n))
	return 1
}

//export golapis_tcp_receive
func golapis_tcp_receive(L *C.lua_State) C.int {
	sock, _ := getTCPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkTCPSocketAffinity(L, sock) {
		return 2
	}

	if sock.closed {
		C.lua_pushnil(L)
		pushGoString(L, "closed")
		return 2
	}

	if !sock.connected {
		C.lua_pushnil(L)
		pushGoString(L, "not connected")
		return 2
	}

	if !checkTCPSocketBusy(L, sock, true, true, false) {
		return 2
	}

	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		C.lua_pushnil(L)
		pushGoString(L, "receive: could not find thread context")
		return 2
	}

	argCount := C.lua_gettop(L)
	mode := "line"
	var size int

	if argCount >= 2 {
		if C.lua_isnumber(L, 2) != 0 {
			size = int(C.lua_tonumber(L, 2))
			if size < 0 {
				C.lua_pushnil(L)
				pushGoString(L, "bad number argument")
				return 2
			}
			if size == 0 {
				pushGoString(L, "")
				return 1
			}
			mode = "size"
		} else if C.lua_isstring(L, 2) != 0 {
			pattern := C.GoString(C.lua_tostring_wrapper(L, 2))
			switch pattern {
			case "*a":
				mode = "all"
			case "*l":
				mode = "line"
			default:
				C.lua_pushnil(L)
				pushGoString(L, "bad pattern")
				return 2
			}
		} else {
			C.lua_pushnil(L)
			pushGoString(L, "bad argument")
			return 2
		}
	}

	if mode == "line" {
		if line, ok := consumeLineFromBuffer(sock); ok {
			pushGoString(L, line)
			return 1
		}
	}

	if mode == "size" {
		// Check if we already have enough buffered data
		bufferedLen := len(sock.readBuf) - sock.readBufPos
		if bufferedLen >= size {
			// Return from buffer without I/O
			data := sock.readBuf[sock.readBufPos : sock.readBufPos+size]
			sock.readBufPos += size
			// Compact buffer if fully consumed
			if sock.readBufPos >= len(sock.readBuf) {
				sock.readBuf = nil
				sock.readBufPos = 0
			}
			C.lua_pushlstring(L, (*C.char)(unsafe.Pointer(&data[0])), C.size_t(len(data)))
			return 1
		}
	}

	// Need to read from network
	// Copy any existing buffered data first
	bufferedLen := len(sock.readBuf) - sock.readBufPos
	var existingData []byte
	if bufferedLen > 0 {
		existingData = make([]byte, bufferedLen)
		copy(existingData, sock.readBuf[sock.readBufPos:])
	}
	sock.readBuf = nil
	sock.readBufPos = 0

	// Capture values for goroutine
	timeout := sock.readTimeout
	conn := sock.conn
	gen := sock.gen

	sock.reading = true
	switch mode {
	case "size":
		go func() {
			result := make([]byte, 0, size)
			if len(existingData) > 0 {
				result = append(result, existingData...)
			}

			remaining := size - len(result)
			buf := socketBufPool.Get().([]byte)
			defer socketBufPool.Put(buf)

			if timeout > 0 {
				conn.SetReadDeadline(time.Now().Add(timeout))
			} else {
				conn.SetReadDeadline(time.Time{})
			}

			for remaining > 0 {
				readSize := remaining
				if readSize > len(buf) {
					readSize = len(buf)
				}

				n, err := conn.Read(buf[:readSize])
				if n > 0 {
					result = append(result, buf[:n]...)
					remaining -= n
				}
				if err != nil {
					errStr := normalizeNetError(err)
					if err == io.EOF {
						errStr = "closed"
					}
					thread.state.eventChan <- &StateEvent{
						Type:       EventResumeThread,
						Thread:     thread,
						ResumeValues: []interface{}{nil, errStr, string(result)},
						OnResume: func(event *StateEvent) {
							sock.reading = false
						},
					}
					return
				}
			}

			dataStr := string(result)
			thread.state.eventChan <- &StateEvent{
				Type:       EventResumeThread,
				Thread:     thread,
				ResumeValues: []interface{}{dataStr},
				OnResume: func(event *StateEvent) {
					sock.reading = false
					if sock.closed || sock.gen != gen {
						event.ResumeValues = []interface{}{nil, "closed"}
					}
				},
			}
		}()
	case "all":
		go func() {
			result := make([]byte, 0)
			if len(existingData) > 0 {
				result = append(result, existingData...)
			}

			buf := socketBufPool.Get().([]byte)
			defer socketBufPool.Put(buf)

			if timeout > 0 {
				conn.SetReadDeadline(time.Now().Add(timeout))
			} else {
				conn.SetReadDeadline(time.Time{})
			}

			for {
				n, err := conn.Read(buf)
				if n > 0 {
					result = append(result, buf[:n]...)
				}
				if err != nil {
					if err == io.EOF {
						thread.state.eventChan <- &StateEvent{
							Type:       EventResumeThread,
							Thread:     thread,
							ResumeValues: []interface{}{string(result)},
							OnResume: func(event *StateEvent) {
								sock.reading = false
								if sock.closed || sock.gen != gen {
									event.ResumeValues = []interface{}{nil, "closed"}
								}
							},
						}
						return
					}

					errStr := normalizeNetError(err)
					thread.state.eventChan <- &StateEvent{
						Type:       EventResumeThread,
						Thread:     thread,
						ResumeValues: []interface{}{nil, errStr, string(result)},
						OnResume: func(event *StateEvent) {
							sock.reading = false
						},
					}
					return
				}
			}
		}()
	case "line":
		go func() {
			lineBuf := make([]byte, 0)
			if len(existingData) > 0 {
				lineBuf = append(lineBuf, existingData...)
			}

			buf := socketBufPool.Get().([]byte)
			defer socketBufPool.Put(buf)

			if timeout > 0 {
				conn.SetReadDeadline(time.Now().Add(timeout))
			} else {
				conn.SetReadDeadline(time.Time{})
			}

			for {
				n, err := conn.Read(buf)
				if n > 0 {
					data := buf[:n]
					if idx := bytes.IndexByte(data, '\n'); idx >= 0 {
						lineBuf = append(lineBuf, data[:idx]...)
						remainder := data[idx+1:]

						line := lineBuf
						if bytes.IndexByte(line, '\r') != -1 {
							filtered := make([]byte, 0, len(line))
							for _, b := range line {
								if b != '\r' {
									filtered = append(filtered, b)
								}
							}
							line = filtered
						}

						thread.state.eventChan <- &StateEvent{
							Type:       EventResumeThread,
							Thread:     thread,
							ResumeValues: []interface{}{string(line)},
							OnResume: func(event *StateEvent) {
								sock.reading = false
								if sock.closed || sock.gen != gen {
									event.ResumeValues = []interface{}{nil, "closed"}
									return
								}
								if len(remainder) > 0 {
									sock.readBuf = append([]byte(nil), remainder...)
									sock.readBufPos = 0
								} else {
									sock.readBuf = nil
									sock.readBufPos = 0
								}
							},
						}
						return
					}
					lineBuf = append(lineBuf, data...)
				}

				if err != nil {
					errStr := normalizeNetError(err)
					if err == io.EOF {
						errStr = "closed"
					}
					partial := lineBuf
					if bytes.IndexByte(partial, '\r') != -1 {
						filtered := make([]byte, 0, len(partial))
						for _, b := range partial {
							if b != '\r' {
								filtered = append(filtered, b)
							}
						}
						partial = filtered
					}
					thread.state.eventChan <- &StateEvent{
						Type:       EventResumeThread,
						Thread:     thread,
						ResumeValues: []interface{}{nil, errStr, string(partial)},
						OnResume: func(event *StateEvent) {
							sock.reading = false
						},
					}
					return
				}
			}
		}()
	}

	return C.lua_yield_wrapper(L, 0)
}

//export golapis_tcp_close
func golapis_tcp_close(L *C.lua_State) C.int {
	sock, _ := getTCPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkTCPSocketAffinity(L, sock) {
		return 2
	}

	if sock.closed {
		C.lua_pushnil(L)
		pushGoString(L, "already closed")
		return 2
	}

	if !checkTCPSocketBusy(L, sock, true, true, true) {
		return 2
	}

	if sock.conn != nil {
		sock.conn.Close()
	}
	sock.conn = nil
	sock.closed = true
	sock.connected = false
	sock.readBuf = nil
	sock.readBufPos = 0
	sock.connecting = false
	sock.reading = false
	sock.writing = false
	sock.gen++

	C.lua_pushinteger(L, 1)
	return 1
}

//export golapis_tcp_setkeepalive
func golapis_tcp_setkeepalive(L *C.lua_State) C.int {
	sock, _ := getTCPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkTCPSocketAffinity(L, sock) {
		return 2
	}

	if sock.closed {
		C.lua_pushnil(L)
		pushGoString(L, "closed")
		return 2
	}

	if !sock.connected {
		C.lua_pushnil(L)
		pushGoString(L, "not connected")
		return 2
	}

	if !checkTCPSocketBusy(L, sock, true, true, true) {
		return 2
	}

	// Stub implementation: just close the connection
	// Real implementation would put the connection into a pool
	if sock.conn != nil {
		sock.conn.Close()
	}
	sock.conn = nil
	sock.connected = false
	sock.readBuf = nil
	sock.readBufPos = 0
	sock.connecting = false
	sock.reading = false
	sock.writing = false
	sock.gen++

	C.lua_pushinteger(L, 1)
	return 1
}

//export golapis_tcp_getreusedtimes
func golapis_tcp_getreusedtimes(L *C.lua_State) C.int {
	sock, _ := getTCPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushinteger(L, 0)
		return 1
	}

	C.lua_pushinteger(L, C.lua_Integer(sock.reusedTimes))
	return 1
}

//export golapis_tcp_gc
func golapis_tcp_gc(L *C.lua_State) C.int {
	sock, id := getTCPSocketFromUserdata(L, 1)
	if sock != nil {
		if !sock.closed && sock.conn != nil {
			sock.conn.Close()
		}
		sock.conn = nil
		sock.closed = true
		sock.connected = false
		sock.readBuf = nil
		sock.readBufPos = 0
		sock.connecting = false
		sock.reading = false
		sock.writing = false
		sock.gen++
		unregisterTCPSocket(id)
	}
	return 0
}

//export golapis_get_phase
func golapis_get_phase(L *C.lua_State) C.int {
	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		// Not in a thread context - return "init"
		// This will cause pgmoon to fall back to luasocket
		pushGoString(L, "init")
		return 1
	}

	// In a valid thread context - return "content"
	// Any phase other than "init" enables cosocket usage
	pushGoString(L, "content")
	return 1
}
