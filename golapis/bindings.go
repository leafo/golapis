package golapis

/*
#include "lua_helpers.h"

// Forward declaration for Go functions
extern int golapis_sleep(lua_State *L);
extern int golapis_http_request(lua_State *L);
extern int golapis_print(lua_State *L);
extern int golapis_req_get_uri_args(lua_State *L);
extern int golapis_timer_at(lua_State *L);
extern int golapis_debug_cancel_timers(lua_State *L);
extern int golapis_debug_pending_timer_count(lua_State *L);

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

static int c_timer_at_wrapper(lua_State *L) {
    return golapis_timer_at(L);
}

static int c_debug_cancel_timers_wrapper(lua_State *L) {
    return golapis_debug_cancel_timers(L);
}

static int c_debug_pending_timer_count_wrapper(lua_State *L) {
    return golapis_debug_pending_timer_count(L);
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
	if thread == nil || thread.httpRequest == nil {
		// Not in HTTP context - return nil (nginx-lua compatible behavior)
		C.lua_pushnil(L)
		return 1
	}

	// Parse query parameters with nginx-lua compatible behavior
	queryArgs := parseQueryString(thread.httpRequest.URL.RawQuery)

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
