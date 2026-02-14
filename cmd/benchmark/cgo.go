package main

/*
#cgo CFLAGS: -I../../luajit/src
#cgo LDFLAGS: -L../../luajit/src -l:libluajit.a -lm -ldl -rdynamic

#include "../../luajit/src/lua.h"
#include "../../luajit/src/lauxlib.h"
#include "../../luajit/src/lualib.h"
#include "../../luajit/src/luajit.h"
#include <stdlib.h>
#include <string.h>

// =============================================================================
// PURE C FUNCTIONS (no Go involvement)
// =============================================================================

// Simple C function that just adds two numbers (baseline for FFI comparison)
static int c_add_numbers(int a, int b) {
    return a + b;
}

// C function that does string work
static int c_string_length(const char* s) {
    return (int)strlen(s);
}

// C function exposed to Lua via C API
static int lua_c_add_numbers(lua_State *L) {
    int a = (int)lua_tointeger(L, 1);
    int b = (int)lua_tointeger(L, 2);
    lua_pushinteger(L, a + b);
    return 1;
}

// C function that does a small amount of work
static int lua_c_increment_counter(lua_State *L) {
    int val = (int)lua_tointeger(L, 1);
    lua_pushinteger(L, val + 1);
    return 1;
}

// Noop C function (measures pure call overhead)
static int lua_c_noop(lua_State *L) {
    (void)L;
    return 0;
}

// C function that pushes a string
static int lua_c_return_string(lua_State *L) {
    lua_pushstring(L, "hello from C");
    return 1;
}

// =============================================================================
// HELPER WRAPPERS (for CGO)
// =============================================================================

static lua_State* new_state() {
    lua_State *L = luaL_newstate();
    if (L) {
        luaL_openlibs(L);
    }
    return L;
}

static void close_state(lua_State *L) {
    lua_close(L);
}

static void register_c_functions(lua_State *L) {
    lua_pushcfunction(L, lua_c_add_numbers);
    lua_setglobal(L, "c_add");

    lua_pushcfunction(L, lua_c_increment_counter);
    lua_setglobal(L, "c_increment");

    lua_pushcfunction(L, lua_c_noop);
    lua_setglobal(L, "c_noop");

    lua_pushcfunction(L, lua_c_return_string);
    lua_setglobal(L, "c_return_string");
}

static int do_string(lua_State *L, const char* code) {
    return luaL_dostring(L, code);
}

static void push_integer(lua_State *L, int val) {
    lua_pushinteger(L, val);
}

static void push_string(lua_State *L, const char* s) {
    lua_pushstring(L, s);
}

static void push_lstring(lua_State *L, const char* s, size_t len) {
    lua_pushlstring(L, s, len);
}

static void push_boolean(lua_State *L, int val) {
    lua_pushboolean(L, val);
}

static int to_integer(lua_State *L, int idx) {
    return (int)lua_tointeger(L, idx);
}

static const char* to_string(lua_State *L, int idx) {
    return lua_tostring(L, idx);
}

static void pop(lua_State *L, int n) {
    lua_pop(L, n);
}

static void get_global(lua_State *L, const char* name) {
    lua_getglobal(L, name);
}

static void set_global(lua_State *L, const char* name) {
    lua_setglobal(L, name);
}

static int pcall(lua_State *L, int nargs, int nresults) {
    return lua_pcall(L, nargs, nresults, 0);
}

static void new_table(lua_State *L) {
    lua_newtable(L);
}

static void set_table_int(lua_State *L, int idx, int key, int val) {
    lua_pushinteger(L, key);
    lua_pushinteger(L, val);
    lua_settable(L, idx < 0 ? idx - 2 : idx);
}

static void set_table(lua_State *L) {
    lua_settable(L, -3);
}

static void set_field(lua_State *L, const char* name) {
    lua_setfield(L, -2, name);
}

static int get_top(lua_State *L) {
    return lua_gettop(L);
}

// For pure CGO overhead measurement
static int c_baseline_add(int a, int b) {
    return a + b;
}

// Batch operation in C (no CGO crossings inside loop)
static long c_sum_loop(int count) {
    long sum = 0;
    for (int i = 0; i < count; i++) {
        sum += i;
    }
    return sum;
}

// Load a Lua chunk and store it in a global variable for repeated calling
// Returns 0 on success, non-zero on error
static int load_chunk(lua_State *L, const char* code, const char* name) {
    int result = luaL_loadstring(L, code);
    if (result != 0) {
        return result;
    }
    lua_setglobal(L, name);
    return 0;
}

// Call a preloaded chunk by name
// Returns 0 on success, non-zero on error
static int call_chunk(lua_State *L, const char* name) {
    lua_getglobal(L, name);
    int result = lua_pcall(L, 0, 0, 0);
    return result;
}

// Call a preloaded chunk by name and return one integer result
// Returns the result, or 0 on error
static int call_chunk_int(lua_State *L, const char* name) {
    lua_getglobal(L, name);
    if (lua_pcall(L, 0, 1, 0) != 0) {
        lua_pop(L, 1);
        return 0;
    }
    int result = (int)lua_tointeger(L, -1);
    lua_pop(L, 1);
    return result;
}

// Flush JIT to avoid memory accumulation during benchmarks
static void flush_jit(lua_State *L) {
    lua_getglobal(L, "jit");
    if (lua_istable(L, -1)) {
        lua_getfield(L, -1, "flush");
        if (lua_isfunction(L, -1)) {
            lua_pcall(L, 0, 0, 0);
        } else {
            lua_pop(L, 1);
        }
    }
    lua_pop(L, 1);
}
*/
import "C"
import "unsafe"

// LuaState wraps a C lua_State pointer
type LuaState struct {
	L *C.lua_State
}

// NewState creates a new Lua state
func NewState() *LuaState {
	return &LuaState{L: C.new_state()}
}

// NewStateNoJIT creates a new Lua state with JIT disabled (for stable benchmarking)
func NewStateNoJIT() *LuaState {
	L := &LuaState{L: C.new_state()}
	// Disable JIT to avoid segfaults during very high iteration counts
	L.DoString("jit.off()")
	return L
}

// Close closes the Lua state
func (s *LuaState) Close() {
	C.close_state(s.L)
}

// RegisterCFunctions registers the benchmark C functions
func (s *LuaState) RegisterCFunctions() {
	C.register_c_functions(s.L)
}

// DoString executes a Lua string
func (s *LuaState) DoString(code string) int {
	ccode := C.CString(code)
	defer C.free(unsafe.Pointer(ccode))
	return int(C.do_string(s.L, ccode))
}

// DoStringC executes a Lua string from a pre-allocated C string
func (s *LuaState) DoStringC(code *C.char) int {
	return int(C.do_string(s.L, code))
}

// PushInteger pushes an integer onto the stack
func (s *LuaState) PushInteger(val int) {
	C.push_integer(s.L, C.int(val))
}

// PushStringC pushes a C string onto the stack
func (s *LuaState) PushStringC(str *C.char) {
	C.push_string(s.L, str)
}

// PushGoString pushes a Go string onto the Lua stack (zero-copy via lua_pushlstring).
func (s *LuaState) PushGoString(str string) {
	if len(str) == 0 {
		C.push_lstring(s.L, nil, 0)
		return
	}
	C.push_lstring(s.L, (*C.char)(unsafe.Pointer(unsafe.StringData(str))), C.size_t(len(str)))
}

// PushBoolean pushes a boolean onto the stack
func (s *LuaState) PushBoolean(val bool) {
	if val {
		C.push_boolean(s.L, 1)
	} else {
		C.push_boolean(s.L, 0)
	}
}

// Pop pops n values from the stack
func (s *LuaState) Pop(n int) {
	C.pop(s.L, C.int(n))
}

// GetGlobalC gets a global using a C string name
func (s *LuaState) GetGlobalC(name *C.char) {
	C.get_global(s.L, name)
}

// SetGlobalC sets a global using a C string name
func (s *LuaState) SetGlobalC(name *C.char) {
	C.set_global(s.L, name)
}

// ToInteger gets an integer from the stack
func (s *LuaState) ToInteger(idx int) int {
	return int(C.to_integer(s.L, C.int(idx)))
}

// ToStringC gets a C string from the stack
func (s *LuaState) ToStringC(idx int) *C.char {
	return C.to_string(s.L, C.int(idx))
}

// PCall calls a function with error handling
func (s *LuaState) PCall(nargs, nresults int) int {
	return int(C.pcall(s.L, C.int(nargs), C.int(nresults)))
}

// NewTable creates a new table
func (s *LuaState) NewTable() {
	C.new_table(s.L)
}

// SetTableInt sets t[key] = val
func (s *LuaState) SetTableInt(idx, key, val int) {
	C.set_table_int(s.L, C.int(idx), C.int(key), C.int(val))
}

// SetTable pops key and value from the stack and sets them on the table at -3.
func (s *LuaState) SetTable() {
	C.set_table(s.L)
}

// SetField pops value from the stack and sets it as a named field on the table at -2.
func (s *LuaState) SetField(name string) {
	cname := C.CString(name)
	C.set_field(s.L, cname)
	C.free(unsafe.Pointer(cname))
}

// GetTop returns the stack top index
func (s *LuaState) GetTop() int {
	return int(C.get_top(s.L))
}

// LoadChunk loads a Lua chunk and stores it as a global function
func (s *LuaState) LoadChunk(code, name string) int {
	ccode := CString(code)
	defer FreeCString(ccode)
	cname := CString(name)
	defer FreeCString(cname)
	return int(C.load_chunk(s.L, ccode, cname))
}

// LoadChunkC loads a Lua chunk from C strings
func (s *LuaState) LoadChunkC(code, name *C.char) int {
	return int(C.load_chunk(s.L, code, name))
}

// CallChunk calls a preloaded chunk by name
func (s *LuaState) CallChunk(name string) int {
	cname := CString(name)
	defer FreeCString(cname)
	return int(C.call_chunk(s.L, cname))
}

// CallChunkC calls a preloaded chunk using a C string name
func (s *LuaState) CallChunkC(name *C.char) int {
	return int(C.call_chunk(s.L, name))
}

// CallChunkInt calls a preloaded chunk and returns an integer result
func (s *LuaState) CallChunkInt(name string) int {
	cname := CString(name)
	defer FreeCString(cname)
	return int(C.call_chunk_int(s.L, cname))
}

// CallChunkIntC calls a preloaded chunk using a C string name and returns int
func (s *LuaState) CallChunkIntC(name *C.char) int {
	return int(C.call_chunk_int(s.L, name))
}

// FlushJIT flushes the JIT cache
func (s *LuaState) FlushJIT() {
	C.flush_jit(s.L)
}

// =============================================================================
// C Function wrappers for benchmarking
// =============================================================================

// CBaselineAdd calls the C baseline add function
func CBaselineAdd(a, b int) int {
	return int(C.c_baseline_add(C.int(a), C.int(b)))
}

// CBaselineAddC calls with pre-converted C ints
func CBaselineAddC(a, b C.int) C.int {
	return C.c_baseline_add(a, b)
}

// CStringLength measures string length in C
func CStringLength(s *C.char) int {
	return int(C.c_string_length(s))
}

// CSumLoop does a sum loop entirely in C
func CSumLoop(count int) int64 {
	return int64(C.c_sum_loop(C.int(count)))
}

// =============================================================================
// Helper functions for benchmark setup
// =============================================================================

// CString creates a C string (caller must free)
func CString(s string) *C.char {
	return C.CString(s)
}

// FreeCString frees a C string
func FreeCString(s *C.char) {
	C.free(unsafe.Pointer(s))
}

// GoString converts C string to Go string
func GoString(s *C.char) string {
	return C.GoString(s)
}
