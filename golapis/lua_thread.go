package golapis

/*
#include "lua_helpers.h"
*/
import "C"
import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"unsafe"
)

// LuaPusher is implemented by types that know how to push themselves
// onto a Lua stack as a single value (table, userdata, etc.).
type LuaPusher interface {
	PushToLua(L *C.lua_State)
}

// CaptureResponse represents the result of a location.capture subrequest,
// pushed to Lua as a table with {status, body, header} fields.
type CaptureResponse struct {
	Status  int
	Body    string
	Headers http.Header
}

// PushToLua pushes the capture response as a Lua table onto L's stack.
func (cr CaptureResponse) PushToLua(L *C.lua_State) {
	C.lua_newtable_wrapper(L)

	C.lua_pushinteger(L, C.lua_Integer(cr.Status))
	cstatus := C.CString("status")
	C.lua_setfield(L, -2, cstatus)
	C.free(unsafe.Pointer(cstatus))

	pushGoString(L, cr.Body)
	cbody := C.CString("body")
	C.lua_setfield(L, -2, cbody)
	C.free(unsafe.Pointer(cbody))

	C.lua_newtable_wrapper(L)
	for key, values := range cr.Headers {
		if len(values) > 0 {
			pushGoString(L, key)
			pushGoString(L, values[0])
			C.lua_settable(L, -3)
		}
	}
	cheader := C.CString("header")
	C.lua_setfield(L, -2, cheader)
	C.free(unsafe.Pointer(cheader))
}

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
	ThreadExited // terminated via golapis.exit()
)

type coStatus int

const (
	coRunning coStatus = iota
	coSuspended
	coNormal
	coDead
)

type coOp int

const (
	coOpNop coOp = iota
	coOpUserResume
	coOpUserYield
)

type coCtx struct {
	co     *C.lua_State
	parent *coCtx
	status coStatus
	coRef  C.int
	thread *LuaThread
}

// LuaThread represents the execution of Lua code in a coroutine
type LuaThread struct {
	state        *GolapisLuaState
	co           *C.lua_State
	status       ThreadStatus
	coRef        C.int               // Lua registry reference to coroutine
	ctxRef       C.int               // Lua registry reference to the context table
	responseChan chan *StateResponse // channel to send final response when thread completes
	outputWriter io.Writer           // per-request output destination (e.g., http.ResponseWriter)
	request      *GolapisRequest     // Request context (nil in CLI mode)

	curCo        *coCtx
	entryCo      *coCtx
	coOp         coOp
	coCtxByState map[*C.lua_State]*coCtx

	// Exit state (set by golapis.exit())
	exited   bool // true if golapis.exit() was called
	exitCode int  // HTTP status code from exit()
}

// newThread creates a new LuaThread from the function currently on top of the stack (internal)
func (gls *GolapisLuaState) newThread() (*LuaThread, error) {
	// Create coroutine
	co := C.lua_newthread(gls.luaState)
	if co == nil {
		return nil, fmt.Errorf("failed to create thread")
	}

	// Stack is now: [function, thread]
	// Store coroutine in registry to prevent GC (luaL_ref pops it)
	coRef := C.luaL_ref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX)

	// Copy the function and move it to the coroutine
	C.lua_pushvalue(gls.luaState, -1)
	C.lua_xmove(gls.luaState, co, 1)
	// Remove the original function from the main stack
	C.lua_pop_wrapper(gls.luaState, 1)

	// Create context table and store registry reference
	C.lua_newtable_wrapper(gls.luaState)
	ctxRef := C.luaL_ref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX)

	thread := &LuaThread{
		state:  gls,
		co:     co,
		status: ThreadCreated,
		coRef:  coRef,
		ctxRef: ctxRef,
	}

	thread.coCtxByState = make(map[*C.lua_State]*coCtx)
	entry := &coCtx{
		co:     co,
		status: coSuspended,
		thread: thread,
	}
	thread.entryCo = entry
	thread.curCo = entry
	thread.coCtxByState[co] = entry
	luaThreadMap[co] = thread
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

// checkExitYield checks if the yield was caused by golapis.exit()
// The exit state is stored on the thread by golapis_exit() before yielding.
// Returns true if this was an exit yield, false otherwise.
func (t *LuaThread) checkExitYield() bool {
	// Exit state is set directly on thread by golapis_exit
	return t.exited
}

func (t *LuaThread) registerCoroutine(co *C.lua_State, coRef C.int) *coCtx {
	if t.coCtxByState == nil {
		t.coCtxByState = make(map[*C.lua_State]*coCtx)
	}
	coctx := t.coCtxByState[co]
	if coctx == nil {
		coctx = &coCtx{
			co:     co,
			status: coSuspended,
			thread: t,
		}
	}
	if coRef != 0 {
		coctx.coRef = coRef
	}
	t.coCtxByState[co] = coctx
	luaThreadMap[co] = t
	return coctx
}

func (t *LuaThread) unregisterCoroutine(co *C.lua_State) {
	delete(t.coCtxByState, co)
	delete(luaThreadMap, co)
}

func (t *LuaThread) getCoCtx(co *C.lua_State) *coCtx {
	if t.coCtxByState == nil {
		return nil
	}
	return t.coCtxByState[co]
}

func (t *LuaThread) cleanupCoroutine(coctx *coCtx) {
	if coctx == nil {
		return
	}
	if debugEnabled {
		debugLog("coroutine.cleanup: co=%p", coctx.co)
	}
	if coctx.coRef != 0 {
		C.luaL_unref_wrapper(t.state.luaState, C.LUA_REGISTRYINDEX, coctx.coRef)
		coctx.coRef = 0
	}
	t.unregisterCoroutine(coctx.co)
}

// resume starts or continues execution of the thread (internal)
// Pass nil or empty slice when no values are needed
func (t *LuaThread) resume(values []interface{}) error {
	return t.resumeInternal(values, -1)
}

func (t *LuaThread) resumeWithArgCount(argCount int) error {
	return t.resumeInternal(nil, argCount)
}

func (t *LuaThread) resumeInternal(values []interface{}, initialArgCount int) error {
	if t.status == ThreadDead || t.status == ThreadExited {
		return fmt.Errorf("cannot resume dead or exited thread")
	}
	if t.curCo == nil || t.curCo.co == nil {
		return fmt.Errorf("cannot resume closed thread")
	}

	pendingArgCount := initialArgCount
	for {
		coctx := t.curCo
		if coctx == nil || coctx.co == nil {
			return fmt.Errorf("cannot resume closed thread")
		}

		if debugEnabled {
			debugLog("thread.resume: co=%p resuming with %d values", coctx.co, len(values))
		}

		// Push return values onto coroutine stack
		nret := C.int(0)
		if pendingArgCount >= 0 {
			nret = C.int(pendingArgCount)
			pendingArgCount = -1
		} else {
			for _, v := range values {
				switch val := v.(type) {
				case string:
					if len(val) == 0 {
						C.lua_pushlstring(coctx.co, nil, 0)
					} else {
						C.lua_pushlstring(coctx.co, (*C.char)(unsafe.Pointer(unsafe.StringData(val))), C.size_t(len(val)))
					}
				case int:
					C.lua_pushinteger(coctx.co, C.lua_Integer(val))
				case int64:
					C.lua_pushinteger(coctx.co, C.lua_Integer(val))
				case float64:
					C.lua_pushnumber(coctx.co, C.lua_Number(val))
				case bool:
					if val {
						C.lua_pushboolean(coctx.co, 1)
					} else {
						C.lua_pushboolean(coctx.co, 0)
					}
				case nil:
					C.lua_pushnil(coctx.co)
				case map[string][]string:
					// Push as nested Lua table (for HTTP headers)
					C.lua_newtable_wrapper(coctx.co)
					for key, headerValues := range val {
						if len(key) == 0 {
							C.lua_pushlstring(coctx.co, nil, 0)
						} else {
							C.lua_pushlstring(coctx.co, (*C.char)(unsafe.Pointer(unsafe.StringData(key))), C.size_t(len(key)))
						}

						// Create array of values for this header
						C.lua_newtable_wrapper(coctx.co)
						for i, hv := range headerValues {
							C.lua_pushinteger(coctx.co, C.lua_Integer(i+1))
							if len(hv) == 0 {
								C.lua_pushlstring(coctx.co, nil, 0)
							} else {
								C.lua_pushlstring(coctx.co, (*C.char)(unsafe.Pointer(unsafe.StringData(hv))), C.size_t(len(hv)))
							}
							C.lua_settable(coctx.co, -3)
						}
						C.lua_settable(coctx.co, -3)
					}
				case LuaPusher:
					val.PushToLua(coctx.co)
				default:
					C.lua_pushnil(coctx.co) // unsupported type, push nil
				}
				nret++
			}
			values = nil
		}

		t.setCtx() // Set golapis.ctx before resuming
		t.status = ThreadRunning
		coctx.status = coRunning
		result := C.lua_resume(coctx.co, nret)
		t.clearCtx() // Clear golapis.ctx after yield/finish

		switch result {
		case 0: // LUA_OK
			coctx.status = coDead
			if coctx.parent == nil {
				t.status = ThreadDead
				return nil
			}

			parent := coctx.parent
			nrets := C.lua_gettop(coctx.co)
			if nrets > 0 {
				C.lua_xmove(coctx.co, parent.co, nrets)
			}
			if debugEnabled {
				debugLog("coroutine.done: co=%p nrets=%d", coctx.co, nrets)
			}
			C.lua_pushboolean(parent.co, 1)
			C.lua_insert_wrapper(parent.co, 1)

			parent.status = coRunning
			t.cleanupCoroutine(coctx)
			t.curCo = parent
			pendingArgCount = int(C.lua_gettop(parent.co))
			continue
		case 1: // LUA_YIELD
			if t.checkExitYield() {
				t.status = ThreadExited
				return nil
			}

			switch t.coOp {
			case coOpUserResume:
				t.coOp = coOpNop
				parent := coctx
				child := t.curCo
				if child == nil || child.parent != parent {
					return fmt.Errorf("coroutine.resume: no parent coroutine")
				}
				if debugEnabled {
					debugLog("scheduler: resume co=%p from parent=%p", child.co, parent.co)
				}
				nargs := C.lua_gettop(parent.co)
				if nargs > 0 {
					C.lua_xmove(parent.co, child.co, nargs)
				}
				parent.status = coNormal
				child.status = coRunning
				t.curCo = child
				pendingArgCount = int(nargs)
				continue
			case coOpUserYield:
				t.coOp = coOpNop
				parent := coctx.parent
				if parent == nil {
					return fmt.Errorf("coroutine.yield: no parent coroutine")
				}
				if debugEnabled {
					debugLog("scheduler: yield co=%p to parent=%p nrets=%d", coctx.co, parent.co, C.lua_gettop(coctx.co))
				}
				nrets := C.lua_gettop(coctx.co)
				if nrets > 0 {
					C.lua_xmove(coctx.co, parent.co, nrets)
				}
				C.lua_pushboolean(parent.co, 1)
				C.lua_insert_wrapper(parent.co, 1)

				coctx.status = coSuspended
				parent.status = coRunning
				t.curCo = parent
				pendingArgCount = int(C.lua_gettop(parent.co))
				continue
			default:
				t.coOp = coOpNop
				coctx.status = coSuspended
				t.status = ThreadYielded
				if debugEnabled {
					debugLog("scheduler: async yield co=%p", coctx.co)
				}
				return nil
			}
		default:
			traceback := t.getTraceback(coctx.co)
			coctx.status = coDead
			parent := coctx.parent
			if parent == nil {
				t.status = ThreadDead
				return errors.New(traceback)
			}

			pushGoString(parent.co, traceback)
			C.lua_pushboolean(parent.co, 0)
			C.lua_insert_wrapper(parent.co, 1)

			parent.status = coRunning
			t.cleanupCoroutine(coctx)
			t.curCo = parent
			pendingArgCount = int(C.lua_gettop(parent.co))
			continue
		}
	}
}

// getTraceback calls debug.traceback(co, msg, 0) to get a full stack trace
func (t *LuaThread) getTraceback(co *C.lua_State) string {
	L := t.state.luaState
	C.lua_push_traceback(L, co)
	traceback := C.GoString(C.lua_tostring_wrapper(L, -1))
	C.lua_pop_wrapper(L, 1)
	return traceback
}

// setRequest assigns the request context to the thread and logs debug info
func (t *LuaThread) setRequest(request *GolapisRequest) {
	t.request = request
	if debugEnabled && request != nil && request.Request != nil {
		r := request.Request
		debugLog("thread.setRequest: co=%p %s %s", t.co, r.Method, r.URL.Path)
	}
}

// close cleans up the thread resources (internal)
// Must be called on the lua state goroutine
func (t *LuaThread) close() {
	if t.co != nil {
		if debugEnabled {
			debugLog("thread.close: co=%p", t.co)
		}
		for _, coctx := range t.coCtxByState {
			if coctx.coRef != 0 {
				C.luaL_unref_wrapper(t.state.luaState, C.LUA_REGISTRYINDEX, coctx.coRef)
				coctx.coRef = 0
			}
			t.unregisterCoroutine(coctx.co)
		}
		t.coCtxByState = nil

		if t.coRef != 0 {
			C.luaL_unref_wrapper(t.state.luaState, C.LUA_REGISTRYINDEX, t.coRef)
			t.coRef = 0
		}

		// Release the context table reference
		if t.ctxRef != 0 {
			C.luaL_unref_wrapper(t.state.luaState, C.LUA_REGISTRYINDEX, t.ctxRef)
			t.ctxRef = 0
		}
		t.co = nil
		t.state.threadWg.Done()
	}
}
