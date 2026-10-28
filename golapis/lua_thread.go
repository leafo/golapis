package golapis

/*
#include "lua_helpers.h"
*/
import "C"
import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
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
// Uses batched CGO to minimize C boundary crossings.
func (cr CaptureResponse) PushToLua(L *C.lua_State) {
	b := AcquireBatch()
	defer ReleaseBatch(b)

	// Count headers with values for size hint
	nHeaders := 0
	for _, values := range cr.Headers {
		if len(values) > 0 {
			nHeaders++
		}
	}

	b.Table()
	b.Int(cr.Status).SetFieldInline("status")
	b.String(cr.Body).SetFieldInline("body")

	b.TableSized(0, nHeaders)
	for key, values := range cr.Headers {
		if len(values) > 0 {
			b.StringEntry(key, values[0])
		}
	}
	b.SetFieldInline("header")

	b.Push(L)
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
	coZombie // light thread finished; results kept until the parent waits on it
)

// pendingIOCanceler is implemented by sockets with cancellable in-flight ops
type pendingIOCanceler interface {
	cancelPendingIO()
}

type coOp int

const (
	coOpNop coOp = iota
	coOpUserResume
	coOpUserYield
	coOpThreadSpawn // thread.spawn: run the new light thread, post the spawner
	coOpSchedYield  // coroutine.yield from a light/entry thread: let posted threads run
)

type coCtx struct {
	co     *C.lua_State
	parent *coCtx
	status coStatus
	coRef  C.int
	thread *LuaThread

	// true while parked on an async op (yielded to the event loop); a
	// resume event is only honored for a coroutine in this state
	asyncPending bool

	// The socket whose I/O this coroutine is parked on, if any. thread.kill
	// cancels it (by closing the socket, as nginx finalizes the cosocket of
	// a killed uthread) so the in-flight read can't consume data meant for
	// a later reader.
	pendingIO pendingIOCanceler

	// Light thread (thread.spawn) bookkeeping, following ngx_lua's uthreads:
	// a finished light thread whose parent isn't waiting becomes a zombie
	// holding its results on its own stack until wait() collects it.
	isLight        bool
	zombieChildren []*coCtx // finished children not yet collected by wait()
	waitingOn      []*coCtx // children this coroutine is parked in wait() for
}

// LuaThread represents the execution of Lua code in a coroutine
type LuaThread struct {
	state        *GolapisLuaState
	pinner       runtime.Pinner // keeps t addressable from exdata2, see registry.go
	prevThread   *LuaThread     // links in the owning state's live list
	nextThread   *LuaThread
	co           *C.lua_State
	status       ThreadStatus
	coRef        C.int               // Lua registry reference to coroutine
	ctxRef       C.int               // Lua registry reference to the context table
	responseChan chan *StateResponse // channel to send final response when thread completes
	outputWriter io.Writer           // per-request output destination (e.g., http.ResponseWriter)
	request      *GolapisRequest     // Request context (nil in CLI mode)
	timerRun     bool                // true for timer callback threads counted as running timers
	phase        string              // explicit get_phase() value, e.g. "init_worker"; "" = derive

	curCo        *coCtx
	entryCo      *coCtx
	coOp         coOp
	coCtxByState map[*C.lua_State]*coCtx

	// Light thread scheduling: coroutines ready to run once the current one
	// parks or ends, and the number of live light threads. The thread only
	// completes when the entry coroutine is done and uthreads is zero.
	posted     []*coCtx
	uthreads   int
	entryDone  bool
	spawnChild *coCtx // set by thread.spawn for the scheduler to start

	// Exit state (set by golapis.exit())
	exited   bool // true if golapis.exit() was called
	exitCode int  // HTTP status code from exit()
}

// newThread creates a new LuaThread from the function currently on top of the stack (internal)
func (gls *GolapisLuaState) newThread() (*LuaThread, error) {
	// Recycled or fresh coroutine; either way the main stack is still [function]
	co, coRef := gls.acquireCoroutine()
	if co == nil {
		return nil, fmt.Errorf("failed to create thread")
	}

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
	gls.trackThread(thread)
	thread.bindCoroutine(co)
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
	t.bindCoroutine(co)
	return coctx
}

func (t *LuaThread) unregisterCoroutine(co *C.lua_State) {
	delete(t.coCtxByState, co)
	unbindCoroutine(co)
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
	t.unregisterCoroutine(coctx.co)
	if coctx.coRef != 0 {
		C.luaL_unref_wrapper(t.state.luaState, C.LUA_REGISTRYINDEX, coctx.coRef)
		coctx.coRef = 0
	}
}

// cleanupZombieChildren releases the finished light threads nobody waited
// on, when their parent coroutine dies
func (t *LuaThread) cleanupZombieChildren(coctx *coCtx) {
	for _, z := range coctx.zombieChildren {
		t.cleanupCoroutine(z)
		t.uthreads--
	}
	coctx.zombieChildren = nil
}

// postCoroutine queues a coroutine to run once the current one parks or ends
func (t *LuaThread) postCoroutine(coctx *coCtx) {
	t.posted = append(t.posted, coctx)
}

// pickPosted returns the next runnable posted coroutine, or nil
func (t *LuaThread) pickPosted() *coCtx {
	for len(t.posted) > 0 {
		next := t.posted[0]
		t.posted = t.posted[1:]
		if next.status == coDead || next.status == coZombie {
			continue
		}
		return next
	}
	return nil
}

// markDead records that a coroutine ended and releases the finished light
// threads it never waited on
func (t *LuaThread) markDead(coctx *coCtx) {
	coctx.status = coDead
	t.cleanupZombieChildren(coctx)
}

// anyAsyncPending reports whether some coroutine is parked on an async op,
// i.e. whether an event can still wake this thread
func (t *LuaThread) anyAsyncPending() bool {
	for _, c := range t.coCtxByState {
		if c.asyncPending {
			return true
		}
	}
	return false
}

// switchTo makes co (or, if nil, the next posted coroutine) current and
// returns the number of values on its stack to resume it with. With nothing
// runnable it parks the thread, or completes it when the entry coroutine
// and every light thread are done. A thread that nothing can wake anymore
// (no posted coroutine, none parked on an async op) is completed as well
// rather than left hanging, e.g. a zombie whose parent is a suspended
// coroutine that is never resumed.
func (t *LuaThread) switchTo(co *coCtx) (int, bool) {
	if co == nil {
		co = t.pickPosted()
	}
	if co != nil {
		co.status = coRunning
		t.curCo = co
		return int(C.lua_gettop(co.co)), true
	}
	if (t.entryDone && t.uthreads == 0) || !t.anyAsyncPending() {
		if debugEnabled && !(t.entryDone && t.uthreads == 0) {
			debugLog("scheduler: nothing can resume thread %p (uthreads=%d); completing", t, t.uthreads)
		}
		t.status = ThreadDead
		return 0, false
	}
	t.status = ThreadYielded
	return 0, false
}

// lightThreadDone handles a light thread that finished (ok) or errored. Its
// results (or the error message) are on its own stack and markDead has
// already run. Returns the coroutine to continue with, or nil if the caller
// should pick from the posted queue.
func (t *LuaThread) lightThreadDone(coctx *coCtx, ok bool) *coCtx {
	parent := coctx.parent
	if parent == nil || parent.status == coDead || parent.status == coZombie {
		// Nobody can wait on it anymore
		t.cleanupCoroutine(coctx)
		t.uthreads--
		return nil
	}

	for _, w := range parent.waitingOn {
		if w != coctx {
			continue
		}
		// Parent is parked in wait() on this thread: hand it the results now
		nrets := C.lua_gettop(coctx.co)
		if nrets > 0 {
			C.lua_xmove(coctx.co, parent.co, nrets)
		}
		pushBool(parent.co, ok)
		C.lua_insert_wrapper(parent.co, 1)
		parent.waitingOn = nil
		parent.asyncPending = false
		t.cleanupCoroutine(coctx)
		t.uthreads--
		if debugEnabled {
			debugLog("thread.done: co=%p delivered %d results to waiting parent %p", coctx.co, nrets, parent.co)
		}
		return parent
	}

	// Parent alive but not waiting: keep results until it does
	pushBool(coctx.co, ok)
	C.lua_insert_wrapper(coctx.co, 1)
	coctx.status = coZombie
	parent.zombieChildren = append(parent.zombieChildren, coctx)
	if debugEnabled {
		debugLog("thread.done: co=%p is now a zombie of %p", coctx.co, parent.co)
	}
	return nil
}

func pushBool(L *C.lua_State, v bool) {
	if v {
		C.lua_pushboolean(L, 1)
	} else {
		C.lua_pushboolean(L, 0)
	}
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
			t.markDead(coctx)

			if coctx == t.entryCo {
				t.entryDone = true
				if n, ok := t.switchTo(nil); ok {
					pendingArgCount = n
					continue
				}
				return nil
			}

			if coctx.isLight {
				if debugEnabled {
					debugLog("thread.done: light thread co=%p ended normally", coctx.co)
				}
				if n, ok := t.switchTo(t.lightThreadDone(coctx, true)); ok {
					pendingArgCount = n
					continue
				}
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
			case coOpThreadSpawn:
				// The spawner yielded the new thread object (its own return
				// value) and is posted; the child runs first with [fn, args].
				// spawn's lua_yield can fail (yield across a C-call boundary,
				// caught by pcall); the op then surfaces on the spawner's next,
				// unrelated yield, recognizable by the missing thread object.
				t.coOp = coOpNop
				parent := coctx
				child := t.spawnChild
				t.spawnChild = nil
				if child == nil {
					return fmt.Errorf("thread.spawn: no child coroutine")
				}
				if C.lua_gettop(parent.co) != 1 || C.lua_tothread_wrapper(parent.co, 1) != child.co {
					if debugEnabled {
						debugLog("scheduler: spawn of co=%p never yielded; discarding", child.co)
					}
					t.cleanupCoroutine(child)
					coctx.status = coSuspended
					coctx.asyncPending = true
					if n, ok := t.switchTo(nil); ok {
						pendingArgCount = n
						continue
					}
					return nil
				}
				if debugEnabled {
					debugLog("scheduler: spawn co=%p from parent=%p", child.co, parent.co)
				}
				t.uthreads++
				parent.status = coNormal
				t.postCoroutine(parent)
				child.status = coRunning
				t.curCo = child
				pendingArgCount = int(C.lua_gettop(child.co)) - 1
				continue
			case coOpSchedYield:
				// Let other posted threads run, then continue this one
				t.coOp = coOpNop
				C.lua_settop(coctx.co, 0)
				coctx.status = coNormal
				t.postCoroutine(coctx)
				if n, ok := t.switchTo(nil); ok {
					pendingArgCount = n
					continue
				}
				return nil
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
				coctx.asyncPending = true
				if debugEnabled {
					debugLog("scheduler: async yield co=%p", coctx.co)
				}
				if n, ok := t.switchTo(nil); ok {
					pendingArgCount = n
					continue
				}
				return nil
			}
		default:
			traceback := t.getTraceback(coctx.co)
			t.markDead(coctx)

			if coctx == t.entryCo {
				// An entry error aborts the whole request, light threads included
				t.status = ThreadDead
				return errors.New(traceback)
			}

			if coctx.isLight {
				log.Printf("lua user thread aborted: %s", traceback)
				C.lua_settop(coctx.co, 0)
				pushGoString(coctx.co, traceback)
				if n, ok := t.switchTo(t.lightThreadDone(coctx, false)); ok {
					pendingArgCount = n
					continue
				}
				return nil
			}

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
			// A coroutine still parked (request aborted) must not linger
			// in a shared semaphore's wait queue or hold a socket read
			t.cancelPendingIO(coctx)
			t.unregisterCoroutine(coctx.co)
			if coctx.coRef != 0 {
				C.luaL_unref_wrapper(t.state.luaState, C.LUA_REGISTRYINDEX, coctx.coRef)
				coctx.coRef = 0
			}
		}
		t.coCtxByState = nil

		if t.coRef != 0 {
			t.state.releaseCoroutine(t.co, t.coRef)
			t.coRef = 0
		}

		// Release the context table reference
		if t.ctxRef != 0 {
			C.luaL_unref_wrapper(t.state.luaState, C.LUA_REGISTRYINDEX, t.ctxRef)
			t.ctxRef = 0
		}
		if t.timerRun {
			t.state.timerMu.Lock()
			if t.state.runningTimers > 0 {
				t.state.runningTimers--
			}
			t.state.timerMu.Unlock()
			t.timerRun = false
		}
		t.co = nil
		t.state.untrackThread(t)
		t.state.threadWg.Done()
	}
}
