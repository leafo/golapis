package golapis

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Concurrency tests for shared dicts. Run with -race to verify mutex
// coverage. Three concurrency surfaces are covered:
//   - raw Go goroutines hitting the engine (the future multi-worker model)
//   - Go goroutines concurrent with a Lua state's event loop (CGO bindings)
//   - parallel HTTP requests through a single state's handler

// checkShdictInvariants verifies the dict's internal consistency: LRU list
// and entry map agree, byte accounting matches entry sizes, and usage stays
// within capacity.
func checkShdictInvariants(t *testing.T, d *SharedDict) {
	t.Helper()

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.lru.Len() != len(d.entries) {
		t.Errorf("lru length %d != entries length %d", d.lru.Len(), len(d.entries))
	}

	var sum int64
	for el := d.lru.Front(); el != nil; el = el.Next() {
		e := el.Value.(*shdictEntry)
		if d.entries[e.key] != e {
			t.Errorf("lru entry %q not in entries map (or mismatched)", e.key)
		}
		if e.lruElem != el {
			t.Errorf("entry %q lruElem does not point back to its element", e.key)
		}
		sum += e.size
	}
	if sum != d.used {
		t.Errorf("used=%d but sum of entry sizes=%d", d.used, sum)
	}
	if d.used < 0 {
		t.Errorf("used went negative: %d", d.used)
	}
	if d.used > d.capacity {
		t.Errorf("used %d exceeds capacity %d", d.used, d.capacity)
	}
}

// Concurrent incrs must be atomic: the final count is exact.
func TestShdictConcurrentIncr(t *testing.T) {
	d, _ := newTestDict(1 << 20)

	const goroutines = 16
	const iters = 1000

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				if _, errStr, _ := d.Incr("counter", 1, true, 0, 0); errStr != "" {
					t.Errorf("incr failed: %s", errStr)
					return
				}
			}
		}()
	}
	wg.Wait()

	val, _, _, errStr := d.Get("counter", false)
	if errStr != "" {
		t.Fatalf("get failed: %s", errStr)
	}
	if want := float64(goroutines * iters); val.Num != want {
		t.Errorf("counter = %v, want %v", val.Num, want)
	}
	checkShdictInvariants(t, d)
}

// Mixed operations on a small dict under contention: evictions, type
// conflicts, flushes, and list ops all racing. The dict must stay internally
// consistent.
func TestShdictConcurrentMixedOps(t *testing.T) {
	// small capacity so forced evictions happen constantly
	d, _ := newTestDict(16 << 10)

	const goroutines = 8
	const iters = 500

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			val := strVal(strings.Repeat("x", 200))
			for i := 0; i < iters; i++ {
				// mix of per-worker and deliberately shared keys
				key := fmt.Sprintf("w%d_%d", g, i%20)
				shared := fmt.Sprintf("shared_%d", i%5)

				switch i % 11 {
				case 0:
					d.Store(0, key, val, 0, uint32(i))
				case 1:
					d.Store(0, shared, numVal(float64(i)), int64(i%3)*10, 0)
				case 2:
					d.Get(key, false)
					d.Get(shared, true)
				case 3:
					d.Incr(shared, 1, true, 0, 0)
				case 4:
					d.Push(key+"_list", i%2 == 0, shdictListNode{str: "item"})
				case 5:
					d.Pop(key+"_list", i%2 == 0)
					d.Llen(key + "_list")
				case 6:
					d.Store(0, key, ShdictValue{Type: shdictTNil}, 0, 0)
				case 7:
					d.TTLms(shared)
					d.Expire(shared, int64(i%2)*1000)
				case 8:
					d.GetKeys(10)
					d.FreeSpace()
				case 9:
					d.FlushExpired(2)
				case 10:
					// type conflict on purpose: push onto a scalar key
					d.Push(shared, true, shdictListNode{isNumber: true, num: 1})
					if i%100 == 0 {
						d.FlushAll()
					}
				}
			}
		}(g)
	}
	wg.Wait()

	checkShdictInvariants(t, d)
}

// Go goroutines and a Lua state's event loop incrementing the same dict
// concurrently: the count must be exact across the CGO boundary.
func TestShdictConcurrentLuaAndGo(t *testing.T) {
	d := shdictTestDict(t, "bind_test")

	const counterKey = "lua_go_counter"
	const luaIters = 2000
	const goWorkers = 4
	const goIters = 1000

	d.Store(0, counterKey, ShdictValue{Type: shdictTNil}, 0, 0)

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()
	gls.Start()
	defer gls.Stop()

	var wg sync.WaitGroup
	luaErr := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		luaErr <- gls.RunString(fmt.Sprintf(`
			local d = golapis.shared.bind_test
			for i = 1, %d do
				local n, err = d:incr(%q, 1, 0)
				if not n then error("incr failed: " .. tostring(err)) end
			end
		`, luaIters, counterKey))
	}()

	for g := 0; g < goWorkers; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < goIters; i++ {
				if _, errStr, _ := d.Incr(counterKey, 1, true, 0, 0); errStr != "" {
					t.Errorf("go incr failed: %s", errStr)
					return
				}
			}
		}()
	}

	wg.Wait()
	gls.Wait()
	if err := <-luaErr; err != nil {
		t.Fatalf("lua error: %v", err)
	}

	val, _, _, errStr := d.Get(counterKey, false)
	if errStr != "" {
		t.Fatalf("get failed: %s", errStr)
	}
	if want := float64(luaIters + goWorkers*goIters); val.Num != want {
		t.Errorf("counter = %v, want %v", val.Num, want)
	}
	checkShdictInvariants(t, d)
}

// Parallel HTTP requests incrementing a shared counter: every response must
// see a distinct value (incr is atomic), proving cross-request persistence
// under concurrent load.
func TestShdictHTTPConcurrentRequests(t *testing.T) {
	d := shdictTestDict(t, "bind_test")
	d.Store(0, "http_counter", ShdictValue{Type: shdictTNil}, 0, 0)

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	err := gls.LoadEntryPoint(CodeEntryPoint{Code: `
		local n, err = golapis.shared.bind_test:incr("http_counter", 1, 0)
		if not n then error("incr failed: " .. tostring(err)) end
		golapis.print(n)
	`})
	if err != nil {
		t.Fatalf("load entrypoint: %v", err)
	}

	gls.Start()
	defer gls.Stop()

	srv := httptest.NewServer(gls.HTTPHandler(DefaultHTTPServerConfig()))
	defer srv.Close()

	const requests = 50

	var wg sync.WaitGroup
	results := make(chan int, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(srv.URL)
			if err != nil {
				t.Errorf("request failed: %v", err)
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
				return
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status %d, body: %s", resp.StatusCode, body)
				return
			}
			n, err := strconv.Atoi(strings.TrimSpace(string(body)))
			if err != nil {
				t.Errorf("unexpected body %q", body)
				return
			}
			results <- n
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[int]bool)
	for n := range results {
		if seen[n] {
			t.Errorf("duplicate counter value %d: incr not atomic", n)
		}
		if n < 1 || n > requests {
			t.Errorf("counter value %d out of range 1..%d", n, requests)
		}
		seen[n] = true
	}
	if len(seen) != requests {
		t.Errorf("got %d distinct values, want %d", len(seen), requests)
	}

	val, _, _, _ := d.Get("http_counter", false)
	if val.Num != float64(requests) {
		t.Errorf("final counter = %v, want %d", val.Num, requests)
	}
}
