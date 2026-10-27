package golapis

/*
#include "lua_helpers.h"
*/
import "C"
import (
	"unsafe"

	"golapis/golapis/internal/ljlayout"
)

// Lookups from a lua_State back to Go objects use LuaJIT's exdata slots
// (an OpenResty extension) instead of package-level maps, so states on
// different goroutines never share any registry.
//
//   exdata  -> *GolapisLuaState. Set on the main state; every coroutine
//              inherits it from lua_newthread.
//   exdata2 -> *LuaThread driving the coroutine. NULL on the main state
//              and on coroutines not yet registered.
//
// Storing a Go pointer in C memory is allowed once the object is pinned,
// and every callback resolves it with a plain memory read: the field
// offsets come from the C compiler (see ljlayout), so there is no cgo
// transition and no handle table on the hot path.

func exdataSlot(L *C.lua_State) *unsafe.Pointer {
	return (*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(L), ljlayout.Exdata))
}

func exdata2Slot(L *C.lua_State) *unsafe.Pointer {
	return (*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(L), ljlayout.Exdata2))
}

func (gls *GolapisLuaState) registerState() {
	gls.pinner.Pin(gls)
	*exdataSlot(gls.luaState) = unsafe.Pointer(gls)
}

func (gls *GolapisLuaState) unregisterState() {
	*exdataSlot(gls.luaState) = nil
	gls.pinner.Unpin()
}

func getLuaStateFromRegistry(L *C.lua_State) *GolapisLuaState {
	return (*GolapisLuaState)(*exdataSlot(L))
}

func getLuaThreadFromRegistry(L *C.lua_State) *LuaThread {
	return (*LuaThread)(*exdata2Slot(L))
}

// trackThread pins a new thread and records it in the owning state so Stop
// can finalize whatever is still parked. Event loop only.
func (gls *GolapisLuaState) trackThread(t *LuaThread) {
	t.pinner.Pin(t)
	t.nextThread = gls.threadList
	if gls.threadList != nil {
		gls.threadList.prevThread = t
	}
	gls.threadList = t
}

// untrackThread is the inverse of trackThread; called once from close.
func (gls *GolapisLuaState) untrackThread(t *LuaThread) {
	if t.prevThread != nil {
		t.prevThread.nextThread = t.nextThread
	} else {
		gls.threadList = t.nextThread
	}
	if t.nextThread != nil {
		t.nextThread.prevThread = t.prevThread
	}
	t.prevThread, t.nextThread = nil, nil
	t.pinner.Unpin()
}

// liveThreads snapshots the live list so callers can close threads while
// iterating.
func (gls *GolapisLuaState) liveThreads() []*LuaThread {
	var out []*LuaThread
	for t := gls.threadList; t != nil; t = t.nextThread {
		out = append(out, t)
	}
	return out
}

// bindCoroutine points a coroutine at its driving thread.
func (t *LuaThread) bindCoroutine(co *C.lua_State) {
	*exdata2Slot(co) = unsafe.Pointer(t)
}

// unbindCoroutine clears the pointer. Must run before the thread is
// unpinned, otherwise a stale coroutine could hand out a dangling pointer.
func unbindCoroutine(co *C.lua_State) {
	*exdata2Slot(co) = nil
}
