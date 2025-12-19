package golapis

/*
#include "lua_helpers.h"
*/
import "C"

// luaStateMap maps main lua_State pointers to GolapisLuaState objects
var luaStateMap = make(map[*C.lua_State]*GolapisLuaState)

// luaThreadMap maps coroutine lua_State pointers to LuaThread objects
// This allows async operations to find their thread context
// If we add concurrent GolapisLuaStates then we need to synchronize access to this, but currently there is only one at a time and operations to this are synchronzied to the lua thread
var luaThreadMap = make(map[*C.lua_State]*LuaThread)

func (gls *GolapisLuaState) registerState() {
	luaStateMap[gls.luaState] = gls
}

func (gls *GolapisLuaState) unregisterState() {
	delete(luaStateMap, gls.luaState)
}

func (thread *LuaThread) registerThread() {
	luaThreadMap[thread.co] = thread
}

func (thread *LuaThread) unregisterThread() {
	delete(luaThreadMap, thread.co)
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
