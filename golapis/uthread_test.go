package golapis

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// runLuaCLI runs code as a CLI-mode script and returns its output
func runLuaCLI(t *testing.T, code string) (string, error) {
	t.Helper()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	var out bytes.Buffer
	gls.SetOutputWriter(&out)
	gls.Start()
	defer gls.Stop()

	err := gls.RunString(code)
	gls.Wait()
	return out.String(), err
}

func TestThreadSpawnRunsChildFirst(t *testing.T) {
	out, err := runLuaCLI(t, `
		golapis.say("before")
		local th = golapis.thread.spawn(function(a, b)
			golapis.say("child ", a, b)
			golapis.sleep(0.01)
			golapis.say("child after sleep")
			return "done", 42
		end, "x", "y")
		golapis.say("parent continues: ", type(th))
		local ok, v1, v2 = golapis.thread.wait(th)
		golapis.say("wait: ", tostring(ok), " ", v1, " ", v2)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "before\nchild xy\nparent continues: thread\nchild after sleep\nwait: true done 42\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestThreadWaitCollectsZombie(t *testing.T) {
	// The child finishes before the parent calls wait: results are kept
	out, err := runLuaCLI(t, `
		local th = golapis.thread.spawn(function() return 1, nil, 3 end)
		golapis.say(coroutine.status(th))
		golapis.say(select("#", golapis.thread.wait(th)), " ", table.concat({tostring(select(1, golapis.thread.wait(th)))}, ","))
	`)
	// second wait on the same thread must report it was already collected
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.HasPrefix(out, "zombie\n") {
		t.Errorf("output = %q", out)
	}
	if !strings.Contains(out, "4 nil") {
		// 4 values: true, 1, nil, 3; the second wait returns nil, err
		t.Errorf("output = %q", out)
	}
}

func TestThreadsSleepConcurrently(t *testing.T) {
	// Measured inside Lua, from just before the spawns, so state startup
	// doesn't count; sequential sleeps would take >= 150ms
	out, err := runLuaCLI(t, `
		local function worker(name)
			golapis.sleep(0.05)
			return name
		end
		golapis.update_time()
		local start = golapis.now()
		local a = golapis.thread.spawn(worker, "a")
		local b = golapis.thread.spawn(worker, "b")
		local c = golapis.thread.spawn(worker, "c")
		local _, ra = golapis.thread.wait(a)
		local _, rb = golapis.thread.wait(b)
		local _, rc = golapis.thread.wait(c)
		golapis.update_time()
		golapis.say(ra, rb, rc, " ", golapis.now() - start < 0.14)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "abc true\n" {
		t.Errorf("output = %q (false means the sleeps ran sequentially)", out)
	}
}

func TestThreadWaitFirstOfMany(t *testing.T) {
	out, err := runLuaCLI(t, `
		local slow = golapis.thread.spawn(function() golapis.sleep(0.1) return "slow" end)
		local fast = golapis.thread.spawn(function() golapis.sleep(0.01) return "fast" end)
		local ok, who = golapis.thread.wait(slow, fast)
		golapis.say("first: ", who)
		local ok2, who2 = golapis.thread.wait(slow)
		golapis.say("then: ", who2)
		-- fast was collected by the first wait
		local r, e = golapis.thread.wait(fast)
		golapis.say(tostring(r), " ", e)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "first: fast\nthen: slow\nnil already waited or killed\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestThreadErrorReportedToWait(t *testing.T) {
	out, err := runLuaCLI(t, `
		local th = golapis.thread.spawn(function()
			golapis.sleep(0.01)
			error("boom")
		end)
		local ok, msg = golapis.thread.wait(th)
		golapis.say(tostring(ok), " ", msg:match("boom") or msg)
		golapis.say("parent survives")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "false boom\nparent survives\n" {
		t.Errorf("output = %q", out)
	}
}

func TestThreadKill(t *testing.T) {
	out, err := runLuaCLI(t, `
		local ran = false
		local th = golapis.thread.spawn(function()
			golapis.sleep(0.05)
			ran = true
		end)
		assert(golapis.thread.kill(th) == 1)
		local r, e = golapis.thread.kill(th)
		golapis.say(tostring(r), " ", e)
		golapis.sleep(0.1)
		golapis.say("ran: ", tostring(ran))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "nil already waited or killed\nran: false\n" {
		t.Errorf("output = %q", out)
	}
}

func TestThreadOutlivesEntry(t *testing.T) {
	// The request only completes once the light thread is done, and its
	// output after the entry returned still reaches the response
	w, _, err := runLuaWithHTTP(t, `
		golapis.thread.spawn(function()
			golapis.sleep(0.02)
			golapis.say("from thread")
		end)
		golapis.say("entry done")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if got := w.Body.String(); got != "entry done\nfrom thread\n" {
		t.Errorf("body = %q", got)
	}
}

func TestThreadSchedulerYield(t *testing.T) {
	out, err := runLuaCLI(t, `
		local th = golapis.thread.spawn(function()
			golapis.say("child start")
			coroutine.yield()
			golapis.say("child after yield")
		end)
		golapis.say("parent")
		coroutine.yield()
		golapis.say("parent after yield")
		golapis.thread.wait(th)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "child start\nparent\nchild after yield\nparent after yield\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestThreadMisuse(t *testing.T) {
	out, err := runLuaCLI(t, `
		local co = coroutine.create(function() end)
		local ok, e = pcall(golapis.thread.wait, co)
		golapis.say(tostring(ok), " ", e)

		local th = golapis.thread.spawn(function() golapis.sleep(0.01) end)
		local ok2, e2 = coroutine.resume(th)
		golapis.say(tostring(ok2), " ", e2)

		-- only the spawner may wait
		local inner = golapis.thread.spawn(function()
			local ok3, e3 = pcall(golapis.thread.wait, th)
			golapis.say(tostring(ok3), " ", e3)
		end)
		golapis.thread.wait(inner)
		golapis.thread.wait(th)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	for _, want := range []string{
		"false attempt to wait on a coroutine that is not a user thread",
		"false attempt to resume a light thread",
		"false only the parent coroutine can wait on the thread",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestThreadExitEndsRequest(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.thread.spawn(function()
			golapis.say("thread")
			golapis.exit(200)
		end)
		golapis.say("never reached")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if got := w.Body.String(); got != "thread\n" {
		t.Errorf("body = %q", got)
	}
}

func TestThreadWithRawSocket(t *testing.T) {
	// The websocket shape: the entry coroutine is parked reading the client
	// while a light thread pushes data on a timer
	_, server := startRawSocketServer(t, `
		golapis.status = 101
		golapis.send_headers()
		local sock = assert(golapis.req.socket(true))

		local ticker = golapis.thread.spawn(function()
			for i = 1, 3 do
				golapis.sleep(0.01)
				sock:send("tick " .. i .. "\n")
			end
		end)

		local line = assert(sock:receive("*l"))
		golapis.thread.wait(ticker)
		sock:send("got " .. line .. "\n")
	`)

	conn, reader, _ := dialRaw(t, server, "")
	for i := 1; i <= 3; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		if !strings.HasPrefix(line, "tick ") {
			t.Fatalf("line = %q", line)
		}
	}
	io.WriteString(conn, "hello\n")
	if line, _ := reader.ReadString('\n'); line != "got hello\n" {
		t.Errorf("line = %q", line)
	}
}

func TestThreadKillWhileResumingCoroutine(t *testing.T) {
	// Killing a light thread that is inside coroutine.resume of a plain
	// coroutine parked on an async op must not let that coroutine finish
	// and resume the dead thread
	out, err := runLuaCLI(t, `
		local th = golapis.thread.spawn(function()
			local co = coroutine.create(function() golapis.sleep(0.02) end)
			coroutine.resume(co)
			golapis.say("never: thread resumed after kill")
		end)
		assert(golapis.thread.kill(th) == 1)
		golapis.sleep(0.05)
		golapis.say("entry done")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "entry done\n" {
		t.Errorf("output = %q", out)
	}
}

func TestThreadZombieWithAbandonedParentCompletes(t *testing.T) {
	// A light thread spawned from a plain coroutine that is never resumed
	// again has no one to collect it; the request must still complete
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			golapis.thread.spawn(function() return 1 end)
			coroutine.yield()
		end)
		coroutine.resume(co)
		golapis.say("entry done")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if got := w.Body.String(); got != "entry done\n" {
		t.Errorf("body = %q", got)
	}
}

func TestThreadSpawnYieldAcrossCBoundary(t *testing.T) {
	// spawn inside a gsub callback can't yield; the error is caught by
	// pcall and must leave the spawner's scheduling state untouched
	out, err := runLuaCLI(t, `
		local ok, e = pcall(string.gsub, "a", "a", function()
			golapis.thread.spawn(function() golapis.say("child must not run") end)
			return "b"
		end)
		golapis.say(tostring(ok), " ", tostring(e):match("C%-call boundary") and "boundary" or e)
		golapis.update_time()
		local start = golapis.now()
		golapis.sleep(0.03)
		golapis.update_time()
		golapis.say("slept ", golapis.now() - start >= 0.025)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "false boundary\nslept true\n" {
		t.Errorf("output = %q", out)
	}
}

func TestThreadCannotResumeAsyncParkedCoroutine(t *testing.T) {
	// A light thread must not be able to coroutine.resume() a plain
	// coroutine that is parked on an async op
	out, err := runLuaCLI(t, `
		local co = coroutine.create(function()
			golapis.sleep(0.03)
			return "finished"
		end)
		local th = golapis.thread.spawn(function()
			coroutine.resume(co) -- parks co on sleep
		end)
		local ok, e = coroutine.resume(co)
		golapis.say(tostring(ok), " ", e)
		golapis.thread.wait(th)
		golapis.sleep(0.05)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "false cannot resume non-suspended coroutine\n" {
		t.Errorf("output = %q", out)
	}
}

func TestThreadSchedulerYieldAcrossCBoundary(t *testing.T) {
	// A scheduler yield that can't happen must not leave coOp set for the
	// next (unrelated) async yield
	out, err := runLuaCLI(t, `
		local ok, e = pcall(string.gsub, "a", "a", function()
			coroutine.yield()
			return "b"
		end)
		golapis.say(tostring(ok), " ", tostring(e):match("C%-call boundary") and "boundary" or e)
		golapis.update_time()
		local start = golapis.now()
		golapis.sleep(0.04)
		golapis.update_time()
		golapis.say("slept ", golapis.now() - start >= 0.035)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "false boundary\nslept true\n" {
		t.Errorf("output = %q", out)
	}
}

func TestThreadKillCancelsPendingRead(t *testing.T) {
	// Killing a thread parked on a socket read must not leave that read
	// running: as in nginx, the socket is finalized, so a later reader
	// sees "closed" rather than "socket busy reading" (or lost bytes)
	_, server := startRawSocketServer(t, `
		golapis.status = 101
		golapis.send_headers()
		local sock = assert(golapis.req.socket(true))
		local reader = golapis.thread.spawn(function()
			sock:receive("*l")
			golapis.log(golapis.ERR, "killed reader must not get here")
		end)
		assert(golapis.thread.kill(reader) == 1)
		local data, err = sock:receive("*l")
		golapis.log(golapis.INFO, "after kill: ", tostring(data), " ", tostring(err))
		assert(data == nil and err == "closed", tostring(err))
	`)

	conn, reader, _ := dialRaw(t, server, "")
	io.WriteString(conn, "late line\n")
	// The server closed the connection when the thread was killed (EOF, or
	// a reset since our unread line was still buffered on its side)
	if _, err := reader.ReadByte(); err == nil {
		t.Error("expected the server to have closed the socket")
	}
}

func TestThreadKillCancelsNestedCoroutineRead(t *testing.T) {
	// The pending read belongs to a plain coroutine the light thread
	// resumed; killing the thread must cancel that too
	_, server := startRawSocketServer(t, `
		golapis.status = 101
		golapis.send_headers()
		local sock = assert(golapis.req.socket(true))
		local reader = golapis.thread.spawn(function()
			local co = coroutine.create(function() sock:receive("*l") end)
			coroutine.resume(co)
		end)
		assert(golapis.thread.kill(reader) == 1)
		local data, err = sock:receive("*l")
		assert(data == nil and err == "closed", tostring(err))
	`)

	conn, reader, _ := dialRaw(t, server, "")
	io.WriteString(conn, "late line\n")
	if _, err := reader.ReadByte(); err == nil {
		t.Error("expected the server to have closed the socket")
	}
}
