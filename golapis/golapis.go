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

static int run_lua_file(lua_State *L, const char *filename) {
    int result = luaL_loadfile(L, filename);
    if (result != 0) {
        return result;
    }
    result = lua_pcall(L, 0, LUA_MULTRET, 0);
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

// LuaState represents a Lua state with golapis functions initialized
type LuaState struct {
	state        *C.lua_State
	outputBuffer *bytes.Buffer
	outputWriter io.Writer
}

// NewLuaState creates a new Lua state and initializes it with golapis functions
func NewLuaState() *LuaState {
	L := C.new_lua_state()
	if L == nil {
		return nil
	}
	ls := &LuaState{
		state:        L,
		outputBuffer: &bytes.Buffer{},
		outputWriter: os.Stdout,
	}
	ls.registerState()
	ls.SetupGolapis()
	return ls
}

// Close closes the Lua state and frees its resources
func (ls *LuaState) Close() {
	if ls.state != nil {
		ls.unregisterState()
		C.lua_close(ls.state)
		ls.state = nil
	}
}

// SetupGolapis initializes the golapis global table with exported functions
func (ls *LuaState) SetupGolapis() {
	C.setup_golapis_global(ls.state)
}

// SetOutputWriter sets the output writer for golapis.print function
func (ls *LuaState) SetOutputWriter(w io.Writer) {
	ls.outputWriter = w
}

// GetOutput returns the current contents of the output buffer
func (ls *LuaState) GetOutput() string {
	return ls.outputBuffer.String()
}

// ClearOutput clears the output buffer
func (ls *LuaState) ClearOutput() {
	ls.outputBuffer.Reset()
}

// RunString executes a Lua code string
func (ls *LuaState) RunString(code string) error {
	ccode := C.CString(code)
	defer C.free(unsafe.Pointer(ccode))

	result := C.run_lua_string(ls.state, ccode)
	if result != 0 {
		errMsg := C.GoString(C.get_error_string(ls.state))
		C.pop_stack(ls.state, 1)
		return fmt.Errorf("lua error: %s", errMsg)
	}
	return nil
}

// RunFile executes a Lua file
func (ls *LuaState) RunFile(filename string) error {
	cfilename := C.CString(filename)
	defer C.free(unsafe.Pointer(cfilename))

	result := C.run_lua_file(ls.state, cfilename)
	if result != 0 {
		errMsg := C.GoString(C.get_error_string(ls.state))
		C.pop_stack(ls.state, 1)
		return fmt.Errorf("lua error: %s", errMsg)
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
	// Get the LuaState instance from the registry
	ls := getLuaStateFromRegistry(L)
	if ls == nil {
		return 0
	}

	nargs := C.lua_gettop(L)
	for i := C.int(1); i <= nargs; i++ {
		if i > 1 {
			ls.writeOutput("\t")
		}
		if C.lua_isstring(L, i) != 0 {
			str := C.GoString(C.lua_tostring_wrapper(L, i))
			ls.writeOutput(str)
		} else {
			// For non-strings, convert to string using Lua's tostring
			C.lua_getglobal_wrapper(L, C.CString("tostring"))
			C.lua_pushvalue_wrapper(L, i)
			if C.lua_pcall(L, 1, 1, 0) == 0 {
				str := C.GoString(C.lua_tostring_wrapper(L, -1))
				ls.writeOutput(str)
				C.lua_pop_wrapper(L, 1)
			} else {
				ls.writeOutput("<error converting to string>")
				C.lua_pop_wrapper(L, 1)
			}
		}
	}
	ls.writeOutput("\n")
	return 0
}

// Helper function to write output to buffer or writer
func (ls *LuaState) writeOutput(text string) {
	if ls.outputWriter != nil {
		ls.outputWriter.Write([]byte(text))
	} else {
		ls.outputBuffer.WriteString(text)
	}
}

// We need a way to associate the LuaState with the C lua_State
// This is a simplified approach using a global map
var luaStateMap = make(map[*C.lua_State]*LuaState)

func (ls *LuaState) registerState() {
	luaStateMap[ls.state] = ls
}

func (ls *LuaState) unregisterState() {
	delete(luaStateMap, ls.state)
}

func getLuaStateFromRegistry(L *C.lua_State) *LuaState {
	return luaStateMap[L]
}
