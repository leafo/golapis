#ifndef LUA_HELPERS_H
#define LUA_HELPERS_H

#include "../luajit/src/lua.h"
#include "../luajit/src/lauxlib.h"
#include "../luajit/src/lualib.h"
#include <stdlib.h>

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

static int luaL_error_wrapper(lua_State *L, const char *msg) {
    return luaL_error(L, "%s", msg);
}

#endif
