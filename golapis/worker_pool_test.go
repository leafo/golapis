package golapis

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestPool(t *testing.T, workers int, code string) *WorkerPool {
	t.Helper()
	config := DefaultHTTPServerConfig()
	config.Workers = workers
	pool, err := NewWorkerPool(CodeEntryPoint{Code: code}, config)
	if err != nil {
		t.Fatalf("NewWorkerPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// fetch returns the body of a 200 response, or an error. Safe to call from
// any goroutine; getBody wraps it with t.Fatalf for the test goroutine.
func fetch(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GET %s: status %d body %q", url, resp.StatusCode, body)
	}
	return string(body), nil
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	body, err := fetch(url)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// Every worker must serve traffic and report its own id.
func TestWorkerPoolRoundRobin(t *testing.T) {
	const workers = 3
	pool := newTestPool(t, workers, `golapis.say(golapis.worker.id(), " ", golapis.worker.count())`)
	server := httptest.NewServer(pool.Handler())
	defer server.Close()

	seen := map[string]int{}
	for i := 0; i < workers*4; i++ {
		seen[getBody(t, server.URL)]++
	}
	for id := 0; id < workers; id++ {
		want := strconv.Itoa(id) + " " + strconv.Itoa(workers) + "\n"
		if seen[want] != 4 {
			t.Errorf("worker %d served %d requests, want 4 (seen %v)", id, seen[want], seen)
		}
	}
}

// Concurrent requests across workers, run under -race in CI.
func TestWorkerPoolConcurrent(t *testing.T) {
	pool := newTestPool(t, 4, `
		golapis.sleep(0.001)
		golapis.say("w", golapis.worker.id())
	`)
	server := httptest.NewServer(pool.Handler())
	defer server.Close()

	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}
	var errs []error
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := fetch(server.URL)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			counts[strings.TrimSpace(body)]++
		}()
	}
	wg.Wait()
	for _, err := range errs {
		t.Error(err)
	}
	if len(counts) != 4 {
		t.Errorf("expected 4 workers to serve requests, got %v", counts)
	}
}

// A location.capture subrequest runs on the worker that issued it.
func TestWorkerPoolCaptureStaysOnWorker(t *testing.T) {
	pool := newTestPool(t, 3, `
		if golapis.var.request_uri == "/sub" then
			golapis.say(golapis.worker.id())
			return
		end
		local res = golapis.location.capture("/sub")
		golapis.say(golapis.worker.id(), " ", res.body)
	`)
	server := httptest.NewServer(pool.Handler())
	defer server.Close()

	for i := 0; i < 9; i++ {
		body := getBody(t, server.URL+"/")
		parts := strings.Fields(body)
		if len(parts) != 2 || parts[0] != parts[1] {
			t.Errorf("subrequest crossed workers: %q", body)
		}
	}
}

// A single state created directly reports worker 0 of 1.
func TestWorkerInfoDefault(t *testing.T) {
	out, err := runLuaCLI(t, `golapis.say(golapis.worker.id(), " ", golapis.worker.count(), " ", golapis.worker.pid() > 0)`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "0 1 true\n" {
		t.Errorf("output = %q", out)
	}
}

// exiting() flips on BeginShutdown and requests after Close get a 503.
func TestWorkerPoolShutdown(t *testing.T) {
	pool := newTestPool(t, 2, `golapis.say(tostring(golapis.worker.exiting()))`)
	server := httptest.NewServer(pool.Handler())
	defer server.Close()

	if body := getBody(t, server.URL); body != "false\n" {
		t.Errorf("before shutdown: %q", body)
	}
	pool.BeginShutdown()
	if body := getBody(t, server.URL); body != "true\n" {
		t.Errorf("after BeginShutdown: %q", body)
	}
	pool.Close()
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("after Close: status %d, want 503", resp.StatusCode)
	}
}

// A request dispatched onto a worker whose loop has already stopped must
// get a 503, not hang: the Pick/Stop race the pool cannot close itself.
func TestWorkerStoppedStateReturns503(t *testing.T) {
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()
	// A module-level counter shows whether a rejected request ever ran.
	if err := gls.LoadEntryPoint(CodeEntryPoint{Code: `served = (served or 0) + 1; golapis.say(served)`}); err != nil {
		t.Fatal(err)
	}
	gls.Start()
	handler := gls.HTTPHandler(nil)
	gls.Stop()

	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		done <- w.Code
	}()
	select {
	case code := <-done:
		if code != http.StatusServiceUnavailable {
			t.Errorf("status %d, want 503", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request on stopped state hung")
	}

	// Restarting clears exiting() and must not replay the rejected request:
	// the first request served after restart is number one.
	gls.Start()
	w := httptest.NewRecorder()
	gls.HTTPHandler(nil).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || w.Body.String() != "1\n" {
		t.Errorf("after restart: %d %q", w.Code, w.Body.String())
	}
	out, err := runOnState(gls, `golapis.say(tostring(golapis.worker.exiting()))`)
	if err != nil || out != "false\n" {
		t.Errorf("exiting after restart = %q, %v", out, err)
	}
}

func runOnState(gls *GolapisLuaState, code string) (string, error) {
	var out bytes.Buffer
	gls.SetOutputWriter(&out)
	err := gls.RunString(code)
	gls.Wait()
	return out.String(), err
}

// The init hook runs once per worker before any request, sees the worker
// API, and timers it starts keep running.
func TestWorkerPoolInitWorker(t *testing.T) {
	config := DefaultHTTPServerConfig()
	config.Workers = 3
	config.InitWorker = CodeEntryPoint{Code: `
		inits = (inits or 0) + 1
		init_worker = golapis.worker.id()
		init_phase = golapis.get_phase()
		golapis.timer.at(0.01, function() ticked = golapis.get_phase() end)
	`}
	pool, err := NewWorkerPool(CodeEntryPoint{Code: `
		golapis.say(inits, " ", tostring(init_worker == golapis.worker.id()), " ", tostring(ticked),
			" ", init_phase, " ", golapis.get_phase())
	`}, config)
	if err != nil {
		t.Fatalf("NewWorkerPool: %v", err)
	}
	defer pool.Close()
	server := httptest.NewServer(pool.Handler())
	defer server.Close()

	// Poll until every worker reports its init timer fired; six requests in
	// a row cover each of the three workers twice.
	deadline := time.Now().Add(2 * time.Second)
	for {
		ok := true
		for i := 0; i < 6; i++ {
			if getBody(t, server.URL) != "1 true timer init_worker content\n" {
				ok = false
			}
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("workers never reached init state: %q", getBody(t, server.URL))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// An init hook that fails aborts pool creation with the worker named.
func TestWorkerPoolInitWorkerError(t *testing.T) {
	config := DefaultHTTPServerConfig()
	config.Workers = 2
	config.InitWorker = CodeEntryPoint{Code: `error("boom")`}
	pool, err := NewWorkerPool(CodeEntryPoint{Code: `golapis.say("x")`}, config)
	if err == nil {
		pool.Close()
		t.Fatal("expected init error")
	}
	if !regexp.MustCompile(`^worker [01]: init worker .*boom`).MatchString(err.Error()) {
		t.Errorf("error = %v", err)
	}
}

// A recurring timer started from init must not pin shutdown: on
// BeginShutdown it fires prematurely, cannot reschedule, and Wait returns.
func TestWorkerPoolShutdownWindsDownTimers(t *testing.T) {
	config := DefaultHTTPServerConfig()
	config.Workers = 2
	config.InitWorker = CodeEntryPoint{Code: `
		-- Either the premature fire or a normal fire racing with shutdown
		-- ends the chain: both get "process exiting" from timer.at.
		local function tick(premature)
			if premature then
				premature_seen = true
				-- zero-delay timers are still allowed while exiting
				zero_ok = golapis.timer.at(0, function() end)
			end
			local ok, err = golapis.timer.at(0.001, tick)
			if not ok then resched = err end
		end
		assert(golapis.timer.at(0.001, tick))
	`}
	pool, err := NewWorkerPool(CodeEntryPoint{Code: `golapis.say(tostring(premature_seen), " ", tostring(zero_ok), " ", tostring(resched))`}, config)
	if err != nil {
		t.Fatalf("NewWorkerPool: %v", err)
	}
	defer pool.Close()

	time.Sleep(20 * time.Millisecond) // let the timers recur a few times
	pool.BeginShutdown()
	done := make(chan struct{})
	go func() { pool.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after BeginShutdown with a recurring timer")
	}
	server := httptest.NewServer(pool.Handler())
	defer server.Close()
	for i := 0; i < 2; i++ {
		// premature path: fired early and a zero-delay timer was accepted;
		// normal fire racing shutdown: neither, but the chain still ended
		body := getBody(t, server.URL)
		if body != "true true process exiting\n" && body != "nil nil process exiting\n" {
			t.Errorf("worker state after shutdown: %q", body)
		}
	}
}

// One worker's init error must not wait for another worker's long init:
// the slow one is interrupted and the fast error is the one reported.
func TestWorkerPoolInitErrorCancelsSlowInit(t *testing.T) {
	config := DefaultHTTPServerConfig()
	config.Workers = 2
	config.InitWorker = CodeEntryPoint{Code: `
		if golapis.worker.id() == 0 then error("fast failure") end
		golapis.sleep(5)
	`}
	start := time.Now()
	pool, err := NewWorkerPool(CodeEntryPoint{Code: `golapis.say("x")`}, config)
	if err == nil {
		pool.Close()
		t.Fatal("expected init error")
	}
	if !strings.Contains(err.Error(), "worker 0: ") || !strings.Contains(err.Error(), "fast failure") {
		t.Errorf("error = %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("startup failure took %v, slow init was not interrupted", took)
	}
}
