package golapis

/*
#cgo CFLAGS: -I../luajit/src
#cgo LDFLAGS: -L../luajit/src -l:libluajit.a -lm -ldl

#include "../luajit/src/lua.h"
#include "../luajit/src/lauxlib.h"
#include "../luajit/src/lualib.h"
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

static const char* get_error_string(lua_State *L) {
    return lua_tostring(L, -1);
}

static void pop_stack(lua_State *L, int n) {
    lua_pop(L, n);
}

// Forward declaration for Go function
extern int golapis_sleep(lua_State *L);
extern int golapis_http_request(lua_State *L);
extern int golapis_print(lua_State *L);

static int c_sleep_wrapper(lua_State *L) {
    return golapis_sleep(L);
}

static int c_http_request_wrapper(lua_State *L) {
    return golapis_http_request(L);
}

static int c_print_wrapper(lua_State *L) {
    return golapis_print(L);
}

static void setup_golapis_global(lua_State *L) {
    lua_newtable(L);                    // Create new table `golapis`

    lua_pushstring(L, "1.0.0");
    lua_setfield(L, -2, "version");

    lua_pushcfunction(L, c_sleep_wrapper);
    lua_setfield(L, -2, "sleep");

    lua_pushcfunction(L, c_print_wrapper);
    lua_setfield(L, -2, "print");

    // Create http table
    lua_newtable(L);
    lua_pushcfunction(L, c_http_request_wrapper);
    lua_setfield(L, -2, "request");
    lua_setfield(L, -2, "http");        // Add http table to `golapis`

    lua_setglobal(L, "golapis");       // Set global golapis = table
}

// Wrapper functions only for macros that can't be called directly from Go
static void lua_newtable_wrapper(lua_State *L) {
    lua_newtable(L);
}

static const char* lua_tostring_wrapper(lua_State *L, int idx) {
    return lua_tostring(L, idx);
}

static void lua_getglobal_wrapper(lua_State *L, const char *name) {
    lua_getglobal(L, name);
}


static void lua_pop_wrapper(lua_State *L, int n) {
    lua_pop(L, n);
}

static void lua_setfield_wrapper(lua_State *L, int idx, const char *k) {
    lua_setfield(L, idx, k);
}

static int luaL_ref_wrapper(lua_State *L, int t) {
    return luaL_ref(L, t);
}

static void luaL_unref_wrapper(lua_State *L, int t, int ref) {
    luaL_unref(L, t, ref);
}

static void lua_rawgeti_wrapper(lua_State *L, int idx, int n) {
    lua_rawgeti(L, idx, n);
}

static int lua_yield_wrapper(lua_State *L, int nresults) {
    return lua_yield(L, nresults);
}

*/
import "C"
import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"
	"unsafe"
)

// GolapisLuaState represents a Lua state with golapis functions initialized
type GolapisLuaState struct {
	luaState     *C.lua_State
	outputBuffer *bytes.Buffer
	outputWriter io.Writer
	eventChan    chan *StateEvent // all operations go through this channel
	running      bool             // is event loop running?
}

// ThreadStatus represents the execution state of a LuaThread
type ThreadStatus int

const (
	ThreadCreated ThreadStatus = iota
	ThreadRunning
	ThreadYielded
	ThreadDead
)

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

// LuaThread represents the execution of Lua code in a coroutine
type LuaThread struct {
	state        *GolapisLuaState
	co           *C.lua_State
	status       ThreadStatus
	ctxRef       C.int               // Lua registry reference to the context table
	responseChan chan *StateResponse // channel to send final response when thread completes
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
		gls.unregisterState()
		C.lua_close(gls.luaState)
		gls.luaState = nil
	}
}

// SetupGolapis initializes the golapis global table with exported functions
func (gls *GolapisLuaState) SetupGolapis() {
	C.setup_golapis_global(gls.luaState)
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
		errMsg := C.GoString(C.get_error_string(gls.luaState))
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
		errMsg := C.GoString(C.get_error_string(gls.luaState))
		C.pop_stack(gls.luaState, 1)
		return fmt.Errorf("lua error: %s", errMsg)
	}
	return nil
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

	// Register thread in map so async ops can find it
	luaThreadMap[co] = thread

	return thread, nil
}

// setCtx assigns the thread's context table to golapis.ctx
func (t *LuaThread) setCtx() {
	L := t.state.luaState
	cGolapis := C.CString("golapis")
	defer C.free(unsafe.Pointer(cGolapis))
	cCtx := C.CString("ctx")
	defer C.free(unsafe.Pointer(cCtx))

	C.lua_getglobal_wrapper(L, cGolapis)
	C.lua_rawgeti_wrapper(L, C.LUA_REGISTRYINDEX, C.int(t.ctxRef))
	C.lua_setfield_wrapper(L, -2, cCtx)
	C.lua_pop_wrapper(L, 1) // pop golapis table
}

// clearCtx sets golapis.ctx to nil
func (t *LuaThread) clearCtx() {
	L := t.state.luaState
	cGolapis := C.CString("golapis")
	defer C.free(unsafe.Pointer(cGolapis))
	cCtx := C.CString("ctx")
	defer C.free(unsafe.Pointer(cCtx))

	C.lua_getglobal_wrapper(L, cGolapis)
	C.lua_pushnil(L)
	C.lua_setfield_wrapper(L, -2, cCtx)
	C.lua_pop_wrapper(L, 1) // pop golapis table
}

// resume starts or continues execution of the thread (internal)
func (t *LuaThread) resume() error {
	if t.status == ThreadDead {
		return fmt.Errorf("cannot resume dead thread")
	}

	t.setCtx() // Set golapis.ctx before resuming
	t.status = ThreadRunning
	result := C.lua_resume(t.co, 0)
	t.clearCtx() // Clear golapis.ctx after yield/finish

	switch result {
	case 0: // LUA_OK
		t.status = ThreadDead
		return nil
	case 1: // LUA_YIELD
		t.status = ThreadYielded
		return nil
	default:
		t.status = ThreadDead
		errMsg := C.GoString(C.get_error_string(t.co))
		return fmt.Errorf("lua error: %s", errMsg)
	}
}

// resumeWithValues pushes return values onto the stack and resumes the thread
func (t *LuaThread) resumeWithValues(values []interface{}) error {
	if t.status == ThreadDead {
		return fmt.Errorf("cannot resume dead thread")
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
		return nil
	case 1: // LUA_YIELD
		t.status = ThreadYielded
		return nil
	default:
		t.status = ThreadDead
		errMsg := C.GoString(C.get_error_string(t.co))
		return fmt.Errorf("lua error: %s", errMsg)
	}
}

// close cleans up the thread resources (internal)
func (t *LuaThread) close() {
	if t.co != nil {
		delete(luaThreadMap, t.co)
		// Release the context table reference
		if t.ctxRef != 0 {
			C.luaL_unref_wrapper(t.state.luaState, C.LUA_REGISTRYINDEX, t.ctxRef)
			t.ctxRef = 0
		}
		t.co = nil
	}
}

//export golapis_http_request
func golapis_http_request(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 1 {
		C.lua_pushnil(L)
		C.lua_pushstring(L, C.CString("http.request expects exactly one argument (url)"))
		return 2
	}

	url_str := C.GoString(C.lua_tostring_wrapper(L, 1))

	resp, err := http.Get(url_str)
	if err != nil {
		C.lua_pushnil(L)
		C.lua_pushstring(L, C.CString(err.Error()))
		return 2
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		C.lua_pushnil(L)
		C.lua_pushstring(L, C.CString(err.Error()))
		return 2
	}

	// body
	C.lua_pushstring(L, C.CString(string(body)))

	// status code
	C.lua_pushinteger(L, C.lua_Integer(resp.StatusCode))

	// headers
	C.lua_newtable_wrapper(L)
	for key, values := range resp.Header {
		C.lua_pushstring(L, C.CString(key))
		C.lua_newtable_wrapper(L)
		for i, value := range values {
			C.lua_pushinteger(L, C.lua_Integer(i+1))
			C.lua_pushstring(L, C.CString(value))
			C.lua_settable(L, -3)
		}
		C.lua_settable(L, -3)
	}

	return 3
}

//export golapis_sleep
func golapis_sleep(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 1 {
		C.lua_pushnil(L)
		C.lua_pushstring(L, C.CString("sleep expects exactly one argument (seconds)"))
		return 2
	}

	if C.lua_isnumber(L, 1) == 0 {
		C.lua_pushnil(L)
		C.lua_pushstring(L, C.CString("sleep argument must be a number"))
		return 2
	}

	seconds := float64(C.lua_tonumber(L, 1))

	// Get the current thread
	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		C.lua_pushnil(L)
		C.lua_pushstring(L, C.CString("sleep: could not find thread context"))
		return 2
	}

	// Start async timer that will send to eventChan when done
	go func() {
		time.Sleep(time.Duration(seconds * float64(time.Second)))
		thread.state.eventChan <- &StateEvent{
			Type:       EventResumeThread,
			Thread:     thread,
			ReturnVals: nil, // sleep returns nothing
			Response:   nil, // no response needed for internal events
		}
	}()

	// Yield with 0 values (nginx-lua pattern)
	return C.lua_yield_wrapper(L, 0)
}

//export golapis_print
func golapis_print(L *C.lua_State) C.int {
	// Get the GolapisLuaState instance from the registry
	gls := getLuaStateFromRegistry(L)
	if gls == nil {
		return 0
	}

	nargs := C.lua_gettop(L)
	for i := C.int(1); i <= nargs; i++ {
		if i > 1 {
			gls.writeOutput("\t")
		}
		if C.lua_isstring(L, i) != 0 {
			str := C.GoString(C.lua_tostring_wrapper(L, i))
			gls.writeOutput(str)
		} else {
			// For non-strings, convert to string using Lua's tostring
			C.lua_getglobal_wrapper(L, C.CString("tostring"))
			C.lua_pushvalue(L, i)
			if C.lua_pcall(L, 1, 1, 0) == 0 {
				str := C.GoString(C.lua_tostring_wrapper(L, -1))
				gls.writeOutput(str)
				C.lua_pop_wrapper(L, 1)
			} else {
				gls.writeOutput("<error converting to string>")
				C.lua_pop_wrapper(L, 1)
			}
		}
	}
	gls.writeOutput("\n")
	return 0
}

// Helper function to write output to buffer or writer
func (gls *GolapisLuaState) writeOutput(text string) {
	if gls.outputWriter != nil {
		gls.outputWriter.Write([]byte(text))
	} else {
		gls.outputBuffer.WriteString(text)
	}
}

// We need a way to associate the GolapisLuaState with the C lua_State
// This is a simplified approach using a global map
var luaStateMap = make(map[*C.lua_State]*GolapisLuaState)

// luaThreadMap maps coroutine lua_State pointers to LuaThread objects
// This allows async operations to find their thread context
var luaThreadMap = make(map[*C.lua_State]*LuaThread)

func (gls *GolapisLuaState) registerState() {
	luaStateMap[gls.luaState] = gls
}

func (gls *GolapisLuaState) unregisterState() {
	delete(luaStateMap, gls.luaState)
}

func getLuaStateFromRegistry(L *C.lua_State) *GolapisLuaState {
	// First check if L is a main state
	if gls, ok := luaStateMap[L]; ok {
		return gls
	}
	// Otherwise, L might be a coroutine - find its thread and get parent state
	if thread, ok := luaThreadMap[L]; ok {
		return thread.state
	}
	return nil
}

func getLuaThreadFromRegistry(L *C.lua_State) *LuaThread {
	return luaThreadMap[L]
}
