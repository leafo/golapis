package golapis

/*
#include "lua_helpers.h"
*/
import "C"
import "runtime/cgo"

// Lookups from a lua_State back to Go objects use LuaJIT's exdata slots
// (an OpenResty extension) instead of package-level maps, so states on
// different goroutines never share any registry.
//
//   exdata  -> cgo.Handle of the owning GolapisLuaState. Set on the main
//              state; every coroutine inherits it from lua_newthread.
//   exdata2 -> cgo.Handle of the LuaThread that drives the coroutine. NULL
//              on the main state and on coroutines not yet registered.
//
// A cgo.Handle is a uintptr, which is what makes stashing it in C memory
// legal under the cgo pointer rules.

func (gls *GolapisLuaState) registerState() {
	gls.handle = cgo.NewHandle(gls)
	gls.threads = make(map[*LuaThread]struct{})
	C.golapis_set_exdata(gls.luaState, C.uintptr_t(gls.handle))
}

func (gls *GolapisLuaState) unregisterState() {
	C.golapis_set_exdata(gls.luaState, 0)
	gls.handle.Delete()
}

func getLuaStateFromRegistry(L *C.lua_State) *GolapisLuaState {
	h := C.golapis_get_exdata(L)
	if h == 0 {
		return nil
	}
	return cgo.Handle(h).Value().(*GolapisLuaState)
}

func getLuaThreadFromRegistry(L *C.lua_State) *LuaThread {
	h := C.golapis_get_exdata2(L)
	if h == 0 {
		return nil
	}
	return cgo.Handle(h).Value().(*LuaThread)
}

// trackThread gives a new thread its handle and records it in the owning
// state so Stop can finalize whatever is still parked. Event loop only.
func (gls *GolapisLuaState) trackThread(t *LuaThread) {
	t.handle = cgo.NewHandle(t)
	gls.threads[t] = struct{}{}
}

// untrackThread is the inverse of trackThread; called once from close.
func (gls *GolapisLuaState) untrackThread(t *LuaThread) {
	delete(gls.threads, t)
	t.handle.Delete()
	t.handle = 0
}

// bindCoroutine points a coroutine at its driving thread.
func (t *LuaThread) bindCoroutine(co *C.lua_State) {
	C.golapis_set_exdata2(co, C.uintptr_t(t.handle))
}

// unbindCoroutine clears the pointer. Must run before the thread's handle
// is deleted, otherwise a stale coroutine could resolve a dead handle.
func unbindCoroutine(co *C.lua_State) {
	C.golapis_set_exdata2(co, 0)
}
