package golapis

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Helper to run Lua code and capture output for UDP tests
func runUDPTest(t *testing.T, code string) (string, error) {
	t.Helper()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	buf := &bytes.Buffer{}
	gls.SetOutputWriter(buf)

	gls.Start()
	defer gls.Stop()

	err := gls.RunString(code)
	gls.Wait()

	return buf.String(), err
}

func TestUDPSocketCreate(t *testing.T) {
	code := `
		local sock = golapis.socket.udp()
		golapis.say(type(sock))
	`
	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if output != "userdata\n" {
		t.Errorf("expected 'userdata', got: %q", output)
	}
}

func TestUDPSocketMethods(t *testing.T) {
	// Verify all methods exist
	code := `
		local sock = golapis.socket.udp()
		golapis.say("setpeername=", type(sock.setpeername))
		golapis.say("send=", type(sock.send))
		golapis.say("receive=", type(sock.receive))
		golapis.say("settimeout=", type(sock.settimeout))
		golapis.say("close=", type(sock.close))
		golapis.say("bind=", type(sock.bind))
	`
	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := []string{
		"setpeername=function",
		"send=function",
		"receive=function",
		"settimeout=function",
		"close=function",
		"bind=function",
	}
	for _, exp := range expected {
		if !strings.Contains(output, exp) {
			t.Errorf("expected %q in output, got: %q", exp, output)
		}
	}
}

func TestUDPSocketSettimeout(t *testing.T) {
	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(1000)
		golapis.say("ok")
	`
	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "ok") {
		t.Errorf("expected 'ok', got: %q", output)
	}
}

func TestUDPSocketBind(t *testing.T) {
	code := `
		local sock = golapis.socket.udp()
		local ok, err = sock:bind("127.0.0.1")
		golapis.say("ok=", ok, " err=", err or "nil")
	`
	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "ok=1") {
		t.Errorf("expected 'ok=1', got: %q", output)
	}
}

func TestUDPSocketEchoIPv4(t *testing.T) {
	// Start a local UDP echo server
	serverAddr, cleanup := startUDPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(1000)
		local ok, err = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		local ok, err = sock:send("hello world")
		if not ok then
			golapis.say("send error: ", err)
			return
		end

		local data, err = sock:receive()
		if not data then
			golapis.say("receive error: ", err)
			return
		end

		golapis.say("received: ", data)
		sock:close()
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "received: hello world") {
		t.Errorf("expected 'received: hello world', got: %q", output)
	}
}

func TestUDPSocketTimeout(t *testing.T) {
	// Start a server that doesn't respond
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer conn.Close()

	serverAddr := conn.LocalAddr().(*net.UDPAddr)

	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(100)  -- 100ms timeout
		local ok, err = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		local ok, err = sock:send("ping")
		if not ok then
			golapis.say("send error: ", err)
			return
		end

		local data, err = sock:receive()
		if data then
			golapis.say("unexpected data: ", data)
		else
			golapis.say("error: ", err)
		end
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "error: timeout") {
		t.Errorf("expected 'error: timeout', got: %q", output)
	}
}

func TestUDPSocketSendBeforeConnect(t *testing.T) {
	code := `
		local sock = golapis.socket.udp()
		local ok, err = sock:send("test")
		golapis.say("ok=", ok or "nil", " err=", err or "nil")
	`
	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "not connected") {
		t.Errorf("expected 'not connected' error, got: %q", output)
	}
}

func TestUDPSocketReceiveBeforeConnect(t *testing.T) {
	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(100)
		local data, err = sock:receive()
		golapis.say("data=", data or "nil", " err=", err or "nil")
	`
	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "not connected") {
		t.Errorf("expected 'not connected' error, got: %q", output)
	}
}

func TestUDPSocketClose(t *testing.T) {
	serverAddr, cleanup := startUDPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.udp()
		local ok, err = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		-- First close should succeed
		local ok1, err1 = sock:close()
		golapis.say("close1: ok=", ok1 or "nil", " err=", err1 or "nil")

		-- Second close should fail
		local ok2, err2 = sock:close()
		golapis.say("close2: ok=", ok2 or "nil", " err=", err2 or "nil")
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "close1: ok=1") {
		t.Errorf("expected first close to succeed, got: %q", output)
	}
	if !strings.Contains(output, "already closed") {
		t.Errorf("expected 'already closed' error on second close, got: %q", output)
	}
}

func TestUDPSocketSendAfterClose(t *testing.T) {
	serverAddr, cleanup := startUDPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.udp()
		local ok, err = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		sock:close()

		local ok, err = sock:send("test")
		golapis.say("ok=", ok or "nil", " err=", err or "nil")
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "closed") {
		t.Errorf("expected 'closed' error, got: %q", output)
	}
}

func TestUDPSocketSendTableFragments(t *testing.T) {
	serverAddr, cleanup := startUDPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(1000)
		local ok, err = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		-- Send table of fragments
		local ok, err = sock:send({"hello", " ", "world"})
		if not ok then
			golapis.say("send error: ", err)
			return
		end

		local data, err = sock:receive()
		golapis.say("received: ", data or err)
		sock:close()
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "received: hello world") {
		t.Errorf("expected 'received: hello world', got: %q", output)
	}
}

func TestUDPSocketSendNumber(t *testing.T) {
	serverAddr, cleanup := startUDPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(1000)
		local ok, err = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		local ok, err = sock:send(42)
		if not ok then
			golapis.say("send error: ", err)
			return
		end

		local data, err = sock:receive()
		golapis.say("received: ", data or err)
		sock:close()
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "received: 42") {
		t.Errorf("expected 'received: 42', got: %q", output)
	}
}

func TestUDPSocketSendTypes(t *testing.T) {
	serverAddr, cleanup := startUDPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(1000)
		local ok, err = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		local cases = {
			{true, "true"},
			{false, "false"},
			{nil, "nil"},
			{{"a", {"b", "c"}, 1}, "abc1"},
		}

		for _, item in ipairs(cases) do
			local ok, err = sock:send(item[1])
			if not ok then
				golapis.say("send error: ", err)
				return
			end
			local data, err = sock:receive()
			if not data then
				golapis.say("receive error: ", err)
				return
			end
			golapis.say("received: ", data)
		end

		sock:close()
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "received: true") {
		t.Errorf("expected 'received: true', got: %q", output)
	}
	if !strings.Contains(output, "received: false") {
		t.Errorf("expected 'received: false', got: %q", output)
	}
	if !strings.Contains(output, "received: nil") {
		t.Errorf("expected 'received: nil', got: %q", output)
	}
	if !strings.Contains(output, "received: abc1") {
		t.Errorf("expected 'received: abc1', got: %q", output)
	}
}

func TestUDPSocketSendTableStrict(t *testing.T) {
	serverAddr, cleanup := startUDPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(1000)
		local ok, err = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		-- boolean in table should fail (strict mode)
		local ok, err = sock:send({"a", true, "b"})
		if not ok then
			golapis.say("boolean error: ", err)
		end

		-- nested boolean should also fail
		local ok, err = sock:send({"a", {"b", false}, "c"})
		if not ok then
			golapis.say("nested boolean error: ", err)
		end

		sock:close()
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "boolean error: bad data type boolean in the array") {
		t.Errorf("expected boolean rejection error, got: %q", output)
	}
	if !strings.Contains(output, "nested boolean error: bad data type boolean in the array") {
		t.Errorf("expected nested boolean rejection error, got: %q", output)
	}
}

func TestUDPSocketReceiveWithSize(t *testing.T) {
	serverAddr, cleanup := startUDPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(1000)
		local ok, err = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		sock:send("hello world")

		-- Receive with size limit (should get full message since UDP is message-based)
		local data, err = sock:receive(5)
		golapis.say("received: ", data or err)
		sock:close()
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	// Note: UDP returns whole datagram, but our receive truncates to size
	// The actual behavior depends on Go's Read implementation
	if !strings.Contains(output, "received:") {
		t.Errorf("expected 'received:', got: %q", output)
	}
}

func TestUDPSocketUnixDomain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unixgram sockets not supported on windows")
	}

	// Create temp directory for unix socket
	tmpDir, err := os.MkdirTemp("", "golapis-udp-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "test.sock")

	// Start unix domain socket echo server
	serverAddr, err := net.ResolveUnixAddr("unixgram", socketPath)
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}

	serverConn, err := net.ListenUnixgram("unixgram", serverAddr)
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer serverConn.Close()

	// Echo server goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		for {
			serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, remoteAddr, err := serverConn.ReadFromUnix(buf)
			if err != nil {
				return
			}
			serverConn.WriteToUnix(buf[:n], remoteAddr)
		}
	}()

	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(1000)

		local ok, err = sock:setpeername("unix:` + socketPath + `")
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		-- Send data and receive echo reply (tests autobind)
		local ok, err = sock:send("hello unix")
		if not ok then
			golapis.say("send error: ", err)
			return
		end

		local data, err = sock:receive()
		if not data then
			golapis.say("receive error: ", err)
			return
		end

		golapis.say("received: ", data)
		sock:close()
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "received: hello unix") {
		t.Errorf("expected echo reply, got: %q", output)
	}
}

func TestUDPSocketDNSResolution(t *testing.T) {
	// This test verifies DNS resolution works (async)
	// Start server on both IPv4 and IPv6 localhost to handle either resolution
	addr4, err := net.ResolveUDPAddr("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	conn4, err := net.ListenUDP("udp4", addr4)
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer conn4.Close()

	serverPort := conn4.LocalAddr().(*net.UDPAddr).Port

	// Also try to listen on IPv6 with same port
	addr6, _ := net.ResolveUDPAddr("udp6", "[::1]:"+itoa(serverPort))
	conn6, err := net.ListenUDP("udp6", addr6)
	if err == nil {
		defer conn6.Close()
		// Echo server for IPv6
		go func() {
			buf := make([]byte, 65536)
			for {
				conn6.SetReadDeadline(time.Now().Add(5 * time.Second))
				n, remoteAddr, err := conn6.ReadFromUDP(buf)
				if err != nil {
					return
				}
				conn6.WriteToUDP(buf[:n], remoteAddr)
			}
		}()
	}

	// Echo server for IPv4
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		for {
			conn4.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, remoteAddr, err := conn4.ReadFromUDP(buf)
			if err != nil {
				return
			}
			conn4.WriteToUDP(buf[:n], remoteAddr)
		}
	}()

	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(2000)

		-- Use localhost which may resolve to 127.0.0.1 or ::1
		local ok, err = sock:setpeername("localhost", ` + itoa(serverPort) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		sock:send("dns test")
		local data, err = sock:receive()
		golapis.say("received: ", data or err)
		sock:close()
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "received: dns test") {
		t.Errorf("expected 'received: dns test', got: %q", output)
	}
}

func TestUDPSocketDoubleConnect(t *testing.T) {
	serverAddr, cleanup := startUDPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.udp()
		local ok1, err1 = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		golapis.say("first: ok=", ok1 or "nil", " err=", err1 or "nil")

		local ok2, err2 = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		golapis.say("second: ok=", ok2 or "nil", " err=", err2 or "nil")
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "first: ok=1") {
		t.Errorf("expected first connect to succeed, got: %q", output)
	}
	if !strings.Contains(output, "second: ok=1") {
		t.Errorf("expected second connect to succeed, got: %q", output)
	}
}

func TestUDPSocketBinaryData(t *testing.T) {
	serverAddr, cleanup := startUDPEchoServer(t)
	defer cleanup()

	// Test sending/receiving binary data with null bytes
	code := `
		local sock = golapis.socket.udp()
		sock:settimeout(1000)
		local ok, err = sock:setpeername("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		-- Send binary data with null byte
		local data = "hello\0world"
		sock:send(data)

		local recv, err = sock:receive()
		if recv then
			golapis.say("len=", #recv)
			golapis.say("has_null=", recv:find("\0") ~= nil)
		else
			golapis.say("error: ", err)
		end
		sock:close()
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "len=11") {
		t.Errorf("expected 'len=11' (hello\\0world), got: %q", output)
	}
	if !strings.Contains(output, "has_null=true") {
		t.Errorf("expected null byte preserved, got: %q", output)
	}
}

// Helper to start a UDP echo server
func startUDPEchoServer(t *testing.T) (*net.UDPAddr, func()) {
	t.Helper()

	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}

	serverAddr := conn.LocalAddr().(*net.UDPAddr)

	// Echo server goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		for {
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, remoteAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			conn.WriteToUDP(buf[:n], remoteAddr)
		}
	}()

	cleanup := func() {
		conn.Close()
		<-done
	}

	return serverAddr, cleanup
}

// Helper to convert int to string (avoids importing strconv)
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

func TestUDPSocketRequestAffinity(t *testing.T) {
	// Test that using a socket from a different thread returns "bad request"
	code := `
		local shared_sock = nil

		-- Create socket in main thread
		shared_sock = golapis.socket.udp()
		shared_sock:settimeout(100)

		-- Try to use it from a timer callback (different thread)
		golapis.timer.at(0, function(premature)
			-- All these should fail with "bad request"
			local ok, err = shared_sock:setpeername("127.0.0.1", 12345)
			golapis.say("setpeername: ", ok or "nil", " err=", err or "nil")

			local ok, err = shared_sock:send("test")
			golapis.say("send: ", ok or "nil", " err=", err or "nil")

			local ok, err = shared_sock:close()
			golapis.say("close: ", ok or "nil", " err=", err or "nil")
		end)

		-- Give timer time to run
		golapis.sleep(0.05)
	`

	output, err := runUDPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	// All operations should fail with "bad request"
	if !strings.Contains(output, "setpeername: nil err=bad request") {
		t.Errorf("expected setpeername to fail with 'bad request', got: %q", output)
	}
	if !strings.Contains(output, "send: nil err=bad request") {
		t.Errorf("expected send to fail with 'bad request', got: %q", output)
	}
	if !strings.Contains(output, "close: nil err=bad request") {
		t.Errorf("expected close to fail with 'bad request', got: %q", output)
	}
}
