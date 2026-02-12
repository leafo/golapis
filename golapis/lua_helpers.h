#ifndef LUA_HELPERS_H
#define LUA_HELPERS_H

#include "../luajit/src/lua.h"
#include "../luajit/src/lauxlib.h"
#include "../luajit/src/lualib.h"
#include <stdlib.h>
#include <stdint.h>
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

static void lua_setglobal_wrapper(lua_State *L, const char *name) {
    lua_setglobal(L, name);
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

static int lua_isnil_wrapper(lua_State *L, int idx) {
    return lua_isnil(L, idx);
}

static int lua_istable_wrapper(lua_State *L, int idx) {
    return lua_istable(L, idx);
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

// Batch push bytecode interpreter
// Executes a sequence of encoded Lua stack operations from a byte buffer.
// STR/SETF opcodes embed raw pointers to Go string data directly in the buffer.
// STRI/SETFI opcodes embed string bytes inline in the buffer.
// Returns 0 on success, -1 on error.
#define BATCH_OP_NIL    0x01
#define BATCH_OP_TRUE   0x02
#define BATCH_OP_FALSE  0x03
#define BATCH_OP_INT    0x04
#define BATCH_OP_NUM    0x05
#define BATCH_OP_STR    0x06
#define BATCH_OP_STRI   0x07
#define BATCH_OP_TABLE  0x08
#define BATCH_OP_TABLEA 0x09
#define BATCH_OP_SET    0x0A
#define BATCH_OP_SETF   0x0B
#define BATCH_OP_SETFI  0x0C
#define BATCH_OP_SETI   0x0D
#define BATCH_OP_POP    0x0E

static inline uint32_t read_u32(const unsigned char *p) {
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8) |
           ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}

static inline uint64_t read_u64(const unsigned char *p) {
    return (uint64_t)p[0] | ((uint64_t)p[1] << 8) |
           ((uint64_t)p[2] << 16) | ((uint64_t)p[3] << 24) |
           ((uint64_t)p[4] << 32) | ((uint64_t)p[5] << 40) |
           ((uint64_t)p[6] << 48) | ((uint64_t)p[7] << 56);
}

static inline int64_t read_i64(const unsigned char *p) {
    return (int64_t)read_u64(p);
}

static inline double read_f64(const unsigned char *p) {
    double d;
    memcpy(&d, p, 8);
    return d;
}

static int lua_batch_push(lua_State *L,
                          const unsigned char *instr, size_t instr_len) {
    size_t pos = 0;
    while (pos < instr_len) {
        unsigned char op = instr[pos++];
        switch (op) {
        case BATCH_OP_NIL:
            lua_pushnil(L);
            break;
        case BATCH_OP_TRUE:
            lua_pushboolean(L, 1);
            break;
        case BATCH_OP_FALSE:
            lua_pushboolean(L, 0);
            break;
        case BATCH_OP_INT:
            if (pos + 8 > instr_len) return -1;
            lua_pushinteger(L, (lua_Integer)read_i64(instr + pos));
            pos += 8;
            break;
        case BATCH_OP_NUM:
            if (pos + 8 > instr_len) return -1;
            lua_pushnumber(L, (lua_Number)read_f64(instr + pos));
            pos += 8;
            break;
        case BATCH_OP_STR: {
            // Embedded pointer: u64 ptr + u32 len
            if (pos + 12 > instr_len) return -1;
            const char *ptr = (const char *)(uintptr_t)read_u64(instr + pos);
            uint32_t slen = read_u32(instr + pos + 8);
            pos += 12;
            lua_pushlstring(L, ptr, slen);
            break;
        }
        case BATCH_OP_STRI: {
            if (pos + 4 > instr_len) return -1;
            uint32_t slen = read_u32(instr + pos);
            pos += 4;
            if (pos + slen > instr_len) return -1;
            lua_pushlstring(L, (const char *)(instr + pos), slen);
            pos += slen;
            break;
        }
        case BATCH_OP_TABLE:
            lua_newtable(L);
            break;
        case BATCH_OP_TABLEA: {
            if (pos + 8 > instr_len) return -1;
            uint32_t narr = read_u32(instr + pos);
            uint32_t nrec = read_u32(instr + pos + 4);
            pos += 8;
            lua_createtable(L, (int)narr, (int)nrec);
            break;
        }
        case BATCH_OP_SET:
            lua_settable(L, -3);
            break;
        case BATCH_OP_SETF: {
            // Embedded pointer: u64 ptr + u32 len
            if (pos + 12 > instr_len) return -1;
            const char *ptr = (const char *)(uintptr_t)read_u64(instr + pos);
            uint32_t slen = read_u32(instr + pos + 8);
            pos += 12;
            lua_pushlstring(L, ptr, slen);
            lua_insert(L, -2);
            lua_settable(L, -3);
            break;
        }
        case BATCH_OP_SETFI: {
            if (pos + 4 > instr_len) return -1;
            uint32_t slen = read_u32(instr + pos);
            pos += 4;
            if (pos + slen > instr_len) return -1;
            lua_pushlstring(L, (const char *)(instr + pos), slen);
            pos += slen;
            lua_insert(L, -2);
            lua_settable(L, -3);
            break;
        }
        case BATCH_OP_SETI: {
            if (pos + 4 > instr_len) return -1;
            uint32_t idx = read_u32(instr + pos);
            pos += 4;
            lua_rawseti(L, -2, (int)idx);
            break;
        }
        case BATCH_OP_POP: {
            if (pos + 1 > instr_len) return -1;
            unsigned char count = instr[pos++];
            lua_pop(L, (int)count);
            break;
        }
        default:
            return -1; // unknown opcode
        }
    }
    return 0;
}

// Wrapper for lua_gc (memory management and garbage collection)
static int lua_gc_wrapper(lua_State *L, int what, int data) {
    return lua_gc(L, what, data);
}

// Wrapper for lua_pushthread (returns 1 if main thread)
static int lua_pushthread_wrapper(lua_State *L) {
    return lua_pushthread(L);
}

// Wrapper for lua_tothread (macro in Lua)
static lua_State* lua_tothread_wrapper(lua_State *L, int idx) {
    return lua_tothread(L, idx);
}

#endif
