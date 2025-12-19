package golapis

/*
#cgo CFLAGS: -I../luajit/src
#cgo LDFLAGS: -L../luajit/src -l:libluajit.a -lm -ldl -rdynamic

#include "../luajit/src/lua.h"
#include "../luajit/src/lauxlib.h"
#include "../luajit/src/lualib.h"
#include "lua_helpers.h"
#include <stdlib.h>
#include <stdio.h>

static int panic_handler(lua_State *L) {
    const char *msg = lua_tostring(L, -1);
    if (msg == NULL) msg = "error object is not a string";
    printf("PANIC: unprotected error in call to Lua API (%s)\n", msg);
    return 0;
}

static lua_State* new_lua_state() {
    lua_State *L = luaL_newstate();
    if (L) {
        lua_atpanic(L, panic_handler);
        luaL_openlibs(L);
    }
    return L;
}

static int load_lua_file(lua_State *L, const char *filename) {
    int result = luaL_loadfile(L, filename);
    fflush(stdout);
    return result;
}

static void pop_stack(lua_State *L, int n) {
    lua_pop(L, n);
}

*/
import "C"
import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"unsafe"
)

// GolapisLuaState represents a Lua state with golapis functions initialized
type GolapisLuaState struct {
	luaState     *C.lua_State
	golapisRef   C.int // registry reference to golapis table
	outputBuffer *bytes.Buffer
	outputWriter io.Writer
	eventChan    chan *StateEvent // all operations go through this channel
	running      bool             // is event loop running?
	threadWg     sync.WaitGroup   // tracks active threads for Wait()
	stopping     atomic.Bool      // true when event loop is stopping

	// Timer tracking
	pendingTimers map[*PendingTimer]struct{} // set of pending timers
	timerMu       sync.Mutex                 // protects pendingTimers
}

// PendingTimer represents a scheduled timer waiting to fire
// The coroutine is created when the timer is initially scheduled so that the
// arguments can be passed directly without having to serialize them through
// go.
type PendingTimer struct {
	State      *GolapisLuaState // parent state
	CoRef      C.int            // registry ref to coroutine (prevents GC)
	Co         *C.lua_State     // the coroutine pointer
	cancelChan chan struct{}    // closed to signal premature cancellation
	cancelOnce sync.Once        // ensures cancel channel is only closed once
}

// Cancel closes the timer's cancel channel, signaling premature cancellation.
// Safe to call multiple times.
func (t *PendingTimer) Cancel() {
	t.cancelOnce.Do(func() {
		close(t.cancelChan)
	})
}

// StateEventType represents the type of event sent to the state's event loop
type StateEventType int

const (
	EventRunFile StateEventType = iota
	EventRunString
	EventResumeThread // async operation completed
	EventTimerFire    // timer expired, execute callback
	EventStop         // shutdown the event loop
)

// StateEvent represents an operation to be performed on the Lua state
type StateEvent struct {
	Type StateEventType

	// For RunFile/RunString
	Filename     string
	Code         string
	OutputWriter io.Writer     // output destination for this request (e.g., http.ResponseWriter)
	Request      *http.Request // HTTP request for this event (nil in CLI mode)

	// For ResumeThread (async completion)
	Thread     *LuaThread
	ReturnVals []interface{}

	// For EventTimerFire
	Timer     *PendingTimer // timer that fired
	Premature bool          // true if timer was cancelled early (shutdown)

	// Response channel - caller blocks on this
	Response chan *StateResponse
}

// StateResponse represents the result of a state operation
type StateResponse struct {
	Error  error
	Output string
}

// NewGolapisLuaState creates a new Lua state and initializes it with golapis functions
func NewGolapisLuaState() *GolapisLuaState {
	L := C.new_lua_state()
	if L == nil {
		return nil
	}
	gls := &GolapisLuaState{
		luaState:      L,
		outputBuffer:  &bytes.Buffer{},
		outputWriter:  os.Stdout,
		eventChan:     make(chan *StateEvent, 100), // buffered channel for events
		pendingTimers: make(map[*PendingTimer]struct{}),
	}
	gls.registerState()
	gls.SetupGolapis()
	return gls
}

// Close closes the Lua state and frees its resources
func (gls *GolapisLuaState) Close() {
	if gls.luaState != nil {
		// Release the golapis table reference
		if gls.golapisRef != 0 {
			C.luaL_unref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX, gls.golapisRef)
			gls.golapisRef = 0
		}
		gls.unregisterState()
		C.lua_close(gls.luaState)
		gls.luaState = nil
	}
}

// SetOutputWriter sets the output writer for golapis.print function
func (gls *GolapisLuaState) SetOutputWriter(w io.Writer) {
	gls.outputWriter = w
}

// GetOutput returns the current contents of the output buffer
func (gls *GolapisLuaState) GetOutput() string {
	return gls.outputBuffer.String()
}

// ClearOutput clears the output buffer
func (gls *GolapisLuaState) ClearOutput() {
	gls.outputBuffer.Reset()
}

// Start launches the event loop goroutine
func (gls *GolapisLuaState) Start() {
	gls.stopping.Store(false)
	gls.running = true
	go gls.eventLoop()
}

// Stop shuts down the event loop, cleaning up timers without firing callbacks
func (gls *GolapisLuaState) Stop() {
	if !gls.running {
		return
	}
	// Hard stop: clean up timers without firing callbacks
	gls.stopping.Store(true)
	gls.CancelAllTimers()

	resp := make(chan *StateResponse, 1)
	gls.eventChan <- &StateEvent{
		Type:     EventStop,
		Response: resp,
	}
	<-resp
}

// Wait blocks until all active threads have completed execution
func (gls *GolapisLuaState) Wait() {
	gls.threadWg.Wait()
}

// RunFile sends a request to execute a Lua file
func (gls *GolapisLuaState) RunFile(filename string) error {
	resp := make(chan *StateResponse, 1)
	gls.eventChan <- &StateEvent{
		Type:     EventRunFile,
		Filename: filename,
		Response: resp,
	}
	result := <-resp
	return result.Error
}

// RunString sends a request to execute a Lua code string
func (gls *GolapisLuaState) RunString(code string) error {
	resp := make(chan *StateResponse, 1)
	gls.eventChan <- &StateEvent{
		Type:     EventRunString,
		Code:     code,
		Response: resp,
	}
	result := <-resp
	return result.Error
}

// eventLoop is the main event loop that processes all state operations
func (gls *GolapisLuaState) eventLoop() {
	for event := range gls.eventChan {
		var resp *StateResponse

		switch event.Type {
		case EventRunFile:
			resp = gls.handleRunFile(event)
		case EventRunString:
			resp = gls.handleRunString(event)
		case EventResumeThread:
			resp = gls.handleResumeThread(event)
		case EventTimerFire:
			gls.handleTimerFire(event)
		case EventStop:
			gls.running = false
			// End end and cleanup the pending timers
			gls.timerMu.Lock()
			for timer := range gls.pendingTimers {
				C.luaL_unref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX, timer.CoRef)
				gls.threadWg.Done()
			}
			gls.pendingTimers = make(map[*PendingTimer]struct{})
			gls.timerMu.Unlock()

			if event.Response != nil {
				event.Response <- &StateResponse{}
			}
			return
		}

		// If resp is nil, the handler took ownership of the response channel
		// (e.g., thread is still running and will send response when done)
		if resp != nil && event.Response != nil {
			event.Response <- resp
		}
	}
}

// handleRunFile executes a Lua file (internal, called by event loop)
// Returns nil if thread is still running (response will be sent later via thread.responseChan)
func (gls *GolapisLuaState) handleRunFile(event *StateEvent) *StateResponse {
	if err := gls.loadFile(event.Filename); err != nil {
		return &StateResponse{Error: err}
	}

	thread, err := gls.newThread()
	if err != nil {
		return &StateResponse{Error: err}
	}

	// Store the response channel, output writer, and HTTP request on the thread
	thread.responseChan = event.Response
	thread.outputWriter = event.OutputWriter
	thread.httpRequest = event.Request

	if err := thread.resume(nil); err != nil {
		thread.close()
		return &StateResponse{Error: err}
	}

	// If thread is dead, clean up and respond immediately
	if thread.status == ThreadDead {
		thread.close()
		return &StateResponse{}
	}

	// Thread yielded - don't respond yet, response will be sent when thread completes
	// Return nil to signal that event loop should NOT send response
	return nil
}

// handleRunString executes a Lua code string (internal, called by event loop)
func (gls *GolapisLuaState) handleRunString(event *StateEvent) *StateResponse {
	if err := gls.loadString(event.Code); err != nil {
		return &StateResponse{Error: err}
	}

	thread, err := gls.newThread()
	if err != nil {
		return &StateResponse{Error: err}
	}

	// Store the response channel, output writer, and HTTP request on the thread
	thread.responseChan = event.Response
	thread.outputWriter = event.OutputWriter
	thread.httpRequest = event.Request

	if err := thread.resume(nil); err != nil {
		thread.close()
		return &StateResponse{Error: err}
	}

	// If thread is dead, clean up and respond immediately
	if thread.status == ThreadDead {
		thread.close()
		return &StateResponse{}
	}

	// Thread yielded - don't respond yet, response will be sent when thread completes
	// Return nil to signal that event loop should NOT send response
	return nil
}

// handleResumeThread resumes a thread after an async operation completes
// Returns nil because response is sent via thread.responseChan
func (gls *GolapisLuaState) handleResumeThread(event *StateEvent) *StateResponse {
	thread := event.Thread

	if err := thread.resume(event.ReturnVals); err != nil {
		// Send error to the original caller
		if thread.responseChan != nil {
			thread.responseChan <- &StateResponse{Error: err}
		}
		thread.close()
		return nil
	}

	if thread.status == ThreadDead {
		// Thread completed - send success to the original caller
		if thread.responseChan != nil {
			thread.responseChan <- &StateResponse{}
		}
		thread.close()
	}

	// Response sent via thread.responseChan, not through event.Response
	return nil
}

// handleTimerFire executes a timer callback in a new thread context
func (gls *GolapisLuaState) handleTimerFire(event *StateEvent) {
	timer := event.Timer

	// Remove timer from pending set
	gls.timerMu.Lock()
	delete(gls.pendingTimers, timer)
	gls.timerMu.Unlock()

	co := timer.Co

	// Push premature flag as first argument
	if event.Premature {
		C.lua_pushboolean(co, 1)
	} else {
		C.lua_pushboolean(co, 0)
	}

	// Move premature flag to position 2 (after the callback function at position 1)
	// Stack before: [callback, arg1, arg2, ..., premature]
	// Stack after:  [callback, premature, arg1, arg2, ...]
	C.lua_insert_wrapper(co, 2)

	// Count args: total stack minus the callback function
	nargs := C.lua_gettop(co) - 1

	// Create context table for this timer thread
	C.lua_newtable_wrapper(gls.luaState)
	ctxRef := C.luaL_ref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX)

	// Create LuaThread wrapper for the coroutine
	// Note: threadWg was already incremented when timer was scheduled in golapis_timer_at
	thread := &LuaThread{
		state:  gls,
		co:     co,
		status: ThreadCreated,
		ctxRef: ctxRef,
	}

	// Register thread in map so async ops within the callback can find it
	luaThreadMap[co] = thread

	if debugEnabled {
		debugLog("handleTimerFire: co=%p premature=%v nargs=%d", co, event.Premature, nargs)
	}

	// Set context and resume
	thread.setCtx()
	thread.status = ThreadRunning
	result := C.lua_resume(co, nargs)
	thread.clearCtx()

	switch result {
	case 0: // LUA_OK - completed
		thread.status = ThreadDead
		if debugEnabled {
			debugLog("handleTimerFire: co=%p completed", co)
		}
		thread.close()
	case 1: // LUA_YIELD - callback yielded (e.g., called sleep or http.request)
		thread.status = ThreadYielded
		if debugEnabled {
			debugLog("handleTimerFire: co=%p yielded", co)
		}
		// Thread will be resumed later via EventResumeThread
	default: // Error
		thread.status = ThreadDead
		errMsg := C.GoString(C.lua_tostring_wrapper(co, -1))
		if debugEnabled {
			debugLog("handleTimerFire: co=%p error: %s", co, errMsg)
		}
		// Log error but don't fail - timer callbacks are fire-and-forget
		fmt.Printf("timer callback error: %s\n", errMsg)
		thread.close()
	}

	// Release the coroutine registry reference (prevents GC of coroutine)
	C.luaL_unref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX, timer.CoRef)
}

// CancelAllTimers cancels all pending timers, triggering them with premature=true
// unless the state is stopping.
func (gls *GolapisLuaState) CancelAllTimers() {
	gls.timerMu.Lock()
	defer gls.timerMu.Unlock()

	for timer := range gls.pendingTimers {
		timer.Cancel()
	}
}

// loadString loads a Lua code string onto the stack, but doesn't execute it (internal)
func (gls *GolapisLuaState) loadString(code string) error {
	ccode := C.CString(code)
	defer C.free(unsafe.Pointer(ccode))

	result := C.luaL_loadstring(gls.luaState, ccode)
	if result != 0 {
		errMsg := C.GoString(C.lua_tostring_wrapper(gls.luaState, -1))
		C.pop_stack(gls.luaState, 1)
		return fmt.Errorf("lua error: %s", errMsg)
	}
	return nil
}

// loadFile loads a Lua file onto the stack, but doesn't execute it (internal)
func (gls *GolapisLuaState) loadFile(filename string) error {
	cfilename := C.CString(filename)
	defer C.free(unsafe.Pointer(cfilename))

	result := C.load_lua_file(gls.luaState, cfilename)
	if result != 0 {
		errMsg := C.GoString(C.lua_tostring_wrapper(gls.luaState, -1))
		C.pop_stack(gls.luaState, 1)
		return fmt.Errorf("lua error: %s", errMsg)
	}
	return nil
}
