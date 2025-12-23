#ifndef LUA_HELPERS_H
#define LUA_HELPERS_H

#include "../luajit/src/lua.h"
#include "../luajit/src/lauxlib.h"
#include "../luajit/src/lualib.h"
#include <stdlib.h>
#include <string.h>

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

static void lua_insert_wrapper(lua_State *L, int idx) {
    lua_insert(L, idx);
}

static int lua_isfunction_wrapper(lua_State *L, int idx) {
    return lua_isfunction(L, idx);
}

static int lua_isboolean_wrapper(lua_State *L, int idx) {
    return lua_isboolean(L, idx);
}

static int lua_toboolean_wrapper(lua_State *L, int idx) {
    return lua_toboolean(L, idx);
}

// For table iteration
static int lua_next_wrapper(lua_State *L, int idx) {
    return lua_next(L, idx);
}

// For getting string with length (handles embedded NULs)
static const char* lua_tolstring_wrapper(lua_State *L, int idx, size_t *len) {
    return lua_tolstring(L, idx, len);
}

// For checking lightuserdata value
static void* lua_touserdata_wrapper(lua_State *L, int idx) {
    return lua_touserdata(L, idx);
}

// Apply the cached headers metatable to the table at top of stack
// The metatable must have been created during initialization with init_headers_metatable
static void setup_headers_metatable(lua_State *L) {
    luaL_getmetatable(L, "golapis.req.headers");  // Push cached metatable from registry
    lua_setmetatable(L, -2);                      // setmetatable(headers_table, metatable)
}

// For raw table access (bypasses metamethods)
static void lua_rawget_wrapper(lua_State *L, int idx) {
    lua_rawget(L, idx);
}

// Get metatable from registry by name
static void luaL_getmetatable_wrapper(lua_State *L, const char *name) {
    luaL_getmetatable(L, name);
}

// Wrapper for luaL_error since CGo can't call variadic functions
static int luaL_error_str(lua_State *L, const char *msg) {
    return luaL_error(L, "%s", msg);
}

// Get traceback from a coroutine error
// L is the main state, co is the coroutine with error message on top of stack
// Pushes traceback string onto L's stack (caller must pop)
static void lua_push_traceback(lua_State *L, lua_State *co) {
    const char *msg = lua_tostring(co, -1);
    lua_getglobal(L, "debug");
    if (!lua_istable(L, -1)) {
        lua_pop(L, 1);
        lua_pushstring(L, msg ? msg : "");
        return;
    }
    lua_getfield(L, -1, "traceback");
    if (!lua_isfunction(L, -1)) {
        lua_pop(L, 2);
        lua_pushstring(L, msg ? msg : "");
        return;
    }
    lua_pushthread(co);
    lua_xmove(co, L, 1);        // move coroutine to main state
    lua_pushstring(L, msg);
    lua_pushinteger(L, 0);      // level
    if (lua_pcall(L, 3, 1, 0) != LUA_OK) {
        lua_pop(L, 1);
        lua_pushstring(L, msg ? msg : "");
    }
    lua_remove(L, -2);          // remove debug table, keep traceback
}

// Wrapper for lua_gc (memory management and garbage collection)
static int lua_gc_wrapper(lua_State *L, int what, int data) {
    return lua_gc(L, what, data);
}

#endif
