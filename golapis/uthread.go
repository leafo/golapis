package golapis

/*
#include <lua.h>
#include <lauxlib.h>
#include "lua_helpers.h"
*/
import "C"

// =============================================================================
// Light threads (ngx.thread.spawn / wait / kill)
// =============================================================================
//
// A light thread is a coroutine that runs inside the request's LuaThread but
// parks and resumes independently of the coroutine that spawned it. spawn()
// runs the child immediately until it first yields, then the spawner
// continues; wait() collects a child's results; the request completes only
// when the entry coroutine and every light thread are done.

// lightThreadArg resolves argument idx as a light thread owned by the current
// coroutine. On failure it pushes an error message and returns nil. A
// coroutine that is no longer registered (collected by wait or kill) returns
// nil with collected set, which callers report as (nil, err) rather than
// raising.
func lightThreadArg(L *C.lua_State, thread *LuaThread, idx C.int, verb string) (coctx *coCtx, collected bool) {
	co := C.lua_tothread_wrapper(L, idx)
	if co == nil {
		pushGoString(L, "coroutine expected")
		return nil, false
	}
	coctx = thread.getCoCtx(co)
	if coctx == nil {
		return nil, true
	}
	if !coctx.isLight {
		pushGoString(L, "attempt to "+verb+" a coroutine that is not a user thread")
		return nil, false
	}
	if coctx.parent != thread.curCo {
		pushGoString(L, "only the parent coroutine can "+verb+" the thread")
		return nil, false
	}
	return coctx, false
}

//export golapis_thread_spawn
func golapis_thread_spawn(L *C.lua_State, yieldable C.int) C.int {
	if C.lua_gettop(L) < 1 || C.lua_isfunction_wrapper(L, 1) == 0 {
		pushGoString(L, "thread.spawn expects a function")
		return -1
	}

	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.curCo == nil {
		pushGoString(L, "thread.spawn: no thread context")
		return -1
	}
	if !checkYieldable(L, yieldable) {
		return -1
	}

	gls := thread.state
	co := C.lua_newthread(gls.luaState)
	if co == nil {
		pushGoString(L, "thread.spawn: failed to create coroutine")
		return -1
	}
	coRef := C.luaL_ref_wrapper(gls.luaState, C.LUA_REGISTRYINDEX)

	// The child starts with [fn, args...] on its stack
	nargs := C.lua_gettop(L)
	C.lua_xmove(L, co, nargs)

	coctx := thread.registerCoroutine(co, coRef)
	coctx.isLight = true
	coctx.parent = thread.curCo

	// Yield the thread object: it becomes spawn()'s return value once the
	// spawner is resumed from the posted queue
	C.lua_rawgeti_wrapper(gls.luaState, C.LUA_REGISTRYINDEX, coRef)
	C.lua_xmove(gls.luaState, L, 1)

	if debugEnabled {
		debugLog("thread.spawn: co=%p parent=%p nargs=%d", co, thread.curCo.co, nargs-1)
	}

	// The scheduler starts the child once the yield lands; curCo and the
	// thread count are only touched there, so a failed yield leaves the
	// spawner's state intact
	thread.spawnChild = coctx
	thread.coOp = coOpThreadSpawn
	return -2
}

//export golapis_thread_wait
func golapis_thread_wait(L *C.lua_State, yieldable C.int) C.int {
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.curCo == nil {
		pushGoString(L, "thread.wait: no thread context")
		return -1
	}
	if !checkYieldable(L, yieldable) {
		return -1
	}

	nargs := C.lua_gettop(L)
	if nargs == 0 {
		pushGoString(L, "at least one coroutine should be specified")
		return -1
	}

	parent := thread.curCo
	var pending []*coCtx
	for i := C.int(1); i <= nargs; i++ {
		coctx, collected := lightThreadArg(L, thread, i, "wait on")
		if collected {
			C.lua_pushnil(L)
			pushGoString(L, "already waited or killed")
			return 2
		}
		if coctx == nil {
			return -1
		}

		switch coctx.status {
		case coZombie:
			// Already finished: collect its results right away
			nrets := C.lua_gettop(coctx.co)
			if nrets > 0 {
				C.lua_xmove(coctx.co, L, nrets)
			}
			parent.removeZombieChild(coctx)
			thread.cleanupCoroutine(coctx)
			thread.uthreads--
			if debugEnabled {
				debugLog("thread.wait: collected zombie co=%p nrets=%d", coctx.co, nrets)
			}
			return nrets
		case coDead:
			C.lua_pushnil(L)
			pushGoString(L, "already waited or killed")
			return 2
		}
		pending = append(pending, coctx)
	}

	parent.waitingOn = pending
	if debugEnabled {
		debugLog("thread.wait: co=%p parked on %d threads", L, len(pending))
	}
	// Park until lightThreadDone resumes us with the first finisher's results
	return -3
}

//export golapis_thread_kill
func golapis_thread_kill(L *C.lua_State) C.int {
	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.curCo == nil {
		pushGoString(L, "thread.kill: no thread context")
		return -1
	}

	coctx, collected := lightThreadArg(L, thread, 1, "kill")
	if collected {
		C.lua_pushnil(L)
		pushGoString(L, "already waited or killed")
		return 2
	}
	if coctx == nil {
		// lightThreadArg pushed the message; return it as (nil, err)
		C.lua_pushnil(L)
		C.lua_insert_wrapper(L, -2)
		return 2
	}

	switch coctx.status {
	case coDead:
		C.lua_pushnil(L)
		pushGoString(L, "already waited or killed")
		return 2
	case coZombie:
		coctx.parent.removeZombieChild(coctx)
	default:
		// Parked on an async op (or posted), possibly inside a plain
		// coroutine it resumed: unregistering everything below it makes any
		// pending completion stale and keeps a finishing child from
		// resuming a dead coroutine
		thread.killResumedCoroutines(coctx)
		thread.cancelPendingIO(coctx)
	}

	thread.markDead(coctx)
	thread.cleanupCoroutine(coctx)
	thread.uthreads--
	if debugEnabled {
		debugLog("thread.kill: co=%p", coctx.co)
	}
	C.lua_pushinteger(L, 1)
	return 1
}

// cancelPendingIO closes the socket a killed coroutine was parked on, so
// the abandoned read can't swallow bytes meant for a later reader
func (t *LuaThread) cancelPendingIO(coctx *coCtx) {
	if coctx.pendingIO == nil {
		return
	}
	if debugEnabled {
		debugLog("thread.kill: co=%p cancelling pending socket I/O", coctx.co)
	}
	coctx.pendingIO.cancelPendingIO()
	coctx.pendingIO = nil
}

// killResumedCoroutines unregisters the plain coroutines that root
// (transitively) resumed. Light threads below root are left alone: their
// parent is dead, so they clean themselves up when they finish.
func (t *LuaThread) killResumedCoroutines(root *coCtx) {
	for _, c := range t.coCtxByState {
		if c == root || c.isLight || c.status == coDead {
			continue
		}
		for p := c.parent; p != nil; p = p.parent {
			if p == root {
				if debugEnabled {
					debugLog("thread.kill: co=%p resumed by killed thread", c.co)
				}
				t.cancelPendingIO(c)
				c.status = coDead
				t.cleanupZombieChildren(c)
				t.cleanupCoroutine(c)
				break
			}
			if p.isLight {
				break // reached another light thread first
			}
		}
	}
}

func (c *coCtx) removeZombieChild(z *coCtx) {
	c.zombieChildren = removeCoCtx(c.zombieChildren, z)
}

func removeCoCtx(list []*coCtx, target *coCtx) []*coCtx {
	for i, c := range list {
		if c == target {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}
