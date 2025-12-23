package golapis

/*
#include "lua_helpers.h"

// Forward declaration for Go functions
extern int golapis_sleep(lua_State *L);
extern int golapis_http_request(lua_State *L);
extern int golapis_print(lua_State *L);
extern int golapis_say(lua_State *L);
extern int golapis_req_get_uri_args(lua_State *L);
extern int golapis_req_get_headers(lua_State *L);
extern int golapis_req_headers_index(lua_State *L);
extern int golapis_req_read_body(lua_State *L);
extern int golapis_req_get_body_data(lua_State *L);
extern int golapis_req_get_post_args(lua_State *L);
extern int golapis_timer_at(lua_State *L);
extern int golapis_debug_cancel_timers(lua_State *L);
extern int golapis_debug_pending_timer_count(lua_State *L);
extern int golapis_var_index(lua_State *L);
extern int golapis_header_index(lua_State *L);
extern int golapis_header_newindex(lua_State *L);
extern int golapis_now(lua_State *L);
extern int golapis_req_start_time(lua_State *L);
extern int golapis_escape_uri(lua_State *L);
extern int golapis_unescape_uri(lua_State *L);
extern int golapis_status_get(lua_State *L);
extern int golapis_status_set(lua_State *L);
extern int golapis_md5(lua_State *L);
extern int golapis_md5_bin(lua_State *L);
extern int golapis_sha1_bin(lua_State *L);
extern int golapis_hmac_sha1(lua_State *L);
extern int golapis_encode_base64(lua_State *L);
extern int golapis_decode_base64(lua_State *L);
extern int golapis_decode_base64mime(lua_State *L);
extern int golapis_exit(lua_State *L);

// UDP socket functions
extern int golapis_socket_udp_new(lua_State *L);
extern int golapis_udp_setpeername(lua_State *L);
extern int golapis_udp_send(lua_State *L);
extern int golapis_udp_receive(lua_State *L);
extern int golapis_udp_settimeout(lua_State *L);
extern int golapis_udp_close(lua_State *L);
extern int golapis_udp_bind(lua_State *L);
extern int golapis_udp_gc(lua_State *L);

static int c_sleep_wrapper(lua_State *L) {
    return golapis_sleep(L);
}

static int c_http_request_wrapper(lua_State *L) {
    return golapis_http_request(L);
}

static int c_print_wrapper(lua_State *L) {
    return golapis_print(L);
}

static int c_say_wrapper(lua_State *L) {
    return golapis_say(L);
}

static int c_req_get_uri_args_wrapper(lua_State *L) {
    return golapis_req_get_uri_args(L);
}

static int c_req_get_headers_wrapper(lua_State *L) {
    return golapis_req_get_headers(L);
}

static int c_req_headers_index_wrapper(lua_State *L) {
    return golapis_req_headers_index(L);
}

static int c_req_read_body_wrapper(lua_State *L) {
    return golapis_req_read_body(L);
}

static int c_req_get_body_data_wrapper(lua_State *L) {
    return golapis_req_get_body_data(L);
}

static int c_req_get_post_args_wrapper(lua_State *L) {
    return golapis_req_get_post_args(L);
}

static int c_req_start_time_wrapper(lua_State *L) {
    return golapis_req_start_time(L);
}

// Initialize the headers metatable in the registry (call once during setup)
static void init_headers_metatable(lua_State *L) {
    luaL_newmetatable(L, "golapis.req.headers");       // Create and register metatable
    lua_pushcfunction(L, c_req_headers_index_wrapper); // Push __index function
    lua_setfield(L, -2, "__index");                    // metatable.__index = function
    lua_pop(L, 1);                                     // Pop metatable (stored in registry)
}

static int c_timer_at_wrapper(lua_State *L) {
    return golapis_timer_at(L);
}

static int c_debug_cancel_timers_wrapper(lua_State *L) {
    return golapis_debug_cancel_timers(L);
}

static int c_debug_pending_timer_count_wrapper(lua_State *L) {
    return golapis_debug_pending_timer_count(L);
}

static int c_var_index_wrapper(lua_State *L) {
    int result = golapis_var_index(L);
    if (result < 0) {
        // Error message is on stack - use luaL_error to add location info
        return luaL_error(L, "%s", lua_tostring(L, -1));
    }
    return result;
}

static int c_header_index_wrapper(lua_State *L) {
    int result = golapis_header_index(L);
    if (result < 0) {
        return luaL_error(L, "%s", lua_tostring(L, -1));
    }
    return result;
}

static int c_header_newindex_wrapper(lua_State *L) {
    int result = golapis_header_newindex(L);
    if (result < 0) {
        return luaL_error(L, "%s", lua_tostring(L, -1));
    }
    return result;
}

static int c_now_wrapper(lua_State *L) {
    return golapis_now(L);
}

static int c_escape_uri_wrapper(lua_State *L) {
    return golapis_escape_uri(L);
}

static int c_unescape_uri_wrapper(lua_State *L) {
    return golapis_unescape_uri(L);
}

static int c_md5_wrapper(lua_State *L) {
    return golapis_md5(L);
}

static int c_md5_bin_wrapper(lua_State *L) {
    return golapis_md5_bin(L);
}

static int c_sha1_bin_wrapper(lua_State *L) {
    return golapis_sha1_bin(L);
}

static int c_hmac_sha1_wrapper(lua_State *L) {
    return golapis_hmac_sha1(L);
}

static int c_encode_base64_wrapper(lua_State *L) {
    return golapis_encode_base64(L);
}

static int c_decode_base64_wrapper(lua_State *L) {
    return golapis_decode_base64(L);
}

static int c_decode_base64mime_wrapper(lua_State *L) {
    return golapis_decode_base64mime(L);
}

static int c_exit_wrapper(lua_State *L) {
    int result = golapis_exit(L);
    if (result < 0) {
        // Error - message is on stack
        return luaL_error(L, "%s", lua_tostring(L, -1));
    }
    // Success - yield from C (not Go, since yield uses longjmp)
    return lua_yield(L, 0);
}

// No-op - Go's time.Now() is already fast (VDSO). Exists for API compatibility.
static int c_update_time_noop(lua_State *L) {
    (void)L;
    return 0;
}

// UDP socket wrappers
static int c_socket_udp_new_wrapper(lua_State *L) {
    return golapis_socket_udp_new(L);
}

static int c_udp_setpeername_wrapper(lua_State *L) {
    return golapis_udp_setpeername(L);
}

static int c_udp_send_wrapper(lua_State *L) {
    return golapis_udp_send(L);
}

static int c_udp_receive_wrapper(lua_State *L) {
    return golapis_udp_receive(L);
}

static int c_udp_settimeout_wrapper(lua_State *L) {
    return golapis_udp_settimeout(L);
}

static int c_udp_close_wrapper(lua_State *L) {
    return golapis_udp_close(L);
}

static int c_udp_bind_wrapper(lua_State *L) {
    return golapis_udp_bind(L);
}

static int c_udp_gc_wrapper(lua_State *L) {
    return golapis_udp_gc(L);
}

// Main table __index metamethod
// Stack: [golapis_table, key]
static int c_main_index_wrapper(lua_State *L) {
    if (lua_type(L, 2) == LUA_TSTRING) {
        const char *key = lua_tostring(L, 2);
        if (strcmp(key, "status") == 0) {
            return golapis_status_get(L);
        }
    }
    // Fall through: rawget the key from the table
    lua_rawget(L, 1);
    return 1;
}

// Main table __newindex metamethod
// Stack: [golapis_table, key, value]
static int c_main_newindex_wrapper(lua_State *L) {
    if (lua_type(L, 2) == LUA_TSTRING) {
        const char *key = lua_tostring(L, 2);
        if (strcmp(key, "status") == 0) {
            int result = golapis_status_set(L);
            if (result < 0) {
                return luaL_error(L, "%s", lua_tostring(L, -1));
            }
            return result;
        }
    }
    // Fall through: rawset the key/value in the table
    lua_rawset(L, 1);
    return 0;
}

// Initialize the main golapis metatable in the registry (call once during setup)
static void init_main_metatable(lua_State *L) {
    luaL_newmetatable(L, "golapis.main");              // Create and register metatable
    lua_pushcfunction(L, c_main_index_wrapper);
    lua_setfield(L, -2, "__index");                    // metatable.__index = handler
    lua_pushcfunction(L, c_main_newindex_wrapper);
    lua_setfield(L, -2, "__newindex");                 // metatable.__newindex = handler
    lua_pop(L, 1);                                     // Pop metatable (stored in registry)
}

// Initialize the UDP socket metatable in the registry (call once during setup)
static void init_udp_socket_metatable(lua_State *L) {
    luaL_newmetatable(L, "golapis.socket.udp");

    // Create methods table for __index
    lua_newtable(L);
    lua_pushcfunction(L, c_udp_setpeername_wrapper);
    lua_setfield(L, -2, "setpeername");
    lua_pushcfunction(L, c_udp_send_wrapper);
    lua_setfield(L, -2, "send");
    lua_pushcfunction(L, c_udp_receive_wrapper);
    lua_setfield(L, -2, "receive");
    lua_pushcfunction(L, c_udp_settimeout_wrapper);
    lua_setfield(L, -2, "settimeout");
    lua_pushcfunction(L, c_udp_close_wrapper);
    lua_setfield(L, -2, "close");
    lua_pushcfunction(L, c_udp_bind_wrapper);
    lua_setfield(L, -2, "bind");
    lua_setfield(L, -2, "__index");  // metatable.__index = methods table

    // GC metamethod
    lua_pushcfunction(L, c_udp_gc_wrapper);
    lua_setfield(L, -2, "__gc");

    lua_pop(L, 1);  // Pop metatable (stored in registry)
}

static int setup_golapis_global(lua_State *L) {
    lua_newtable(L);                    // Create new table `golapis`

    lua_pushstring(L, "1.0.0");
    lua_setfield(L, -2, "version");

    lua_pushcfunction(L, c_sleep_wrapper);
    lua_setfield(L, -2, "sleep");

    lua_pushcfunction(L, c_now_wrapper);
    lua_setfield(L, -2, "now");

    lua_pushcfunction(L, c_update_time_noop);
    lua_setfield(L, -2, "update_time");

    lua_pushcfunction(L, c_print_wrapper);
    lua_setfield(L, -2, "print");

    lua_pushcfunction(L, c_say_wrapper);
    lua_setfield(L, -2, "say");

    lua_pushcfunction(L, c_exit_wrapper);
    lua_setfield(L, -2, "exit");

    lua_pushcfunction(L, c_escape_uri_wrapper);
    lua_setfield(L, -2, "escape_uri");

    lua_pushcfunction(L, c_unescape_uri_wrapper);
    lua_setfield(L, -2, "unescape_uri");

    lua_pushcfunction(L, c_md5_wrapper);
    lua_setfield(L, -2, "md5");

    lua_pushcfunction(L, c_md5_bin_wrapper);
    lua_setfield(L, -2, "md5_bin");

    lua_pushcfunction(L, c_sha1_bin_wrapper);
    lua_setfield(L, -2, "sha1_bin");

    lua_pushcfunction(L, c_hmac_sha1_wrapper);
    lua_setfield(L, -2, "hmac_sha1");

    lua_pushcfunction(L, c_encode_base64_wrapper);
    lua_setfield(L, -2, "encode_base64");

    lua_pushcfunction(L, c_decode_base64_wrapper);
    lua_setfield(L, -2, "decode_base64");

    lua_pushcfunction(L, c_decode_base64mime_wrapper);
    lua_setfield(L, -2, "decode_base64mime");

    // Register golapis.null as lightuserdata(NULL)
    lua_pushlightuserdata(L, NULL);
    lua_setfield(L, -2, "null");

    // HTTP status codes (ngx.HTTP_* equivalents)
    lua_pushinteger(L, 200);
    lua_setfield(L, -2, "HTTP_OK");
    lua_pushinteger(L, 201);
    lua_setfield(L, -2, "HTTP_CREATED");
    lua_pushinteger(L, 204);
    lua_setfield(L, -2, "HTTP_NO_CONTENT");
    lua_pushinteger(L, 301);
    lua_setfield(L, -2, "HTTP_MOVED_PERMANENTLY");
    lua_pushinteger(L, 302);
    lua_setfield(L, -2, "HTTP_MOVED_TEMPORARILY");
    lua_pushinteger(L, 304);
    lua_setfield(L, -2, "HTTP_NOT_MODIFIED");
    lua_pushinteger(L, 400);
    lua_setfield(L, -2, "HTTP_BAD_REQUEST");
    lua_pushinteger(L, 401);
    lua_setfield(L, -2, "HTTP_UNAUTHORIZED");
    lua_pushinteger(L, 403);
    lua_setfield(L, -2, "HTTP_FORBIDDEN");
    lua_pushinteger(L, 404);
    lua_setfield(L, -2, "HTTP_NOT_FOUND");
    lua_pushinteger(L, 405);
    lua_setfield(L, -2, "HTTP_NOT_ALLOWED");
    lua_pushinteger(L, 500);
    lua_setfield(L, -2, "HTTP_INTERNAL_SERVER_ERROR");
    lua_pushinteger(L, 502);
    lua_setfield(L, -2, "HTTP_BAD_GATEWAY");
    lua_pushinteger(L, 503);
    lua_setfield(L, -2, "HTTP_SERVICE_UNAVAILABLE");
    lua_pushinteger(L, 504);
    lua_setfield(L, -2, "HTTP_GATEWAY_TIMEOUT");

    // Special codes (ngx.OK, ngx.ERROR equivalents)
    lua_pushinteger(L, 0);
    lua_setfield(L, -2, "OK");
    lua_pushinteger(L, -1);
    lua_setfield(L, -2, "ERROR");

    // Create http table
    lua_newtable(L);
    lua_pushcfunction(L, c_http_request_wrapper);
    lua_setfield(L, -2, "request");
    lua_setfield(L, -2, "http");        // Add http table to `golapis`

    // Create req table (for HTTP request inspection functions)
    lua_newtable(L);
    lua_pushcfunction(L, c_req_get_uri_args_wrapper);
    lua_setfield(L, -2, "get_uri_args");
    lua_pushcfunction(L, c_req_get_headers_wrapper);
    lua_setfield(L, -2, "get_headers");
    lua_pushcfunction(L, c_req_read_body_wrapper);
    lua_setfield(L, -2, "read_body");
    lua_pushcfunction(L, c_req_get_body_data_wrapper);
    lua_setfield(L, -2, "get_body_data");
    lua_pushcfunction(L, c_req_get_post_args_wrapper);
    lua_setfield(L, -2, "get_post_args");
    lua_pushcfunction(L, c_req_start_time_wrapper);
    lua_setfield(L, -2, "start_time");
    lua_setfield(L, -2, "req");         // Add req table to `golapis`

    // Create timer table
    lua_newtable(L);
    lua_pushcfunction(L, c_timer_at_wrapper);
    lua_setfield(L, -2, "at");
    lua_setfield(L, -2, "timer");       // Add timer table to `golapis`

    // Create debug table
    lua_newtable(L);
    lua_pushcfunction(L, c_debug_cancel_timers_wrapper);
    lua_setfield(L, -2, "cancel_timers");
    lua_pushcfunction(L, c_debug_pending_timer_count_wrapper);
    lua_setfield(L, -2, "pending_timer_count");
    lua_setfield(L, -2, "debug");       // Add debug table to `golapis`

    // Create var proxy table with __index metatable
    lua_newtable(L);                    // Create empty 'var' table
    lua_newtable(L);                    // Create metatable
    lua_pushcfunction(L, c_var_index_wrapper);
    lua_setfield(L, -2, "__index");     // metatable.__index = handler
    lua_setmetatable(L, -2);            // setmetatable(var, metatable)
    lua_setfield(L, -2, "var");         // golapis.var = var

    // Create header proxy table with __index and __newindex metamethods
    lua_newtable(L);                    // Create empty 'header' table
    lua_newtable(L);                    // Create metatable
    lua_pushcfunction(L, c_header_index_wrapper);
    lua_setfield(L, -2, "__index");     // metatable.__index = read handler
    lua_pushcfunction(L, c_header_newindex_wrapper);
    lua_setfield(L, -2, "__newindex");  // metatable.__newindex = write handler
    lua_setmetatable(L, -2);            // setmetatable(header, metatable)
    lua_setfield(L, -2, "header");      // golapis.header = header

    // Create socket table (for TCP/UDP cosockets)
    lua_newtable(L);
    lua_pushcfunction(L, c_socket_udp_new_wrapper);
    lua_setfield(L, -2, "udp");
    lua_setfield(L, -2, "socket");      // golapis.socket = { udp = fn }

    // Initialize cached metatables
    init_headers_metatable(L);
    init_main_metatable(L);
    init_udp_socket_metatable(L);

    // Apply metatable to golapis table (for status magic key)
    luaL_getmetatable(L, "golapis.main");  // Push cached metatable from registry
    lua_setmetatable(L, -2);               // setmetatable(golapis, metatable)

    lua_pushvalue(L, -1);               // Duplicate table for registry ref
    int ref = luaL_ref(L, LUA_REGISTRYINDEX);

    lua_setglobal(L, "golapis");        // Set global golapis = table
    return ref;
}

*/
import "C"
import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

//go:embed bootstrap.lua
var luaBootstrap string

// bufferPool is used to reduce allocations in golapisOutput
var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

//export golapis_http_request
func golapis_http_request(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 1 {
		C.lua_pushnil(L)
		pushGoString(L, "http.request expects exactly one argument (url)")
		return 2
	}

	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "http.request argument must be a string")
		return 2
	}

	url_str := C.GoString(C.lua_tostring_wrapper(L, 1))

	// Get the current thread for async resumption
	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		C.lua_pushnil(L)
		pushGoString(L, "http.request: could not find thread context")
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
		pushGoString(L, "sleep expects exactly one argument (seconds)")
		return 2
	}

	if C.lua_isnumber(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "sleep argument must be a number")
		return 2
	}

	seconds := float64(C.lua_tonumber(L, 1))

	// Get the current thread
	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		C.lua_pushnil(L)
		pushGoString(L, "sleep: could not find thread context")
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

//export golapis_exit
func golapis_exit(L *C.lua_State) C.int {
	// Get optional status code (default 0 = 200 OK)
	status := 0
	if C.lua_gettop(L) >= 1 {
		if C.lua_isnumber(L, 1) == 0 {
			pushGoString(L, "exit status must be a number")
			return -1 // Signal error to C wrapper
		}
		status = int(C.lua_tonumber(L, 1))
	}

	thread := getLuaThreadFromRegistry(L)
	if thread == nil {
		pushGoString(L, "exit: could not find thread context")
		return -1
	}

	// Only allow in HTTP request context
	if thread.request == nil {
		pushGoString(L, "exit can only be called from HTTP request context")
		return -1
	}

	// Validate status (0 means default, or 100-599)
	if status != 0 && (status < 100 || status > 599) {
		pushGoString(L, fmt.Sprintf("invalid exit status: %d", status))
		return -1
	}

	// Set response status if headers not sent
	if !thread.request.HeadersSent {
		if status >= 100 && status <= 599 {
			thread.request.ResponseStatus = status
		}
	}

	// Store exit state on the thread and signal success
	// The C wrapper will do the actual yield
	thread.exited = true
	thread.exitCode = status

	return 0 // Success - C wrapper will yield
}

//export golapis_now
func golapis_now(L *C.lua_State) C.int {
	now := float64(time.Now().UnixNano()) / 1e9
	C.lua_pushnumber(L, C.lua_Number(now))
	return 1
}

//export golapis_req_start_time
func golapis_req_start_time(L *C.lua_State) C.int {
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		C.lua_pushnil(L)
		return 1
	}

	startTime := float64(thread.request.StartTime().UnixNano()) / 1e9
	C.lua_pushnumber(L, C.lua_Number(startTime))
	return 1
}

//export golapis_escape_uri
func golapis_escape_uri(L *C.lua_State) C.int {
	nargs := int(C.lua_gettop(L))
	if nargs < 1 || nargs > 2 {
		C.lua_pushnil(L)
		pushGoString(L, "escape_uri expects 1 or 2 arguments")
		return 2
	}

	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "escape_uri: first argument must be a string")
		return 2
	}

	str := string(luaStringBytes(L, 1))

	// Default type is 2
	escapeType := 2
	if nargs == 2 {
		if C.lua_isnumber(L, 2) == 0 {
			C.lua_pushnil(L)
			pushGoString(L, "escape_uri: second argument must be a number")
			return 2
		}
		escapeType = int(C.lua_tonumber(L, 2))
		if escapeType != 0 && escapeType != 2 {
			C.lua_pushnil(L)
			pushGoString(L, "escape_uri: type must be 0 or 2")
			return 2
		}
	}

	result := escapeURI(str, escapeType)
	pushGoString(L, result)
	return 1
}

//export golapis_unescape_uri
func golapis_unescape_uri(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 1 {
		C.lua_pushnil(L)
		pushGoString(L, "unescape_uri expects exactly 1 argument")
		return 2
	}

	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "unescape_uri: argument must be a string")
		return 2
	}

	str := string(luaStringBytes(L, 1))
	result := unescapeURI(str)
	pushGoString(L, result)
	return 1
}

//export golapis_md5
func golapis_md5(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 1 {
		C.lua_pushnil(L)
		pushGoString(L, "md5 expects exactly 1 argument")
		return 2
	}
	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "md5: argument must be a string")
		return 2
	}
	data := luaStringBytes(L, 1)
	hash := md5.Sum(data)
	pushGoString(L, hex.EncodeToString(hash[:]))
	return 1
}

//export golapis_md5_bin
func golapis_md5_bin(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 1 {
		C.lua_pushnil(L)
		pushGoString(L, "md5_bin expects exactly 1 argument")
		return 2
	}
	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "md5_bin: argument must be a string")
		return 2
	}
	data := luaStringBytes(L, 1)
	hash := md5.Sum(data)
	C.lua_pushlstring(L, (*C.char)(unsafe.Pointer(&hash[0])), C.size_t(len(hash)))
	return 1
}

//export golapis_sha1_bin
func golapis_sha1_bin(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 1 {
		C.lua_pushnil(L)
		pushGoString(L, "sha1_bin expects exactly 1 argument")
		return 2
	}
	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "sha1_bin: argument must be a string")
		return 2
	}
	data := luaStringBytes(L, 1)
	hash := sha1.Sum(data)
	C.lua_pushlstring(L, (*C.char)(unsafe.Pointer(&hash[0])), C.size_t(len(hash)))
	return 1
}

//export golapis_hmac_sha1
func golapis_hmac_sha1(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 2 {
		C.lua_pushnil(L)
		pushGoString(L, "hmac_sha1 expects exactly 2 arguments (secret_key, str)")
		return 2
	}
	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "hmac_sha1: first argument (secret_key) must be a string")
		return 2
	}
	if C.lua_isstring(L, 2) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "hmac_sha1: second argument (str) must be a string")
		return 2
	}
	key := luaStringBytes(L, 1)
	data := luaStringBytes(L, 2)
	h := hmac.New(sha1.New, key)
	h.Write(data)
	digest := h.Sum(nil)
	C.lua_pushlstring(L, (*C.char)(unsafe.Pointer(&digest[0])), C.size_t(len(digest)))
	return 1
}

//export golapis_encode_base64
func golapis_encode_base64(L *C.lua_State) C.int {
	nargs := int(C.lua_gettop(L))
	if nargs < 1 || nargs > 2 {
		C.lua_pushnil(L)
		pushGoString(L, "encode_base64 expects 1 or 2 arguments")
		return 2
	}
	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "encode_base64: first argument must be a string")
		return 2
	}
	data := luaStringBytes(L, 1)

	noPadding := false
	if nargs == 2 && C.lua_toboolean(L, 2) != 0 {
		noPadding = true
	}

	var result string
	if noPadding {
		result = base64.RawStdEncoding.EncodeToString(data)
	} else {
		result = base64.StdEncoding.EncodeToString(data)
	}
	pushGoString(L, result)
	return 1
}

//export golapis_decode_base64
func golapis_decode_base64(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 1 {
		C.lua_pushnil(L)
		pushGoString(L, "decode_base64 expects exactly 1 argument")
		return 2
	}
	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "decode_base64: argument must be a string")
		return 2
	}
	str := string(luaStringBytes(L, 1))

	// Try with padding first, then without (nginx-lua accepts both)
	decoded, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(str)
	}
	if err != nil {
		C.lua_pushnil(L)
		return 1
	}
	if len(decoded) == 0 {
		C.lua_pushlstring(L, nil, 0)
		return 1
	}
	C.lua_pushlstring(L, (*C.char)(unsafe.Pointer(&decoded[0])), C.size_t(len(decoded)))
	return 1
}

//export golapis_decode_base64mime
func golapis_decode_base64mime(L *C.lua_State) C.int {
	if C.lua_gettop(L) != 1 {
		C.lua_pushnil(L)
		pushGoString(L, "decode_base64mime expects exactly 1 argument")
		return 2
	}
	if C.lua_isstring(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "decode_base64mime: argument must be a string")
		return 2
	}
	str := string(luaStringBytes(L, 1))

	// Filter out non-base64 characters (MIME allows whitespace and ignores invalid chars)
	filtered := filterBase64Chars(str)

	decoded, err := base64.StdEncoding.DecodeString(filtered)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(filtered)
	}
	if err != nil {
		C.lua_pushnil(L)
		return 1
	}
	if len(decoded) == 0 {
		C.lua_pushlstring(L, nil, 0)
		return 1
	}
	C.lua_pushlstring(L, (*C.char)(unsafe.Pointer(&decoded[0])), C.size_t(len(decoded)))
	return 1
}

// filterBase64Chars removes non-base64 characters for MIME decoding
func filterBase64Chars(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// Maximum recursion depth for table coercion to prevent stack overflow
const maxTableDepth = 100

// formatLuaNumber formats a number following nginx-lua conventions:
// - Integers within int32 range use decimal format
// - Others use %.14g format (14 significant digits)
func formatLuaNumber(num float64) string {
	// Check if it's an integer within int32 range
	intVal := int32(num)
	if num == float64(intVal) && num >= math.MinInt32 && num <= math.MaxInt32 {
		return strconv.FormatInt(int64(intVal), 10)
	}
	return strconv.FormatFloat(num, 'g', 14, 64)
}

// badArgError returns a user-facing error message for golapis functions.
func badArgError(argNum int, funcName, gotType string) string {
	return fmt.Sprintf("bad argument #%d to 'golapis.%s' (string, number, boolean, nil, golapis.null, or array table expected, got %s)", argNum, funcName, gotType)
}

// getOutputWriter returns the output writer for the current context
func getOutputWriter(L *C.lua_State) io.Writer {
	thread := getLuaThreadFromRegistry(L)
	if thread != nil && thread.outputWriter != nil {
		return thread.outputWriter
	}
	// Fallback for CLI mode or non-thread context
	gls := getLuaStateFromRegistry(L)
	if gls != nil {
		return gls.outputWriter
	}
	return nil
}

// validateArrayTable validates that the table at idx is array-style and returns the max key.
// Returns error if non-array keys are found.
func validateArrayTable(L *C.lua_State, idx C.int) (int, error) {
	maxKey := 0

	// Push nil to start iteration
	C.lua_pushnil(L)
	for C.lua_next_wrapper(L, idx) != 0 {
		// Stack: key at -2, value at -1

		// Check if key is a number
		if C.lua_type(L, -2) != C.LUA_TNUMBER {
			C.lua_pop_wrapper(L, 2) // pop key and value
			return 0, fmt.Errorf("non-array table found")
		}

		keyNum := float64(C.lua_tonumber(L, -2))
		keyInt := int(keyNum)

		// Check if key is a positive integer
		if float64(keyInt) != keyNum || keyInt < 1 {
			C.lua_pop_wrapper(L, 2)
			return 0, fmt.Errorf("non-array table found")
		}

		if keyInt > maxKey {
			maxKey = keyInt
		}

		// Pop value, keep key for next iteration
		C.lua_pop_wrapper(L, 1)
	}

	return maxKey, nil
}

// coerceTableToBytes converts an array-style table to bytes
func coerceTableToBytes(L *C.lua_State, idx C.int, buf *bytes.Buffer, depth int, argNum int, funcName string) error {
	if depth > maxTableDepth {
		return fmt.Errorf("table recursion too deep")
	}

	// Convert negative index to absolute
	if idx < 0 {
		idx = C.lua_gettop(L) + idx + 1
	}

	// Validate it's an array table and find max key
	maxKey, err := validateArrayTable(L, idx)
	if err != nil {
		return errors.New(badArgError(argNum, funcName, err.Error()))
	}

	if maxKey == 0 {
		// Empty table - nothing to output
		return nil
	}

	// Iterate 1 through maxKey
	for i := 1; i <= maxKey; i++ {
		C.lua_rawgeti_wrapper(L, idx, C.int(i))
		err := coerceValueToBytes(L, -1, buf, depth+1, argNum, funcName)
		C.lua_pop_wrapper(L, 1)
		if err != nil {
			return err
		}
	}

	return nil
}

// coerceValueToBytes converts the Lua value at stack index to bytes
// Returns error for unsupported types
func coerceValueToBytes(L *C.lua_State, idx C.int, buf *bytes.Buffer, depth int, argNum int, funcName string) error {
	luaType := C.lua_type(L, idx)

	switch luaType {
	case C.LUA_TSTRING:
		// Get string with length (handles embedded NULs)
		var strLen C.size_t
		cstr := C.lua_tolstring_wrapper(L, idx, &strLen)
		if cstr != nil {
			buf.Write(C.GoBytes(unsafe.Pointer(cstr), C.int(strLen)))
		}

	case C.LUA_TNUMBER:
		num := float64(C.lua_tonumber(L, idx))
		buf.WriteString(formatLuaNumber(num))

	case C.LUA_TBOOLEAN:
		if C.lua_toboolean(L, idx) != 0 {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}

	case C.LUA_TNIL:
		buf.WriteString("nil")

	case C.LUA_TLIGHTUSERDATA:
		// Check if it's NULL (golapis.null)
		ptr := C.lua_touserdata_wrapper(L, idx)
		if ptr == nil {
			buf.WriteString("null")
		} else {
			return errors.New(badArgError(argNum, funcName, "lightuserdata"))
		}

	case C.LUA_TTABLE:
		return coerceTableToBytes(L, idx, buf, depth, argNum, funcName)

	case C.LUA_TFUNCTION:
		return errors.New(badArgError(argNum, funcName, "function"))

	case C.LUA_TUSERDATA:
		return errors.New(badArgError(argNum, funcName, "userdata"))

	case C.LUA_TTHREAD:
		return errors.New(badArgError(argNum, funcName, "thread"))

	default:
		typeName := C.GoString(C.lua_typename(L, luaType))
		return errors.New(badArgError(argNum, funcName, typeName))
	}

	return nil
}

// golapisOutput is the shared implementation for say/print
func golapisOutput(L *C.lua_State, appendNewline bool, funcName string) C.int {
	writer := getOutputWriter(L)
	if writer == nil {
		// No output context - return success anyway (matches nginx-lua behavior)
		C.lua_pushinteger(L, 1)
		return 1
	}

	// Get buffer from pool to reduce allocations
	buf := bufferPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		bufferPool.Put(buf)
	}()

	// Process each argument
	nargs := int(C.lua_gettop(L))
	for i := 1; i <= nargs; i++ {
		err := coerceValueToBytes(L, C.int(i), buf, 0, i, funcName)
		if err != nil {
			C.lua_pushnil(L)
			pushGoString(L, err.Error())
			return 2
		}
	}

	// Append newline if say()
	if appendNewline {
		buf.WriteByte('\n')
	}

	// Write to output
	data := buf.Bytes()
	for len(data) > 0 {
		n, writeErr := writer.Write(data)
		if writeErr != nil {
			C.lua_pushnil(L)
			pushGoString(L, "nginx output filter error")
			return 2
		}
		if n <= 0 {
			C.lua_pushnil(L)
			pushGoString(L, "nginx output filter error")
			return 2
		}
		data = data[n:]
	}

	// Return 1 on success
	C.lua_pushinteger(L, 1)
	return 1
}

//export golapis_print
func golapis_print(L *C.lua_State) C.int {
	return golapisOutput(L, false, "print") // no newline
}

//export golapis_say
func golapis_say(L *C.lua_State) C.int {
	return golapisOutput(L, true, "say") // append newline
}

//export golapis_timer_at
func golapis_timer_at(L *C.lua_State) C.int {
	nargs := int(C.lua_gettop(L))

	// Validate: at least 2 arguments (delay, callback)
	if nargs < 2 {
		C.lua_pushnil(L)
		pushGoString(L, "expecting at least 2 arguments (delay, callback)")
		return 2
	}

	// Validate delay is a number
	if C.lua_isnumber(L, 1) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "delay must be a number")
		return 2
	}

	// Validate callback is a function (and not a C function)
	if C.lua_isfunction_wrapper(L, 2) == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "callback must be a function")
		return 2
	}

	delay := float64(C.lua_tonumber(L, 1))
	if delay < 0 {
		C.lua_pushnil(L)
		pushGoString(L, "delay must be >= 0")
		return 2
	}

	// Get the GolapisLuaState (we need the main Lua state, not the coroutine)
	gls := getLuaStateFromRegistry(L)
	if gls == nil {
		C.lua_pushnil(L)
		pushGoString(L, "timer.at: could not find golapis state")
		return 2
	}

	mainL := gls.luaState

	// Create a new coroutine on the main state
	co := C.lua_newthread(mainL)
	if co == nil {
		C.lua_pushnil(L)
		pushGoString(L, "failed to create timer coroutine")
		return 2
	}

	// Store coroutine in registry to prevent GC (it's currently on main stack)
	coRef := C.luaL_ref_wrapper(mainL, C.LUA_REGISTRYINDEX)

	// Copy callback function to coroutine stack
	C.lua_pushvalue(L, 2) // Push callback onto current stack
	C.lua_xmove(L, co, 1) // Move to coroutine

	// Copy any additional arguments to coroutine stack
	for i := 3; i <= nargs; i++ {
		C.lua_pushvalue(L, C.int(i))
		C.lua_xmove(L, co, 1)
	}

	// Create PendingTimer
	timer := &PendingTimer{
		State:      gls,
		CoRef:      coRef,
		Co:         co,
		cancelChan: make(chan struct{}),
	}

	// Add to pending timers set
	gls.timerMu.Lock()
	gls.pendingTimers[timer] = struct{}{}
	gls.timerMu.Unlock()

	// Track this timer in the wait group so Wait() blocks until timer completes
	gls.threadWg.Add(1)

	// Launch timer goroutine
	go func() {
		select {
		case <-time.After(time.Duration(delay * float64(time.Second))):
			// Normal timer fire
			// Note: if the state stops after this fires, the send can block forever.
			// Acceptable for process shutdown; revisit if states are restarted long-lived.
			gls.eventChan <- &StateEvent{
				Type:      EventTimerFire,
				Timer:     timer,
				Premature: false,
			}
		case <-timer.cancelChan:
			// Cancelled - check if we should fire callback or exit silently
			if !timer.State.stopping.Load() {
				gls.eventChan <- &StateEvent{
					Type:      EventTimerFire,
					Timer:     timer,
					Premature: true,
				}
			}
			// else: hard stop, exit silently without firing callback
		}
	}()

	if debugEnabled {
		debugLog("timer.at: created timer co=%p delay=%v", co, delay)
	}

	// Return true (success)
	C.lua_pushboolean(L, 1)
	return 1
}

// pushQueryArgsToLuaTable creates a Lua table from parsed query args.
// Groups multiple values for the same key into arrays.
// Args with empty keys are always skipped.
func pushQueryArgsToLuaTable(L *C.lua_State, args []queryArg) {
	C.lua_newtable_wrapper(L)

	argBuckets := make(map[string][]queryArg)
	for _, arg := range args {
		if arg.key == "" {
			continue
		}
		argBuckets[arg.key] = append(argBuckets[arg.key], arg)
	}

	for key, keyArgs := range argBuckets {
		if len(keyArgs) == 1 {
			// Single value
			pushGoString(L, key)
			if keyArgs[0].isBoolean {
				C.lua_pushboolean(L, 1) // true
			} else {
				pushGoString(L, keyArgs[0].value)
			}
			C.lua_settable(L, -3)
		} else {
			// Multiple values: {key = {val1, val2, ...}}
			pushGoString(L, key)
			C.lua_newtable_wrapper(L)
			for i, arg := range keyArgs {
				C.lua_pushinteger(L, C.lua_Integer(i+1))
				if arg.isBoolean {
					C.lua_pushboolean(L, 1) // true
				} else {
					pushGoString(L, arg.value)
				}
				C.lua_settable(L, -3)
			}
			C.lua_settable(L, -3)
		}
	}
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
	if thread == nil || thread.request == nil {
		// Not in HTTP context - return nil (nginx-lua compatible behavior)
		C.lua_pushnil(L)
		return 1
	}

	// Parse query parameters with nginx-lua compatible behavior
	queryArgs, truncated := parseQueryString(thread.request.Request.URL.RawQuery, max)

	pushQueryArgsToLuaTable(L, queryArgs)

	// Return table and optional "truncated" error
	if truncated {
		pushGoString(L, "truncated")
		return 2
	}
	return 1
}

//export golapis_req_get_headers
func golapis_req_get_headers(L *C.lua_State) C.int {
	// Get optional max_headers argument (default 100, like nginx)
	// 0 means unlimited
	maxHeaders := 100
	if C.lua_gettop(L) >= 1 && C.lua_isnumber(L, 1) != 0 {
		maxHeaders = int(C.lua_tonumber(L, 1))
	}

	// Get optional raw argument (default false)
	// When true: keep original header casing, no metamethod
	// When false: lowercase keys, add __index metamethod for normalized lookup
	raw := false
	if C.lua_gettop(L) >= 2 && C.lua_type(L, 2) == C.LUA_TBOOLEAN {
		raw = C.lua_toboolean(L, 2) != 0
	}

	// Get current thread's HTTP request
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		// Not in HTTP context - return empty table (nginx-lua compatible behavior)
		C.lua_newtable_wrapper(L)
		return 1
	}

	req := thread.request.Request

	// Create result table
	C.lua_newtable_wrapper(L)

	// Count total header entries (each header line counts as 1)
	count := 0
	truncated := false

	// Collect headers into the table
	// Go's http.Header is map[string][]string where keys are canonicalized
	for name, values := range req.Header {
		if maxHeaders > 0 && count >= maxHeaders {
			truncated = true
			break
		}

		// Determine the key to use
		var key string
		if raw {
			key = name // Keep canonical form (e.g., "Content-Type")
		} else {
			key = strings.ToLower(name) // Normalize to lowercase
		}

		if len(values) == 1 {
			if maxHeaders > 0 && count >= maxHeaders {
				truncated = true
				break
			}
			// Push key
			pushGoString(L, key)
			// Single value - push as string
			pushGoString(L, values[0])
			C.lua_settable(L, -3)
			count++
		} else {
			if maxHeaders > 0 {
				remaining := maxHeaders - count
				if remaining <= 0 {
					truncated = true
					break
				}
				if remaining < len(values) {
					truncated = true
					values = values[:remaining]
				}
			}
			// Push key
			pushGoString(L, key)
			// Multiple values - push as array table
			C.lua_newtable_wrapper(L)
			for i, v := range values {
				C.lua_pushinteger(L, C.lua_Integer(i+1))
				pushGoString(L, v)
				C.lua_settable(L, -3)
			}
			C.lua_settable(L, -3)
			count += len(values)
		}
	}

	// Add Host header (Go moves it from Header to Request.Host)
	if req.Host != "" && (maxHeaders == 0 || count < maxHeaders) {
		var hostKey string
		if raw {
			hostKey = "Host"
		} else {
			hostKey = "host"
		}
		pushGoString(L, hostKey)
		pushGoString(L, req.Host)
		C.lua_settable(L, -3)
		count++
	} else if req.Host != "" && maxHeaders > 0 && count >= maxHeaders {
		truncated = true
	}

	// Add metamethod for normalized lookup (when raw=false)
	if !raw {
		// Call C helper that sets up metatable with __index for normalized header lookup
		C.setup_headers_metatable(L)
	}

	// Return table and optional "truncated" error
	if truncated {
		pushGoString(L, "truncated")
		return 2
	}
	return 1
}

//export golapis_req_headers_index
func golapis_req_headers_index(L *C.lua_State) C.int {
	// __index metamethod for normalized header lookup
	// Stack: [headers_table, key]
	if C.lua_isstring(L, 2) == 0 {
		C.lua_pushnil(L)
		return 1
	}

	key := C.GoString(C.lua_tostring_wrapper(L, 2))

	// Normalize: lowercase, replace underscores with hyphens
	normalized := strings.ToLower(strings.ReplaceAll(key, "_", "-"))

	// Do rawget with normalized key
	pushGoString(L, normalized)
	C.lua_rawget_wrapper(L, 1)

	return 1
}

//export golapis_req_read_body
func golapis_req_read_body(L *C.lua_State) C.int {
	// Read and cache the request body (required before calling get_post_args)
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		// Not in HTTP context - return nil, error
		C.lua_pushnil(L)
		pushGoString(L, "no request found")
		return 2
	}

	_, err := thread.request.ReadBody()
	if err != nil {
		C.lua_pushnil(L)
		pushGoString(L, err.Error())
		return 2
	}

	// Success - return nothing (nginx-lua compatible)
	return 0
}

//export golapis_req_get_body_data
func golapis_req_get_body_data(L *C.lua_State) C.int {
	// Get optional max_bytes argument (0 or nil means no limit)
	maxBytes := 0
	if C.lua_gettop(L) >= 1 && C.lua_isnumber(L, 1) != 0 {
		maxBytes = int(C.lua_tonumber(L, 1))
	}

	// Get current thread's HTTP request
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		// Not in HTTP context - return nil
		C.lua_pushnil(L)
		return 1
	}

	// Check if body was read
	if !thread.request.BodyWasRead() {
		C.lua_pushnil(L)
		return 1
	}

	// Get the cached body
	bodyData := thread.request.GetBody()
	if bodyData == nil || len(bodyData) == 0 {
		// Empty body - return nil (nginx-lua compatible)
		C.lua_pushnil(L)
		return 1
	}

	// Apply max_bytes limit if specified
	if maxBytes > 0 && len(bodyData) > maxBytes {
		bodyData = bodyData[:maxBytes]
	}

	// Push body as string
	cdata := C.CBytes(bodyData)
	C.lua_pushlstring(L, (*C.char)(cdata), C.size_t(len(bodyData)))
	C.free(cdata)
	return 1
}

//export golapis_req_get_post_args
func golapis_req_get_post_args(L *C.lua_State) C.int {
	// Get optional max_args argument (default 100, like nginx)
	// 0 means unlimited
	maxArgs := 100
	if C.lua_gettop(L) >= 1 && C.lua_isnumber(L, 1) != 0 {
		maxArgs = int(C.lua_tonumber(L, 1))
	}

	// Get current thread's HTTP request
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		// Not in HTTP context - return nil
		C.lua_pushnil(L)
		return 1
	}

	// Check if body was read
	if !thread.request.BodyWasRead() {
		C.lua_pushnil(L)
		pushGoString(L, "request body not read")
		return 2
	}

	// Get the cached body
	bodyData := thread.request.GetBody()
	if bodyData == nil {
		// Empty body - return empty table
		C.lua_newtable_wrapper(L)
		return 1
	}

	// Parse body as application/x-www-form-urlencoded using existing parser
	postArgs, truncated := parseQueryString(string(bodyData), maxArgs)

	pushQueryArgsToLuaTable(L, postArgs)

	// Return table and optional "truncated" error
	if truncated {
		pushGoString(L, "truncated")
		return 2
	}
	return 1
}

//export golapis_debug_cancel_timers
func golapis_debug_cancel_timers(L *C.lua_State) C.int {
	gls := getLuaStateFromRegistry(L)
	if gls == nil {
		return 0
	}
	gls.CancelAllTimers() // premature cancel, callbacks fire
	return 0
}

//export golapis_debug_pending_timer_count
func golapis_debug_pending_timer_count(L *C.lua_State) C.int {
	gls := getLuaStateFromRegistry(L)
	if gls == nil {
		C.lua_pushinteger(L, 0)
		return 1
	}
	gls.timerMu.Lock()
	count := len(gls.pendingTimers)
	gls.timerMu.Unlock()
	C.lua_pushinteger(L, C.lua_Integer(count))
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
	if err := gls.runBootstrap(); err != nil {
		panic(fmt.Sprintf("failed to run bootstrap: %v", err))
	}
}

// runBootstrap executes the embedded bootstrap.lua with the golapis table as argument
func (gls *GolapisLuaState) runBootstrap() error {
	ccode := C.CString(luaBootstrap)
	defer C.free(unsafe.Pointer(ccode))

	if C.luaL_loadstring(gls.luaState, ccode) != 0 {
		errMsg := C.GoString(C.lua_tostring_wrapper(gls.luaState, -1))
		C.lua_pop_wrapper(gls.luaState, 1)
		return errors.New(errMsg)
	}

	// Push golapis table from registry as argument
	C.lua_rawgeti_wrapper(gls.luaState, C.LUA_REGISTRYINDEX, gls.golapisRef)

	// Call bootstrap with 1 arg (golapis), 0 returns
	if C.lua_pcall(gls.luaState, 1, 0, 0) != 0 {
		errMsg := C.GoString(C.lua_tostring_wrapper(gls.luaState, -1))
		C.lua_pop_wrapper(gls.luaState, 1)
		return errors.New(errMsg)
	}

	return nil
}

// pushGoString pushes a Go string to the Lua stack without allocating a C string.
// Uses lua_pushlstring with direct pointer access for better performance (~70ns savings).
func pushGoString(L *C.lua_State, s string) {
	if len(s) == 0 {
		C.lua_pushlstring(L, nil, 0)
		return
	}
	C.lua_pushlstring(L, (*C.char)(unsafe.Pointer(unsafe.StringData(s))), C.size_t(len(s)))
}

// luaStringBytes returns a copy of the Lua string at idx, preserving embedded NULs.
func luaStringBytes(L *C.lua_State, idx C.int) []byte {
	var strLen C.size_t
	cstr := C.lua_tolstring_wrapper(L, idx, &strLen)
	if cstr == nil {
		return nil
	}
	return C.GoBytes(unsafe.Pointer(cstr), C.int(strLen))
}

//export golapis_var_index
func golapis_var_index(L *C.lua_State) C.int {
	// Stack: [var_table, key]
	// When __index metamethod is called with a function, Lua passes:
	//   arg 1: the table being indexed
	//   arg 2: the key

	if C.lua_isstring(L, 2) == 0 {
		C.lua_pushnil(L)
		return 1
	}

	key := C.GoString(C.lua_tostring_wrapper(L, 2))

	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		errMsg := C.CString("golapis.var can only be used in HTTP request context")
		defer C.free(unsafe.Pointer(errMsg))
		C.lua_pushstring(L, errMsg)
		return -1
	}

	value := resolveVar(thread.request, key)
	if value == nil {
		C.lua_pushnil(L)
	} else {
		pushGoString(L, *value)
	}
	return 1
}

// resolveVar resolves an nginx-style variable name to its value from the HTTP request.
// Returns nil if the variable is not set or not applicable.
func resolveVar(req *GolapisRequest, key string) *string {
	var result string
	httpReq := req.Request

	switch key {
	case "request_method":
		result = httpReq.Method

	case "request_uri":
		result = httpReq.URL.RequestURI()

	case "request_body":
		if !req.BodyWasRead() {
			return nil
		}
		bodyData := req.GetBody()
		if bodyData == nil {
			return nil
		}
		result = string(bodyData)

	case "scheme":
		if httpReq.TLS != nil {
			result = "https"
		} else {
			result = "http"
		}

	case "server_port":
		_, port, err := net.SplitHostPort(httpReq.Host)
		if err != nil {
			if httpReq.TLS != nil {
				result = "443"
			} else {
				result = "80"
			}
		} else {
			result = port
		}

	case "server_addr":
		// Server address requires additional plumbing from http.Server
		return nil

	case "remote_addr":
		host, _, err := net.SplitHostPort(httpReq.RemoteAddr)
		if err != nil {
			result = httpReq.RemoteAddr
		} else {
			result = host
		}

	case "host":
		host, _, err := net.SplitHostPort(httpReq.Host)
		if err != nil {
			result = httpReq.Host
		} else {
			result = host
		}

	case "args":
		if httpReq.URL.RawQuery == "" {
			return nil
		}
		result = httpReq.URL.RawQuery

	default:
		// Check for http_* pattern (header access)
		if strings.HasPrefix(key, "http_") {
			headerName := http.CanonicalHeaderKey(strings.ReplaceAll(key[5:], "_", "-"))
			// Go's http.Request moves Host header to req.Host, not req.Header
			if headerName == "Host" {
				if httpReq.Host == "" {
					return nil
				}
				result = httpReq.Host
			} else {
				headerValue := httpReq.Header.Get(headerName)
				if headerValue == "" {
					return nil
				}
				result = headerValue
			}
		} else {
			return nil
		}
	}

	return &result
}

// normalizeHeaderName converts underscore to hyphen and canonicalizes the header name.
// This matches ngx.header behavior where content_type becomes Content-Type.
func normalizeHeaderName(key string) string {
	return http.CanonicalHeaderKey(strings.ReplaceAll(key, "_", "-"))
}

// singleValueHeaders lists headers that should only have a single value.
// When setting these headers with a table, only the last value is used.
var singleValueHeaders = map[string]bool{
	"Content-Type":                true,
	"Content-Length":              true,
	"Content-Encoding":            true,
	"Content-Language":            true,
	"Content-Location":            true,
	"Content-Md5":                 true,
	"Content-Range":               true,
	"Location":                    true,
	"Last-Modified":               true,
	"Etag":                        true,
	"Accept-Ranges":               true,
	"Age":                         true,
	"Server":                      true,
	"Retry-After":                 true,
	"Www-Authenticate":            true,
	"Proxy-Authenticate":          true,
	"Transfer-Encoding":           true,
	"Content-Disposition":         true,
	"Access-Control-Max-Age":      true,
	"Access-Control-Allow-Origin": true,
}

//export golapis_header_newindex
func golapis_header_newindex(L *C.lua_State) C.int {
	// Stack: [header_table, key, value]
	// When __newindex metamethod is called, Lua passes:
	//   arg 1: the table being indexed
	//   arg 2: the key
	//   arg 3: the value being assigned
	//
	// Returns -1 with error message on stack to signal error to C wrapper

	// Validate key is a string
	if C.lua_isstring(L, 2) == 0 {
		errMsg := C.CString("header name must be a string")
		defer C.free(unsafe.Pointer(errMsg))
		C.lua_pushstring(L, errMsg)
		return -1
	}

	key := C.GoString(C.lua_tostring_wrapper(L, 2))
	headerName := normalizeHeaderName(key)

	// Get thread context
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		errMsg := C.CString("golapis.header can only be used in HTTP request context")
		defer C.free(unsafe.Pointer(errMsg))
		C.lua_pushstring(L, errMsg)
		return -1
	}

	// Check if headers have already been sent
	if thread.request.HeadersSent {
		errMsg := C.CString(fmt.Sprintf("attempt to set header '%s' after headers have been sent", headerName))
		defer C.free(unsafe.Pointer(errMsg))
		C.lua_pushstring(L, errMsg)
		return -1
	}

	valueType := C.lua_type(L, 3)

	switch valueType {
	case C.LUA_TNIL:
		// nil clears the header
		thread.request.ResponseHeaders.Del(headerName)

	case C.LUA_TSTRING:
		// Single string value
		value := C.GoString(C.lua_tostring_wrapper(L, 3))
		thread.request.ResponseHeaders.Set(headerName, value)

	case C.LUA_TTABLE:
		// Table value - can be multiple values or empty (to clear)
		// First, check if it's empty
		C.lua_pushnil(L)
		if C.lua_next_wrapper(L, 3) == 0 {
			// Empty table - clear the header
			thread.request.ResponseHeaders.Del(headerName)
		} else {
			// Non-empty table - pop the key/value we just pushed
			C.lua_pop_wrapper(L, 2)

			// Clear existing values
			thread.request.ResponseHeaders.Del(headerName)

			// Get max key to iterate properly
			maxKey, err := validateArrayTable(L, 3)
			if err != nil {
				errMsg := C.CString("header value table must be an array")
				defer C.free(unsafe.Pointer(errMsg))
				C.lua_pushstring(L, errMsg)
				return -1
			}

			// For single-value headers, only use the last value
			if singleValueHeaders[headerName] {
				// Get the last value
				C.lua_rawgeti_wrapper(L, 3, C.int(maxKey))
				if C.lua_isstring(L, -1) != 0 {
					value := C.GoString(C.lua_tostring_wrapper(L, -1))
					thread.request.ResponseHeaders.Set(headerName, value)
				}
				C.lua_pop_wrapper(L, 1)
			} else {
				// Multi-value header - add all values
				for i := 1; i <= maxKey; i++ {
					C.lua_rawgeti_wrapper(L, 3, C.int(i))
					if C.lua_isstring(L, -1) != 0 {
						value := C.GoString(C.lua_tostring_wrapper(L, -1))
						thread.request.ResponseHeaders.Add(headerName, value)
					}
					C.lua_pop_wrapper(L, 1)
				}
			}
		}

	default:
		// Convert other types to string
		typeName := C.GoString(C.lua_typename(L, valueType))
		errMsg := C.CString(fmt.Sprintf("header value must be a string, table, or nil (got %s)", typeName))
		defer C.free(unsafe.Pointer(errMsg))
		C.lua_pushstring(L, errMsg)
		return -1
	}

	return 0
}

//export golapis_header_index
func golapis_header_index(L *C.lua_State) C.int {
	// Stack: [header_table, key]
	// When __index metamethod is called, Lua passes:
	//   arg 1: the table being indexed
	//   arg 2: the key

	// Validate key is a string
	if C.lua_isstring(L, 2) == 0 {
		C.lua_pushnil(L)
		return 1
	}

	key := C.GoString(C.lua_tostring_wrapper(L, 2))
	headerName := normalizeHeaderName(key)

	// Get thread context
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		errMsg := C.CString("golapis.header can only be used in HTTP request context")
		defer C.free(unsafe.Pointer(errMsg))
		C.lua_pushstring(L, errMsg)
		return -1
	}

	values := thread.request.ResponseHeaders[headerName]
	if len(values) == 0 {
		C.lua_pushnil(L)
		return 1
	}

	if len(values) == 1 {
		// Single value - return as string
		pushGoString(L, values[0])
		return 1
	}

	// Multiple values - return as table
	C.lua_newtable_wrapper(L)
	for i, v := range values {
		C.lua_pushinteger(L, C.lua_Integer(i+1))
		pushGoString(L, v)
		C.lua_settable(L, -3)
	}
	return 1
}

//export golapis_status_get
func golapis_status_get(L *C.lua_State) C.int {
	// Stack: [golapis_table, "status"]
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		// Not in HTTP context - return 0 (ngx behavior)
		C.lua_pushinteger(L, 0)
		return 1
	}

	C.lua_pushinteger(L, C.lua_Integer(thread.request.ResponseStatus))
	return 1
}

//export golapis_status_set
func golapis_status_set(L *C.lua_State) C.int {
	// Stack: [golapis_table, "status", value]
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.request == nil {
		pushGoString(L, "golapis.status can only be set in HTTP request context")
		return -1
	}

	// Check if headers already sent
	if thread.request.HeadersSent {
		pushGoString(L, "attempt to set status after response headers have been sent")
		return -1
	}

	// Get value from stack position 3
	var status int

	switch C.lua_type(L, 3) {
	case C.LUA_TNUMBER:
		status = int(C.lua_tonumber(L, 3))
	case C.LUA_TSTRING:
		// Auto-convert string to number
		statusStr := C.GoString(C.lua_tostring_wrapper(L, 3))
		var err error
		status, err = strconv.Atoi(statusStr)
		if err != nil {
			pushGoString(L, fmt.Sprintf("invalid status code: %s", statusStr))
			return -1
		}
	default:
		typeName := C.GoString(C.lua_typename(L, C.lua_type(L, 3)))
		pushGoString(L, fmt.Sprintf("status must be a number or string (got %s)", typeName))
		return -1
	}

	// Validate range (RFC 7230: 3-digit integer)
	if status < 100 || status > 999 {
		pushGoString(L, fmt.Sprintf("invalid status code: %d (must be 100-999)", status))
		return -1
	}

	thread.request.ResponseStatus = status
	return 0
}
