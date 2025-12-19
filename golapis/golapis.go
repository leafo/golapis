package golapis

/*
#cgo CFLAGS: -I../luajit/src
#cgo LDFLAGS: -L../luajit/src -l:libluajit.a -lm -ldl

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

static int run_lua_string(lua_State *L, const char *code) {
    int result = luaL_loadstring(L, code);
    if (result != 0) {
        return result;
    }
    result = lua_pcall(L, 0, LUA_MULTRET, 0);
    fflush(stdout);
    return result;
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
	"os"
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
}

// StateEventType represents the type of event sent to the state's event loop
type StateEventType int

const (
	EventRunFile StateEventType = iota
	EventRunString
	EventResumeThread // async operation completed
	EventStop         // shutdown the event loop
)

// StateEvent represents an operation to be performed on the Lua state
type StateEvent struct {
	Type StateEventType

	// For RunFile/RunString
	Filename string
	Code     string

	// For ResumeThread (async completion)
	Thread     *LuaThread
	ReturnVals []interface{}

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
		luaState:     L,
		outputBuffer: &bytes.Buffer{},
		outputWriter: os.Stdout,
		eventChan:    make(chan *StateEvent, 100), // buffered channel for events
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
	gls.running = true
	go gls.eventLoop()
}

// Stop shuts down the event loop
func (gls *GolapisLuaState) Stop() {
	if !gls.running {
		return
	}
	resp := make(chan *StateResponse, 1)
	gls.eventChan <- &StateEvent{
		Type:     EventStop,
		Response: resp,
	}
	<-resp
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
		case EventStop:
			gls.running = false
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

	// Store the response channel on the thread for later
	thread.responseChan = event.Response

	if err := thread.resume(); err != nil {
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
	if err := gls.runString(event.Code); err != nil {
		return &StateResponse{Error: err}
	}
	return &StateResponse{}
}

// handleResumeThread resumes a thread after an async operation completes
// Returns nil because response is sent via thread.responseChan
func (gls *GolapisLuaState) handleResumeThread(event *StateEvent) *StateResponse {
	thread := event.Thread

	if err := thread.resumeWithValues(event.ReturnVals); err != nil {
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

// runString executes a Lua code string (internal)
func (gls *GolapisLuaState) runString(code string) error {
	ccode := C.CString(code)
	defer C.free(unsafe.Pointer(ccode))

	result := C.run_lua_string(gls.luaState, ccode)
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
