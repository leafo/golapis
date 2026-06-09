package golapis

import (
	"container/list"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Shared dictionaries implement the ngx.shared.DICT API. Semantics are ported
// from ngx_http_lua_shdict.c (openresty's lua-nginx-module): value type tags,
// op flags, eviction behavior, and error strings all match the reference
// implementation. Memory accounting approximates the nginx slab allocator with
// a fixed per-entry overhead, so capacities behave similarly but free_space()
// is byte-precise rather than page-granular.

// Value type tags matching OpenResty's shdict encoding
const (
	shdictTNil    uint8 = 0
	shdictTBool   uint8 = 1
	shdictTNumber uint8 = 3
	shdictTString uint8 = 4
	shdictTList   uint8 = 5
)

// Store op flags matching NGX_HTTP_LUA_SHDICT_{ADD,REPLACE,SAFE_STORE}
const (
	shdictOpAdd     = 0x1
	shdictOpReplace = 0x2
	shdictOpSafe    = 0x4
)

const (
	shdictMaxKeyLen = 65535
	// OpenResty rejects lua_shared_dict sizes <= 8191 bytes
	shdictMinSize = 8192
)

// Approximate per-entry byte costs emulating the nginx slab allocation
// (rbtree node + shdict node header, list node header).
const (
	shdictEntryOverhead    = 120
	shdictListNodeOverhead = 48
	shdictListHeadExtra    = 16
)

// ShdictValue carries a scalar shdict value across the Go/Lua boundary.
// Booleans are stored in Num as 0/1.
type ShdictValue struct {
	Type uint8
	Str  string
	Num  float64
}

type shdictListNode struct {
	isNumber bool
	str      string
	num      float64
}

type shdictEntry struct {
	key       string
	valueType uint8
	str       string     // shdictTString value
	num       float64    // shdictTNumber value, or 0/1 for shdictTBool
	list      *list.List // of shdictListNode when valueType == shdictTList
	userFlags uint32
	expiresAt int64         // unix milliseconds; 0 = never expires
	lruElem   *list.Element // element in SharedDict.lru holding *shdictEntry
	size      int64         // accounted bytes, including list nodes
}

// SharedDict is an in-memory key/value store shared by all Lua states in the
// process. All public methods are safe for concurrent use.
type SharedDict struct {
	name     string
	capacity int64

	mu      sync.Mutex
	used    int64
	entries map[string]*shdictEntry
	lru     *list.List // front = most recently used
	now     func() int64
}

// lookup result states
const (
	shdictNotFound = iota
	shdictFound
	shdictFoundStale
)

func newSharedDict(name string, capacity int64) *SharedDict {
	return &SharedDict{
		name:     name,
		capacity: capacity,
		entries:  make(map[string]*shdictEntry),
		lru:      list.New(),
		now:      func() int64 { return time.Now().UnixMilli() },
	}
}

func scalarPayloadSize(val ShdictValue) int64 {
	switch val.Type {
	case shdictTString:
		return int64(len(val.Str))
	case shdictTNumber:
		return 8
	case shdictTBool:
		return 1
	}
	return 0
}

func scalarEntrySize(key string, val ShdictValue) int64 {
	return shdictEntryOverhead + int64(len(key)) + scalarPayloadSize(val)
}

func listHeadSize(key string) int64 {
	return shdictEntryOverhead + int64(len(key)) + shdictListHeadExtra
}

func listNodeSize(node shdictListNode) int64 {
	if node.isNumber {
		return shdictListNodeOverhead + 8
	}
	return shdictListNodeOverhead + int64(len(node.str))
}

func (e *shdictEntry) expired(nowMs int64) bool {
	return e.expiresAt != 0 && e.expiresAt-nowMs <= 0
}

// lookup finds an entry and moves it to the LRU front (matching
// ngx_http_lua_shdict_lookup, which promotes found nodes even when stale).
func (d *SharedDict) lookup(key string) (*shdictEntry, int) {
	e, ok := d.entries[key]
	if !ok {
		return nil, shdictNotFound
	}
	d.lru.MoveToFront(e.lruElem)
	if e.expired(d.now()) {
		return e, shdictFoundStale
	}
	return e, shdictFound
}

// peek finds an entry without touching LRU order (used by ttl/expire).
func (d *SharedDict) peek(key string) *shdictEntry {
	return d.entries[key]
}

func (d *SharedDict) removeEntry(e *shdictEntry) {
	delete(d.entries, e.key)
	d.lru.Remove(e.lruElem)
	d.used -= e.size
}

// evictStep ports ngx_http_lua_shdict_expire(ctx, n): with n == 1 it removes
// up to 2 expired entries from the LRU tail; with n == 0 it force-removes the
// oldest entry regardless of expiry plus up to 2 expired ones. Returns the
// number of entries freed.
func (d *SharedDict) evictStep(n int) int {
	freed := 0
	nowMs := d.now()
	for n < 3 {
		tail := d.lru.Back()
		if tail == nil {
			return freed
		}
		e := tail.Value.(*shdictEntry)
		if n != 0 {
			if !e.expired(nowMs) {
				return freed
			}
		}
		n++
		d.removeEntry(e)
		freed++
	}
	return freed
}

// allocate checks that need bytes fit within capacity, force-evicting LRU
// entries when safe is false (up to 30 attempts, matching the reference
// implementation). Returns whether the allocation fits and whether any
// non-expired entries may have been forcibly evicted.
func (d *SharedDict) allocate(need int64, safe bool) (bool, bool) {
	if d.used+need <= d.capacity {
		return true, false
	}
	if safe {
		return false, false
	}
	forcible := false
	for i := 0; i < 30; i++ {
		if d.evictStep(0) == 0 {
			break
		}
		forcible = true
		if d.used+need <= d.capacity {
			return true, forcible
		}
	}
	return false, forcible
}

func (d *SharedDict) insertEntry(key string, val ShdictValue, expiresAt int64, flags uint32, size int64) *shdictEntry {
	e := &shdictEntry{
		key:       key,
		valueType: val.Type,
		str:       val.Str,
		num:       val.Num,
		userFlags: flags,
		expiresAt: expiresAt,
		size:      size,
	}
	e.lruElem = d.lru.PushFront(e)
	d.entries[key] = e
	d.used += size
	return e
}

func (d *SharedDict) expiresFromTTL(ttlMs int64) int64 {
	if ttlMs > 0 {
		return d.now() + ttlMs
	}
	return 0
}

// Get retrieves a scalar value. A miss is signaled by val.Type == shdictTNil.
// With stale set, expired values are returned with isStale true.
func (d *SharedDict) Get(key string, stale bool) (val ShdictValue, flags uint32, isStale bool, errStr string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !stale {
		d.evictStep(1)
	}

	e, state := d.lookup(key)
	if state == shdictNotFound || (state == shdictFoundStale && !stale) {
		return ShdictValue{Type: shdictTNil}, 0, false, ""
	}

	if e.valueType == shdictTList {
		return ShdictValue{Type: shdictTNil}, 0, false, "value is a list"
	}

	val = ShdictValue{Type: e.valueType, Str: e.str, Num: e.num}
	return val, e.userFlags, state == shdictFoundStale, ""
}

// Store implements set/safe_set/add/safe_add/replace/delete via the op
// bitmask. A nil val.Type removes the key (set(key, nil) semantics).
func (d *SharedDict) Store(op int, key string, val ShdictValue, exptimeMs int64, flags uint32) (ok bool, errStr string, forcible bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.evictStep(1)

	e, state := d.lookup(key)

	if op&shdictOpReplace != 0 && state != shdictFound {
		return false, "not found", false
	}

	if op&shdictOpAdd != 0 && state == shdictFound {
		return false, "exists", false
	}

	if e != nil {
		d.removeEntry(e)
	}

	if val.Type == shdictTNil {
		return true, "", false
	}

	need := scalarEntrySize(key, val)
	fits, forcible := d.allocate(need, op&shdictOpSafe != 0)
	if !fits {
		return false, "no memory", forcible
	}

	d.insertEntry(key, val, d.expiresFromTTL(exptimeMs), flags, need)
	return true, "", forcible
}

// Incr increments a numeric value. With hasInit, a missing or stale key is
// (re)initialized to init+delta with user flags reset and initTTLMs applied.
func (d *SharedDict) Incr(key string, delta float64, hasInit bool, init float64, initTTLMs int64) (newVal float64, errStr string, forcible bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.evictStep(1)

	e, state := d.lookup(key)

	if state == shdictFound {
		if e.valueType != shdictTNumber {
			return 0, "not a number", false
		}
		e.num += delta
		return e.num, "", false
	}

	if !hasInit {
		return 0, "not found", false
	}

	num := delta + init

	if state == shdictFoundStale && e.valueType == shdictTNumber {
		// reuse the expired slot, matching the value-size-matched path
		e.num = num
		e.userFlags = 0
		e.expiresAt = d.expiresFromTTL(initTTLMs)
		return num, "", false
	}

	if e != nil {
		d.removeEntry(e)
	}

	val := ShdictValue{Type: shdictTNumber, Num: num}
	need := scalarEntrySize(key, val)
	fits, forcible := d.allocate(need, false)
	if !fits {
		return 0, "no memory", forcible
	}

	d.insertEntry(key, val, d.expiresFromTTL(initTTLMs), 0, need)
	return num, "", forcible
}

// TTLms returns the remaining TTL in milliseconds: 0 means never expires, and
// the value may be negative for stale entries. Does not update LRU order.
func (d *SharedDict) TTLms(key string) (ms int64, found bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	e := d.peek(key)
	if e == nil {
		return 0, false
	}
	if e.expiresAt == 0 {
		return 0, true
	}
	return e.expiresAt - d.now(), true
}

// Expire updates a key's TTL; exptimeMs <= 0 removes the expiry. Does not
// update LRU order. Stale-but-present entries can be revived.
func (d *SharedDict) Expire(key string, exptimeMs int64) (found bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	e := d.peek(key)
	if e == nil {
		return false
	}
	e.expiresAt = d.expiresFromTTL(exptimeMs)
	return true
}

// GetKeys returns up to maxCount non-expired keys, oldest first (LRU tail
// order). maxCount <= 0 means unlimited.
func (d *SharedDict) GetKeys(maxCount int) []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	nowMs := d.now()
	var keys []string
	for el := d.lru.Back(); el != nil; el = el.Prev() {
		e := el.Value.(*shdictEntry)
		if e.expired(nowMs) {
			continue
		}
		keys = append(keys, e.key)
		if maxCount > 0 && len(keys) >= maxCount {
			break
		}
	}
	return keys
}

// FlushAll marks every entry expired (still visible to get_stale) and frees a
// few entries immediately, matching the reference implementation.
func (d *SharedDict) FlushAll() {
	d.mu.Lock()
	defer d.mu.Unlock()

	nowMs := d.now()
	for el := d.lru.Front(); el != nil; el = el.Next() {
		el.Value.(*shdictEntry).expiresAt = nowMs - 1
	}
	d.evictStep(0)
}

// FlushExpired removes up to attempts expired entries (0 = unlimited),
// returning the number freed.
func (d *SharedDict) FlushExpired(attempts int) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	nowMs := d.now()
	freed := 0
	for el := d.lru.Back(); el != nil; {
		prev := el.Prev()
		e := el.Value.(*shdictEntry)
		if e.expired(nowMs) {
			d.removeEntry(e)
			freed++
			if attempts > 0 && freed >= attempts {
				break
			}
		}
		el = prev
	}
	return freed
}

func (d *SharedDict) Capacity() int64 {
	return d.capacity
}

func (d *SharedDict) FreeSpace() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.used >= d.capacity {
		return 0
	}
	return d.capacity - d.used
}

// Push appends a node to the list at key (left or right). headFail
// distinguishes a list-head allocation failure (Lua returns false) from a
// node allocation failure (Lua returns nil). Push never force-evicts.
func (d *SharedDict) Push(key string, left bool, node shdictListNode) (newLen int, errStr string, headFail bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.evictStep(1)

	e, state := d.lookup(key)

	switch state {
	case shdictFound:
		if e.valueType != shdictTList {
			return 0, "value not a list", false
		}
	case shdictFoundStale:
		if e.valueType == shdictTList {
			// reuse the expired list: drop its nodes and revive it
			d.used -= e.size - listHeadSize(key)
			e.size = listHeadSize(key)
			e.list.Init()
			e.expiresAt = 0
		} else {
			d.removeEntry(e)
			e = nil
			state = shdictNotFound
		}
	}

	if state == shdictNotFound {
		headSize := listHeadSize(key)
		if d.used+headSize > d.capacity {
			return 0, "no memory", true
		}
		e = d.insertEntry(key, ShdictValue{Type: shdictTList}, 0, 0, headSize)
		e.list = list.New()
	}

	nodeSize := listNodeSize(node)
	if d.used+nodeSize > d.capacity {
		if e.list.Len() == 0 {
			d.removeEntry(e)
		}
		return 0, "no memory", false
	}

	if left {
		e.list.PushFront(node)
	} else {
		e.list.PushBack(node)
	}
	e.size += nodeSize
	d.used += nodeSize
	return e.list.Len(), "", false
}

// Pop removes and returns a node from the list at key. found is false for
// missing or expired keys. The entry is removed once the list empties.
func (d *SharedDict) Pop(key string, left bool) (node shdictListNode, found bool, errStr string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.evictStep(1)

	e, state := d.lookup(key)
	if state != shdictFound {
		return shdictListNode{}, false, ""
	}
	if e.valueType != shdictTList {
		return shdictListNode{}, false, "value not a list"
	}

	var el *list.Element
	if left {
		el = e.list.Front()
	} else {
		el = e.list.Back()
	}
	if el == nil {
		return shdictListNode{}, false, ""
	}

	node = el.Value.(shdictListNode)
	e.list.Remove(el)
	nodeSize := listNodeSize(node)
	e.size -= nodeSize
	d.used -= nodeSize

	if e.list.Len() == 0 {
		d.removeEntry(e)
	}
	return node, true, ""
}

// Llen returns the list length at key (0 for missing or expired keys).
func (d *SharedDict) Llen(key string) (n int, errStr string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.evictStep(1)

	e, state := d.lookup(key)
	if state != shdictFound {
		return 0, ""
	}
	if e.valueType != shdictTList {
		return 0, "value not a list"
	}
	return e.list.Len(), ""
}

// =============================================================================
// Process-global registry
// =============================================================================

// Shared dicts are registered before any Lua state is created (states snapshot
// the registry during SetupGolapis), so they are shared by all states in the
// process. The Lua userdata for a dict holds a uint64 index into
// sharedDictsByID.

var (
	sharedDictsMu     sync.RWMutex
	sharedDictsByName = make(map[string]*SharedDict)
	sharedDictsByID   []*SharedDict
)

// RegisterSharedDict declares a shared dictionary. Must be called before any
// GolapisLuaState is created; dicts registered later are invisible to
// existing states.
func RegisterSharedDict(name string, capacity int64) error {
	if name == "" {
		return fmt.Errorf("shared dict name cannot be empty")
	}
	if capacity < shdictMinSize {
		return fmt.Errorf("shared dict %q size %d is too small (minimum %d bytes)", name, capacity, shdictMinSize)
	}

	sharedDictsMu.Lock()
	defer sharedDictsMu.Unlock()

	if _, exists := sharedDictsByName[name]; exists {
		return fmt.Errorf("shared dict %q already declared", name)
	}

	d := newSharedDict(name, capacity)
	sharedDictsByName[name] = d
	sharedDictsByID = append(sharedDictsByID, d)
	return nil
}

func getSharedDictByID(id uint64) *SharedDict {
	sharedDictsMu.RLock()
	defer sharedDictsMu.RUnlock()
	if id >= uint64(len(sharedDictsByID)) {
		return nil
	}
	return sharedDictsByID[id]
}

// registeredSharedDicts returns all dicts in registration order.
func registeredSharedDicts() []*SharedDict {
	sharedDictsMu.RLock()
	defer sharedDictsMu.RUnlock()
	out := make([]*SharedDict, len(sharedDictsByID))
	copy(out, sharedDictsByID)
	return out
}

// ParseHumanSize parses a byte size with an optional k/m/g suffix
// (case-insensitive), e.g. "8192", "64k", "10m", "1g".
func ParseHumanSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}

	mult := int64(1)
	switch s[len(s)-1] {
	case 'k', 'K':
		mult = 1 << 10
		s = s[:len(s)-1]
	case 'm', 'M':
		mult = 1 << 20
		s = s[:len(s)-1]
	case 'g', 'G':
		mult = 1 << 30
		s = s[:len(s)-1]
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	if n <= 0 {
		return 0, fmt.Errorf("size must be positive, got %d", n)
	}
	if n > math.MaxInt64/mult {
		return 0, fmt.Errorf("size %q is too large", s)
	}
	return n * mult, nil
}
