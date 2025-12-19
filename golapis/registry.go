package golapis

/*
#include "lua_helpers.h"
*/
import "C"

// luaStateMap maps main lua_State pointers to GolapisLuaState objects
var luaStateMap = make(map[*C.lua_State]*GolapisLuaState)

// luaThreadMap maps coroutine lua_State pointers to LuaThread objects
// This allows async operations to find their thread context
var luaThreadMap = make(map[*C.lua_State]*LuaThread)

func (gls *GolapisLuaState) registerState() {
	luaStateMap[gls.luaState] = gls
}

func (gls *GolapisLuaState) unregisterState() {
	delete(luaStateMap, gls.luaState)
}

func getLuaStateFromRegistry(L *C.lua_State) *GolapisLuaState {
	// First check if L is a main state
	if gls, ok := luaStateMap[L]; ok {
		return gls
	}
	// Otherwise, L might be a coroutine - find its thread and get parent state
	if thread, ok := luaThreadMap[L]; ok {
		return thread.state
	}
	return nil
}

func getLuaThreadFromRegistry(L *C.lua_State) *LuaThread {
	return luaThreadMap[L]
}
