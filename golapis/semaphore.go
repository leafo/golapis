package golapis

/*
#include <lua.h>
#include <lauxlib.h>
#include "lua_helpers.h"
*/
import "C"
import (
	"math"
	"sync"
	"time"
	"unsafe"
)

// =============================================================================
// Semaphore (ngx.semaphore)
// =============================================================================
//
// A counting semaphore shared by every coroutine in the state, across
// requests: a waiter parks until another coroutine posts. Follows
// ngx_http_lua_semaphore.c: wait() takes a unit immediately if none is
// queued ahead of it, a zero timeout never parks, and post() only bumps the
// count and schedules the handler; the handler, run by the event loop, hands
// units to waiters FIFO and resumes them one at a time. Deferring the
// handoff means a waiter that goes away in between (killed, request aborted)
// never consumes a unit.

type Semaphore struct {
	count       int
	waiters     []*semaWaiter // FIFO; holds only parked waiters
	wakePending bool          // queued on the state's readySemaphores
}

// semaWaiter is a coroutine parked in wait(). done is set by whichever of
// the handler, timeout or kill gets there first; the others then stand down.
type semaWaiter struct {
	sem    *Semaphore
	thread *LuaThread
	co     *coCtx
	timer  *scheduledEntry // timeout entry, removed when woken or killed
	done   bool
}

// cancelPendingIO is called by thread.kill (and thread close) on a parked
// waiter
func (w *semaWaiter) cancelPendingIO() {
	w.claim()
	w.sem.removeWaiter(w)
}

// claim marks the waiter settled and drops its timeout entry
func (w *semaWaiter) claim() {
	w.done = true
	if w.timer != nil {
		w.timer.cancelPendingIO()
		w.timer = nil
	}
}

func (s *Semaphore) removeWaiter(w *semaWaiter) {
	for i, c := range s.waiters {
		if c == w {
			s.waiters = append(s.waiters[:i], s.waiters[i+1:]...)
			return
		}
	}
}

// live reports whether the waiter's coroutine is still parked in wait()
func (w *semaWaiter) live() bool {
	if w.done || w.thread.co == nil {
		return false
	}
	return w.thread.getCoCtx(w.co.co) == w.co && w.co.asyncPending
}

// scheduleWake queues the semaphore's handler on the event loop
func (s *Semaphore) scheduleWake(gls *GolapisLuaState) {
	if s.wakePending || len(s.waiters) == 0 || s.count == 0 {
		return
	}
	s.wakePending = true
	gls.readySemaphores = append(gls.readySemaphores, s)
}

// runSemaphoreHandlers hands out units on every semaphore posted since the
// last event. Resuming a waiter may post again, so loop until nothing is
// ready. Runs on the event-loop goroutine.
func (gls *GolapisLuaState) runSemaphoreHandlers() {
	for len(gls.readySemaphores) > 0 {
		sem := gls.readySemaphores[0]
		gls.readySemaphores = gls.readySemaphores[1:]
		sem.wakePending = false
		sem.wake(gls)
	}
}

// wake gives units to waiters in order, resuming each one. A waiter whose
// coroutine is gone is dropped without consuming a unit.
func (s *Semaphore) wake(gls *GolapisLuaState) {
	for len(s.waiters) > 0 && s.count > 0 {
		w := s.waiters[0]
		s.waiters = s.waiters[1:]
		if !w.live() {
			continue
		}
		w.claim()
		s.count--
		if debugEnabled {
			debugLog("semaphore.wake: co=%p count=%d waiters=%d", w.co.co, s.count, len(s.waiters))
		}
		gls.handleResumeThread(&StateEvent{
			Type:         EventResumeThread,
			Thread:       w.thread,
			Co:           w.co,
			ResumeValues: []interface{}{true},
		})
	}
}

// Semaphore registry - maps semaphore ID to Go object
var (
	semaphoreMap           = make(map[uint64]*Semaphore)
	semaphoreMu            sync.Mutex
	semaphoreIDSeq         uint64
	cStrSemaphoreMetatable = C.CString("golapis.semaphore") // allocated once, never freed
)

func registerSemaphore(sem *Semaphore) uint64 {
	semaphoreMu.Lock()
	defer semaphoreMu.Unlock()
	semaphoreIDSeq++
	semaphoreMap[semaphoreIDSeq] = sem
	return semaphoreIDSeq
}

func unregisterSemaphore(id uint64) {
	semaphoreMu.Lock()
	defer semaphoreMu.Unlock()
	delete(semaphoreMap, id)
}

// getSemaphoreFromUserdata extracts the Semaphore at stack index 1, verifying
// its metatable so foreign userdata can't be misinterpreted
func getSemaphoreFromUserdata(L *C.lua_State) (*Semaphore, uint64) {
	if C.lua_getmetatable(L, 1) == 0 {
		return nil, 0
	}
	C.luaL_getmetatable_wrapper(L, cStrSemaphoreMetatable)
	same := C.lua_rawequal(L, -1, -2) != 0
	C.lua_pop_wrapper(L, 2)
	if !same {
		return nil, 0
	}
	ptr := C.lua_touserdata_wrapper(L, 1)
	if ptr == nil {
		return nil, 0
	}
	id := *(*uint64)(ptr)
	semaphoreMu.Lock()
	defer semaphoreMu.Unlock()
	return semaphoreMap[id], id
}

// optNumber reads a numeric argument like ngx's `tonumber(v) or def`
func optNumber(L *C.lua_State, idx C.int, def float64) float64 {
	if C.lua_gettop(L) < idx || C.lua_isnumber(L, idx) == 0 {
		return def
	}
	return float64(C.lua_tonumber(L, idx))
}

//export golapis_semaphore_new
func golapis_semaphore_new(L *C.lua_State) C.int {
	n := optNumber(L, 1, 0)
	if n < 0 {
		pushGoString(L, "no negative number")
		return -1
	}

	id := registerSemaphore(&Semaphore{count: int(n)})
	ptr := C.lua_newuserdata(L, C.size_t(unsafe.Sizeof(uint64(0))))
	*(*uint64)(ptr) = id
	C.luaL_getmetatable_wrapper(L, cStrSemaphoreMetatable)
	C.lua_setmetatable(L, -2)

	if debugEnabled {
		debugLog("semaphore.new: id=%d count=%d", id, int(n))
	}
	return 1
}

//export golapis_semaphore_wait
func golapis_semaphore_wait(L *C.lua_State, yieldable C.int) C.int {
	sem, id := getSemaphoreFromUserdata(L)
	if sem == nil {
		pushGoString(L, "not a semaphore instance")
		return -1
	}

	if C.lua_gettop(L) < 2 || C.lua_isnumber(L, 2) == 0 {
		pushGoString(L, "timeout must be a number")
		return -1
	}
	seconds := float64(C.lua_tonumber(L, 2))
	maxSeconds := float64(time.Duration(1<<63-1)) / float64(time.Second)
	if seconds < 0 || math.IsNaN(seconds) || seconds > maxSeconds {
		pushGoString(L, "invalid timeout")
		return -1
	}

	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.curCo == nil {
		pushGoString(L, "could not find thread context")
		return -1
	}

	// A unit is available and nobody is queued ahead of us
	if len(sem.waiters) == 0 && sem.count > 0 {
		sem.count--
		C.lua_pushboolean(L, 1)
		return 1
	}

	if seconds == 0 {
		C.lua_pushnil(L)
		pushGoString(L, "timeout")
		return 2
	}

	if !checkYieldable(L, yieldable) {
		return -1
	}
	if thread.state.timerSched == nil {
		pushGoString(L, "timer scheduler is not running")
		return -1
	}

	issuingCo := thread.curCo
	w := &semaWaiter{sem: sem, thread: thread, co: issuingCo}
	sem.waiters = append(sem.waiters, w)
	issuingCo.pendingIO = w

	w.timer = thread.state.timerSched.addResume(&StateEvent{
		Type:         EventResumeThread,
		Thread:       thread,
		Co:           issuingCo,
		ResumeValues: []interface{}{nil, "timeout"},
		OnResume: func(event *StateEvent) {
			if w.done {
				// woken or killed before the timer could be removed
				event.Stale = true
				return
			}
			w.done = true
			w.timer = nil
			sem.removeWaiter(w)
		},
	}, time.Duration(seconds*float64(time.Second)))

	if debugEnabled {
		debugLog("semaphore.wait: id=%d co=%p timeout=%v waiters=%d", id, L, seconds, len(sem.waiters))
	}
	return -3
}

//export golapis_semaphore_post
func golapis_semaphore_post(L *C.lua_State) C.int {
	sem, id := getSemaphoreFromUserdata(L)
	if sem == nil {
		pushGoString(L, "not a semaphore instance")
		return -1
	}

	n := optNumber(L, 2, 1)
	if n < 1 {
		pushGoString(L, "positive number required")
		return -1
	}

	gls := getLuaStateFromRegistry(L)
	if gls == nil {
		pushGoString(L, "could not find thread context")
		return -1
	}

	sem.count += int(n)
	if debugEnabled {
		debugLog("semaphore.post: id=%d n=%d count=%d waiters=%d", id, int(n), sem.count, len(sem.waiters))
	}
	sem.scheduleWake(gls)

	C.lua_pushboolean(L, 1)
	return 1
}

//export golapis_semaphore_count
func golapis_semaphore_count(L *C.lua_State) C.int {
	sem, _ := getSemaphoreFromUserdata(L)
	if sem == nil {
		pushGoString(L, "not a semaphore instance")
		return -1
	}

	// Like ngx.semaphore: negative means that many coroutines are waiting
	C.lua_pushinteger(L, C.lua_Integer(sem.count-len(sem.waiters)))
	return 1
}

//export golapis_semaphore_gc
func golapis_semaphore_gc(L *C.lua_State) C.int {
	ptr := C.lua_touserdata_wrapper(L, 1)
	if ptr != nil {
		id := *(*uint64)(ptr)
		// Waiters, if any, keep their own reference and simply time out
		unregisterSemaphore(id)
	}
	return 0
}
