package golapis

/*
#include "lua_helpers.h"

// Forward declaration for Go functions
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

static int setup_golapis_global(lua_State *L) {
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

    lua_pushvalue(L, -1);               // Duplicate table for registry ref
    int ref = luaL_ref(L, LUA_REGISTRYINDEX);

    lua_setglobal(L, "golapis");        // Set global golapis = table
    return ref;
}

*/
import "C"
import (
	"io/ioutil"
	"net/http"
	"time"
	"unsafe"
)

//export golapis_http_request
func golapis_http_request(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 1 {
		C.lua_pushnil(L)
		pushCString(L, "http.request expects exactly one argument (url)")
		return 2
	}

	url_str := C.GoString(C.lua_tostring_wrapper(L, 1))

	resp, err := http.Get(url_str)
	if err != nil {
		C.lua_pushnil(L)
		pushCString(L, err.Error())
		return 2
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		C.lua_pushnil(L)
		pushCString(L, err.Error())
		return 2
	}

	// body
	pushCString(L, string(body))

	// status code
	C.lua_pushinteger(L, C.lua_Integer(resp.StatusCode))

	// headers
	C.lua_newtable_wrapper(L)
	for key, values := range resp.Header {
		pushCString(L, key)
		C.lua_newtable_wrapper(L)
		for i, value := range values {
			C.lua_pushinteger(L, C.lua_Integer(i+1))
			pushCString(L, value)
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
		pushCString(L, "sleep expects exactly one argument (seconds)")
		return 2
	}

	if C.lua_isnumber(L, 1) == 0 {
		C.lua_pushnil(L)
		pushCString(L, "sleep argument must be a number")
		return 2
	}

	seconds := float64(C.lua_tonumber(L, 1))

	// Get the current thread
	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		C.lua_pushnil(L)
		pushCString(L, "sleep: could not find thread context")
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
			cToString := C.CString("tostring")
			C.lua_getglobal_wrapper(L, cToString)
			C.free(unsafe.Pointer(cToString))
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

// writeOutput writes output to buffer or writer
func (gls *GolapisLuaState) writeOutput(text string) {
	if gls.outputWriter != nil {
		gls.outputWriter.Write([]byte(text))
	} else {
		gls.outputBuffer.WriteString(text)
	}
}

// SetupGolapis initializes the golapis global table with exported functions
func (gls *GolapisLuaState) SetupGolapis() {
	gls.golapisRef = C.setup_golapis_global(gls.luaState)
}

func pushCString(L *C.lua_State, s string) {
	cstr := C.CString(s)
	C.lua_pushstring(L, cstr)
	C.free(unsafe.Pointer(cstr))
}
