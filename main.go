package main

/*
#cgo CFLAGS: -I./luajit/src
#cgo LDFLAGS: -L./luajit/src -l:libluajit.a -lm -ldl

#include "luajit/src/lua.h"
#include "luajit/src/lauxlib.h"
#include "luajit/src/lualib.h"
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
*/
import "C"
import (
	"fmt"
	"os"
	"unsafe"
)

type LuaState struct {
	state *C.lua_State
}

func NewLuaState() *LuaState {
	L := C.new_lua_state()
	if L == nil {
		return nil
	}
	return &LuaState{state: L}
}

func (ls *LuaState) Close() {
	if ls.state != nil {
		C.lua_close(ls.state)
		ls.state = nil
	}
}

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

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <lua_file>\n", os.Args[0])
		os.Exit(1)
	}

	lua := NewLuaState()
	if lua == nil {
		fmt.Println("Failed to create Lua state")
		os.Exit(1)
	}
	defer lua.Close()

	filename := os.Args[1]
	if err := lua.RunFile(filename); err != nil {
		fmt.Printf("Error running Lua file: %v\n", err)
		os.Exit(1)
	}
}