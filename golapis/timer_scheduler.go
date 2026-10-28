package golapis

import (
	"container/heap"
	"sync"
	"sync/atomic"
	"time"
)

// A scheduler is created by Start and outlives Stop: Stop only flips
// stopped, so the event loop goroutine can keep reading gls.timerSched
// while another goroutine stops the state. Work offered to a stopped
// scheduler is refused and the caller reports "timer scheduler is not
// running"; any thread parked on it is closed by the stop event.
type timerScheduler struct {
	state   *GolapisLuaState
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	stopped atomic.Bool
	heap    scheduledEntryHeap

	cancelMu     sync.Mutex
	cancelTimers []*PendingTimer
}

func newTimerScheduler(state *GolapisLuaState) *timerScheduler {
	return &timerScheduler{
		state: state,
		wake:  make(chan struct{}, 1),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

func (s *timerScheduler) start() {
	go s.loop()
}

func (s *timerScheduler) stopAndWait() {
	if s.stopped.Swap(true) {
		return
	}
	close(s.stop)
	<-s.done
}

// addTimer queues a timer callback; false if the scheduler is stopped.
func (s *timerScheduler) addTimer(timer *PendingTimer, delay time.Duration) bool {
	if s.stopped.Load() {
		return false
	}
	entry := &scheduledEntry{
		deadline: time.Now().Add(delay),
		timer:    timer,
		index:    -1,
	}

	s.state.timerMu.Lock()
	timer.schedEntry = entry
	heap.Push(&s.heap, entry)
	s.state.timerMu.Unlock()
	s.notify()
	return true
}

// addResume queues a delayed resume; nil if the scheduler is stopped.
func (s *timerScheduler) addResume(event *StateEvent, delay time.Duration) *scheduledEntry {
	if s.stopped.Load() {
		return nil
	}
	entry := &scheduledEntry{
		deadline: time.Now().Add(delay),
		event:    event,
		index:    -1,
		sched:    s,
	}

	s.state.timerMu.Lock()
	heap.Push(&s.heap, entry)
	s.state.timerMu.Unlock()
	s.notify()
	return entry
}

// removeResume drops a resume entry that is no longer wanted (e.g. a
// semaphore wait that was posted before its timeout). If the scheduler
// already popped it, the event still fires and its OnResume must treat it
// as stale.
func (s *timerScheduler) removeResume(entry *scheduledEntry) {
	s.state.timerMu.Lock()
	if entry.index >= 0 {
		heap.Remove(&s.heap, entry.index)
		entry.index = -1
	}
	s.state.timerMu.Unlock()
}

func (s *timerScheduler) cancelPendingTimers() {
	s.state.timerMu.Lock()
	timers := make([]*PendingTimer, 0, len(s.state.pendingTimers))
	for timer := range s.state.pendingTimers {
		entry := timer.schedEntry
		if entry != nil && entry.index >= 0 {
			heap.Remove(&s.heap, entry.index)
			entry.index = -1
			timer.schedEntry = nil
		}
		if timer.claim() {
			timers = append(timers, timer)
		}
	}
	s.state.timerMu.Unlock()

	if len(timers) == 0 {
		return
	}

	s.cancelMu.Lock()
	s.cancelTimers = append(s.cancelTimers, timers...)
	s.cancelMu.Unlock()
	s.notify()
}

func (s *timerScheduler) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *timerScheduler) loop() {
	defer close(s.done)

	var clock *time.Timer
	for {
		s.drainCancelledTimers()

		next, delay := s.nextTimer()
		if next == nil {
			select {
			case <-s.wake:
			case <-s.stop:
				stopTimer(clock)
				return
			}
			continue
		}

		if delay <= 0 {
			s.fire(next)
			continue
		}

		if clock == nil {
			clock = time.NewTimer(delay)
		} else {
			resetTimer(clock, delay)
		}

		select {
		case <-clock.C:
		case <-s.wake:
			stopTimer(clock)
		case <-s.stop:
			stopTimer(clock)
			return
		}
	}
}

func (s *timerScheduler) drainCancelledTimers() {
	s.cancelMu.Lock()
	timers := s.cancelTimers
	s.cancelTimers = nil
	s.cancelMu.Unlock()

	for _, timer := range timers {
		if !s.state.stopping.Load() {
			s.state.enqueueTimerFire(timer, true)
		}
	}
}

func (s *timerScheduler) nextTimer() (*scheduledEntry, time.Duration) {
	s.state.timerMu.Lock()
	defer s.state.timerMu.Unlock()

	for s.heap.Len() > 0 {
		entry := s.heap[0]
		if entry.timer != nil && entry.timer.fired.Load() {
			heap.Pop(&s.heap)
			entry.index = -1
			entry.timer.schedEntry = nil
			continue
		}
		delay := time.Until(entry.deadline)
		if delay <= 0 {
			heap.Pop(&s.heap)
			entry.index = -1
			if entry.timer != nil {
				entry.timer.schedEntry = nil
			}
		}
		return entry, delay
	}

	return nil, 0
}

func (s *timerScheduler) fire(entry *scheduledEntry) {
	if entry.timer != nil {
		if entry.timer.claim() && !s.state.stopping.Load() {
			s.state.enqueueTimerFire(entry.timer, false)
		}
		return
	}

	if entry.event != nil && !s.state.stopping.Load() {
		s.state.enqueueEvent(entry.event)
	}
}

func resetTimer(timer *time.Timer, delay time.Duration) {
	stopTimer(timer)
	timer.Reset(delay)
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

type scheduledEntry struct {
	deadline time.Time
	index    int
	timer    *PendingTimer
	event    *StateEvent
	sched    *timerScheduler // set by addResume
}

// cancelPendingIO lets a resume entry serve as a parked coroutine's pendingIO:
// thread.kill or request abort drops it from the heap
func (e *scheduledEntry) cancelPendingIO() {
	e.sched.removeResume(e)
}

type scheduledEntryHeap []*scheduledEntry

func (h scheduledEntryHeap) Len() int {
	return len(h)
}

func (h scheduledEntryHeap) Less(i, j int) bool {
	return h[i].deadline.Before(h[j].deadline)
}

func (h scheduledEntryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *scheduledEntryHeap) Push(x any) {
	entry := x.(*scheduledEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}

func (h *scheduledEntryHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	entry.index = -1
	*h = old[:n-1]
	return entry
}
