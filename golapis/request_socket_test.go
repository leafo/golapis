package golapis

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// startRawSocketServer runs code as the entrypoint behind a real HTTP server
// so req.socket(true) can hijack an actual TCP connection.
func startRawSocketServer(t *testing.T, code string) (*GolapisLuaState, *httptest.Server) {
	t.Helper()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	if err := gls.LoadEntryPoint(CodeEntryPoint{Code: code}); err != nil {
		gls.Close()
		t.Fatalf("LoadEntryPoint failed: %v", err)
	}
	gls.Start()

	server := httptest.NewServer(gls.HTTPHandler(DefaultHTTPServerConfig()))
	t.Cleanup(func() {
		server.Close()
		gls.Wait()
		gls.Stop()
		gls.Close()
	})
	return gls, server
}

// dialRaw opens a TCP connection to the server and sends an upgrade-style
// request, returning the connection and a reader positioned after the
// response headers.
func dialRaw(t *testing.T, server *httptest.Server, extra string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	req := "GET /ws HTTP/1.1\r\nHost: test\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n" + extra
	return dialRawRequest(t, server, req)
}

// dialRawRequest sends a raw HTTP request string and returns the connection
// and a reader positioned after the response headers.
func dialRawRequest(t *testing.T, server *httptest.Server, req string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("write request: %v", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return conn, reader, resp
}

func TestReqSocketEcho(t *testing.T) {
	_, server := startRawSocketServer(t, `
		golapis.status = 101
		golapis.header["Upgrade"] = "test"
		golapis.header["Connection"] = "Upgrade"
		assert(golapis.send_headers())
		assert(golapis.flush(true))

		local sock = assert(golapis.req.socket(true))
		assert(golapis.headers_sent == true)
		while true do
			local line, err = sock:receive("*l")
			if not line then break end
			if line == "quit" then
				sock:send("bye\n")
				break
			end
			assert(sock:send("echo:" .. line .. "\n"))
		end
	`)

	conn, reader, resp := dialRaw(t, server, "")
	if resp.StatusCode != 101 {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	if got := resp.Header.Get("Upgrade"); got != "test" {
		t.Errorf("Upgrade header = %q", got)
	}
	if resp.Header.Get("Content-Length") != "" || resp.Header.Get("Transfer-Encoding") != "" {
		t.Errorf("101 response must not carry a body framing header: %v", resp.Header)
	}

	for _, msg := range []string{"hello", "world"} {
		io.WriteString(conn, msg+"\n")
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read echo: %v", err)
		}
		if line != "echo:"+msg+"\n" {
			t.Errorf("echo = %q", line)
		}
	}

	io.WriteString(conn, "quit\n")
	line, _ := reader.ReadString('\n')
	if line != "bye\n" {
		t.Errorf("final line = %q", line)
	}

	// After the thread finishes the handler closes the hijacked connection
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Errorf("expected EOF after handler returned, got %v", err)
	}
}

func TestReqSocketConsumesBufferedBytes(t *testing.T) {
	// Data sent in the same packet as the request headers ends up in
	// net/http's bufio reader; the socket must see it.
	_, server := startRawSocketServer(t, `
		golapis.status = 101
		golapis.send_headers()
		local sock = assert(golapis.req.socket(true))
		local line = assert(sock:receive("*l"))
		sock:send("got:" .. line .. "\n")
	`)

	_, reader, resp := dialRaw(t, server, "early-bytes\n")
	if resp.StatusCode != 101 {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if line != "got:early-bytes\n" {
		t.Errorf("line = %q", line)
	}
}

func TestReqSocketBareWhenHeadersUnsent(t *testing.T) {
	// Like ngx.req.socket(true), nothing is sent on the script's behalf: the
	// script writes the whole response, status line included
	_, server := startRawSocketServer(t, `
		golapis.status = 500 -- ignored: never sent
		golapis.header["X-Test"] = "1"
		local sock = assert(golapis.req.socket(true))
		assert(golapis.headers_sent == true)
		sock:send("HTTP/1.1 418 Teapot\r\nContent-Length: 4\r\n\r\nbrew")
	`)

	_, _, resp := dialRaw(t, server, "")
	if resp.StatusCode != 418 {
		t.Fatalf("status = %d, want the script's own 418", resp.StatusCode)
	}
	if resp.Header.Get("X-Test") != "" {
		t.Errorf("accumulated headers must not be sent: %v", resp.Header)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "brew" {
		t.Errorf("body = %q", body)
	}
}

func TestReqSocketOutputAfterHijack(t *testing.T) {
	_, server := startRawSocketServer(t, `
		golapis.status = 101
		golapis.send_headers()
		local sock = assert(golapis.req.socket(true))

		local ok, err = golapis.say("nope")
		assert(ok == nil and err == "raw request socket acquired", tostring(err))
		ok, err = golapis.flush()
		assert(ok == nil and err == "raw request socket acquired", tostring(err))

		local sock2, err2 = golapis.req.socket(true)
		assert(sock2 == nil and err2 == "duplicate call", tostring(err2))

		ok, err = sock:setkeepalive()
		assert(ok == nil and err == "cannot pool a request socket", tostring(err))

		sock:send("checks-passed\n")
	`)

	_, reader, _ := dialRaw(t, server, "")
	if line, _ := reader.ReadString('\n'); line != "checks-passed\n" {
		t.Errorf("line = %q", line)
	}
}

func TestReqSocketLuaErrorAfterHijack(t *testing.T) {
	// An error after hijacking must not try to write a 500 to the raw
	// connection; it just closes it.
	_, server := startRawSocketServer(t, `
		golapis.status = 101
		golapis.send_headers()
		local sock = assert(golapis.req.socket(true))
		sock:send("before-error\n")
		error("boom")
	`)

	_, reader, _ := dialRaw(t, server, "")
	if line, _ := reader.ReadString('\n'); line != "before-error\n" {
		t.Errorf("line = %q", line)
	}
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read rest: %v", err)
	}
	if len(rest) != 0 {
		t.Errorf("unexpected data after error: %q", rest)
	}
}

func TestReqSocketCloseAllUnblocksThread(t *testing.T) {
	gls, server := startRawSocketServer(t, `
		golapis.status = 101
		golapis.send_headers()
		local sock = assert(golapis.req.socket(true))
		sock:settimeout(0)
		local data, err = sock:receive(1)
		golapis.log(golapis.INFO, "receive ended: ", tostring(err))
	`)

	dialRaw(t, server, "")

	// Wait for the thread to be blocked in receive, then simulate shutdown
	deadline := time.Now().Add(2 * time.Second)
	for {
		gls.hijacked.mu.Lock()
		n := len(gls.hijacked.conns)
		gls.hijacked.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	gls.hijacked.closeAll()

	done := make(chan struct{})
	go func() {
		gls.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("thread did not finish after closing hijacked connections")
	}
}

func TestReqSocketNonRawUnsupported(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `
		local sock, err = golapis.req.socket()
		assert(sock == nil, "expected nil socket")
		assert(err:find("raw mode"), err)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
}

func TestReqSocketWithoutHijacker(t *testing.T) {
	// httptest.ResponseRecorder does not implement http.Hijacker
	_, _, err := runLuaWithHTTP(t, `
		local sock, err = golapis.req.socket(true)
		assert(sock == nil, "expected nil socket")
		assert(err == "connection does not support hijacking", err)
		assert(golapis.headers_sent == false)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
}

func TestReqSocketOutsideRequest(t *testing.T) {
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()
	gls.Start()
	defer gls.Stop()

	err := gls.RunString(`
		local sock, err = golapis.req.socket(true)
		assert(sock == nil and err == "no request found", tostring(err))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
}

func TestReqSocketInCapture(t *testing.T) {
	// A subrequest's output is captured into a buffer; there is no
	// connection to hijack
	w, err := runLuaEntryPointHTTP(t, "/test", `
		if golapis.var.request_uri == "/sub" then
			local sock, err = golapis.req.socket(true)
			golapis.say(tostring(sock), " ", tostring(err))
			return
		end
		local res = golapis.location.capture("/sub")
		golapis.print(res.body)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(w.Body.String(), "nil connection does not support hijacking") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestReqSocketFlushesPendingBody(t *testing.T) {
	// print() output sitting in net/http's buffer must reach the client
	// before the connection is handed over (a 1xx status rejects body
	// writes outright, so this only applies to body-bearing statuses)
	_, server := startRawSocketServer(t, `
		golapis.header["Transfer-Encoding"] = "identity"
		assert(golapis.print("buffered-before-hijack\n"))
		local sock = assert(golapis.req.socket(true))
		sock:send("raw-after\n")
	`)

	_, reader, resp := dialRaw(t, server, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{"buffered-before-hijack\n", "raw-after\n"} {
		line, err := reader.ReadString('\n')
		if err != nil || line != want {
			t.Fatalf("line = %q (%v), want %q", line, err, want)
		}
	}
}

func TestReqSocketRejectsConnectAndReadBody(t *testing.T) {
	_, server := startRawSocketServer(t, `
		golapis.status = 101
		golapis.send_headers()
		local sock = assert(golapis.req.socket(true))

		local ok, err = sock:connect("127.0.0.1", 1)
		assert(ok == nil and err == "attempt to connect a request socket", tostring(err))

		ok, err = golapis.req.read_body()
		assert(ok == nil and err == "raw request socket acquired", tostring(err))

		-- the client connection is still intact after the rejected connect
		sock:send("still-open\n")
	`)

	_, reader, _ := dialRaw(t, server, "")
	if line, _ := reader.ReadString('\n'); line != "still-open\n" {
		t.Errorf("line = %q", line)
	}
}

func TestReqSocketAfterCloseAllRefused(t *testing.T) {
	gls, server := startRawSocketServer(t, `
		golapis.status = 101
		golapis.send_headers()
		local sock, err = golapis.req.socket(true)
		golapis.log(golapis.INFO, "socket: ", tostring(sock), " ", tostring(err))
		assert(sock == nil and err == "closed", tostring(err))
	`)

	// Simulate shutdown having already run before the request hijacks
	gls.hijacked.closeAll()

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(conn, "GET /ws HTTP/1.1\r\nHost: test\r\n\r\n")

	// Lua rejected the socket, so the handler closes the connection
	data, _ := io.ReadAll(conn)
	if !strings.HasPrefix(string(data), "HTTP/1.1 101") {
		t.Errorf("response = %q", data)
	}

	done := make(chan struct{})
	go func() {
		gls.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("thread did not finish")
	}
}

func TestReqSocketRejectsChunkedResponse(t *testing.T) {
	// Output before hijack with no framing header commits a chunked body;
	// raw writes can't continue that stream so the hijack is refused
	_, server := startRawSocketServer(t, `
		assert(golapis.print("chunk\n"))
		local sock, err = golapis.req.socket(true)
		assert(sock == nil, "expected hijack to fail")
		assert(err:find("chunked encoding"), err)
		golapis.print("still-http\n")
	`)

	_, _, resp := dialRaw(t, server, "")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "chunk\nstill-http\n" {
		t.Errorf("body = %q", body)
	}
}

func TestReqSocketRejectsExplicitChunked(t *testing.T) {
	_, server := startRawSocketServer(t, `
		golapis.header["Transfer-Encoding"] = "chunked"
		golapis.send_headers()
		local sock, err = golapis.req.socket(true)
		assert(sock == nil and err:find("chunked encoding"), tostring(err))
		golapis.print("ok")
	`)

	_, _, resp := dialRaw(t, server, "")
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q", body)
	}
}

func TestReqSocketClosedByStop(t *testing.T) {
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	if err := gls.LoadEntryPoint(CodeEntryPoint{Code: `
		golapis.status = 101
		golapis.send_headers()
		local sock = assert(golapis.req.socket(true))
		sock:settimeout(0)
		sock:receive(1)
	`}); err != nil {
		t.Fatalf("LoadEntryPoint failed: %v", err)
	}
	gls.Start()
	defer gls.Close()

	server := httptest.NewServer(gls.HTTPHandler(DefaultHTTPServerConfig()))
	defer server.Close()

	dialRaw(t, server, "")

	deadline := time.Now().Add(2 * time.Second)
	for {
		gls.hijacked.mu.Lock()
		n := len(gls.hijacked.conns)
		gls.hijacked.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Stop() must close the raw socket so the blocked thread can finish
	done := make(chan struct{})
	go func() {
		gls.Stop()
		gls.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not unblock the raw socket thread")
	}
}

func TestReqSocketUnreadBodyAfterSendHeaders(t *testing.T) {
	// net/http normally discards an unread request body when the headers
	// are written; after send_headers() the body must still be readable
	// from the raw socket
	_, server := startRawSocketServer(t, `
		golapis.status = 101
		assert(golapis.send_headers())
		local sock = assert(golapis.req.socket(true))
		local data, err = sock:receive(5)
		assert(data == "hello", tostring(err))
		sock:send("got:" .. data .. "\n")
	`)

	_, reader, resp := dialRawRequest(t, server,
		"POST /ws HTTP/1.1\r\nHost: test\r\nContent-Length: 5\r\n\r\nhello")
	if resp.StatusCode != 101 {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	if line, _ := reader.ReadString('\n'); line != "got:hello\n" {
		t.Errorf("line = %q", line)
	}
}

func TestReqSocketUnreadBodyBare(t *testing.T) {
	// With no headers sent, the body is simply whatever is buffered
	_, server := startRawSocketServer(t, `
		local sock = assert(golapis.req.socket(true))
		local data = assert(sock:receive(5))
		sock:send("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\n" .. data)
	`)

	_, _, resp := dialRawRequest(t, server,
		"POST /ws HTTP/1.1\r\nHost: test\r\nContent-Length: 5\r\n\r\nhello")
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Errorf("body = %q", body)
	}
}

func TestReqSocketStopStartCycle(t *testing.T) {
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	if err := gls.LoadEntryPoint(CodeEntryPoint{Code: `
		golapis.status = 101
		golapis.send_headers()
		local sock, err = golapis.req.socket(true)
		if not sock then
			error("req.socket failed: " .. tostring(err))
		end
		sock:settimeout(0)
		sock:receive(1)
		_G.resume_count = (_G.resume_count or 0) + 1
	`}); err != nil {
		t.Fatalf("LoadEntryPoint failed: %v", err)
	}
	gls.Start()
	defer gls.Close()

	server := httptest.NewServer(gls.HTTPHandler(DefaultHTTPServerConfig()))
	defer server.Close()

	// Find the thread parked on a raw socket (other than skip) so a stale
	// resume for it can be injected after the restart. Poll the socket
	// registry itself: the connection is tracked before the socket is
	// registered. Only immutable fields are read; the rest belong to the
	// event loop.
	findParked := func(skip *LuaThread) *LuaThread {
		deadline := time.Now().Add(2 * time.Second)
		for {
			tcpSocketMu.Lock()
			for _, sock := range tcpSocketMap {
				if sock.isRequestSocket && sock.ownerThread != nil && sock.ownerThread != skip && sock.ownerThread.state == gls {
					tcpSocketMu.Unlock()
					return sock.ownerThread
				}
			}
			tcpSocketMu.Unlock()
			if time.Now().After(deadline) {
				return nil
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	dialRaw(t, server, "")
	parked := findParked(nil)
	if parked == nil {
		t.Fatal("no thread parked on a request socket")
	}

	gls.Stop()
	gls.Wait()
	gls.Start()
	defer gls.Stop()

	// Whether the receive error resumed the script before EventStop or the
	// thread was finalized by it, the count must not change from here on
	if err := gls.RunString(`_G.count_before = _G.resume_count or 0`); err != nil {
		t.Fatal(err)
	}

	// A late completion of the async read queues a resume for a thread
	// that Stop() already finalized
	gls.eventChan <- &StateEvent{
		Type:         EventResumeThread,
		Thread:       parked,
		ResumeValues: []interface{}{nil, "closed"},
	}
	if err := gls.RunString(`assert((_G.resume_count or 0) == _G.count_before, "stale thread resumed after restart")`); err != nil {
		t.Fatal(err)
	}

	// Raw sockets must work again after the restart
	conn, _, resp := dialRaw(t, server, "")
	if resp.StatusCode != 101 {
		t.Fatalf("status after restart = %d, want 101", resp.StatusCode)
	}
	if findParked(parked) == nil {
		t.Fatal("req.socket was refused after restart")
	}
	conn.Close()
}
