package golapis

/*
#include "lua_helpers.h"

// Forward declaration for Go functions
extern int golapis_sleep(lua_State *L);
extern int golapis_http_request(lua_State *L);
extern int golapis_print(lua_State *L);
extern int golapis_req_get_uri_args(lua_State *L);

static int c_sleep_wrapper(lua_State *L) {
    return golapis_sleep(L);
}

static int c_http_request_wrapper(lua_State *L) {
    return golapis_http_request(L);
}

static int c_print_wrapper(lua_State *L) {
    return golapis_print(L);
}

static int c_req_get_uri_args_wrapper(lua_State *L) {
    return golapis_req_get_uri_args(L);
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

    // Create req table (for HTTP request inspection functions)
    lua_newtable(L);
    lua_pushcfunction(L, c_req_get_uri_args_wrapper);
    lua_setfield(L, -2, "get_uri_args");
    lua_setfield(L, -2, "req");         // Add req table to `golapis`

    lua_pushvalue(L, -1);               // Duplicate table for registry ref
    int ref = luaL_ref(L, LUA_REGISTRYINDEX);

    lua_setglobal(L, "golapis");        // Set global golapis = table
    return ref;
}

*/
import "C"
import (
	"io"
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

	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushCString(L, "http.request argument must be a string")
		return 2
	}

	url_str := C.GoString(C.lua_tostring_wrapper(L, 1))

	// Get the current thread for async resumption
	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		C.lua_pushnil(L)
		pushCString(L, "http.request: could not find thread context")
		return 2
	}

	// Start async HTTP request
	go func() {
		resp, err := http.Get(url_str)

		var returnVals []interface{}

		if err != nil {
			returnVals = []interface{}{nil, err.Error()}
		} else {
			body, readErr := ioutil.ReadAll(resp.Body)
			resp.Body.Close()

			if readErr != nil {
				returnVals = []interface{}{nil, readErr.Error()}
			} else {
				returnVals = []interface{}{
					string(body),
					resp.StatusCode,
					resp.Header,
				}
			}
		}

		thread.state.eventChan <- &StateEvent{
			Type:       EventResumeThread,
			Thread:     thread,
			ReturnVals: returnVals,
			Response:   nil,
		}
	}()

	return C.lua_yield_wrapper(L, 0)
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
	// Get writer from thread (preferred) or fall back to state's writer
	var writer io.Writer
	thread := getLuaThreadFromRegistry(L)
	if thread != nil && thread.outputWriter != nil {
		writer = thread.outputWriter
	} else {
		// Fallback for CLI mode or non-thread context
		gls := getLuaStateFromRegistry(L)
		if gls != nil {
			writer = gls.outputWriter
		}
	}

	if writer == nil {
		return 0
	}

	nargs := C.lua_gettop(L)
	for i := C.int(1); i <= nargs; i++ {
		if i > 1 {
			writer.Write([]byte("\t"))
		}
		if C.lua_isstring(L, i) != 0 {
			str := C.GoString(C.lua_tostring_wrapper(L, i))
			writer.Write([]byte(str))
		} else {
			// For non-strings, convert to string using Lua's tostring
			cToString := C.CString("tostring")
			C.lua_getglobal_wrapper(L, cToString)
			C.free(unsafe.Pointer(cToString))
			C.lua_pushvalue(L, i)
			if C.lua_pcall(L, 1, 1, 0) == 0 {
				str := C.GoString(C.lua_tostring_wrapper(L, -1))
				writer.Write([]byte(str))
				C.lua_pop_wrapper(L, 1)
			} else {
				writer.Write([]byte("<error converting to string>"))
				C.lua_pop_wrapper(L, 1)
			}
		}
	}
	writer.Write([]byte("\n"))
	return 0
}

//export golapis_req_get_uri_args
func golapis_req_get_uri_args(L *C.lua_State) C.int {
	// Get optional max argument (default 100, like nginx)
	max := 100
	if C.lua_gettop(L) >= 1 && C.lua_isnumber(L, 1) != 0 {
		max = int(C.lua_tonumber(L, 1))
	}

	// Get current thread's HTTP request
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.httpRequest == nil {
		// Not in HTTP context - return nil (nginx-lua compatible behavior)
		C.lua_pushnil(L)
		return 1
	}

	// Parse query parameters from the request URL
	queryValues := thread.httpRequest.URL.Query()

	// Create result table
	C.lua_newtable_wrapper(L)

	count := 0
	for key, values := range queryValues {
		if count >= max {
			break
		}
		count++

		if len(values) == 1 {
			// Single value: {key = "value"}
			pushCString(L, key)
			pushCString(L, values[0])
			C.lua_settable(L, -3)
		} else {
			// Multiple values: {key = {"val1", "val2"}}
			pushCString(L, key)
			C.lua_newtable_wrapper(L)
			for i, v := range values {
				C.lua_pushinteger(L, C.lua_Integer(i+1))
				pushCString(L, v)
				C.lua_settable(L, -3)
			}
			C.lua_settable(L, -3)
		}
	}

	return 1
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
