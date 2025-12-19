package golapis

/*
#include "lua_helpers.h"
*/
import "C"
import (
	"fmt"
	"io"
	"net/http"
	"unsafe"
)

// Static C strings - allocated once at package init, never freed
var cStrCtx *C.char

func init() {
	cStrCtx = C.CString("ctx")
}

// ThreadStatus represents the execution state of a LuaThread
type ThreadStatus int

const (
	ThreadCreated ThreadStatus = iota
	ThreadRunning
	ThreadYielded
	ThreadDead
)

// LuaThread represents the execution of Lua code in a coroutine
type LuaThread struct {
	state        *GolapisLuaState
	co           *C.lua_State
	status       ThreadStatus
	ctxRef       C.int               // Lua registry reference to the context table
	responseChan chan *StateResponse // channel to send final response when thread completes
	outputWriter io.Writer           // per-request output destination (e.g., http.ResponseWriter)
	httpRequest  *http.Request       // HTTP request for this thread (nil in CLI mode)
}

// newThread creates a new LuaThread from the function currently on top of the stack (internal)
func (gls *GolapisLuaState) newThread() (*LuaThread, error) {
	// Create coroutine
	co := C.lua_newthread(gls.luaState)
	if co == nil {
		return nil, fmt.Errorf("failed to create thread")
	}

	// Stack is now: [function, thread]
	// Copy the function and move it to the coroutine
	C.lua_pushvalue(gls.luaState, -2)
	C.lua_xmove(gls.luaState, co, 1)

	// Remove the thread from main stack (function remains)
	C.lua_pop_wrapper(gls.luaState, 1)

	// Create context table and store registry reference
	C.lua_newtable_wrapper(gls.luaState)
	ctxRef := C.luaL_ref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX)

	thread := &LuaThread{
		state:  gls,
		co:     co,
		status: ThreadCreated,
		ctxRef: ctxRef,
	}

	thread.registerThread()
	gls.threadWg.Add(1)
	if debugEnabled {
		debugLog("newThread: created thread co=%p", co)
	}

	return thread, nil
}

// setCtx assigns the thread's context table to golapis.ctx
// this should be called before execution of the thread is resumed
func (t *LuaThread) setCtx() {
	L := t.state.luaState
	C.lua_rawgeti_wrapper(L, C.LUA_REGISTRYINDEX, t.state.golapisRef)
	C.lua_rawgeti_wrapper(L, C.LUA_REGISTRYINDEX, t.ctxRef)
	C.lua_setfield_wrapper(L, -2, cStrCtx)
	C.lua_pop_wrapper(L, 1) // pop golapis table
}

// clearCtx sets golapis.ctx to nil
// this should be called after execution of thread yields or ends
func (t *LuaThread) clearCtx() {
	L := t.state.luaState
	C.lua_rawgeti_wrapper(L, C.LUA_REGISTRYINDEX, t.state.golapisRef)
	C.lua_pushnil(L)
	C.lua_setfield_wrapper(L, -2, cStrCtx)
	C.lua_pop_wrapper(L, 1) // pop golapis table
}

// resume starts or continues execution of the thread (internal)
// Pass nil or empty slice when no values are needed
func (t *LuaThread) resume(values []interface{}) error {
	if t.status == ThreadDead {
		return fmt.Errorf("cannot resume dead thread")
	}

	if debugEnabled {
		debugLog("thread.resume: co=%p resuming with %d values", t.co, len(values))
	}

	// Push return values onto coroutine stack
	nret := C.int(0)
	for _, v := range values {
		switch val := v.(type) {
		case string:
			cstr := C.CString(val)
			C.lua_pushstring(t.co, cstr)
			C.free(unsafe.Pointer(cstr))
		case int:
			C.lua_pushinteger(t.co, C.lua_Integer(val))
		case int64:
			C.lua_pushinteger(t.co, C.lua_Integer(val))
		case float64:
			C.lua_pushnumber(t.co, C.lua_Number(val))
		case bool:
			if val {
				C.lua_pushboolean(t.co, 1)
			} else {
				C.lua_pushboolean(t.co, 0)
			}
		case nil:
			C.lua_pushnil(t.co)
		case map[string][]string:
			// Push as nested Lua table (for HTTP headers)
			C.lua_newtable_wrapper(t.co)
			for key, headerValues := range val {
				ckey := C.CString(key)
				C.lua_pushstring(t.co, ckey)
				C.free(unsafe.Pointer(ckey))

				// Create array of values for this header
				C.lua_newtable_wrapper(t.co)
				for i, hv := range headerValues {
					C.lua_pushinteger(t.co, C.lua_Integer(i+1))
					cv := C.CString(hv)
					C.lua_pushstring(t.co, cv)
					C.free(unsafe.Pointer(cv))
					C.lua_settable(t.co, -3)
				}
				C.lua_settable(t.co, -3)
			}
		default:
			C.lua_pushnil(t.co) // unsupported type, push nil
		}
		nret++
	}

	t.setCtx() // Set golapis.ctx before resuming
	t.status = ThreadRunning
	result := C.lua_resume(t.co, nret)
	t.clearCtx() // Clear golapis.ctx after yield/finish

	switch result {
	case 0: // LUA_OK
		t.status = ThreadDead
		if debugEnabled {
			debugLog("thread.resume: co=%p completed", t.co)
		}
		return nil
	case 1: // LUA_YIELD
		t.status = ThreadYielded
		if debugEnabled {
			debugLog("thread.resume: co=%p yielded", t.co)
		}
		return nil
	default:
		t.status = ThreadDead
		errMsg := C.GoString(C.lua_tostring_wrapper(t.co, -1))
		if debugEnabled {
			debugLog("thread.resume: co=%p error: %s", t.co, errMsg)
		}
		return fmt.Errorf("lua error: %s", errMsg)
	}
}

// close cleans up the thread resources (internal)
// Must be called on the lua state goroutine
func (t *LuaThread) close() {
	if t.co != nil {
		if debugEnabled {
			debugLog("thread.close: co=%p", t.co)
		}
		t.unregisterThread()

		// Release the context table reference
		if t.ctxRef != 0 {
			C.luaL_unref_wrapper(t.state.luaState, C.LUA_REGISTRYINDEX, t.ctxRef)
			t.ctxRef = 0
		}
		t.co = nil
		t.state.threadWg.Done()
	}
}
