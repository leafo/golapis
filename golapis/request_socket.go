package golapis

/*
#include <lua.h>
#include <lauxlib.h>
#include "lua_helpers.h"
*/
import "C"
import (
	"net"
	"sync"
)

// =============================================================================
// Raw request socket (ngx.req.socket(true))
// =============================================================================

// Hijacked connections are tracked per state so they can be closed on
// shutdown: http.Server.Shutdown ignores hijacked connections, and a Lua
// thread blocked in receive() would otherwise keep the process alive.
type hijackedConns struct {
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool // set by closeAll; later adds are refused
}

// add tracks conn. Returns false if closeAll already ran, in which case the
// caller must not use the connection.
func (h *hijackedConns) add(conn net.Conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	if h.conns == nil {
		h.conns = make(map[net.Conn]struct{})
	}
	h.conns[conn] = struct{}{}
	return true
}

func (h *hijackedConns) remove(conn net.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, conn)
}

// reset re-enables tracking after closeAll, for a Stop()/Start() cycle
func (h *hijackedConns) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = false
}

// closeAll closes every tracked connection. Pending socket operations fail
// with "closed", letting their Lua threads finish.
func (h *hijackedConns) closeAll() {
	h.mu.Lock()
	conns := h.conns
	h.conns = nil
	h.closed = true
	h.mu.Unlock()

	// Close outside the lock: hijackedConn.Close calls back into remove
	for conn := range conns {
		conn.Close()
	}
}

// CloseHijackedConns closes every connection taken over by req.socket(true).
// http.Server.Shutdown ignores hijacked connections, so embedders that run
// their own server must call this before Wait() or a Lua thread blocked on
// a raw socket read keeps the state alive. Stop() calls it as well.
func (gls *GolapisLuaState) CloseHijackedConns() {
	gls.hijacked.closeAll()
}

// hijackedConn wraps a hijacked net.Conn so that closing it (from the socket
// or from the handler after the thread finishes) also untracks it.
type hijackedConn struct {
	net.Conn
	state     *GolapisLuaState
	closeOnce sync.Once
	closeErr  error
}

func (c *hijackedConn) Close() error {
	c.closeOnce.Do(func() {
		c.state.hijacked.remove(c)
		c.closeErr = c.Conn.Close()
	})
	return c.closeErr
}

//export golapis_req_socket
func golapis_req_socket(L *C.lua_State) C.int {
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		C.lua_pushnil(L)
		pushGoString(L, "no request found")
		return 2
	}

	raw := C.lua_gettop(L) >= 1 && C.lua_toboolean(L, 1) != 0
	if !raw {
		C.lua_pushnil(L)
		pushGoString(L, "only raw mode (req.socket(true)) is supported")
		return 2
	}

	w, ok := thread.outputWriter.(*headerFlushingWriter)
	if !ok {
		// Output is captured into a buffer (location.capture subrequest)
		C.lua_pushnil(L)
		pushGoString(L, ErrHijackNotSupported.Error())
		return 2
	}

	conn, buffered, err := w.Hijack()
	if err != nil {
		C.lua_pushnil(L)
		pushGoString(L, err.Error())
		return 2
	}

	hc := &hijackedConn{Conn: conn, state: thread.state}
	thread.request.hijackedConn = hc
	if !thread.state.hijacked.add(hc) {
		// Shutdown already closed the tracked connections; don't hand out
		// one that nothing will close
		hc.Close()
		C.lua_pushnil(L)
		pushGoString(L, "closed")
		return 2
	}

	sock := &TCPSocket{
		conn:            hc,
		connectTimeout:  defaultSocketTimeout,
		readTimeout:     defaultSocketTimeout,
		writeTimeout:    defaultSocketTimeout,
		connected:       true,
		isRequestSocket: true,
		ownerThread:     thread,
		readBuf:         buffered,
	}
	id := registerTCPSocket(sock)
	pushTCPSocketUserdata(L, id)

	if debugEnabled {
		debugLog("req.socket: id=%d co=%p buffered=%d", id, L, len(buffered))
	}
	return 1
}
