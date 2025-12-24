package golapis

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Helper to run Lua code and capture output for TCP tests
func runTCPTest(t *testing.T, code string) (string, error) {
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

func TestTCPSocketCreate(t *testing.T) {
	code := `
		local sock = golapis.socket.tcp()
		golapis.say(type(sock))
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if output != "userdata\n" {
		t.Errorf("expected 'userdata', got: %q", output)
	}
}

func TestTCPSocketMethods(t *testing.T) {
	// Verify all methods exist
	code := `
		local sock = golapis.socket.tcp()
		golapis.say("connect=", type(sock.connect))
		golapis.say("send=", type(sock.send))
		golapis.say("receive=", type(sock.receive))
		golapis.say("settimeout=", type(sock.settimeout))
		golapis.say("close=", type(sock.close))
		golapis.say("setkeepalive=", type(sock.setkeepalive))
		golapis.say("getreusedtimes=", type(sock.getreusedtimes))
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := []string{
		"connect=function",
		"send=function",
		"receive=function",
		"settimeout=function",
		"close=function",
		"setkeepalive=function",
		"getreusedtimes=function",
	}
	for _, exp := range expected {
		if !strings.Contains(output, exp) {
			t.Errorf("expected %q in output, got: %q", exp, output)
		}
	}
}

func TestTCPSocketSettimeout(t *testing.T) {
	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)
		golapis.say("ok")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "ok") {
		t.Errorf("expected 'ok', got: %q", output)
	}
}

func TestTCPSocketEchoIPv4(t *testing.T) {
	// Start a local TCP echo server
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		local bytes, err = sock:send("hello world")
		if not bytes then
			golapis.say("send error: ", err)
			return
		end
		golapis.say("sent bytes: ", bytes)

		local data, err = sock:receive(11)
		if not data then
			golapis.say("receive error: ", err)
			return
		end

		golapis.say("received: ", data)
		sock:close()
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "sent bytes: 11") {
		t.Errorf("expected 'sent bytes: 11', got: %q", output)
	}
	if !strings.Contains(output, "received: hello world") {
		t.Errorf("expected 'received: hello world', got: %q", output)
	}
}

func TestTCPSocketReceiveExactBytes(t *testing.T) {
	// Test that receive returns exactly the requested number of bytes
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		sock:send("hello world test")

		-- Receive in chunks
		local data1, err = sock:receive(5)
		if not data1 then
			golapis.say("receive1 error: ", err)
			return
		end
		golapis.say("chunk1: [", data1, "] len=", #data1)

		local data2, err = sock:receive(6)
		if not data2 then
			golapis.say("receive2 error: ", err)
			return
		end
		golapis.say("chunk2: [", data2, "] len=", #data2)

		sock:close()
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "chunk1: [hello] len=5") {
		t.Errorf("expected 'chunk1: [hello] len=5', got: %q", output)
	}
	if !strings.Contains(output, "chunk2: [ world] len=6") {
		t.Errorf("expected 'chunk2: [ world] len=6', got: %q", output)
	}
}

func TestTCPSocketTimeout(t *testing.T) {
	// Start a server that accepts but doesn't send
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().(*net.TCPAddr)

	// Accept connections but don't respond
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Hold connection open but don't send anything
			time.Sleep(5 * time.Second)
			conn.Close()
		}
	}()

	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(100)  -- 100ms timeout
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		local data, err = sock:receive(10)
		if data then
			golapis.say("unexpected data: ", data)
		else
			golapis.say("error: ", err)
		end
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "error: timeout") {
		t.Errorf("expected 'error: timeout', got: %q", output)
	}
}

func TestTCPSocketSendBeforeConnect(t *testing.T) {
	code := `
		local sock = golapis.socket.tcp()
		local ok, err = sock:send("test")
		golapis.say("ok=", ok or "nil", " err=", err or "nil")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "not connected") {
		t.Errorf("expected 'not connected' error, got: %q", output)
	}
}

func TestTCPSocketReceiveBeforeConnect(t *testing.T) {
	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(100)
		local data, err = sock:receive(10)
		golapis.say("data=", data or "nil", " err=", err or "nil")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "not connected") {
		t.Errorf("expected 'not connected' error, got: %q", output)
	}
}

func TestTCPSocketClose(t *testing.T) {
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
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

	output, err := runTCPTest(t, code)
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

func TestTCPSocketSendAfterClose(t *testing.T) {
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		sock:close()

		local ok, err = sock:send("test")
		golapis.say("ok=", ok or "nil", " err=", err or "nil")
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "closed") {
		t.Errorf("expected 'closed' error, got: %q", output)
	}
}

func TestTCPSocketSendTableFragments(t *testing.T) {
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		-- Send table of fragments
		local bytes, err = sock:send({"hello", " ", "world"})
		if not bytes then
			golapis.say("send error: ", err)
			return
		end
		golapis.say("sent bytes: ", bytes)

		local data, err = sock:receive(11)
		golapis.say("received: ", data or err)
		sock:close()
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "sent bytes: 11") {
		t.Errorf("expected 'sent bytes: 11', got: %q", output)
	}
	if !strings.Contains(output, "received: hello world") {
		t.Errorf("expected 'received: hello world', got: %q", output)
	}
}

func TestTCPSocketUnixDomain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets not supported on windows")
	}

	// Create temp directory for unix socket
	tmpDir, err := os.MkdirTemp("", "golapis-tcp-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "test.sock")

	// Start unix domain socket echo server
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer listener.Close()

	// Echo server goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)

		local ok, err = sock:connect("unix:` + socketPath + `")
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		local bytes, err = sock:send("hello unix")
		if not bytes then
			golapis.say("send error: ", err)
			return
		end

		local data, err = sock:receive(10)
		if not data then
			golapis.say("receive error: ", err)
			return
		end

		golapis.say("received: ", data)
		sock:close()
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "received: hello unix") {
		t.Errorf("expected echo reply, got: %q", output)
	}
}

func TestTCPSocketDNSResolution(t *testing.T) {
	// Start server on localhost
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer listener.Close()

	serverPort := listener.Addr().(*net.TCPAddr).Port

	// Also try IPv6
	listener6, err := net.Listen("tcp6", "[::1]:"+itoa(serverPort))
	if err == nil {
		defer listener6.Close()
		go func() {
			for {
				conn, err := listener6.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					io.Copy(c, c)
				}(conn)
			}
		}()
	}

	// Echo server
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(2000)

		local ok, err = sock:connect("localhost", ` + itoa(serverPort) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		sock:send("dns test")
		local data, err = sock:receive(8)
		golapis.say("received: ", data or err)
		sock:close()
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "received: dns test") {
		t.Errorf("expected 'received: dns test', got: %q", output)
	}
}

func TestTCPSocketDoubleConnect(t *testing.T) {
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		local ok1, err1 = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		golapis.say("first: ok=", ok1 or "nil", " err=", err1 or "nil")

		local ok2, err2 = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		golapis.say("second: ok=", ok2 or "nil", " err=", err2 or "nil")
	`

	output, err := runTCPTest(t, code)
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

func TestTCPSocketBinaryData(t *testing.T) {
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	// Test sending/receiving binary data with null bytes
	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		-- Send binary data with null byte
		local data = "hello\0world"
		sock:send(data)

		local recv, err = sock:receive(11)
		if recv then
			golapis.say("len=", #recv)
			golapis.say("has_null=", recv:find("\0") ~= nil)
		else
			golapis.say("error: ", err)
		end
		sock:close()
	`

	output, err := runTCPTest(t, code)
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

func TestTCPSocketSetkeepalive(t *testing.T) {
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		-- setkeepalive should close the connection (stub behavior)
		local ok, err = sock:setkeepalive()
		golapis.say("setkeepalive: ok=", ok or "nil", " err=", err or "nil")

		-- Socket should now be unusable
		local bytes, err = sock:send("test")
		golapis.say("send after setkeepalive: ok=", bytes or "nil", " err=", err or "nil")
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "setkeepalive: ok=1") {
		t.Errorf("expected setkeepalive to succeed, got: %q", output)
	}
	if !strings.Contains(output, "send after setkeepalive: ok=nil err=not connected") {
		t.Errorf("expected 'not connected' after setkeepalive, got: %q", output)
	}
}

func TestTCPSocketGetreusedtimes(t *testing.T) {
	code := `
		local sock = golapis.socket.tcp()
		local times = sock:getreusedtimes()
		golapis.say("reused times: ", times)
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "reused times: 0") {
		t.Errorf("expected 'reused times: 0', got: %q", output)
	}
}

func TestTCPSocketRequestAffinity(t *testing.T) {
	// Test that using a socket from a different thread returns "bad request"
	code := `
		local shared_sock = nil

		-- Create socket in main thread
		shared_sock = golapis.socket.tcp()
		shared_sock:settimeout(100)

		-- Try to use it from a timer callback (different thread)
		golapis.timer.at(0, function(premature)
			-- All these should fail with "bad request"
			local ok, err = shared_sock:connect("127.0.0.1", 12345)
			golapis.say("connect: ", ok or "nil", " err=", err or "nil")

			local ok, err = shared_sock:send("test")
			golapis.say("send: ", ok or "nil", " err=", err or "nil")

			local ok, err = shared_sock:close()
			golapis.say("close: ", ok or "nil", " err=", err or "nil")
		end)

		-- Give timer time to run
		golapis.sleep(0.2)
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	// All operations should fail with "bad request"
	if !strings.Contains(output, "connect: nil err=bad request") {
		t.Errorf("expected connect to fail with 'bad request', got: %q", output)
	}
	if !strings.Contains(output, "send: nil err=bad request") {
		t.Errorf("expected send to fail with 'bad request', got: %q", output)
	}
	if !strings.Contains(output, "close: nil err=bad request") {
		t.Errorf("expected close to fail with 'bad request', got: %q", output)
	}
}

func TestGetPhase(t *testing.T) {
	code := `
		local phase = golapis.get_phase()
		golapis.say("phase: ", phase)
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "phase: content") {
		t.Errorf("expected 'phase: content', got: %q", output)
	}
}

func TestTCPSocketConnectRefused(t *testing.T) {
	// Reserve a port and close it to ensure connection is refused
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)
		local ok, err = sock:connect("127.0.0.1", ` + itoa(port) + `)
		golapis.say("ok=", ok or "nil")
		golapis.say("has_error=", err ~= nil)
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "ok=nil") {
		t.Errorf("expected connect to fail, got: %q", output)
	}
	if !strings.Contains(output, "has_error=true") {
		t.Errorf("expected error message, got: %q", output)
	}
}

// Helper to start a TCP echo server
func startTCPEchoServer(t *testing.T) (*net.TCPAddr, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}

	serverAddr := listener.Addr().(*net.TCPAddr)

	// Echo server goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	cleanup := func() {
		listener.Close()
	}

	return serverAddr, cleanup
}
