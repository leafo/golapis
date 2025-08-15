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

static lua_State* create_coroutine(lua_State *L) {
    return lua_newthread(L);
}

static int call_coroutine_with_function(lua_State *L) {
    // Create a new coroutine
    lua_State *co = lua_newthread(L);

    // Push a copy of the function to the coroutine
    lua_pushvalue(L, -2);  // Copy the function (which should be at -2 now due to thread at -1)
    lua_xmove(L, co, 1);   // Move copy to coroutine

    int result = lua_resume(co, 0);

    // Clean up: remove the thread from main stack
    lua_pop(L, 1);

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

static void lua_pushvalue_wrapper(lua_State *L, int idx) {
    lua_pushvalue(L, idx);
}

static void lua_pop_wrapper(lua_State *L, int n) {
    lua_pop(L, n);
}

static void lua_xmove_wrapper(lua_State *from, lua_State *to, int n) {
    lua_xmove(from, to, n);
}

static int lua_resume_wrapper(lua_State *L, int narg) {
    return lua_resume(L, narg);
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

// RunString executes a Lua code string
func (gls *GolapisLuaState) RunString(code string) error {
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

// LoadFile loads a Lua file onto the stack, but doesn't execute it
func (gls *GolapisLuaState) LoadFile(filename string) error {
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

// CallLoadedAsCoroutine executes the previously loaded Lua code as a coroutine
func (gls *GolapisLuaState) CallLoadedAsCoroutine() error {
	// Create coroutine and register it to use the same GolapisLuaState for golapis functions
	co := C.create_coroutine(gls.luaState)
	if co == nil {
		return fmt.Errorf("failed to create coroutine")
	}

	// Register the coroutine to use the same GolapisLuaState for golapis functions
	luaStateMap[co] = gls
	defer delete(luaStateMap, co)

	// The loaded function should be at top of stack (-1)
	// After lua_newthread, stack is: [function, thread]
	// So function is now at -2, thread at -1
	C.lua_pushvalue_wrapper(gls.luaState, -2) // Copy the function
	C.lua_xmove_wrapper(gls.luaState, co, 1)  // Move copy to coroutine

	// Remove the thread object from main stack
	C.lua_pop_wrapper(gls.luaState, 1)

	// Resume the coroutine
	result := C.lua_resume_wrapper(co, 0)

	if result != 0 && result != 1 { // LUA_YIELD = 1, which is OK for coroutines
		errMsg := C.GoString(C.get_error_string(co))
		return fmt.Errorf("lua coroutine error: %s", errMsg)
	}
	return nil
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

	seconds := C.lua_tonumber(L, 1)
	duration := time.Duration(float64(seconds) * float64(time.Second))
	time.Sleep(duration)

	return 0
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
			C.lua_pushvalue_wrapper(L, i)
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

func (gls *GolapisLuaState) registerState() {
	luaStateMap[gls.luaState] = gls
}

func (gls *GolapisLuaState) unregisterState() {
	delete(luaStateMap, gls.luaState)
}

func getLuaStateFromRegistry(L *C.lua_State) *GolapisLuaState {
	return luaStateMap[L]
}
