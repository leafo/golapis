package golapis

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSemaphoreWaitAndPost(t *testing.T) {
	out, err := runLuaCLI(t, `
		local sema = golapis.semaphore.new()
		golapis.say("count ", sema:count())
		local th = golapis.thread.spawn(function()
			golapis.say("waiting")
			local ok, err = sema:wait(1)
			golapis.say("woke ", tostring(ok), " ", tostring(err))
		end)
		golapis.say("count while waiting ", sema:count())
		golapis.sleep(0.01)
		golapis.say("posting")
		assert(sema:post())
		golapis.thread.wait(th)
		golapis.say("count ", sema:count())
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "count 0\nwaiting\ncount while waiting -1\nposting\nwoke true nil\ncount 0\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestSemaphoreFastPathAndTimeout(t *testing.T) {
	out, err := runLuaCLI(t, `
		local sema = golapis.semaphore.new(2)
		golapis.say(tostring(sema:wait(0)), " ", tostring(sema:wait(0)))
		golapis.update_time()
		local start = golapis.now()
		local ok, err = sema:wait(0.02)
		golapis.update_time()
		golapis.say(tostring(ok), " ", err, " ", golapis.now() - start >= 0.015)
		-- a zero timeout never parks
		golapis.say(select(2, sema:wait(0)))
		golapis.say("count ", sema:count())
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "true true\nnil timeout true\ntimeout\ncount 0\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestSemaphorePostWakesInOrder(t *testing.T) {
	out, err := runLuaCLI(t, `
		local sema = golapis.semaphore.new()
		local ths = {}
		for i = 1, 3 do
			ths[i] = golapis.thread.spawn(function()
				assert(sema:wait(1))
				golapis.say("woke ", i)
			end)
		end
		sema:post(2)
		golapis.sleep(0.01)
		golapis.say("count ", sema:count())
		sema:post()
		for i = 1, 3 do golapis.thread.wait(ths[i]) end
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "woke 1\nwoke 2\ncount -1\nwoke 3\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestSemaphoreTimedOutWaiterDoesNotConsumeUnit(t *testing.T) {
	out, err := runLuaCLI(t, `
		local sema = golapis.semaphore.new()
		local ok, err = sema:wait(0.01)
		golapis.say(tostring(ok), " ", err)
		sema:post()
		golapis.say("count ", sema:count())
		golapis.say(tostring(sema:wait(0)))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "nil timeout\ncount 1\ntrue\n" {
		t.Errorf("output = %q", out)
	}
}

func TestSemaphorePostAfterWaiterWokeIgnoresStaleTimeout(t *testing.T) {
	// The waiter's timeout timer still fires after post woke it; it must
	// not resume the coroutine a second time (e.g. while parked elsewhere)
	out, err := runLuaCLI(t, `
		local sema = golapis.semaphore.new()
		local th = golapis.thread.spawn(function()
			assert(sema:wait(0.03))
			-- now parked on something else when the old timer fires
			golapis.update_time()
			local start = golapis.now()
			golapis.sleep(0.05)
			golapis.update_time()
			golapis.say("slept fully ", golapis.now() - start >= 0.045)
		end)
		sema:post()
		golapis.thread.wait(th)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "slept fully true\n" {
		t.Errorf("output = %q", out)
	}
}

func TestSemaphoreKilledWaiterDoesNotConsumeUnit(t *testing.T) {
	out, err := runLuaCLI(t, `
		local sema = golapis.semaphore.new()
		local th = golapis.thread.spawn(function() sema:wait(1) end)
		golapis.say("count ", sema:count())
		golapis.thread.kill(th)
		golapis.say("count ", sema:count())
		sema:post()
		golapis.say("count ", sema:count())
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "count -1\ncount 0\ncount 1\n" {
		t.Errorf("output = %q", out)
	}
}

func TestSemaphoreArgumentErrors(t *testing.T) {
	out, err := runLuaCLI(t, `
		local sema = golapis.semaphore.new()
		local function try(f, ...) local ok, e = pcall(f, ...) golapis.say(tostring(ok), " ", tostring(e):gsub("^.-: ", "")) end
		try(golapis.semaphore.new, -1)
		try(sema.wait, sema)
		try(sema.wait, sema, -1)
		try(sema.wait, sema, math.huge)
		try(sema.post, sema, 0)
		try(sema.wait, {}, 1)
		-- non-numbers coerce like ngx: tonumber(n) or default
		golapis.say(golapis.semaphore.new({}):count(), " ", golapis.semaphore.new("3"):count())
		golapis.say(type(require("ngx.semaphore").new))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	for _, want := range []string{
		"false no negative number",
		"false timeout must be a number",
		"false invalid timeout",
		"false positive number required",
		"false not a semaphore instance",
		"0 3",
		"function",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestSemaphoreAcrossRequests(t *testing.T) {
	// The pub/sub shape: one request parks on a shared semaphore, another
	// request posts to it
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	if err := gls.LoadEntryPoint(CodeEntryPoint{Code: `
		_G.sema = _G.sema or golapis.semaphore.new()
		if golapis.var.request_uri == "/wait" then
			local ok, err = _G.sema:wait(2)
			golapis.say("wait: ", tostring(ok), " ", tostring(err))
		else
			_G.sema:post()
			golapis.say("posted")
		end
	`}); err != nil {
		t.Fatalf("LoadEntryPoint failed: %v", err)
	}
	gls.Start()
	defer gls.Close()
	defer gls.Stop()

	server := httptest.NewServer(gls.HTTPHandler(DefaultHTTPServerConfig()))
	defer server.Close()

	waitResult := make(chan string, 1)
	go func() {
		resp, err := http.Get(server.URL + "/wait")
		if err != nil {
			waitResult <- "error: " + err.Error()
			return
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		waitResult <- string(body)
	}()

	// Make sure the waiter is parked before posting
	time.Sleep(30 * time.Millisecond)
	resp, err := http.Get(server.URL + "/post")
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "posted\n" {
		t.Errorf("post body = %q", body)
	}

	select {
	case got := <-waitResult:
		if got != "wait: true nil\n" {
			t.Errorf("wait body = %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("waiting request never completed")
	}
}

func TestSemaphorePostThenKillKeepsUnit(t *testing.T) {
	// The unit is handed over by the handler, not by post(): a waiter killed
	// in between never consumes it
	out, err := runLuaCLI(t, `
		local sema = golapis.semaphore.new()
		local th = golapis.thread.spawn(function() sema:wait(1) end)
		sema:post()
		golapis.thread.kill(th)
		golapis.say("count ", sema:count(), " ", tostring(sema:wait(0)))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "count 1 true\n" {
		t.Errorf("output = %q", out)
	}
}

func TestSemaphoreWakesManyInOrder(t *testing.T) {
	// More waiters than the event channel can buffer; order must hold
	out, err := runLuaCLI(t, `
		local sema = golapis.semaphore.new()
		local order, ths = {}, {}
		for i = 1, 150 do
			ths[i] = golapis.thread.spawn(function()
				assert(sema:wait(5))
				order[#order + 1] = i
			end)
		end
		sema:post(150)
		for i = 1, 150 do golapis.thread.wait(ths[i]) end
		local ok = #order == 150
		for i = 1, 150 do if order[i] ~= i then ok = false end end
		golapis.say(ok and "in order" or ("out of order: " .. table.concat(order, ",", 1, 10)))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "in order\n" {
		t.Errorf("output = %q", out)
	}
}

func TestSemaphoreWokenWaiterDropsTimer(t *testing.T) {
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()
	gls.Start()
	defer gls.Stop()

	err := gls.RunString(`
		local sema = golapis.semaphore.new()
		for i = 1, 20 do
			local th = golapis.thread.spawn(function() assert(sema:wait(60)) end)
			sema:post()
			golapis.thread.wait(th)
		end
	`)
	gls.Wait()
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	gls.timerMu.Lock()
	n := gls.timerSched.heap.Len()
	gls.timerMu.Unlock()
	if n != 0 {
		t.Errorf("%d stale timeout entries left in the timer heap", n)
	}
}

func TestSemaphoreAbortedRequestLeavesNoWaiter(t *testing.T) {
	// A coroutine parked in wait() whose request is aborted (entry error)
	// must be removed from the shared semaphore's queue right away
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()
	gls.Start()
	defer gls.Stop()

	if err := gls.RunString(`
		_G.sema = golapis.semaphore.new()
		golapis.thread.spawn(function() _G.sema:wait(60) end)
		error("abort")
	`); err == nil || !strings.Contains(err.Error(), "abort") {
		t.Fatalf("expected abort error, got %v", err)
	}
	gls.Wait()

	if err := gls.RunString(`assert(_G.sema:count() == 0, "count " .. _G.sema:count())`); err != nil {
		t.Fatal(err)
	}
}
