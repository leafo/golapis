package golapis

/*
#include "lua_helpers.h"
*/
import "C"

// Coroutines for request entry threads and timer callbacks are recycled
// through lua_resetthread (an OpenResty extension) instead of being created
// and collected per request: the reset keeps the already-grown Lua stack,
// and the registry ref that pins the coroutine is reused along with it.
// Only coroutines Lua code never holds are recycled; a coroutine.create or
// thread.spawn object may still be referenced by the script after it ends.

// coroutinePoolMax bounds the retained stacks per state.
const coroutinePoolMax = 256

type pooledCoroutine struct {
	co  *C.lua_State
	ref C.int // registry ref keeping co alive
}

// acquireCoroutine returns a reset coroutine with the main stack unchanged,
// or a fresh one when the pool is empty. Event loop only.
func (gls *GolapisLuaState) acquireCoroutine() (*C.lua_State, C.int) {
	if n := len(gls.freeCoroutines); n > 0 {
		p := gls.freeCoroutines[n-1]
		gls.freeCoroutines = gls.freeCoroutines[:n-1]
		return p.co, p.ref
	}
	co := C.lua_newthread(gls.luaState)
	if co == nil {
		return nil, 0
	}
	// luaL_ref pops the thread the main stack just received
	return co, C.luaL_ref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX)
}

// releaseCoroutine resets a finished (or abandoned) coroutine for reuse, or
// drops the ref when the pool is full. Event loop only.
func (gls *GolapisLuaState) releaseCoroutine(co *C.lua_State, ref C.int) {
	if len(gls.freeCoroutines) < coroutinePoolMax {
		C.lua_resetthread(gls.luaState, co)
		gls.freeCoroutines = append(gls.freeCoroutines, pooledCoroutine{co: co, ref: ref})
		return
	}
	C.luaL_unref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX, ref)
}
