package golapis

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
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
		golapis.say("settimeouts=", type(sock.settimeouts))
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
		"settimeouts=function",
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

func TestTCPSocketSettimeouts(t *testing.T) {
	code := `
		local sock = golapis.socket.tcp()
		sock:settimeouts(1000, 1000, 1000)
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

func TestTCPSocketReceiveZeroBytes(t *testing.T) {
	// Test that receive(0) returns empty string immediately (OpenResty parity)
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

		local data, err = sock:receive(0)
		if data == nil then
			golapis.say("receive error: ", err)
			return
		end
		golapis.say("data: [", data, "] len=", #data)
		sock:close()
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "data: [] len=0") {
		t.Errorf("expected 'data: [] len=0', got: %q", output)
	}
}

func TestTCPSocketReceiveLineDefault(t *testing.T) {
	serverAddr, cleanup := startTCPWriteServer(t, []byte("hello\r\nworld\r\n"))
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		local line1, err = sock:receive()
		if not line1 then
			golapis.say("receive1 error: ", err)
			return
		end
		golapis.say("line1: ", line1)

		local line2, err = sock:receive("*l")
		if not line2 then
			golapis.say("receive2 error: ", err)
			return
		end
		golapis.say("line2: ", line2)
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "line1: hello") {
		t.Errorf("expected 'line1: hello', got: %q", output)
	}
	if !strings.Contains(output, "line2: world") {
		t.Errorf("expected 'line2: world', got: %q", output)
	}
}

func TestTCPSocketReceiveAll(t *testing.T) {
	serverAddr, cleanup := startTCPWriteServer(t, []byte("hello world"))
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end

		local data, err = sock:receive("*a")
		if not data then
			golapis.say("receive error: ", err)
			return
		end
		golapis.say("data: ", data)
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "data: hello world") {
		t.Errorf("expected 'data: hello world', got: %q", output)
	}
}

func TestTCPSocketReceiveNumericStringSize(t *testing.T) {
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

		sock:send("hello world")
		local data, err = sock:receive("5")
		if not data then
			golapis.say("receive error: ", err)
			return
		end
		golapis.say("data: ", data)
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "data: hello") {
		t.Errorf("expected 'data: hello', got: %q", output)
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

func TestTCPSocketSetkeepaliveBasic(t *testing.T) {
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		local ok, err = sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end
		local ok, err = sock:setkeepalive()
		golapis.say("setkeepalive: ok=", ok or "nil", " err=", err or "nil")

		-- subsequent ops on the same userdata return "closed"
		local bytes, err = sock:send("test")
		golapis.say("send: ok=", bytes or "nil", " err=", err or "nil")
		local data, err = sock:receive()
		golapis.say("receive: ok=", data or "nil", " err=", err or "nil")
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "setkeepalive: ok=1") {
		t.Errorf("expected setkeepalive=1, got: %q", output)
	}
	if !strings.Contains(output, "send: ok=nil err=closed") {
		t.Errorf("expected 'closed' after setkeepalive, got: %q", output)
	}
	if !strings.Contains(output, "receive: ok=nil err=closed") {
		t.Errorf("expected receive 'closed' after setkeepalive, got: %q", output)
	}
}

func TestTCPSocketGetreusedtimesFresh(t *testing.T) {
	code := `
		local sock = golapis.socket.tcp()
		local n, err = sock:getreusedtimes()
		golapis.say("fresh: ", n or "nil", " err=", err or "nil")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "fresh: 0 err=nil") {
		t.Errorf("expected 'fresh: 0', got: %q", output)
	}
}

func TestTCPSocketGetreusedtimesAfterClose(t *testing.T) {
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		assert(sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		assert(sock:close())
		local n, err = sock:getreusedtimes()
		golapis.say("after close: n=", n or "nil", " err=", err or "nil")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "after close: n=nil err=closed") {
		t.Errorf("expected nil/closed after close, got: %q", output)
	}
}

func TestTCPSocketGetreusedtimesAfterSetkeepalive(t *testing.T) {
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		assert(sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		assert(sock:setkeepalive())
		local n, err = sock:getreusedtimes()
		golapis.say("after setkeepalive: n=", n or "nil", " err=", err or "nil")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "after setkeepalive: n=nil err=closed") {
		t.Errorf("expected nil/closed after setkeepalive, got: %q", output)
	}
}

func TestTCPSocketPoolReuseSameKey(t *testing.T) {
	serverAddr, accepts, cleanup := startCountingTCPServer(t)
	defer cleanup()

	code := `
		local sock = golapis.socket.tcp()
		assert(sock:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		golapis.say("first reused=", sock:getreusedtimes())
		assert(sock:setkeepalive())

		local sock2 = golapis.socket.tcp()
		assert(sock2:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		golapis.say("second reused=", sock2:getreusedtimes())
		assert(sock2:setkeepalive())

		local sock3 = golapis.socket.tcp()
		assert(sock3:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		golapis.say("third reused=", sock3:getreusedtimes())
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "first reused=0") {
		t.Errorf("expected first reused=0: %q", output)
	}
	if !strings.Contains(output, "second reused=1") {
		t.Errorf("expected second reused=1: %q", output)
	}
	if !strings.Contains(output, "third reused=2") {
		t.Errorf("expected third reused=2: %q", output)
	}
	if got := accepts(); got != 1 {
		t.Errorf("expected 1 accept across 3 connects, got %d", got)
	}
}

func TestTCPSocketPoolReuseRoundTrip(t *testing.T) {
	// Verify the reused socket can actually round-trip data — i.e. the
	// watcher didn't steal a byte during the reclaim handoff.
	serverAddr, accepts, cleanup := startCountingTCPServer(t)
	defer cleanup()

	code := `
		local s = golapis.socket.tcp()
		s:settimeout(1000)
		assert(s:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		assert(s:send("first\n"))
		local line = assert(s:receive("*l"))
		golapis.say("first=", line)
		assert(s:setkeepalive())

		local s2 = golapis.socket.tcp()
		s2:settimeout(1000)
		assert(s2:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		golapis.say("reused=", s2:getreusedtimes())
		assert(s2:send("second\n"))
		local line2 = assert(s2:receive("*l"))
		golapis.say("second=", line2)
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "first=first") {
		t.Errorf("expected first=first, got: %q", output)
	}
	if !strings.Contains(output, "reused=1") {
		t.Errorf("expected reused=1, got: %q", output)
	}
	if !strings.Contains(output, "second=second") {
		t.Errorf("expected second=second on reused socket, got: %q", output)
	}
	if got := accepts(); got != 1 {
		t.Errorf("expected 1 accept (single conn reused), got %d", got)
	}
}

func TestTCPSocketPoolDifferentKeys(t *testing.T) {
	addrA, acceptsA, cleanupA := startCountingTCPServer(t)
	defer cleanupA()
	addrB, acceptsB, cleanupB := startCountingTCPServer(t)
	defer cleanupB()

	code := `
		local function cycle(host, port)
			local s = golapis.socket.tcp()
			assert(s:connect(host, port))
			assert(s:setkeepalive())
			local s2 = golapis.socket.tcp()
			assert(s2:connect(host, port))
			golapis.say(host, ":", port, " reused=", s2:getreusedtimes())
		end
		cycle("` + addrA.IP.String() + `", ` + itoa(addrA.Port) + `)
		cycle("` + addrB.IP.String() + `", ` + itoa(addrB.Port) + `)
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, " reused=1") {
		t.Errorf("expected reuse on each pool, got: %q", output)
	}
	if got := acceptsA(); got != 1 {
		t.Errorf("server A: expected 1 accept, got %d", got)
	}
	if got := acceptsB(); got != 1 {
		t.Errorf("server B: expected 1 accept, got %d", got)
	}
}

func TestTCPSocketPoolCustomPoolName(t *testing.T) {
	addrA, _, cleanupA := startCountingTCPServer(t)
	defer cleanupA()
	addrB, acceptsB, cleanupB := startCountingTCPServer(t)
	defer cleanupB()

	// First connect to A under custom pool "shared", setkeepalive.
	// Then connect to B (different host:port) under the SAME custom pool —
	// must take the pooled conn from A's setkeepalive (sharing across hosts).
	code := `
		local s = golapis.socket.tcp()
		assert(s:connect("` + addrA.IP.String() + `", ` + itoa(addrA.Port) + `, {pool="shared"}))
		assert(s:setkeepalive())

		local s2 = golapis.socket.tcp()
		assert(s2:connect("` + addrB.IP.String() + `", ` + itoa(addrB.Port) + `, {pool="shared"}))
		golapis.say("reused=", s2:getreusedtimes())
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "reused=1") {
		t.Errorf("expected custom pool reuse, got: %q", output)
	}
	if got := acceptsB(); got != 0 {
		t.Errorf("server B should not have been dialed; got %d accepts", got)
	}
}

func TestTCPSocketPoolEviction(t *testing.T) {
	serverAddr, accepts, cleanup := startCountingTCPServer(t)
	defer cleanup()

	// pool_size=2, three sockets enter the pool — oldest must be evicted.
	// All four in a row with the same host:port and pool_size=2.
	code := `
		local function open()
			local s = golapis.socket.tcp()
			assert(s:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
			return s
		end
		local s1 = open()
		local s2 = open()
		local s3 = open()
		assert(s1:setkeepalive(60000, 2))
		assert(s2:setkeepalive(60000, 2))
		assert(s3:setkeepalive(60000, 2)) -- evicts s1

		-- Now we should be able to reuse exactly 2 conns
		local r1 = golapis.socket.tcp()
		assert(r1:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		golapis.say("r1 reused=", r1:getreusedtimes())
		local r2 = golapis.socket.tcp()
		assert(r2:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		golapis.say("r2 reused=", r2:getreusedtimes())
		-- third must dial fresh
		local r3 = golapis.socket.tcp()
		assert(r3:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		golapis.say("r3 reused=", r3:getreusedtimes())
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "r1 reused=1") {
		t.Errorf("expected r1 reused=1, got: %q", output)
	}
	if !strings.Contains(output, "r2 reused=1") {
		t.Errorf("expected r2 reused=1, got: %q", output)
	}
	if !strings.Contains(output, "r3 reused=0") {
		t.Errorf("expected r3 reused=0 (fresh dial), got: %q", output)
	}
	// 3 initial dials + 1 fresh dial after eviction = 4
	if got := accepts(); got != 4 {
		t.Errorf("expected 4 accepts (3 + 1 after eviction), got %d", got)
	}
}

func TestTCPSocketPoolIdleTimeout(t *testing.T) {
	serverAddr, accepts, cleanup := startCountingTCPServer(t)
	defer cleanup()

	code := `
		local s = golapis.socket.tcp()
		assert(s:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		assert(s:setkeepalive(50)) -- 50ms idle timeout
		golapis.sleep(0.2)         -- wait long enough for watcher to evict

		local s2 = golapis.socket.tcp()
		assert(s2:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		golapis.say("reused=", s2:getreusedtimes())
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "reused=0") {
		t.Errorf("expected fresh dial after idle timeout, got: %q", output)
	}
	if got := accepts(); got != 2 {
		t.Errorf("expected 2 accepts (idle timeout forced redial), got %d", got)
	}
}

func TestTCPSocketPoolUnreadData(t *testing.T) {
	// Server sends "line1\nextra"; client does line-mode receive which
	// over-reads chunks and leaves "extra" in sock.readBuf. setkeepalive
	// must reject with "unread data in buffer".
	serverAddr, cleanup := startTCPWriteServer(t, []byte("line1\nextra"))
	defer cleanup()

	code := `
		local s = golapis.socket.tcp()
		assert(s:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		local line = s:receive("*l")
		golapis.say("got=", line)
		local ok, err = s:setkeepalive()
		golapis.say("setkeepalive: ok=", ok or "nil", " err=", err or "nil")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "got=line1") {
		t.Errorf("expected got=line1, got: %q", output)
	}
	if !strings.Contains(output, "setkeepalive: ok=nil err=unread data in buffer") {
		t.Errorf("expected unread data error, got: %q", output)
	}
}

func TestTCPSocketSetkeepaliveNotConnected(t *testing.T) {
	code := `
		local s = golapis.socket.tcp()
		local ok, err = s:setkeepalive()
		golapis.say("ok=", ok or "nil", " err=", err or "nil")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "ok=nil err=not connected") {
		t.Errorf("expected 'not connected', got: %q", output)
	}
}

func TestTCPSocketSetkeepaliveClosed(t *testing.T) {
	serverAddr, cleanup := startTCPEchoServer(t)
	defer cleanup()

	code := `
		local s = golapis.socket.tcp()
		assert(s:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		assert(s:close())
		local ok, err = s:setkeepalive()
		golapis.say("ok=", ok or "nil", " err=", err or "nil")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "ok=nil err=closed") {
		t.Errorf("expected 'closed' after close, got: %q", output)
	}
}

func TestTCPSocketPoolServerClose(t *testing.T) {
	// Server accepts a connection, replies with nothing; we'll close it
	// from the server side after the client setkeepalives.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	serverAddr := listener.Addr().(*net.TCPAddr)

	var acceptsCount int32
	connsCh := make(chan net.Conn, 8)
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&acceptsCount, 1)
			connsCh <- c
		}
	}()

	code := `
		local s = golapis.socket.tcp()
		assert(s:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		assert(s:setkeepalive(60000))
		golapis.sleep(0.05) -- let watcher start its Read

		-- (Go test will close the server-side conn here; we sleep to allow detection)
		golapis.sleep(0.2)

		local s2 = golapis.socket.tcp()
		assert(s2:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		golapis.say("reused=", s2:getreusedtimes())
	`

	// Run the test in a goroutine, kill the server-side conn during the sleep.
	done := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := runTCPTest(t, code)
		done <- struct {
			out string
			err error
		}{out, err}
	}()

	// Grab the server-side conn and close it after a short delay
	// (after the client's setkeepalive but during the 200ms sleep).
	select {
	case c := <-connsCh:
		time.Sleep(80 * time.Millisecond)
		c.Close()
	case <-time.After(2 * time.Second):
		t.Fatalf("never accepted")
	}

	res := <-done
	if res.err != nil {
		t.Fatalf("Lua error: %v", res.err)
	}
	if !strings.Contains(res.out, "reused=0") {
		t.Errorf("expected fresh dial after server close, got: %q", res.out)
	}
	if atomic.LoadInt32(&acceptsCount) != 2 {
		t.Errorf("expected 2 accepts (watcher detected close, second connect dialed), got %d", acceptsCount)
	}
}

func TestTCPSocketPoolShutdownDrains(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	serverAddr := listener.Addr().(*net.TCPAddr)

	eofCh := make(chan struct{}, 4)
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var b [1]byte
				_, err := c.Read(b[:])
				if err != nil {
					eofCh <- struct{}{}
				}
			}(c)
		}
	}()

	code := `
		local s = golapis.socket.tcp()
		assert(s:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		assert(s:setkeepalive(60000))
	`

	// Manual lifecycle so we can observe Close behavior.
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("nil state")
	}
	buf := &bytes.Buffer{}
	gls.SetOutputWriter(buf)
	gls.Start()
	if err := gls.RunString(code); err != nil {
		t.Fatalf("RunString: %v", err)
	}
	gls.Wait()
	gls.Stop()
	gls.Close()

	select {
	case <-eofCh:
		// good
	case <-time.After(2 * time.Second):
		t.Fatalf("server never observed EOF/close after state shutdown")
	}
}

func TestTCPSocketPoolStartAfterStop(t *testing.T) {
	// After Stop()/Start() cycle, setkeepalive must work again
	// (tcpPoolsClosed should be reset by Start).
	serverAddr, _, cleanup := startCountingTCPServer(t)
	defer cleanup()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("nil state")
	}
	defer gls.Close()

	buf := &bytes.Buffer{}
	gls.SetOutputWriter(buf)

	code := `
		local s = golapis.socket.tcp()
		assert(s:connect("` + serverAddr.IP.String() + `", ` + itoa(serverAddr.Port) + `))
		local ok, err = s:setkeepalive()
		golapis.say("ok=", ok or "nil", " err=", err or "nil")
	`

	gls.Start()
	if err := gls.RunString(code); err != nil {
		t.Fatalf("first run: %v", err)
	}
	gls.Wait()
	gls.Stop()

	gls.Start()
	if err := gls.RunString(code); err != nil {
		t.Fatalf("second run after restart: %v", err)
	}
	gls.Wait()
	gls.Stop()

	out := buf.String()
	// Two successful setkeepalive results
	count := strings.Count(out, "ok=1 err=nil")
	if count != 2 {
		t.Errorf("expected 2 successful setkeepalive across Stop/Start, got %d in %q", count, out)
	}
}

// TestTCPSocketPoolStress hammers the pool with many concurrent timer-spawned
// coroutines, each doing several connect/round-trip/setkeepalive cycles. Run
// under -race to catch any synchronization regressions in the hot path. It
// also asserts that pool reuse meaningfully reduces the number of TCP accepts.
func TestTCPSocketPoolStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in -short mode")
	}
	serverAddr, accepts, cleanup := startCountingTCPServer(t)
	defer cleanup()

	const workers = 100
	const cyclesPerWorker = 10
	const totalOps = workers * cyclesPerWorker

	code := `
		local host = "` + serverAddr.IP.String() + `"
		local port = ` + itoa(serverAddr.Port) + `
		local workers = ` + itoa(workers) + `
		local cycles = ` + itoa(cyclesPerWorker) + `

		local done = 0
		local errors_count = 0
		local first_error = nil

		local function fail(msg)
			errors_count = errors_count + 1
			if first_error == nil then first_error = msg end
		end

		local function worker()
			for i = 1, cycles do
				local s = golapis.socket.tcp()
				s:settimeout(2000)
				local ok, err = s:connect(host, port)
				if not ok then fail("connect: " .. tostring(err)); return end
				local sent, err = s:send("ping\n")
				if not sent then fail("send: " .. tostring(err)); return end
				local line, err = s:receive("*l")
				if not line then fail("recv: " .. tostring(err)); return end
				if line ~= "ping" then fail("bad echo: " .. tostring(line)); return end
				local ok, err = s:setkeepalive()
				if not ok then fail("ska: " .. tostring(err)); return end
			end
		end

		for i = 1, workers do
			golapis.timer.at(0, function(premature)
				if not premature then worker() end
				done = done + 1
			end)
		end

		while done < workers do
			golapis.sleep(0.01)
		end

		golapis.say("done=", done)
		golapis.say("errors=", errors_count)
		if first_error then golapis.say("first_error=", first_error) end
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "done="+itoa(workers)) {
		t.Errorf("expected all %d workers to finish, got: %q", workers, output)
	}
	if !strings.Contains(output, "errors=0") {
		t.Errorf("got errors during stress run: %q", output)
	}
	a := accepts()
	t.Logf("stress accepts=%d, total_ops=%d (pool reuse should keep accepts much lower)", a, totalOps)
	if int(a) >= totalOps {
		t.Errorf("pool reuse failed: %d accepts for %d ops", a, totalOps)
	}
}

// TestTCPSocketPoolIdleRaceStress exercises the natural-expiry-vs-checkout
// race directly: very short idle timeouts force watchers to expire often,
// while concurrent connects race to claim entries. Any lurking bug where an
// expired connection gets handed back would show up as a round-trip failure.
func TestTCPSocketPoolIdleRaceStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in -short mode")
	}
	serverAddr, _, cleanup := startCountingTCPServer(t)
	defer cleanup()

	code := `
		local host = "` + serverAddr.IP.String() + `"
		local port = ` + itoa(serverAddr.Port) + `
		local workers = 50
		local cycles = 10
		local done = 0
		local errors_count = 0
		local first_error = nil

		local function worker()
			for i = 1, cycles do
				local s = golapis.socket.tcp()
				s:settimeout(2000)
				local ok, err = s:connect(host, port)
				if not ok then
					errors_count = errors_count + 1
					if not first_error then first_error = "connect: " .. tostring(err) end
					return
				end
				assert(s:send("ping\n"))
				local line, err = s:receive("*l")
				if not line or line ~= "ping" then
					errors_count = errors_count + 1
					if not first_error then first_error = "recv: " .. tostring(line) .. "/" .. tostring(err) end
					return
				end
				-- 5ms idle timeout: watchers fire constantly, racing with checkouts
				assert(s:setkeepalive(5))
				-- Random small sleep to vary timing
				if (i % 3) == 0 then golapis.sleep(0.003) end
			end
		end

		for i = 1, workers do
			golapis.timer.at(0, function(premature)
				if not premature then worker() end
				done = done + 1
			end)
		end
		while done < workers do golapis.sleep(0.005) end
		golapis.say("done=", done, " errors=", errors_count)
		if first_error then golapis.say("first_error=", first_error) end
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "done=50") {
		t.Errorf("expected all workers done, got: %q", output)
	}
	if !strings.Contains(output, "errors=0") {
		t.Errorf("got errors during idle race stress: %q", output)
	}
}

// TestTCPSocketPoolEvictionStress repeatedly fills and overflows a small pool
// to exercise the eviction path under churn.
func TestTCPSocketPoolEvictionStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in -short mode")
	}
	serverAddr, _, cleanup := startCountingTCPServer(t)
	defer cleanup()

	code := `
		local host = "` + serverAddr.IP.String() + `"
		local port = ` + itoa(serverAddr.Port) + `

		-- Open many connections with a small pool size to drive eviction.
		for i = 1, 50 do
			local s = golapis.socket.tcp()
			s:settimeout(2000)
			assert(s:connect(host, port))
			assert(s:send("ping\n"))
			assert(s:receive("*l"))
			assert(s:setkeepalive(60000, 3)) -- pool_size=3
		end

		golapis.say("done")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "done") {
		t.Errorf("expected 'done', got: %q", output)
	}
}

func TestTCPSocketPoolUnixSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "pool.sock")

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer listener.Close()

	var acceptsCount int32
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&acceptsCount, 1)
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(c)
		}
	}()

	code := `
		local s = golapis.socket.tcp()
		assert(s:connect("unix:` + sockPath + `"))
		-- Round-trip ensures the server-side Accept goroutine has run
		assert(s:send("ping\n"))
		assert(s:receive("*l"))
		assert(s:setkeepalive())
		local s2 = golapis.socket.tcp()
		assert(s2:connect("unix:` + sockPath + `"))
		golapis.say("reused=", s2:getreusedtimes())
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "reused=1") {
		t.Errorf("expected unix socket reuse, got: %q", output)
	}
	if got := atomic.LoadInt32(&acceptsCount); got != 1 {
		t.Errorf("expected 1 accept, got %d", got)
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

func TestTCPSocketChildCoroutineCanUseSocket(t *testing.T) {
	// Test that child coroutines created via coroutine.create can use sockets
	// created in the parent coroutine (same request context)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	// Echo server
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	code := `
		local port = ` + itoa(port) + `

		-- Create socket in main coroutine
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)

		local ok, err = sock:connect("127.0.0.1", port)
		if not ok then
			golapis.say("connect failed: ", err)
			return
		end
		golapis.say("connected")

		-- Create a child coroutine that uses the same socket
		local co = coroutine.create(function()
			-- Send from child coroutine
			local bytes, err = sock:send("hello from child\n")
			if not bytes then
				golapis.say("child send failed: ", err)
				return
			end
			golapis.say("child sent: ", bytes, " bytes")

			-- Receive from child coroutine
			local line, err = sock:receive("*l")
			if not line then
				golapis.say("child receive failed: ", err)
				return
			end
			golapis.say("child received: ", line)
		end)

		-- Resume the child coroutine
		local ok, err = coroutine.resume(co)
		if not ok then
			golapis.say("coroutine error: ", err)
		end

		sock:close()
		golapis.say("done")
	`

	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	// Socket operations from child coroutine should succeed
	if strings.Contains(output, "bad request") {
		t.Errorf("child coroutine should be able to use socket, got 'bad request': %q", output)
	}
	if !strings.Contains(output, "connected") {
		t.Errorf("expected 'connected', got: %q", output)
	}
	if !strings.Contains(output, "child sent: 17 bytes") {
		t.Errorf("expected 'child sent: 17 bytes', got: %q", output)
	}
	if !strings.Contains(output, "child received: hello from child") {
		t.Errorf("expected 'child received: hello from child', got: %q", output)
	}
	if !strings.Contains(output, "done") {
		t.Errorf("expected 'done', got: %q", output)
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

func startTCPWriteServer(t *testing.T, payload []byte) (*net.TCPAddr, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}

	serverAddr := listener.Addr().(*net.TCPAddr)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				c.Write(payload)
			}(conn)
		}
	}()

	cleanup := func() {
		listener.Close()
	}

	return serverAddr, cleanup
}

// startCountingTCPServer returns an echo server that counts Accept calls.
// The accepts function returns the running count.
func startCountingTCPServer(t *testing.T) (*net.TCPAddr, func() int32, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	serverAddr := listener.Addr().(*net.TCPAddr)

	var count int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&count, 1)
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	accepts := func() int32 { return atomic.LoadInt32(&count) }
	cleanup := func() { listener.Close() }
	return serverAddr, accepts, cleanup
}
