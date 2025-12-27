package golapis

/*
#cgo CFLAGS: -I../luajit/src

#include "../luajit/src/lua.h"
#include "../luajit/src/lauxlib.h"
#include "../luajit/src/lualib.h"
#include "../luajit/src/luajit.h"
#include "lua_helpers.h"
#include <stdlib.h>
#include <stdio.h>

static const char* get_luajit_version() {
    return LUAJIT_VERSION;
}

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

static void flush_stdout() {
    fflush(stdout);
}

*/
import "C"
import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

//go:embed moonscript_bootstrap.lua
var moonscriptBootstrap string

// LuaJITVersion returns the version string of the embedded LuaJIT library
func LuaJITVersion() string {
	return C.GoString(C.get_luajit_version())
}

// GolapisLuaState represents a Lua state with golapis functions initialized
type GolapisLuaState struct {
	luaState      *C.lua_State
	golapisRef    C.int // registry reference to golapis table
	entrypointRef C.int // registry reference to loaded entrypoint function (0 = not set)
	outputBuffer  *bytes.Buffer
	outputWriter  io.Writer
	// Note: sends from the event loop goroutine can block if this buffer is full;
	// use a non-blocking send with a goroutine fallback in that case.
	eventChan chan *StateEvent // all operations go through this channel
	running   bool             // is event loop running?
	threadWg  sync.WaitGroup   // tracks active threads for Wait()
	stopping  atomic.Bool      // true when event loop is stopping

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
	EventRunEntryPoint // run the loaded entrypoint
	EventResumeThread  // async operation completed
	EventTimerFire     // timer expired, execute callback
	EventStop          // shutdown the event loop
)

// StateEvent represents an operation to be performed on the Lua state
type StateEvent struct {
	Type StateEventType

	// For RunFile/RunString
	Filename     string
	Code         string
	OutputWriter io.Writer       // output destination for this request (e.g., http.ResponseWriter)
	Request      *GolapisRequest // Request context for this event (nil in CLI mode)

	// For ResumeThread (async completion)
	Thread     *LuaThread
	ResumeValues []interface{}
	OnResume   func(event *StateEvent) // Called on main thread before resuming Lua (for state mutation)

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
	Thread *LuaThread // The completed thread (for accessing request context, headers, etc.)
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
		// Release the entrypoint reference
		if gls.entrypointRef != 0 {
			C.luaL_unref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX, gls.entrypointRef)
			gls.entrypointRef = 0
		}
		gls.unregisterState()
		C.lua_close(gls.luaState)
		C.flush_stdout() // Go exit doesn't flush C stdout buffers
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

// GetLuaMemoryKB returns the current Lua memory usage in kilobytes
func (gls *GolapisLuaState) GetLuaMemoryKB() int {
	return int(C.lua_gc_wrapper(gls.luaState, C.LUA_GCCOUNT, 0))
}

// ForceLuaGC triggers a full garbage collection cycle in the Lua state
func (gls *GolapisLuaState) ForceLuaGC() {
	C.lua_gc_wrapper(gls.luaState, C.LUA_GCCOLLECT, 0)
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

// RunFile sends a request to execute a Lua file.
// The args are passed to the chunk and become available as ... in the script.
func (gls *GolapisLuaState) RunFile(filename string, args []string) error {
	resp := make(chan *StateResponse, 1)

	// Convert args to []interface{} for ResumeValues
	var initArgs []interface{}
	for _, arg := range args {
		initArgs = append(initArgs, arg)
	}

	gls.eventChan <- &StateEvent{
		Type:       EventRunFile,
		Filename:   filename,
		ResumeValues: initArgs,
		Response:   resp,
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

// RunEntryPoint executes the loaded entry point with optional arguments.
// Arguments are passed to the Lua chunk and become available as ... in the script.
// Use LoadEntryPoint to compile the code first.
func (gls *GolapisLuaState) RunEntryPoint(args ...interface{}) error {
	resp := make(chan *StateResponse, 1)
	gls.eventChan <- &StateEvent{
		Type:       EventRunEntryPoint,
		ResumeValues: args,
		Response:   resp,
	}
	result := <-resp
	return result.Error
}

// RequireModule executes require("name") to load a module
func (gls *GolapisLuaState) RequireModule(name string) error {
	return gls.RunString(fmt.Sprintf("require(%q)", name))
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
		case EventRunEntryPoint:
			resp = gls.handleRunEntryPoint(event)
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

	// Store the response channel, output writer, and request context on the thread
	thread.responseChan = event.Response
	thread.outputWriter = event.OutputWriter
	thread.setRequest(event.Request)

	if err := thread.resume(event.ResumeValues); err != nil {
		thread.close()
		return &StateResponse{Error: err}
	}

	// If thread is dead or exited, clean up and respond immediately
	if thread.status == ThreadDead || thread.status == ThreadExited {
		thread.close()
		return &StateResponse{Thread: thread}
	}

	// Thread yielded - don't respond yet, response will be sent when thread completes
	// Return nil to signal that event loop should NOT send response
	return nil
}

// handleRunEntryPoint executes the loaded entrypoint function (internal, called by event loop)
// Returns nil if thread is still running (response will be sent later via thread.responseChan)
func (gls *GolapisLuaState) handleRunEntryPoint(event *StateEvent) *StateResponse {
	if gls.entrypointRef == 0 {
		return &StateResponse{Error: errors.New("no entrypoint loaded")}
	}

	// Retrieve loaded entrypoint from registry
	C.lua_rawgeti_wrapper(gls.luaState, C.LUA_REGISTRYINDEX, gls.entrypointRef)

	thread, err := gls.newThread()
	if err != nil {
		return &StateResponse{Error: err}
	}

	// Store the response channel, output writer, and request context on the thread
	thread.responseChan = event.Response
	thread.outputWriter = event.OutputWriter
	thread.setRequest(event.Request)

	if err := thread.resume(event.ResumeValues); err != nil {
		thread.close()
		return &StateResponse{Error: err}
	}

	// If thread is dead or exited, clean up and respond immediately
	if thread.status == ThreadDead || thread.status == ThreadExited {
		thread.close()
		return &StateResponse{Thread: thread}
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

	// Store the response channel, output writer, and request context on the thread
	thread.responseChan = event.Response
	thread.outputWriter = event.OutputWriter
	thread.setRequest(event.Request)

	if err := thread.resume(nil); err != nil {
		thread.close()
		return &StateResponse{Error: err}
	}

	// If thread is dead or exited, clean up and respond immediately
	if thread.status == ThreadDead || thread.status == ThreadExited {
		thread.close()
		return &StateResponse{Thread: thread}
	}

	// Thread yielded - don't respond yet, response will be sent when thread completes
	// Return nil to signal that event loop should NOT send response
	return nil
}

// handleResumeThread resumes a thread after an async operation completes
// Returns nil because response is sent via thread.responseChan
func (gls *GolapisLuaState) handleResumeThread(event *StateEvent) *StateResponse {
	thread := event.Thread

	// Execute callback on main thread before resuming Lua
	if event.OnResume != nil {
		event.OnResume(event)
	}

	if err := thread.resume(event.ResumeValues); err != nil {
		// Send error to the original caller
		if thread.responseChan != nil {
			thread.responseChan <- &StateResponse{Error: err}
		}
		thread.close()
		return nil
	}

	if thread.status == ThreadDead || thread.status == ThreadExited {
		// Thread completed or exited - send success to the original caller
		if thread.responseChan != nil {
			thread.responseChan <- &StateResponse{Thread: thread}
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
		coRef:  timer.CoRef,
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

	if debugEnabled {
		debugLog("handleTimerFire: co=%p premature=%v nargs=%d", co, event.Premature, nargs)
	}

	if err := thread.resumeWithArgCount(int(nargs)); err != nil {
		if debugEnabled {
			debugLog("handleTimerFire: co=%p error: %s", co, err)
		}
		fmt.Printf("timer callback error: %s\n", err)
		thread.close()
		return
	}

	switch thread.status {
	case ThreadDead, ThreadExited:
		if debugEnabled {
			debugLog("handleTimerFire: co=%p completed", co)
		}
		thread.close()
	case ThreadYielded:
		if debugEnabled {
			debugLog("handleTimerFire: co=%p yielded", co)
		}
		// Thread will be resumed later via EventResumeThread
	}

	// Coroutine registry reference is released in thread.close() after completion.
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
		return errors.New(errMsg)
	}
	return nil
}

// loadFile loads a Lua file onto the stack, but doesn't execute it (internal)
// For .moon files, it uses the MoonScript bootstrap to compile them first.
func (gls *GolapisLuaState) loadFile(filename string) error {
	if strings.HasSuffix(filename, ".moon") {
		return gls.loadMoonFile(filename)
	}

	cfilename := C.CString(filename)
	defer C.free(unsafe.Pointer(cfilename))

	result := C.load_lua_file(gls.luaState, cfilename)
	if result != 0 {
		errMsg := C.GoString(C.lua_tostring_wrapper(gls.luaState, -1))
		C.pop_stack(gls.luaState, 1)
		return errors.New(errMsg)
	}
	return nil
}

// loadMoonFile loads a MoonScript file by running the bootstrap that compiles it.
// The compiled function is left on the stack, same as loadFile.
func (gls *GolapisLuaState) loadMoonFile(filename string) error {
	// Load the bootstrap code
	ccode := C.CString(moonscriptBootstrap)
	defer C.free(unsafe.Pointer(ccode))

	result := C.luaL_loadstring(gls.luaState, ccode)
	if result != 0 {
		errMsg := C.GoString(C.lua_tostring_wrapper(gls.luaState, -1))
		C.pop_stack(gls.luaState, 1)
		return fmt.Errorf("failed to load moonscript bootstrap: %s", errMsg)
	}

	// Push filename as argument to the bootstrap
	cfilename := C.CString(filename)
	defer C.free(unsafe.Pointer(cfilename))
	C.lua_pushstring(gls.luaState, cfilename)

	// Call bootstrap with 1 arg, 1 return value (the compiled function)
	result = C.lua_pcall(gls.luaState, 1, 1, 0)
	if result != 0 {
		errMsg := C.GoString(C.lua_tostring_wrapper(gls.luaState, -1))
		C.pop_stack(gls.luaState, 1)
		return errors.New(errMsg)
	}

	// Stack now has compiled function, ready for newThread()
	return nil
}

// storeEntryPoint stores the function on top of the stack as the entrypoint.
// Assumes a compiled function is already on the stack.
func (gls *GolapisLuaState) storeEntryPoint() {
	if gls.entrypointRef != 0 {
		C.luaL_unref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX, gls.entrypointRef)
		gls.entrypointRef = 0
	}
	gls.entrypointRef = C.luaL_ref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX)
}

// LoadEntryPoint loads an EntryPoint (file or code string) and stores the compiled function in the registry.
func (gls *GolapisLuaState) LoadEntryPoint(entry EntryPoint) error {
	return entry.load(gls)
}

// SetupNgxAlias sets the global "ngx" to the golapis table for nginx-lua compatibility
func (gls *GolapisLuaState) SetupNgxAlias() {
	// Push golapis table from registry
	C.lua_rawgeti_wrapper(gls.luaState, C.LUA_REGISTRYINDEX, gls.golapisRef)
	// Set as global "ngx"
	cname := C.CString("ngx")
	defer C.free(unsafe.Pointer(cname))
	C.lua_setglobal_wrapper(gls.luaState, cname)
}
