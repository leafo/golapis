package golapis

/*
#include "lua_helpers.h"
*/
import "C"
import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// =============================================================================
// Shared Socket Infrastructure (reusable for TCP)
// =============================================================================

// socketBufPool provides reusable buffers for socket receive operations.
// Used by both UDP and TCP sockets. Max size matches OpenResty's 65536 limit.
var socketBufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 65536)
	},
}

// normalizeNetError converts network errors to OpenResty-compatible error strings.
func normalizeNetError(err error) string {
	if err == nil {
		return ""
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return "timeout"
	}
	return err.Error()
}

// =============================================================================
// UDP Socket Implementation
// =============================================================================

// UDPSocket represents a UDP cosocket compatible with ngx.socket.udp
type UDPSocket struct {
	conn        net.Conn      // *net.UDPConn or *net.UnixConn
	timeout     time.Duration // per-socket timeout (0 = no timeout)
	localAddr   string        // bind address (for outgoing connections)
	connected   bool          // true after successful setpeername
	closed      bool          // true after close() called
	isUnix      bool          // true for unix:/ domain sockets
	gen         uint64        // increments to invalidate in-flight async operations
	ownerThread *C.lua_State  // coroutine that created this socket (for request affinity)
}

// UDP socket registry - maps socket ID to Go object
var (
	udpSocketMap       = make(map[uint64]*UDPSocket)
	udpSocketMu        sync.Mutex
	udpSocketIDSeq     uint64
	unixAutobindSeq    uint64                            // counter for unique abstract socket paths
	cStrUDPMetatable   = C.CString("golapis.socket.udp") // allocated once, never freed
	unixAutobindPrefix = fmt.Sprintf("@golapis-%d-", os.Getpid())
)

func registerUDPSocket(sock *UDPSocket) uint64 {
	udpSocketMu.Lock()
	defer udpSocketMu.Unlock()
	udpSocketIDSeq++
	udpSocketMap[udpSocketIDSeq] = sock
	return udpSocketIDSeq
}

func getUDPSocketByID(id uint64) *UDPSocket {
	udpSocketMu.Lock()
	defer udpSocketMu.Unlock()
	return udpSocketMap[id]
}

func unregisterUDPSocket(id uint64) {
	udpSocketMu.Lock()
	defer udpSocketMu.Unlock()
	delete(udpSocketMap, id)
}

// getUDPSocketFromUserdata extracts the UDPSocket from Lua userdata at stack index
func getUDPSocketFromUserdata(L *C.lua_State, idx C.int) (*UDPSocket, uint64) {
	ptr := C.lua_touserdata_wrapper(L, idx)
	if ptr == nil {
		return nil, 0
	}
	id := *(*uint64)(ptr)
	return getUDPSocketByID(id), id
}

// checkSocketAffinity verifies the socket belongs to the current thread.
// Returns true if affinity check passes.
// Returns false and pushes (nil, "bad request") to Lua stack if it fails.
func checkSocketAffinity(L *C.lua_State, sock *UDPSocket) bool {
	if sock.ownerThread != L {
		C.lua_pushnil(L)
		pushGoString(L, "bad request")
		return false
	}
	return true
}

func luaAbsIndex(L *C.lua_State, idx C.int) C.int {
	if idx > 0 || idx <= C.LUA_REGISTRYINDEX {
		return idx
	}
	return C.lua_gettop(L) + idx + 1
}

func appendLuaNumber(buf *[]byte, num float64) {
	if num == float64(int64(num)) {
		*buf = strconv.AppendInt(*buf, int64(num), 10)
	} else {
		*buf = strconv.AppendFloat(*buf, num, 'g', -1, 64)
	}
}

func appendLuaValue(L *C.lua_State, idx C.int, buf *[]byte, strict bool) (bool, string) {
	idx = luaAbsIndex(L, idx)

	switch C.lua_type(L, idx) {
	case C.LUA_TSTRING:
		var length C.size_t
		cstr := C.lua_tolstring_wrapper(L, idx, &length)
		*buf = append(*buf, C.GoBytes(unsafe.Pointer(cstr), C.int(length))...)
		return true, ""

	case C.LUA_TNUMBER:
		appendLuaNumber(buf, float64(C.lua_tonumber(L, idx)))
		return true, ""

	case C.LUA_TBOOLEAN:
		if strict {
			return false, "bad data type boolean in the array"
		}
		if C.lua_toboolean(L, idx) != 0 {
			*buf = append(*buf, []byte("true")...)
		} else {
			*buf = append(*buf, []byte("false")...)
		}
		return true, ""

	case C.LUA_TNIL:
		if strict {
			return false, "bad data type nil in the array"
		}
		*buf = append(*buf, []byte("nil")...)
		return true, ""

	case C.LUA_TTABLE:
		tableLen := int(C.lua_objlen(L, idx))
		for i := 1; i <= tableLen; i++ {
			C.lua_rawgeti_wrapper(L, idx, C.int(i))
			ok, errMsg := appendLuaValue(L, -1, buf, true)
			C.lua_pop_wrapper(L, 1)
			if !ok {
				return false, errMsg
			}
		}
		return true, ""
	}

	typeName := C.GoString(C.lua_typename(L, C.lua_type(L, idx)))
	return false, fmt.Sprintf("string, number, boolean, nil, or array table expected, got %s", typeName)
}

// =============================================================================
// Exported Functions (called from C wrappers)
// =============================================================================

//export golapis_socket_udp_new
func golapis_socket_udp_new(L *C.lua_State) C.int {
	sock := &UDPSocket{
		timeout:     0,
		ownerThread: L,
	}
	id := registerUDPSocket(sock)

	// Create userdata containing the socket ID
	ptr := C.lua_newuserdata(L, C.size_t(unsafe.Sizeof(uint64(0))))
	*(*uint64)(ptr) = id

	// Apply the UDP socket metatable
	C.luaL_getmetatable_wrapper(L, cStrUDPMetatable)
	C.lua_setmetatable(L, -2)

	return 1
}

//export golapis_udp_settimeout
func golapis_udp_settimeout(L *C.lua_State) C.int {
	sock, _ := getUDPSocketFromUserdata(L, 1)
	if sock == nil {
		return 0
	}
	if !checkSocketAffinity(L, sock) {
		return 2
	}

	if C.lua_gettop(L) < 2 || C.lua_isnumber(L, 2) == 0 {
		return 0
	}

	ms := float64(C.lua_tonumber(L, 2))
	sock.timeout = time.Duration(ms) * time.Millisecond
	return 0 // settimeout returns nothing per OpenResty spec
}

//export golapis_udp_bind
func golapis_udp_bind(L *C.lua_State) C.int {
	sock, _ := getUDPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkSocketAffinity(L, sock) {
		return 2
	}

	if C.lua_gettop(L) < 2 || C.lua_isstring(L, 2) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "bind requires an address argument")
		return 2
	}

	addr := C.GoString(C.lua_tostring_wrapper(L, 2))
	ip := net.ParseIP(addr)
	if ip == nil {
		C.lua_pushnil(L)
		pushGoString(L, "bad address")
		return 2
	}

	sock.localAddr = ip.String()
	C.lua_pushinteger(L, 1)
	return 1
}

//export golapis_udp_setpeername
func golapis_udp_setpeername(L *C.lua_State) C.int {
	sock, _ := getUDPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkSocketAffinity(L, sock) {
		return 2
	}

	if sock.closed {
		C.lua_pushnil(L)
		pushGoString(L, "closed")
		return 2
	}

	if sock.connected && sock.conn != nil {
		sock.conn.Close()
		sock.conn = nil
		sock.connected = false
		sock.isUnix = false
		sock.gen++
	}

	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		C.lua_pushnil(L)
		pushGoString(L, "setpeername: could not find thread context")
		return 2
	}

	if C.lua_gettop(L) < 2 || C.lua_isstring(L, 2) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "setpeername requires host argument")
		return 2
	}

	arg1 := C.GoString(C.lua_tostring_wrapper(L, 2))

	// Unix domain socket: "unix:/path"
	if strings.HasPrefix(arg1, "unix:") {
		if runtime.GOOS != "linux" {
			C.lua_pushnil(L)
			pushGoString(L, "unix domain sockets require linux")
			return 2
		}

		path := arg1[5:]
		remoteAddr, err := net.ResolveUnixAddr("unixgram", path)
		if err != nil {
			C.lua_pushnil(L)
			pushGoString(L, err.Error())
			return 2
		}
		// Autobind to abstract socket for bidirectional communication (like OpenResty)
		seq := atomic.AddUint64(&unixAutobindSeq, 1)
		localPath := unixAutobindPrefix + strconv.FormatUint(seq, 10)
		localAddr, err := net.ResolveUnixAddr("unixgram", localPath)
		if err != nil {
			C.lua_pushnil(L)
			pushGoString(L, err.Error())
			return 2
		}
		conn, err := net.DialUnix("unixgram", localAddr, remoteAddr)
		if err != nil {
			C.lua_pushnil(L)
			pushGoString(L, err.Error())
			return 2
		}
		sock.conn = conn
		sock.connected = true
		sock.isUnix = true
		C.lua_pushinteger(L, 1)
		return 1
	}

	// Network socket - requires port
	if C.lua_gettop(L) < 3 || C.lua_isnumber(L, 3) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "setpeername requires port argument")
		return 2
	}

	port := int(C.lua_tonumber(L, 3))
	host := arg1

	// Check if host is an IP address (sync) or domain name (async DNS)
	if ip := net.ParseIP(host); ip != nil {
		// Direct IP address - synchronous connect
		var localAddr *net.UDPAddr
		if sock.localAddr != "" {
			localAddr = &net.UDPAddr{IP: net.ParseIP(sock.localAddr)}
		}

		conn, err := net.DialUDP("udp", localAddr, &net.UDPAddr{IP: ip, Port: port})
		if err != nil {
			C.lua_pushnil(L)
			pushGoString(L, err.Error())
			return 2
		}
		sock.conn = conn
		sock.connected = true
		C.lua_pushinteger(L, 1)
		return 1
	}

	// Domain name - async dial (resolver handles multi-IP fallback)
	// Capture values for goroutine (read-only in goroutine)
	localAddrStr := sock.localAddr
	gen := sock.gen

	go func() {
		var localAddr net.Addr
		if localAddrStr != "" {
			localAddr = &net.UDPAddr{IP: net.ParseIP(localAddrStr)}
		}

		dialer := &net.Dialer{LocalAddr: localAddr}
		conn, err := dialer.Dial("udp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			thread.state.eventChan <- &StateEvent{
				Type:       EventResumeThread,
				Thread:     thread,
				ReturnVals: []interface{}{nil, err.Error()},
			}
			return
		}

		// State mutation happens on main thread via OnResume callback
		thread.state.eventChan <- &StateEvent{
			Type:       EventResumeThread,
			Thread:     thread,
			ReturnVals: []interface{}{1},
			OnResume: func(event *StateEvent) {
				if sock.closed || sock.gen != gen {
					if conn != nil {
						conn.Close()
					}
					event.ReturnVals = []interface{}{nil, "closed"}
					return
				}
				sock.conn = conn
				sock.connected = true
			},
		}
	}()

	return C.lua_yield_wrapper(L, 0)
}

//export golapis_udp_send
func golapis_udp_send(L *C.lua_State) C.int {
	sock, _ := getUDPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkSocketAffinity(L, sock) {
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

	if C.lua_gettop(L) < 2 {
		C.lua_pushnil(L)
		pushGoString(L, "send requires data argument")
		return 2
	}

	var data []byte
	ok, errMsg := appendLuaValue(L, 2, &data, false)
	if !ok {
		C.lua_pushnil(L)
		pushGoString(L, errMsg)
		return 2
	}

	_, err := sock.conn.Write(data)
	if err != nil {
		C.lua_pushnil(L)
		pushGoString(L, err.Error())
		return 2
	}

	C.lua_pushinteger(L, 1)
	return 1
}

//export golapis_udp_receive
func golapis_udp_receive(L *C.lua_State) C.int {
	sock, _ := getUDPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkSocketAffinity(L, sock) {
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

	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		C.lua_pushnil(L)
		pushGoString(L, "receive: could not find thread context")
		return 2
	}

	// Optional size argument (default 65536, max 65536)
	size := 65536
	if C.lua_gettop(L) >= 2 && C.lua_isnumber(L, 2) != 0 {
		size = int(C.lua_tonumber(L, 2))
		if size <= 0 {
			size = 65536
		} else if size > 65536 {
			size = 65536
		}
	}

	// Capture values for goroutine
	timeout := sock.timeout
	conn := sock.conn

	go func() {
		buf := socketBufPool.Get().([]byte)
		defer socketBufPool.Put(buf)

		if timeout > 0 {
			conn.SetReadDeadline(time.Now().Add(timeout))
		} else {
			// Clear any previous deadline
			conn.SetReadDeadline(time.Time{})
		}

		n, err := conn.Read(buf[:size])

		if err != nil {
			thread.state.eventChan <- &StateEvent{
				Type:       EventResumeThread,
				Thread:     thread,
				ReturnVals: []interface{}{nil, normalizeNetError(err)},
			}
			return
		}

		// Return data as string (binary-safe)
		thread.state.eventChan <- &StateEvent{
			Type:       EventResumeThread,
			Thread:     thread,
			ReturnVals: []interface{}{string(buf[:n])},
		}
	}()

	return C.lua_yield_wrapper(L, 0)
}

//export golapis_udp_close
func golapis_udp_close(L *C.lua_State) C.int {
	sock, _ := getUDPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkSocketAffinity(L, sock) {
		return 2
	}

	if sock.closed {
		C.lua_pushnil(L)
		pushGoString(L, "already closed")
		return 2
	}

	if sock.conn != nil {
		sock.conn.Close()
	}
	sock.conn = nil
	sock.closed = true
	sock.connected = false
	sock.gen++

	C.lua_pushinteger(L, 1)
	return 1
}

//export golapis_udp_gc
func golapis_udp_gc(L *C.lua_State) C.int {
	sock, id := getUDPSocketFromUserdata(L, 1)
	if sock != nil {
		if !sock.closed && sock.conn != nil {
			sock.conn.Close()
		}
		sock.conn = nil
		sock.closed = true
		sock.connected = false
		sock.gen++
		unregisterUDPSocket(id)
	}
	return 0
}
