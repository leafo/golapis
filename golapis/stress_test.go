package golapis

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Stress tests for light threads, semaphores and raw sockets under load.
// Skipped unless STRESS=1. STRESS_N scales the connection/thread counts and
// STRESS_WAVES repeats the websocket run in one state (watch the Lua heap
// between waves for leaks). The websocket test needs the stock
// lua-resty-websocket library: STRESS_RESTY_LIB=/path/to/lua-resty-websocket/lib

func stressN(t *testing.T, def int) int {
	if os.Getenv("STRESS") == "" {
		t.Skip("set STRESS=1")
	}
	if v := os.Getenv("STRESS_N"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			t.Fatalf("bad STRESS_N %q", v)
		}
		return n
	}
	return def
}

// ---- minimal websocket client ----

type wsConn struct {
	c net.Conn
	r *bufio.Reader
}

func wsDial(addr string) (*wsConn, error) {
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(60 * time.Second))
	fmt.Fprintf(c, "GET /ws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	r := bufio.NewReader(c)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		c.Close()
		return nil, err
	}
	if resp.StatusCode != 101 {
		c.Close()
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return &wsConn{c: c, r: r}, nil
}

func (w *wsConn) send(op byte, payload []byte) error {
	var mask [4]byte
	rand.Read(mask[:])
	hdr := []byte{0x80 | op}
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n < 65536:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	buf := append(hdr, mask[:]...)
	for i, b := range payload {
		buf = append(buf, b^mask[i%4])
	}
	_, err := w.c.Write(buf)
	return err
}

func (w *wsConn) recv() (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(w.r, h[:]); err != nil {
		return 0, nil, err
	}
	op := h[0] & 0xf
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(w.r, b[:]); err != nil {
			return 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err := io.ReadFull(w.r, b[:]); err != nil {
			return 0, nil, err
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(w.r, p); err != nil {
		return 0, nil, err
	}
	return op, p, nil
}

// stressClient bounds every request so a server-side stall fails the test
// with its logs instead of hanging until go test's timeout
var stressClient = &http.Client{Timeout: 10 * time.Second}

// ---- leak checks ----

// checkNoLeaks runs once the server is closed and gls.Wait() has returned,
// so nothing registers threads concurrently with the map read below
func checkNoLeaks(t *testing.T, gls *GolapisLuaState, baselineGoroutines int) {
	t.Helper()
	gls.Wait()

	threads := len(gls.liveThreads())
	if threads != 0 {
		t.Errorf("%d LuaThreads still registered", threads)
	}

	gls.hijacked.mu.Lock()
	hc := len(gls.hijacked.conns)
	gls.hijacked.mu.Unlock()
	if hc != 0 {
		t.Errorf("%d hijacked connections still tracked", hc)
	}

	gls.timerMu.Lock()
	th := gls.timerSched.heap.Len()
	gls.timerMu.Unlock()
	if th != 0 {
		t.Errorf("%d entries left in timer heap", th)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && runtime.NumGoroutine() > baselineGoroutines+5 {
		time.Sleep(50 * time.Millisecond)
	}
	if g := runtime.NumGoroutine(); g > baselineGoroutines+5 {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Errorf("goroutines: %d (baseline %d)\n%s", g, baselineGoroutines, buf[:n])
	}
}

func luaMemKB(t *testing.T, gls *GolapisLuaState) float64 {
	t.Helper()
	var out strings.Builder
	gls.SetOutputWriter(&out)
	err := gls.RunString(`collectgarbage("collect") collectgarbage("collect") golapis.say(collectgarbage("count"))`)
	gls.SetOutputWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(out.String()), 64)
	if err != nil {
		t.Fatalf("bad collectgarbage output %q", out.String())
	}
	return v
}

// ---- tests ----

const pubsubScript = `
local server = require "resty.websocket.server"
local semaphore = require "ngx.semaphore"

_G.subs = _G.subs or {}
_G.nsubs = _G.nsubs or 0
_G.published = _G.published or 0

local uri = ngx.var.request_uri
if uri:match("^/publish") then
  local msg = ngx.req.get_uri_args().msg
  local n = 0
  for sub in pairs(_G.subs) do
    sub.queue[#sub.queue + 1] = msg
    sub.sema:post()
    n = n + 1
  end
  _G.published = _G.published + 1
  ngx.say("published to ", n)
  return
elseif uri:match("^/stats") then
  ngx.say(_G.nsubs, " ", _G.published)
  return
end

local wb, err = server:new{ timeout = 30000 }
if not wb then ngx.log(ngx.ERR, "ws: ", err) return ngx.exit(444) end

local sub = { sema = semaphore.new(), queue = {} }
_G.subs[sub] = true
_G.nsubs = _G.nsubs + 1

local writer = ngx.thread.spawn(function()
  while true do
    local ok, err = sub.sema:wait(10)
    if not ok and err ~= "timeout" then return end
    while #sub.queue > 0 do
      local m = table.remove(sub.queue, 1)
      local bytes, err = wb:send_text(m)
      if not bytes then return end
    end
  end
end)

while true do
  local data, typ, err = wb:recv_frame()
  if not data or typ == "close" then break end
  if typ == "text" then
    sub.queue[#sub.queue + 1] = "echo: " .. data
    sub.sema:post()
  elseif typ == "ping" then
    wb:send_pong(data)
  end
end
_G.subs[sub] = nil
_G.nsubs = _G.nsubs - 1
ngx.thread.kill(writer)
wb:send_close()
`

func TestStressWebsocketPubSub(t *testing.T) {
	n := stressN(t, 1000)
	lib := os.Getenv("STRESS_RESTY_LIB")
	if lib == "" {
		t.Skip("set STRESS_RESTY_LIB to lua-resty-websocket's lib directory")
	}
	baseline := runtime.NumGoroutine()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	if err := gls.LoadEntryPoint(CodeEntryPoint{Code: pubsubScript}); err != nil {
		t.Fatalf("LoadEntryPoint: %v", err)
	}
	gls.SetupNgxAlias()
	gls.Start()
	defer gls.Close()
	defer gls.Stop()
	// Set once: doing this per request grows package.path forever
	if err := gls.RunString(`package.path = "` + lib + `/?.lua;" .. package.path`); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(gls.HTTPHandler(DefaultHTTPServerConfig()))
	defer server.Close()
	addr := server.Listener.Addr().String()

	memBefore := luaMemKB(t, gls)

	var (
		wg         sync.WaitGroup
		dialErrs   atomic.Int64
		echoErrs   atomic.Int64
		echoes     atomic.Int64
		broadcasts atomic.Int64
		stop       = make(chan struct{})
	)

	// Publisher: broadcasts constantly while clients run
	pubDone := make(chan int)
	go func() {
		count := 0
		for {
			select {
			case <-stop:
				pubDone <- count
				return
			default:
			}
			resp, err := stressClient.Get(server.URL + "/publish?msg=bcast" + strconv.Itoa(count))
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				count++
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	waves, _ := strconv.Atoi(os.Getenv("STRESS_WAVES"))
	if waves == 0 {
		waves = 1
	}
	start := time.Now()
	for wave := 0; wave < waves; wave++ {
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ws, err := wsDial(addr)
				if err != nil {
					dialErrs.Add(1)
					return
				}
				defer ws.c.Close()
				rounds := 5 + i%10
				for r := 0; r < rounds; r++ {
					// keep connections alive long enough for broadcasts to overlap
					time.Sleep(time.Duration(i%7) * time.Millisecond)
					msg := fmt.Sprintf("c%d-r%d", i, r)
					if err := ws.send(1, []byte(msg)); err != nil {
						echoErrs.Add(1)
						return
					}
					// Read until our echo shows up; broadcasts interleave
					for {
						op, p, err := ws.recv()
						if err != nil {
							echoErrs.Add(1)
							return
						}
						if op != 1 {
							continue
						}
						s := string(p)
						if s == "echo: "+msg {
							echoes.Add(1)
							break
						}
						if strings.HasPrefix(s, "bcast") {
							broadcasts.Add(1)
							continue
						}
						echoErrs.Add(1)
						return
					}
				}
				switch i % 3 {
				case 0:
					// graceful: close frame, expect close reply
					ws.send(8, []byte{3, 232})
					for {
						op, _, err := ws.recv()
						if err != nil || op == 8 {
							break
						}
					}
				case 1:
					// abrupt TCP close
				case 2:
					// RST: linger 0
					if tc, ok := ws.c.(*net.TCPConn); ok {
						tc.SetLinger(0)
					}
				}
			}(i)
		}
		wg.Wait()
		if waves > 1 {
			t.Logf("wave %d: luaKB=%.0f", wave, luaMemKB(t, gls))
		}
	}
	elapsed := time.Since(start)
	close(stop)
	published := <-pubDone

	// Wait for the server side to notice every disconnect
	var stats string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := stressClient.Get(server.URL + "/stats")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			stats = strings.TrimSpace(string(b))
			if strings.HasPrefix(stats, "0 ") {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	memAfter := luaMemKB(t, gls)

	t.Logf("conns=%d elapsed=%v dialErrs=%d echoErrs=%d echoes=%d broadcastsSeen=%d published=%d stats=%q luaKB before=%.0f after=%.0f",
		n, elapsed, dialErrs.Load(), echoErrs.Load(), echoes.Load(), broadcasts.Load(), published, stats, memBefore, memAfter)
	if dialErrs.Load() != 0 || echoErrs.Load() != 0 {
		t.Errorf("errors: dial=%d echo=%d", dialErrs.Load(), echoErrs.Load())
	}
	if !strings.HasPrefix(stats, "0 ") {
		t.Errorf("subscribers never drained: %q", stats)
	}

	server.Close()
	checkNoLeaks(t, gls, baseline)
}

func TestStressLightThreads(t *testing.T) {
	n := stressN(t, 10000)
	baseline := runtime.NumGoroutine()
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	gls.Start()
	defer gls.Close()
	defer gls.Stop()

	var out strings.Builder
	gls.SetOutputWriter(&out)
	start := time.Now()
	err := gls.RunString(`
		local N = ` + strconv.Itoa(n) + `
		local sema = golapis.semaphore.new()
		local ths = {}
		-- N threads parked on one semaphore, released in one post
		for i = 1, N do
			ths[i] = golapis.thread.spawn(function()
				assert(sema:wait(30))
				golapis.sleep(0.001)
				return i
			end)
		end
		sema:post(N)
		local sum = 0
		for i = 1, N do
			local ok, v = golapis.thread.wait(ths[i])
			assert(ok, v)
			sum = sum + v
		end
		golapis.say("sum ok ", sum == N * (N + 1) / 2)

		-- kill storm: threads parked on sleep, on the semaphore, and on each other
		local victims, inners = {}, {}
		for i = 1, N do
			if i % 3 == 0 then
				victims[i] = golapis.thread.spawn(function() golapis.sleep(30) end)
			elseif i % 3 == 1 then
				victims[i] = golapis.thread.spawn(function() sema:wait(30) end)
			else
				victims[i] = golapis.thread.spawn(function()
					local inner = golapis.thread.spawn(function() sema:wait(30) end)
					inners[#inners + 1] = inner
					golapis.thread.wait(inner)
				end)
			end
		end
		coroutine.yield() -- let them all park
		for i = 1, N do assert(golapis.thread.kill(victims[i])) end
		-- like ngx, killing a parent orphans its children: they stay parked
		golapis.say("orphans still waiting ", -sema:count() == #inners)
		sema:post(#inners)
		coroutine.yield()
		golapis.say("sema count after release ", sema:count())

		-- deep nesting: a chain of threads each waiting on the next
		local function chain(d)
			if d == 0 then return 0 end
			local th = golapis.thread.spawn(chain, d - 1)
			local ok, v = golapis.thread.wait(th)
			return v + 1
		end
		golapis.say("chain ", chain(190))

		-- zombies: many finished threads nobody waits on
		for i = 1, N do golapis.thread.spawn(function() return i end) end
		coroutine.yield()
		golapis.say("done")
	`)
	gls.Wait()
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	t.Logf("n=%d elapsed=%v out=%q", n, time.Since(start), out.String())
	want := "sum ok true\norphans still waiting true\nsema count after release 0\nchain 190\ndone\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
	checkNoLeaks(t, gls, baseline)
}

func TestStressSemaphoreRequests(t *testing.T) {
	n := stressN(t, 2000)
	baseline := runtime.NumGoroutine()
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	if err := gls.LoadEntryPoint(CodeEntryPoint{Code: `
		_G.sema = _G.sema or golapis.semaphore.new()
		local uri = golapis.var.request_uri
		if uri:match("^/wait") then
			local timeout = tonumber(golapis.req.get_uri_args().t)
			local ok, err = _G.sema:wait(timeout)
			golapis.say(ok and "woke" or err)
		elseif uri:match("^/post") then
			_G.sema:post(tonumber(golapis.req.get_uri_args().n))
			golapis.say("ok")
		else
			golapis.say(_G.sema:count())
		end
	`}); err != nil {
		t.Fatalf("LoadEntryPoint: %v", err)
	}
	gls.Start()
	defer gls.Close()
	defer gls.Stop()
	server := httptest.NewServer(gls.HTTPHandler(DefaultHTTPServerConfig()))
	defer server.Close()

	get := func(path string) string {
		resp, err := stressClient.Get(server.URL + path)
		if err != nil {
			return "error: " + err.Error()
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return strings.TrimSpace(string(b))
	}

	var wg sync.WaitGroup
	results := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// timeouts spread between 50ms and 1s so posts race with expiry
			results <- get(fmt.Sprintf("/wait?t=%.3f", 0.05+float64(i%20)*0.05))
		}(i)
	}
	// Post in bursts while waiters are arriving and expiring
	posts := 0
	for posts < n*3/4 {
		if r := get("/post?n=25"); r != "ok" {
			t.Fatalf("post failed: %s", r)
		}
		posts += 25
		time.Sleep(3 * time.Millisecond)
	}
	wg.Wait()
	close(results)

	woke, timedOut, other := 0, 0, 0
	for r := range results {
		switch r {
		case "woke":
			woke++
		case "timeout":
			timedOut++
		default:
			other++
			t.Errorf("unexpected result %q", r)
		}
	}
	count := get("/count")
	t.Logf("n=%d posted=%d woke=%d timedOut=%d other=%d leftover=%s", n, posts, woke, timedOut, other, count)
	if woke+timedOut != n {
		t.Errorf("woke+timedOut=%d, want %d", woke+timedOut, n)
	}
	if c, _ := strconv.Atoi(count); woke+c != posts {
		t.Errorf("units lost: woke(%d)+leftover(%d) != posted(%d)", woke, c, posts)
	}

	server.Close()
	checkNoLeaks(t, gls, baseline)
}
