package golapis

import (
	"container/heap"
	"time"
)

type timerScheduler struct {
	state *GolapisLuaState
	wake  chan struct{}
	stop  chan struct{}
	done  chan struct{}
	heap  pendingTimerHeap
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
	close(s.stop)
	<-s.done
}

func (s *timerScheduler) add(timer *PendingTimer, delay time.Duration) {
	s.state.timerMu.Lock()
	timer.deadline = time.Now().Add(delay)
	timer.index = -1
	heap.Push(&s.heap, timer)
	s.state.timerMu.Unlock()
	s.notify()
}

func (s *timerScheduler) remove(timer *PendingTimer) {
	s.state.timerMu.Lock()
	if timer.index >= 0 {
		heap.Remove(&s.heap, timer.index)
		timer.index = -1
	}
	s.state.timerMu.Unlock()
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
			if next.claim() && !s.state.stopping.Load() {
				s.state.enqueueTimerFire(next, false)
			}
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

func (s *timerScheduler) nextTimer() (*PendingTimer, time.Duration) {
	s.state.timerMu.Lock()
	defer s.state.timerMu.Unlock()

	for s.heap.Len() > 0 {
		timer := s.heap[0]
		if timer.fired.Load() {
			heap.Pop(&s.heap)
			timer.index = -1
			continue
		}
		delay := time.Until(timer.deadline)
		if delay <= 0 {
			heap.Pop(&s.heap)
			timer.index = -1
		}
		return timer, delay
	}

	return nil, 0
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

type pendingTimerHeap []*PendingTimer

func (h pendingTimerHeap) Len() int {
	return len(h)
}

func (h pendingTimerHeap) Less(i, j int) bool {
	return h[i].deadline.Before(h[j].deadline)
}

func (h pendingTimerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *pendingTimerHeap) Push(x interface{}) {
	timer := x.(*PendingTimer)
	timer.index = len(*h)
	*h = append(*h, timer)
}

func (h *pendingTimerHeap) Pop() interface{} {
	old := *h
	n := len(old)
	timer := old[n-1]
	old[n-1] = nil
	timer.index = -1
	*h = old[:n-1]
	return timer
}
