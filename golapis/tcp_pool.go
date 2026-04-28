package golapis

import (
	"container/list"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

const (
	tcpEntryAvailable int32 = 0
	tcpEntryClaimed   int32 = 1
)

type tcpPoolEntry struct {
	conn        net.Conn
	reused      int
	pool        *tcpPool
	listElem    *list.Element
	state       int32 // accessed via sync/atomic exclusively after creation
	idleTimeout time.Duration
	owner       *GolapisLuaState
	done        chan struct{} // closed by watcher when it exits
	broken      atomic.Bool   // set by watcher if it observed unexpected data/error/expiry
}

type tcpPool struct {
	key     string
	maxSize int
	list    *list.List
}

func newTCPPool(key string, maxSize int) *tcpPool {
	return &tcpPool{key: key, maxSize: maxSize, list: list.New()}
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
}

// closePoolEntry closes the conn after waking any pending watcher Read. Used
// outside the pool lock by callers (eviction, drain, broken-discard).
func closePoolEntry(e *tcpPoolEntry) {
	e.conn.SetReadDeadline(time.Unix(1, 0))
	e.conn.Close()
}

// tryTakeFromPool claims and returns the MRU pooled connection for the given
// key, or returns false if no pool exists or every available entry was found
// "broken" (watcher observed unexpected data, a non-timeout error, or natural
// idle expiry). On success the entry is removed from the list, the watcher
// has fully exited, and the read deadline is cleared.
func tryTakeFromPool(state *GolapisLuaState, key string) (*tcpPoolEntry, bool) {
	for {
		state.tcpPoolsMu.Lock()
		pool, ok := state.tcpPools[key]
		if !ok {
			state.tcpPoolsMu.Unlock()
			return nil, false
		}
		var claimed *tcpPoolEntry
		for elem := pool.list.Front(); elem != nil; elem = elem.Next() {
			e := elem.Value.(*tcpPoolEntry)
			if atomic.CompareAndSwapInt32(&e.state, tcpEntryAvailable, tcpEntryClaimed) {
				pool.list.Remove(elem)
				e.listElem = nil
				claimed = e
				break
			}
		}
		state.tcpPoolsMu.Unlock()
		if claimed == nil {
			return nil, false
		}
		// Wake the watcher's pending Read (if any) so it exits promptly.
		// The watcher's first action is its own SetReadDeadline; if our
		// deadline-past gets overridden by that, the watcher will still
		// notice state=Claimed on its post-SetReadDeadline recheck and exit.
		claimed.conn.SetReadDeadline(time.Unix(1, 0))
		<-claimed.done
		if claimed.broken.Load() {
			// Watcher observed unexpected data, a non-timeout error
			// (e.g. server-side EOF), or that the natural idle expiry
			// had already fired. Discard and try the next entry.
			claimed.conn.Close()
			continue
		}
		// Reset the deadline so the caller can use the conn cleanly.
		claimed.conn.SetReadDeadline(time.Time{})
		return claimed, true
	}
}

// evictLRUUnlocked atomically claims and detaches one least-recently-used
// entry from the pool. Caller must hold state.tcpPoolsMu. Returns the entry
// (caller is responsible for closing the conn outside the lock) or nil if
// none could be claimed (every entry is currently CAS-claimed by another
// goroutine — they'll free up shortly).
func (p *tcpPool) evictLRUUnlocked() *tcpPoolEntry {
	for elem := p.list.Back(); elem != nil; elem = elem.Prev() {
		e := elem.Value.(*tcpPoolEntry)
		if atomic.CompareAndSwapInt32(&e.state, tcpEntryAvailable, tcpEntryClaimed) {
			p.list.Remove(elem)
			e.listElem = nil
			return e
		}
	}
	return nil
}

// removeFromList removes the entry's list node under the state lock. No-op
// if the entry has already been removed (listElem == nil). Used by the watcher
// after it wins the CAS.
func (p *tcpPool) removeFromList(state *GolapisLuaState, e *tcpPoolEntry) {
	state.tcpPoolsMu.Lock()
	defer state.tcpPoolsMu.Unlock()
	if e.listElem != nil {
		p.list.Remove(e.listElem)
		e.listElem = nil
	}
}

// watch is the per-entry idle watcher. It checks state both before and after
// SetReadDeadline so that a reclaim happening while the goroutine starts up
// cannot be silently overridden. When Read returns it tries to claim the
// entry: on success removes from list and closes the conn; on failure marks
// broken (if appropriate) and exits, signalling done.
func (e *tcpPoolEntry) watch() {
	defer close(e.done)

	// Fast-path: reclaim/eviction may have happened before we even started.
	if atomic.LoadInt32(&e.state) != tcpEntryAvailable {
		return
	}
	expiresAt := time.Now().Add(e.idleTimeout)
	e.conn.SetReadDeadline(expiresAt)
	// Recheck after SetReadDeadline: if a reclaim's deadline-past got
	// overridden by our SetReadDeadline above, we'll see state=Claimed
	// here and exit without ever calling Read.
	if atomic.LoadInt32(&e.state) != tcpEntryAvailable {
		return
	}

	var b [1]byte
	n, err := e.conn.Read(b[:])

	// reclaimWake means the wake was definitely caused by a forced deadline
	// (reclaim/eviction setting the deadline to the past) and the natural
	// idle expiry had not yet fired. A timeout at or after `expiresAt`
	// means the idle timer fired naturally — even if a reclaim races in
	// to win the CAS afterwards, the connection has aged out and must be
	// discarded. n>0 or non-timeout errors (EOF, reset, closed) also mean
	// the connection is unusable.
	reclaimWake := n == 0 && err != nil && isTimeoutErr(err) && time.Now().Before(expiresAt)

	if !atomic.CompareAndSwapInt32(&e.state, tcpEntryAvailable, tcpEntryClaimed) {
		// Lost the CAS to a reclaim/eviction/shutdown. If the wake
		// wasn't a clean forced one, mark broken so the reclaimer
		// discards the connection.
		if !reclaimWake {
			e.broken.Store(true)
		}
		return
	}
	// We won. Remove from list and close.
	e.pool.removeFromList(e.owner, e)
	e.conn.Close()
}
