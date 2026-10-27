package golapis

import (
	"bytes"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// startEchoServer returns the address of a TCP echo server that lives until
// the test ends.
func startEchoServer(t *testing.T) *net.TCPAddr {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				buf := make([]byte, 4096)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					if _, err := conn.Write(buf[:n]); err != nil {
						return
					}
				}
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr)
}

// multiStateScript exercises every registry-backed path at once: light
// threads, timers, sleeps and cosockets. It runs `rounds` rounds across
// `threads` light threads and prints a single summary line.
func multiStateScript(addr *net.TCPAddr, threads, rounds int) string {
	return `
		local host, port = "` + addr.IP.String() + `", ` + itoa(addr.Port) + `
		local threads, rounds = ` + itoa(threads) + `, ` + itoa(rounds) + `
		local echoed, timers = 0, 0
		local ths = {}
		for i = 1, threads do
			ths[i] = golapis.thread.spawn(function()
				for r = 1, rounds do
					local sock = golapis.socket.tcp()
					sock:settimeout(1000)
					assert(sock:connect(host, port))
					local msg = "t" .. i .. "r" .. r
					assert(sock:send(msg .. "\n"))
					local line = assert(sock:receive("*l"))
					assert(line == msg, "bad echo: " .. tostring(line))
					sock:close()
					golapis.sleep(0.001)
					echoed = echoed + 1
				end
			end)
		end
		local fired = golapis.semaphore.new()
		for i = 1, threads do
			golapis.timer.at(0.001 * i, function(premature)
				if not premature then timers = timers + 1 end
				fired:post()
			end)
		end
		for i = 1, threads do
			assert(golapis.thread.wait(ths[i]))
		end
		for i = 1, threads do
			assert(fired:wait(1))
		end
		golapis.say("echoed ", echoed, " timers ", timers)
	`
}

func runStateScript(t *testing.T, gls *GolapisLuaState, code string) string {
	t.Helper()
	var out bytes.Buffer
	gls.SetOutputWriter(&out)
	if err := gls.RunString(code); err != nil {
		t.Errorf("Lua error: %v", err)
	}
	gls.Wait()
	return out.String()
}

func newStartedState(t *testing.T) *GolapisLuaState {
	t.Helper()
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	gls.Start()
	return gls
}

// Sanity check for the shared script on a single state.
func TestMultiStateScriptSingle(t *testing.T) {
	addr := startEchoServer(t)
	gls := newStartedState(t)
	defer gls.Close()
	out := runStateScript(t, gls, multiStateScript(addr, 4, 10))
	if out != "echoed 40 timers 4\n" {
		t.Errorf("output = %q", out)
	}
}

// Two states must be able to run concurrently on separate goroutines without
// racing on any package-level registry.
func TestMultiStateConcurrentRun(t *testing.T) {
	addr := startEchoServer(t)
	const threads, rounds = 4, 10
	want := "echoed 40 timers 4\n"

	var wg sync.WaitGroup
	outs := make([]string, 3)
	for i := range outs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			gls := newStartedState(t)
			defer gls.Close()
			outs[i] = runStateScript(t, gls, multiStateScript(addr, threads, rounds))
		}(i)
	}
	wg.Wait()
	for i, out := range outs {
		if out != want {
			t.Errorf("state %d output = %q, want %q", i, out, want)
		}
	}
}

// Creating and closing states while another state is busy must not disturb
// the busy one, and the survivor must still work after the others are gone.
func TestMultiStateCreateCloseWhileActive(t *testing.T) {
	addr := startEchoServer(t)

	survivor := newStartedState(t)
	defer survivor.Close()

	busyDone := make(chan string, 1)
	go func() {
		busyDone <- runStateScript(t, survivor, multiStateScript(addr, 4, 60))
	}()

	// Churn short-lived states while the survivor is mid-run.
	deadline := time.Now().Add(300 * time.Millisecond)
	churned := 0
	for time.Now().Before(deadline) {
		gls := newStartedState(t)
		out := runStateScript(t, gls, multiStateScript(addr, 2, 2))
		gls.Close()
		if out != "echoed 4 timers 2\n" {
			t.Errorf("churned state output = %q", out)
		}
		churned++
	}
	if churned == 0 {
		t.Fatal("no states were churned while the survivor was active")
	}

	select {
	case out := <-busyDone:
		if out != "echoed 240 timers 4\n" {
			t.Errorf("survivor output = %q", out)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("survivor did not finish")
	}

	// The survivor must still be fully functional after the churn.
	out := runStateScript(t, survivor, multiStateScript(addr, 2, 3))
	if !strings.HasPrefix(out, "echoed 6 timers 2") {
		t.Errorf("survivor post-churn output = %q", out)
	}
}

// A state closed while it still has parked threads and pending timers must
// not touch a sibling state that keeps running.
func TestMultiStateCloseWithPendingWork(t *testing.T) {
	addr := startEchoServer(t)

	survivor := newStartedState(t)
	defer survivor.Close()
	busyDone := make(chan string, 1)
	go func() {
		busyDone <- runStateScript(t, survivor, multiStateScript(addr, 4, 40))
	}()

	for i := 0; i < 5; i++ {
		victim := newStartedState(t)
		var out bytes.Buffer
		victim.SetOutputWriter(&out)
		// Park threads on sleep and leave timers pending, then close the
		// state out from under them. RunString must come back promptly
		// with the stop error rather than waiting out the sleeps.
		runErr := make(chan error, 1)
		go func() {
			runErr <- victim.RunString(`
				for i = 1, 4 do
					golapis.thread.spawn(function() golapis.sleep(10) end)
					golapis.timer.at(10, function() end)
				end
				golapis.sleep(10)
			`)
		}()
		time.Sleep(10 * time.Millisecond)
		start := time.Now()
		victim.Close()
		select {
		case err := <-runErr:
			if err == nil || !strings.Contains(err.Error(), "stopped") {
				t.Errorf("victim %d RunString error = %v, want stop error", i, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("victim %d RunString did not return after Close", i)
		}
		if took := time.Since(start); took > time.Second {
			t.Errorf("victim %d Close took %v", i, took)
		}
	}

	select {
	case out := <-busyDone:
		if out != "echoed 160 timers 4\n" {
			t.Errorf("survivor output = %q", out)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("survivor did not finish")
	}
}
