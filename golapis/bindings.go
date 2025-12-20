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
extern int golapis_timer_at(lua_State *L);
extern int golapis_debug_cancel_timers(lua_State *L);
extern int golapis_debug_pending_timer_count(lua_State *L);
extern int golapis_var_index(lua_State *L);
extern int golapis_header_index(lua_State *L);
extern int golapis_header_newindex(lua_State *L);

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

static int setup_golapis_global(lua_State *L) {
    lua_newtable(L);                    // Create new table `golapis`

    lua_pushstring(L, "1.0.0");
    lua_setfield(L, -2, "version");

    lua_pushcfunction(L, c_sleep_wrapper);
    lua_setfield(L, -2, "sleep");

    lua_pushcfunction(L, c_print_wrapper);
    lua_setfield(L, -2, "print");

    lua_pushcfunction(L, c_say_wrapper);
    lua_setfield(L, -2, "say");

    // Register golapis.null as lightuserdata(NULL)
    lua_pushlightuserdata(L, NULL);
    lua_setfield(L, -2, "null");

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

    // Initialize cached metatables
    init_headers_metatable(L);

    lua_pushvalue(L, -1);               // Duplicate table for registry ref
    int ref = luaL_ref(L, LUA_REGISTRYINDEX);

    lua_setglobal(L, "golapis");        // Set global golapis = table
    return ref;
}

*/
import "C"
import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
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

	// Use bytes.Buffer for accumulation
	buf := &bytes.Buffer{}

	// Process each argument
	nargs := int(C.lua_gettop(L))
	for i := 1; i <= nargs; i++ {
		err := coerceValueToBytes(L, C.int(i), buf, 0, i, funcName)
		if err != nil {
			C.lua_pushnil(L)
			pushCString(L, err.Error())
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
			pushCString(L, "nginx output filter error")
			return 2
		}
		if n <= 0 {
			C.lua_pushnil(L)
			pushCString(L, "nginx output filter error")
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
		pushCString(L, "expecting at least 2 arguments (delay, callback)")
		return 2
	}

	// Validate delay is a number
	if C.lua_isnumber(L, 1) == 0 {
		C.lua_pushnil(L)
		pushCString(L, "delay must be a number")
		return 2
	}

	// Validate callback is a function (and not a C function)
	if C.lua_isfunction_wrapper(L, 2) == 0 {
		C.lua_pushnil(L)
		pushCString(L, "callback must be a function")
		return 2
	}

	delay := float64(C.lua_tonumber(L, 1))
	if delay < 0 {
		C.lua_pushnil(L)
		pushCString(L, "delay must be >= 0")
		return 2
	}

	// Get the GolapisLuaState (we need the main Lua state, not the coroutine)
	gls := getLuaStateFromRegistry(L)
	if gls == nil {
		C.lua_pushnil(L)
		pushCString(L, "timer.at: could not find golapis state")
		return 2
	}

	mainL := gls.luaState

	// Create a new coroutine on the main state
	co := C.lua_newthread(mainL)
	if co == nil {
		C.lua_pushnil(L)
		pushCString(L, "failed to create timer coroutine")
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
	queryArgs := parseQueryString(thread.request.Request.URL.RawQuery)

	// Create result table
	C.lua_newtable_wrapper(L)

	count := 0
	for key, args := range queryArgs {
		if count >= max {
			break
		}
		count++

		if len(args) == 1 {
			// Single value
			pushCString(L, key)
			if args[0].isBoolean {
				C.lua_pushboolean(L, 1) // true
			} else {
				pushCString(L, args[0].value)
			}
			C.lua_settable(L, -3)
		} else {
			// Multiple values: {key = {val1, val2, ...}}
			pushCString(L, key)
			C.lua_newtable_wrapper(L)
			for i, arg := range args {
				C.lua_pushinteger(L, C.lua_Integer(i+1))
				if arg.isBoolean {
					C.lua_pushboolean(L, 1) // true
				} else {
					pushCString(L, arg.value)
				}
				C.lua_settable(L, -3)
			}
			C.lua_settable(L, -3)
		}
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
			pushCString(L, key)
			// Single value - push as string
			pushCString(L, values[0])
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
			pushCString(L, key)
			// Multiple values - push as array table
			C.lua_newtable_wrapper(L)
			for i, v := range values {
				C.lua_pushinteger(L, C.lua_Integer(i+1))
				pushCString(L, v)
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
		pushCString(L, hostKey)
		pushCString(L, req.Host)
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
		pushCString(L, "truncated")
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
	pushCString(L, normalized)
	C.lua_rawget_wrapper(L, 1)

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
}

func pushCString(L *C.lua_State, s string) {
	cstr := C.CString(s)
	C.lua_pushstring(L, cstr)
	C.free(unsafe.Pointer(cstr))
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

	value := resolveVar(thread.request.Request, key)
	if value == nil {
		C.lua_pushnil(L)
	} else {
		pushCString(L, *value)
	}
	return 1
}

// resolveVar resolves an nginx-style variable name to its value from the HTTP request.
// Returns nil if the variable is not set or not applicable.
func resolveVar(req *http.Request, key string) *string {
	var result string

	switch key {
	case "request_method":
		result = req.Method

	case "request_uri":
		result = req.URL.RequestURI()

	case "scheme":
		if req.TLS != nil {
			result = "https"
		} else {
			result = "http"
		}

	case "server_port":
		_, port, err := net.SplitHostPort(req.Host)
		if err != nil {
			if req.TLS != nil {
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
		host, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			result = req.RemoteAddr
		} else {
			result = host
		}

	case "host":
		host, _, err := net.SplitHostPort(req.Host)
		if err != nil {
			result = req.Host
		} else {
			result = host
		}

	case "args":
		if req.URL.RawQuery == "" {
			return nil
		}
		result = req.URL.RawQuery

	default:
		// Check for http_* pattern (header access)
		if strings.HasPrefix(key, "http_") {
			headerName := http.CanonicalHeaderKey(strings.ReplaceAll(key[5:], "_", "-"))
			// Go's http.Request moves Host header to req.Host, not req.Header
			if headerName == "Host" {
				if req.Host == "" {
					return nil
				}
				result = req.Host
			} else {
				headerValue := req.Header.Get(headerName)
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
		pushCString(L, values[0])
		return 1
	}

	// Multiple values - return as table
	C.lua_newtable_wrapper(L)
	for i, v := range values {
		C.lua_pushinteger(L, C.lua_Integer(i+1))
		pushCString(L, v)
		C.lua_settable(L, -3)
	}
	return 1
}
