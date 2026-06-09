package golapis

import (
	"fmt"
	"testing"
)

// fakeClock provides a controllable millisecond clock for TTL tests.
type fakeClock struct {
	ms int64
}

func (c *fakeClock) now() int64            { return c.ms }
func (c *fakeClock) advance(deltaMs int64) { c.ms += deltaMs }

func newTestDict(capacity int64) (*SharedDict, *fakeClock) {
	clock := &fakeClock{ms: 1000000}
	d := newSharedDict("test", capacity)
	d.now = clock.now
	return d, clock
}

func strVal(s string) ShdictValue  { return ShdictValue{Type: shdictTString, Str: s} }
func numVal(n float64) ShdictValue { return ShdictValue{Type: shdictTNumber, Num: n} }
func boolVal(b bool) ShdictValue {
	v := ShdictValue{Type: shdictTBool}
	if b {
		v.Num = 1
	}
	return v
}

func mustSet(t *testing.T, d *SharedDict, key string, val ShdictValue, exptimeMs int64) {
	t.Helper()
	ok, errStr, _ := d.Store(0, key, val, exptimeMs, 0)
	if !ok {
		t.Fatalf("set %q failed: %s", key, errStr)
	}
}

func TestShdictSetGet(t *testing.T) {
	d, _ := newTestDict(1 << 20)

	mustSet(t, d, "str", strVal("hello"), 0)
	mustSet(t, d, "num", numVal(3.5), 0)
	mustSet(t, d, "bool", boolVal(true), 0)

	val, flags, stale, errStr := d.Get("str", false)
	if val.Type != shdictTString || val.Str != "hello" || flags != 0 || stale || errStr != "" {
		t.Errorf("get str = %+v flags=%d stale=%v err=%q", val, flags, stale, errStr)
	}

	val, _, _, _ = d.Get("num", false)
	if val.Type != shdictTNumber || val.Num != 3.5 {
		t.Errorf("get num = %+v", val)
	}

	val, _, _, _ = d.Get("bool", false)
	if val.Type != shdictTBool || val.Num != 1 {
		t.Errorf("get bool = %+v", val)
	}

	val, _, _, _ = d.Get("missing", false)
	if val.Type != shdictTNil {
		t.Errorf("get missing = %+v, want nil type", val)
	}
}

func TestShdictUserFlags(t *testing.T) {
	d, _ := newTestDict(1 << 20)

	ok, _, _ := d.Store(0, "k", strVal("v"), 0, 42)
	if !ok {
		t.Fatal("set failed")
	}
	_, flags, _, _ := d.Get("k", false)
	if flags != 42 {
		t.Errorf("flags = %d, want 42", flags)
	}
}

func TestShdictExpiry(t *testing.T) {
	d, clock := newTestDict(1 << 20)

	mustSet(t, d, "k", strVal("v"), 5000)

	val, _, _, _ := d.Get("k", false)
	if val.Type != shdictTString {
		t.Fatal("value should be live")
	}

	clock.advance(5001)

	// stale get sees the expired value (checked before plain get, whose
	// housekeeping pass physically removes expired entries — same as OpenResty)
	val, _, stale, _ := d.Get("k", true)
	if val.Type != shdictTString || val.Str != "v" || !stale {
		t.Errorf("get_stale = %+v stale=%v", val, stale)
	}

	val, _, _, _ = d.Get("k", false)
	if val.Type != shdictTNil {
		t.Errorf("expired value returned: %+v", val)
	}
}

func TestShdictDelete(t *testing.T) {
	d, _ := newTestDict(1 << 20)

	mustSet(t, d, "k", strVal("v"), 0)

	// set(key, nil) removes; returns true even when key is absent
	ok, _, _ := d.Store(0, "k", ShdictValue{Type: shdictTNil}, 0, 0)
	if !ok {
		t.Error("delete existing should return true")
	}
	ok, _, _ = d.Store(0, "k", ShdictValue{Type: shdictTNil}, 0, 0)
	if !ok {
		t.Error("delete missing should return true")
	}
	val, _, _, _ := d.Get("k", false)
	if val.Type != shdictTNil {
		t.Error("key should be gone")
	}
}

func TestShdictAddReplace(t *testing.T) {
	d, clock := newTestDict(1 << 20)

	// add on missing key succeeds
	ok, errStr, _ := d.Store(shdictOpAdd, "k", strVal("a"), 1000, 0)
	if !ok {
		t.Fatalf("add failed: %s", errStr)
	}
	// add on live key fails with "exists"
	ok, errStr, _ = d.Store(shdictOpAdd, "k", strVal("b"), 0, 0)
	if ok || errStr != "exists" {
		t.Errorf("add on live = %v %q, want false exists", ok, errStr)
	}
	// replace on live key succeeds
	ok, errStr, _ = d.Store(shdictOpReplace, "k", strVal("c"), 0, 0)
	if !ok {
		t.Errorf("replace failed: %s", errStr)
	}

	clock.advance(2000)
	mustSet(t, d, "stale", strVal("x"), 1000)
	clock.advance(1001)

	// replace on stale key fails with "not found"
	ok, errStr, _ = d.Store(shdictOpReplace, "stale", strVal("y"), 0, 0)
	if ok || errStr != "not found" {
		t.Errorf("replace on stale = %v %q, want false not found", ok, errStr)
	}
	// add on stale key overwrites it
	ok, errStr, _ = d.Store(shdictOpAdd, "stale", strVal("z"), 0, 0)
	if !ok {
		t.Errorf("add on stale failed: %s", errStr)
	}
	val, _, _, _ := d.Get("stale", false)
	if val.Str != "z" {
		t.Errorf("value = %q, want z", val.Str)
	}

	// replace on missing key fails
	ok, errStr, _ = d.Store(shdictOpReplace, "nope", strVal("v"), 0, 0)
	if ok || errStr != "not found" {
		t.Errorf("replace on missing = %v %q", ok, errStr)
	}
}

func TestShdictLRUEviction(t *testing.T) {
	// capacity fits ~3 entries: each is 120 + 2 (key) + 100 (value) = 222
	d, _ := newTestDict(700)

	bigVal := strVal(string(make([]byte, 100)))
	mustSet(t, d, "k1", bigVal, 0)
	mustSet(t, d, "k2", bigVal, 0)
	mustSet(t, d, "k3", bigVal, 0)

	// k4 doesn't fit; set must evict the LRU entry (k1) and report forcible
	ok, errStr, forcible := d.Store(0, "k4", bigVal, 0, 0)
	if !ok {
		t.Fatalf("set k4 failed: %s", errStr)
	}
	if !forcible {
		t.Error("expected forcible=true after eviction")
	}
	if val, _, _, _ := d.Get("k1", false); val.Type != shdictTNil {
		t.Error("k1 should have been evicted")
	}
	if val, _, _, _ := d.Get("k4", false); val.Type != shdictTString {
		t.Error("k4 should be present")
	}
}

func TestShdictLRUOrder(t *testing.T) {
	d, _ := newTestDict(700)

	bigVal := strVal(string(make([]byte, 100)))
	mustSet(t, d, "k1", bigVal, 0)
	mustSet(t, d, "k2", bigVal, 0)
	mustSet(t, d, "k3", bigVal, 0)

	// touch k1 so k2 becomes the LRU entry
	d.Get("k1", false)

	ok, _, _ := d.Store(0, "k4", bigVal, 0, 0)
	if !ok {
		t.Fatal("set k4 failed")
	}
	if val, _, _, _ := d.Get("k2", false); val.Type != shdictTNil {
		t.Error("k2 should have been evicted (was LRU)")
	}
	if val, _, _, _ := d.Get("k1", false); val.Type != shdictTString {
		t.Error("k1 should survive (was touched)")
	}
}

func TestShdictSafeSet(t *testing.T) {
	d, _ := newTestDict(700)

	bigVal := strVal(string(make([]byte, 100)))
	mustSet(t, d, "k1", bigVal, 0)
	mustSet(t, d, "k2", bigVal, 0)
	mustSet(t, d, "k3", bigVal, 0)

	ok, errStr, forcible := d.Store(shdictOpSafe, "k4", bigVal, 0, 0)
	if ok || errStr != "no memory" || forcible {
		t.Errorf("safe_set = %v %q forcible=%v, want false no-memory false", ok, errStr, forcible)
	}
	// nothing was evicted
	for _, k := range []string{"k1", "k2", "k3"} {
		if val, _, _, _ := d.Get(k, false); val.Type != shdictTString {
			t.Errorf("%s should still be present", k)
		}
	}
}

func TestShdictNoMemoryTooBig(t *testing.T) {
	d, _ := newTestDict(shdictMinSize)

	huge := strVal(string(make([]byte, shdictMinSize*2)))
	ok, errStr, _ := d.Store(0, "k", huge, 0, 0)
	if ok || errStr != "no memory" {
		t.Errorf("oversized set = %v %q", ok, errStr)
	}
}

func TestShdictHousekeepingEvictsExpired(t *testing.T) {
	d, clock := newTestDict(1 << 20)

	mustSet(t, d, "old1", strVal("v"), 100)
	mustSet(t, d, "old2", strVal("v"), 100)
	clock.advance(200)

	// any set runs evictStep(1), removing up to 2 expired LRU-tail entries
	mustSet(t, d, "new", strVal("v"), 0)

	d.mu.Lock()
	_, ok1 := d.entries["old1"]
	_, ok2 := d.entries["old2"]
	d.mu.Unlock()
	if ok1 || ok2 {
		t.Errorf("expired entries not removed by housekeeping: old1=%v old2=%v", ok1, ok2)
	}
}

func TestShdictAccountingInvariant(t *testing.T) {
	d, clock := newTestDict(1 << 16)

	checkInvariant := func(label string) {
		t.Helper()
		d.mu.Lock()
		used := d.used
		var actual int64
		for _, e := range d.entries {
			actual += e.size
		}
		d.mu.Unlock()
		if used != actual {
			t.Errorf("%s: used=%d but sum of entry sizes=%d", label, used, actual)
		}
		if used < 0 {
			t.Errorf("%s: used went negative: %d", label, used)
		}
	}

	mustSet(t, d, "a", strVal("hello"), 0)
	checkInvariant("after set")
	mustSet(t, d, "a", strVal("a longer replacement value"), 0)
	checkInvariant("after replace")
	d.Store(0, "a", ShdictValue{Type: shdictTNil}, 0, 0)
	checkInvariant("after delete")

	d.Push("list", false, shdictListNode{str: "one"})
	d.Push("list", false, shdictListNode{isNumber: true, num: 2})
	checkInvariant("after pushes")
	d.Pop("list", true)
	checkInvariant("after pop")
	mustSet(t, d, "list2", strVal("scalar"), 0)
	d.Push("list2", false, shdictListNode{str: "x"}) // fails: not a list
	checkInvariant("after failed push")

	mustSet(t, d, "ttl", strVal("v"), 50)
	clock.advance(100)
	d.FlushExpired(0)
	checkInvariant("after flush_expired")

	d.FlushAll()
	checkInvariant("after flush_all")
}

func TestShdictIncr(t *testing.T) {
	d, clock := newTestDict(1 << 20)

	// missing without init
	_, errStr, _ := d.Incr("k", 1, false, 0, 0)
	if errStr != "not found" {
		t.Errorf("incr missing = %q, want not found", errStr)
	}

	// missing with init: value = init + delta
	n, errStr, _ := d.Incr("k", 2, true, 10, 0)
	if errStr != "" || n != 12 {
		t.Errorf("incr init = %v %q, want 12", n, errStr)
	}

	// existing number
	n, _, _ = d.Incr("k", 3, false, 0, 0)
	if n != 15 {
		t.Errorf("incr = %v, want 15", n)
	}

	// non-number value
	mustSet(t, d, "s", strVal("x"), 0)
	_, errStr, _ = d.Incr("s", 1, false, 0, 0)
	if errStr != "not a number" {
		t.Errorf("incr on string = %q, want not a number", errStr)
	}

	// list value
	d.Push("l", false, shdictListNode{str: "x"})
	_, errStr, _ = d.Incr("l", 1, false, 0, 0)
	if errStr != "not a number" {
		t.Errorf("incr on list = %q, want not a number", errStr)
	}

	// init_ttl applies on the create path
	n, _, _ = d.Incr("ttl", 1, true, 0, 5000)
	if n != 1 {
		t.Fatalf("incr init_ttl = %v", n)
	}
	ms, found := d.TTLms("ttl")
	if !found || ms != 5000 {
		t.Errorf("ttl after incr init = %d %v, want 5000", ms, found)
	}

	// init_ttl does NOT apply to in-place increments
	clock.advance(1000)
	d.Incr("ttl", 1, true, 0, 9000)
	ms, _ = d.TTLms("ttl")
	if ms != 4000 {
		t.Errorf("ttl after in-place incr = %d, want 4000 (unchanged expiry)", ms)
	}

	// stale number reuse: flags reset, init_ttl applied
	clock.advance(10000)
	n, errStr, _ = d.Incr("ttl", 5, true, 100, 0)
	if errStr != "" || n != 105 {
		t.Errorf("incr stale reuse = %v %q, want 105", n, errStr)
	}
}

func TestShdictTTLAndExpire(t *testing.T) {
	d, clock := newTestDict(1 << 20)

	// missing
	if _, found := d.TTLms("nope"); found {
		t.Error("ttl on missing should not be found")
	}
	if d.Expire("nope", 1000) {
		t.Error("expire on missing should not be found")
	}

	// never-expires
	mustSet(t, d, "forever", strVal("v"), 0)
	ms, found := d.TTLms("forever")
	if !found || ms != 0 {
		t.Errorf("ttl never-expire = %d %v, want 0 true", ms, found)
	}

	// with ttl
	mustSet(t, d, "k", strVal("v"), 3000)
	ms, _ = d.TTLms("k")
	if ms != 3000 {
		t.Errorf("ttl = %d, want 3000", ms)
	}

	// negative ttl for stale entries
	clock.advance(4000)
	ms, found = d.TTLms("k")
	if !found || ms >= 0 {
		t.Errorf("stale ttl = %d %v, want negative found", ms, found)
	}

	// expire revives a stale entry
	if !d.Expire("k", 2000) {
		t.Error("expire on stale-but-present should succeed")
	}
	val, _, _, _ := d.Get("k", false)
	if val.Type != shdictTString {
		t.Error("revived entry should be readable")
	}

	// expire(key, 0) removes the expiry
	if !d.Expire("k", 0) {
		t.Error("expire 0 failed")
	}
	ms, _ = d.TTLms("k")
	if ms != 0 {
		t.Errorf("ttl after expire 0 = %d, want 0", ms)
	}
}

func TestShdictGetKeys(t *testing.T) {
	d, clock := newTestDict(1 << 20)

	for i := 1; i <= 5; i++ {
		mustSet(t, d, fmt.Sprintf("k%d", i), strVal("v"), 0)
	}
	mustSet(t, d, "expired", strVal("v"), 100)
	clock.advance(200)

	keys := d.GetKeys(0)
	if len(keys) != 5 {
		t.Fatalf("got %d keys, want 5: %v", len(keys), keys)
	}
	// oldest first
	if keys[0] != "k1" || keys[4] != "k5" {
		t.Errorf("key order = %v, want k1..k5", keys)
	}

	keys = d.GetKeys(2)
	if len(keys) != 2 {
		t.Errorf("max_count=2 returned %d keys", len(keys))
	}
}

func TestShdictFlushAll(t *testing.T) {
	d, _ := newTestDict(1 << 20)

	mustSet(t, d, "a", strVal("1"), 0)
	mustSet(t, d, "b", strVal("2"), 0)
	d.FlushAll()

	if val, _, _, _ := d.Get("a", false); val.Type != shdictTNil {
		t.Error("a should be flushed")
	}
	if len(d.GetKeys(0)) != 0 {
		t.Error("get_keys should be empty after flush_all")
	}
}

func TestShdictFlushExpired(t *testing.T) {
	d, clock := newTestDict(1 << 20)

	mustSet(t, d, "e1", strVal("v"), 100)
	mustSet(t, d, "e2", strVal("v"), 100)
	mustSet(t, d, "e3", strVal("v"), 100)
	mustSet(t, d, "live", strVal("v"), 0)
	clock.advance(200)

	if n := d.FlushExpired(2); n != 2 {
		t.Errorf("flush_expired(2) = %d, want 2", n)
	}
	if n := d.FlushExpired(0); n != 1 {
		t.Errorf("flush_expired(0) = %d, want 1", n)
	}
	if val, _, _, _ := d.Get("live", false); val.Type != shdictTString {
		t.Error("live entry should survive flush_expired")
	}
}

func TestShdictLists(t *testing.T) {
	d, clock := newTestDict(1 << 20)

	// llen on missing key is 0
	if n, errStr := d.Llen("l"); n != 0 || errStr != "" {
		t.Errorf("llen missing = %d %q", n, errStr)
	}

	// rpush builds 1,2; lpush prepends 0
	d.Push("l", false, shdictListNode{isNumber: true, num: 1})
	d.Push("l", false, shdictListNode{isNumber: true, num: 2})
	n, errStr, _ := d.Push("l", true, shdictListNode{isNumber: true, num: 0})
	if errStr != "" || n != 3 {
		t.Fatalf("push = %d %q", n, errStr)
	}

	// lpop returns 0, rpop returns 2
	node, found, _ := d.Pop("l", true)
	if !found || !node.isNumber || node.num != 0 {
		t.Errorf("lpop = %+v %v", node, found)
	}
	node, _, _ = d.Pop("l", false)
	if node.num != 2 {
		t.Errorf("rpop = %+v", node)
	}

	// popping the last node deletes the entry
	d.Pop("l", true)
	d.mu.Lock()
	_, exists := d.entries["l"]
	d.mu.Unlock()
	if exists {
		t.Error("entry should be deleted when list empties")
	}

	// pop on missing key
	if _, found, _ := d.Pop("l", true); found {
		t.Error("pop on missing should not be found")
	}

	// type mismatch errors
	mustSet(t, d, "scalar", strVal("v"), 0)
	if _, errStr, _ := d.Push("scalar", false, shdictListNode{str: "x"}); errStr != "value not a list" {
		t.Errorf("push on scalar = %q", errStr)
	}
	if _, _, errStr := d.Pop("scalar", true); errStr != "value not a list" {
		t.Errorf("pop on scalar = %q", errStr)
	}
	if _, errStr := d.Llen("scalar"); errStr != "value not a list" {
		t.Errorf("llen on scalar = %q", errStr)
	}

	d.Push("l2", false, shdictListNode{str: "x"})
	if _, _, _, errStr := d.Get("l2", false); errStr != "value is a list" {
		t.Errorf("get on list = %q", errStr)
	}

	// set over a list destroys it
	mustSet(t, d, "l2", strVal("now scalar"), 0)
	val, _, _, errStr := d.Get("l2", false)
	if errStr != "" || val.Str != "now scalar" {
		t.Errorf("set over list: %+v %q", val, errStr)
	}

	// push onto an expired scalar resets it to a fresh list
	mustSet(t, d, "exp", strVal("v"), 100)
	clock.advance(200)
	n, errStr, _ = d.Push("exp", false, shdictListNode{str: "fresh"})
	if errStr != "" || n != 1 {
		t.Errorf("push on expired scalar = %d %q", n, errStr)
	}

	// push onto an expired list reuses it as a fresh list with no expiry
	d.Push("expl", false, shdictListNode{str: "a"})
	d.Expire("expl", 100)
	clock.advance(200)
	n, errStr, _ = d.Push("expl", false, shdictListNode{str: "b"})
	if errStr != "" || n != 1 {
		t.Errorf("push on expired list = %d %q", n, errStr)
	}
	if ms, found := d.TTLms("expl"); !found || ms != 0 {
		t.Errorf("reused list ttl = %d %v, want 0 (never)", ms, found)
	}
}

func TestShdictListNoMemory(t *testing.T) {
	// head needs 120+1+16 = 137; capacity below that fails head alloc
	d, _ := newTestDict(shdictMinSize)
	d.capacity = 130

	_, errStr, headFail := d.Push("k", false, shdictListNode{str: "v"})
	if errStr != "no memory" || !headFail {
		t.Errorf("head alloc fail = %q headFail=%v", errStr, headFail)
	}

	// head fits but first node doesn't: entry must not linger
	d.capacity = 140
	_, errStr, headFail = d.Push("k", false, shdictListNode{str: string(make([]byte, 100))})
	if errStr != "no memory" || headFail {
		t.Errorf("node alloc fail = %q headFail=%v", errStr, headFail)
	}
	d.mu.Lock()
	_, exists := d.entries["k"]
	used := d.used
	d.mu.Unlock()
	if exists || used != 0 {
		t.Errorf("empty list head should be removed on node alloc failure: exists=%v used=%d", exists, used)
	}
}

func TestShdictCapacityFreeSpace(t *testing.T) {
	d, _ := newTestDict(8192)

	if d.Capacity() != 8192 {
		t.Errorf("capacity = %d", d.Capacity())
	}
	if d.FreeSpace() != 8192 {
		t.Errorf("free_space = %d", d.FreeSpace())
	}
	mustSet(t, d, "k", strVal("hello"), 0)
	want := int64(8192) - (shdictEntryOverhead + 1 + 5)
	if d.FreeSpace() != want {
		t.Errorf("free_space = %d, want %d", d.FreeSpace(), want)
	}
}

func TestRegisterSharedDict(t *testing.T) {
	if err := RegisterSharedDict("", 8192); err == nil {
		t.Error("empty name should fail")
	}
	if err := RegisterSharedDict("tiny_dict_test", 100); err == nil {
		t.Error("too-small capacity should fail")
	}
	if err := RegisterSharedDict("register_test_dict", 8192); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := RegisterSharedDict("register_test_dict", 8192); err == nil {
		t.Error("duplicate name should fail")
	}

	sharedDictsMu.RLock()
	d := sharedDictsByName["register_test_dict"]
	sharedDictsMu.RUnlock()
	if d == nil {
		t.Fatal("dict not in registry")
	}
	found := false
	for id, dd := range registeredSharedDicts() {
		if dd == d {
			if getSharedDictByID(uint64(id)) != d {
				t.Error("getSharedDictByID mismatch")
			}
			found = true
		}
	}
	if !found {
		t.Error("dict not in ID list")
	}
}

func TestParseHumanSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"8192", 8192, false},
		{"64k", 64 * 1024, false},
		{"64K", 64 * 1024, false},
		{"10m", 10 * 1024 * 1024, false},
		{"1G", 1 << 30, false},
		{" 1m ", 1 << 20, false},
		{"", 0, true},
		{"abc", 0, true},
		{"-5m", 0, true},
		{"0", 0, true},
		{"1.5m", 0, true},
		{"m", 0, true},
		{"9223372036854775807k", 0, true}, // overflow
		{"9007199254740993g", 0, true},    // overflow
	}
	for _, c := range cases {
		got, err := ParseHumanSize(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseHumanSize(%q) = %d, want error", c.in, got)
			}
		} else if err != nil || got != c.want {
			t.Errorf("ParseHumanSize(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}
