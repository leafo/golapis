package golapis

/*
#include "lua_helpers.h"
*/
import "C"
import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// =============================================================================
// TCP receiveuntil — ngx.socket.tcp:receiveuntil(pattern, opts?) compatible
// =============================================================================

// tcpUntilReader holds the state of one receiveuntil iterator. The KMP
// matcher state persists across iterator calls so that a partial pattern
// match can span sized reads (and survive timeouts), matching ngx_lua's
// compiled-pattern semantics. Only the owning coroutine touches a reader:
// either on the event-loop goroutine (fast path / OnResume) or in the single
// in-flight read goroutine while the coroutine is suspended.
type tcpUntilReader struct {
	sockID    uint64
	pattern   []byte
	failure   []int // KMP failure table
	state     int   // number of pattern bytes currently held as a partial match
	inclusive bool
	// sectionDone is set when the pattern was matched during a sized read
	// (ngx_lua's cp->state == -1): the next iterator call returns the
	// nil,nil,nil end-of-section marker and resets it.
	sectionDone bool
}

var (
	untilReaderMap        = make(map[uint64]*tcpUntilReader)
	untilReaderMu         sync.Mutex
	untilReaderIDSeq      uint64
	cStrUntilReaderMetaTb = C.CString("golapis.socket.tcp.reader") // allocated once, never freed
)

func registerUntilReader(r *tcpUntilReader) uint64 {
	untilReaderMu.Lock()
	defer untilReaderMu.Unlock()
	untilReaderIDSeq++
	untilReaderMap[untilReaderIDSeq] = r
	return untilReaderIDSeq
}

func getUntilReaderByID(id uint64) *tcpUntilReader {
	untilReaderMu.Lock()
	defer untilReaderMu.Unlock()
	return untilReaderMap[id]
}

func unregisterUntilReader(id uint64) {
	untilReaderMu.Lock()
	defer untilReaderMu.Unlock()
	delete(untilReaderMap, id)
}

// buildKMPFailure computes the classic KMP failure table: failure[i] is the
// length of the longest proper prefix of pattern[0:i+1] that is also a
// suffix. Equivalent to ngx_lua's compiled "recovering" DFA edges.
func buildKMPFailure(pattern []byte) []int {
	failure := make([]int, len(pattern))
	k := 0
	for i := 1; i < len(pattern); i++ {
		for k > 0 && pattern[i] != pattern[k] {
			k = failure[k-1]
		}
		if pattern[i] == pattern[k] {
			k++
		}
		failure[i] = k
	}
	return failure
}

// feed runs input bytes through the matcher, appending emitted section data
// to out. It stops at a full pattern match or once len(out) reaches budget
// (budget 0 = unlimited). Returns the new out slice, how many input bytes
// were consumed, and whether the pattern completed.
//
// Emission mirrors ngx_lua's read_until filter: bytes held as a partial
// pattern match are withheld from out until disambiguated; a failed partial
// match flushes its no-longer-matching prefix bytes atomically, so a sized
// call may return slightly more than budget bytes (ngx_lua has the same
// overshoot). On a full match the held pattern bytes are dropped (or
// appended, when inclusive) and the matcher resets for the next section.
func (r *tcpUntilReader) feed(input []byte, out []byte, budget int) ([]byte, int, bool) {
	pat := r.pattern
	i := 0
	for i < len(input) {
		c := input[i]

		if c == pat[r.state] {
			i++
			r.state++
			if r.state == len(pat) {
				if r.inclusive {
					out = append(out, pat...)
				}
				r.state = 0
				return out, i, true
			}
			continue
		}

		if r.state == 0 {
			out = append(out, c)
			i++
			if budget > 0 && len(out) >= budget {
				return out, i, false
			}
			continue
		}

		// Mismatch with a partial match held: KMP fallback. The prefix bytes
		// that fell out of the match window are emitted; the current byte is
		// either absorbed into the shorter match or re-examined at state 0.
		k := r.failure[r.state-1]
		for k > 0 && c != pat[k] {
			k = r.failure[k-1]
		}
		if c == pat[k] {
			out = append(out, pat[:r.state-k]...)
			r.state = k + 1
			i++
		} else {
			out = append(out, pat[:r.state]...)
			r.state = 0
		}
		if budget > 0 && len(out) >= budget {
			return out, i, false
		}
	}
	return out, i, false
}

// restorePendingPattern reinstates withheld partial-match bytes from a
// previously active receiveuntil reader into the front of the socket's read
// buffer and resets that reader's matcher state. Mirrors ngx_lua's
// ngx_http_lua_socket_tcp_read_prepare: it runs when a read operation with a
// different input filter starts (a plain receive/receiveany, or another
// reader — pass nil or the calling reader as current); the same reader
// resuming keeps its state untouched. Must run on the event-loop goroutine.
func (sock *TCPSocket) restorePendingPattern(current *tcpUntilReader) {
	prev := sock.activeUntilReader
	if prev == nil || prev == current {
		return
	}
	sock.activeUntilReader = nil
	if prev.state <= 0 {
		return
	}
	restored := make([]byte, 0, prev.state+len(sock.readBuf)-sock.readBufPos)
	restored = append(restored, prev.pattern[:prev.state]...)
	restored = append(restored, sock.readBuf[sock.readBufPos:]...)
	sock.readBuf = restored
	sock.readBufPos = 0
	prev.state = 0
}

// setActiveUntilReader records (or clears) the reader holding withheld
// partial-match bytes after one of its read operations completes.
func (sock *TCPSocket) setActiveUntilReader(r *tcpUntilReader) {
	if r.state > 0 {
		sock.activeUntilReader = r
	} else if sock.activeUntilReader == r {
		sock.activeUntilReader = nil
	}
}

// =============================================================================
// Exported Functions (called from C wrappers)
// =============================================================================

//export golapis_tcp_receiveuntil
func golapis_tcp_receiveuntil(L *C.lua_State) C.int {
	n := int(C.lua_gettop(L))
	if n != 2 && n != 3 {
		pushGoString(L, fmt.Sprintf("expecting 2 or 3 arguments (including the object), but got %d", n))
		return -1
	}

	// Per OpenResty (luaL_checktype on self), a bad self raises a Lua
	// argument error rather than returning nil, err.
	sock, sockID := getTCPSocketFromUserdata(L, 1)
	if sock == nil {
		typeName := C.GoString(C.lua_typename(L, C.lua_type(L, 1)))
		pushGoString(L, fmt.Sprintf("bad argument #1 to 'receiveuntil' (tcp socket expected, got %s)", typeName))
		return -1
	}
	if !checkTCPSocketAffinity(L, sock, sockID) {
		return 2
	}

	inclusive := false
	if n == 3 {
		if C.lua_istable_wrapper(L, 3) == 0 {
			pushGoString(L, "expecting table as the 3rd argument")
			return -1
		}
		C.lua_getfield(L, 3, cStrInclusive)
		switch C.lua_type(L, -1) {
		case C.LUA_TNIL:
			// not set
		case C.LUA_TBOOLEAN:
			inclusive = C.lua_toboolean_wrapper(L, -1) != 0
		default:
			typeName := C.GoString(C.lua_typename(L, C.lua_type(L, -1)))
			C.lua_pop_wrapper(L, 1)
			pushGoString(L, fmt.Sprintf("bad \"inclusive\" option value type: %s", typeName))
			return -1
		}
		C.lua_pop_wrapper(L, 1)
	}

	if C.lua_isstring(L, 2) == 0 {
		pushGoString(L, "expecting string pattern as the 2nd argument")
		return -1
	}
	var patLen C.size_t
	patPtr := C.lua_tolstring_wrapper(L, 2, &patLen)
	if patLen == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "pattern is empty")
		return 2
	}
	pattern := C.GoBytes(unsafe.Pointer(patPtr), C.int(patLen))

	reader := &tcpUntilReader{
		sockID:    sockID,
		pattern:   pattern,
		failure:   buildKMPFailure(pattern),
		inclusive: inclusive,
	}
	readerID := registerUntilReader(reader)

	if debugEnabled {
		debugLog("tcp.receiveuntil: id=%d reader=%d pattern_len=%d inclusive=%v", sockID, readerID, len(pattern), inclusive)
	}

	// Build the iterator closure: upvalue 1 is the socket userdata, upvalue 2
	// is the reader-state userdata (its __gc unregisters the reader).
	C.lua_pushvalue(L, 1)
	ptr := C.lua_newuserdata(L, C.size_t(unsafe.Sizeof(uint64(0))))
	*(*uint64)(ptr) = readerID
	C.luaL_getmetatable_wrapper(L, cStrUntilReaderMetaTb)
	C.lua_setmetatable(L, -2)
	C.golapis_push_receiveuntil_iterator(L, 2)
	return 1
}

var cStrInclusive = C.CString("inclusive") // allocated once, never freed

//export golapis_tcp_receiveuntil_iterator
func golapis_tcp_receiveuntil_iterator(L *C.lua_State) C.int {
	// NOTE: error returns use -2 (not -1) because this function yields and
	// lua_yield's pass-through return value is -1; see the C wrapper.
	n := int(C.lua_gettop(L))
	if n > 1 {
		pushGoString(L, fmt.Sprintf("expecting 0 or 1 argument, but seen %d", n))
		return -2
	}

	size := 0
	if n == 1 {
		if C.lua_isnumber(L, 1) == 0 {
			pushGoString(L, "bad argument #1 (number expected)")
			return -2
		}
		size = int(C.lua_tonumber(L, 1))
		if size < 0 {
			size = 0
		}
	}

	sock, sockID := getTCPSocketFromUserdata(L, C.lua_upvalueindex_wrapper(1))
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "closed")
		return 2
	}
	if !checkTCPSocketAffinity(L, sock, sockID) {
		return 2
	}

	readerPtr := C.lua_touserdata_wrapper(L, C.lua_upvalueindex_wrapper(2))
	var reader *tcpUntilReader
	if readerPtr != nil {
		reader = getUntilReaderByID(*(*uint64)(readerPtr))
	}
	if reader == nil {
		C.lua_pushnil(L)
		pushGoString(L, "closed")
		return 2
	}

	// Per ngx_lua, a closed/unconnected socket reports "closed" (there is no
	// separate "not connected" state for the iterator).
	if sock.closed || !sock.connected || sock.conn == nil {
		C.lua_pushnil(L)
		pushGoString(L, "closed")
		return 2
	}

	if !checkTCPSocketBusy(L, sock, true, true, false) {
		return 2
	}

	// End-of-section marker: the pattern was consumed by a previous sized
	// call; signal it with three nils and start the next section.
	if reader.sectionDone {
		reader.sectionDone = false
		C.lua_pushnil(L)
		C.lua_pushnil(L)
		C.lua_pushnil(L)
		return 3
	}

	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		C.lua_pushnil(L)
		pushGoString(L, "receiveuntil: could not find thread context")
		return 2
	}

	if debugEnabled {
		debugLog("tcp.receiveuntil.iter: id=%d size=%d state=%d", sockID, size, reader.state)
	}

	// A different reader may be withholding partial-match bytes; restore
	// them to the buffer front before reading (no-op for this reader).
	sock.restorePendingPattern(reader)

	// Fast path: satisfy the call from already-buffered data without yielding.
	var out []byte
	if buffered := len(sock.readBuf) - sock.readBufPos; buffered > 0 {
		newOut, consumed, matched := reader.feed(sock.readBuf[sock.readBufPos:], out, size)
		out = newOut
		sock.readBufPos += consumed
		if sock.readBufPos >= len(sock.readBuf) {
			sock.readBuf = nil
			sock.readBufPos = 0
		}
		if matched {
			if size > 0 {
				reader.sectionDone = true
			}
			sock.setActiveUntilReader(reader)
			pushGoString(L, string(out))
			return 1
		}
		if size > 0 && len(out) >= size {
			sock.setActiveUntilReader(reader)
			pushGoString(L, string(out))
			return 1
		}
	}

	// Need to read from the network: same yield/resume pattern as receive.
	timeout := sock.readTimeout
	conn := sock.conn
	gen := sock.gen
	sized := size > 0

	sock.reading = true
	go func() {
		buf := socketBufPool.Get().([]byte)
		defer socketBufPool.Put(buf)

		if timeout > 0 {
			conn.SetReadDeadline(time.Now().Add(timeout))
		} else {
			conn.SetReadDeadline(time.Time{})
		}

		result := out
		for {
			nread, err := conn.Read(buf)
			if nread > 0 {
				var consumed int
				var matched bool
				result, consumed, matched = reader.feed(buf[:nread], result, size)
				if matched || (sized && len(result) >= size) {
					// Unconsumed bytes go back to the socket's read buffer.
					var leftover []byte
					if consumed < nread {
						leftover = append([]byte(nil), buf[consumed:nread]...)
					}
					if debugEnabled {
						debugLog("tcp.receiveuntil.iter: id=%d returned=%d matched=%v leftover=%d", sockID, len(result), matched, len(leftover))
					}
					thread.state.eventChan <- &StateEvent{
						Type:         EventResumeThread,
						Thread:       thread,
						ResumeValues: []interface{}{string(result)},
						OnResume: func(event *StateEvent) {
							sock.reading = false
							if sock.closed || sock.gen != gen {
								event.ResumeValues = []interface{}{nil, "closed"}
								return
							}
							if len(leftover) > 0 {
								sock.readBuf = leftover
								sock.readBufPos = 0
							}
							if matched && sized {
								reader.sectionDone = true
							}
							sock.setActiveUntilReader(reader)
						},
					}
					return
				}
				// Not matched and budget not reached: feed consumed everything.
			}
			if err != nil {
				errStr := normalizeNetError(err)
				if debugEnabled {
					debugLog("tcp.receiveuntil.iter: id=%d error=%s partial=%d", sockID, errStr, len(result))
				}
				// Matcher state persists across timeouts so a retry resumes
				// mid-pattern; withheld partial-match bytes are not flushed
				// into the partial data (same as ngx_lua).
				thread.state.eventChan <- &StateEvent{
					Type:         EventResumeThread,
					Thread:       thread,
					ResumeValues: []interface{}{nil, errStr, string(result)},
					OnResume: func(event *StateEvent) {
						sock.reading = false
						if sock.gen == gen {
							sock.setActiveUntilReader(reader)
						}
						if errStr != "timeout" && sock.gen == gen {
							sock.closeOnError()
						}
					},
				}
				return
			}
		}
	}()

	return C.lua_yield_wrapper(L, 0)
}

//export golapis_tcp_until_reader_gc
func golapis_tcp_until_reader_gc(L *C.lua_State) C.int {
	ptr := C.lua_touserdata_wrapper(L, 1)
	if ptr != nil {
		unregisterUntilReader(*(*uint64)(ptr))
	}
	return 0
}
