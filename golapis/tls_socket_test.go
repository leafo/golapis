package golapis

import (
	"bytes"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tlsTestServer starts an HTTPS server whose certificate (valid for
// example.com and 127.0.0.1) is written to a PEM file for ssl_verify
func tlsTestServer(t *testing.T, handler http.Handler) (server *httptest.Server, host, port, certPath string) {
	t.Helper()
	if handler == nil {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "hello %s", r.URL.Path)
		})
	}
	server = httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	host, port, _ = net.SplitHostPort(server.Listener.Addr().String())
	certPath = filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(certPath, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return server, host, port, certPath
}

// runTLS runs code with `host`, `port` and a connected-socket helper bound
func runTLS(t *testing.T, gls *GolapisLuaState, host, port, code string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	gls.SetOutputWriter(&out)
	err := gls.RunString(`
		local host, port = "` + host + `", ` + port + `
		local function connected()
			local s = golapis.socket.tcp()
			s:settimeout(3000)
			assert(s:connect(host, port))
			return s
		end
		local function say(...)
			local t = {}
			for i = 1, select("#", ...) do t[i] = tostring((select(i, ...))) end
			golapis.say(table.concat(t, " "))
		end
	` + code)
	gls.Wait()
	gls.SetOutputWriter(nil)
	return out.String(), err
}

func newTLSState(t *testing.T) *GolapisLuaState {
	t.Helper()
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	gls.Start()
	t.Cleanup(func() { gls.Stop(); gls.Close() })
	return gls
}

func TestSSLHandshakeAndRequest(t *testing.T) {
	_, host, port, _ := tlsTestServer(t, nil)
	gls := newTLSState(t)
	out, err := runTLS(t, gls, host, port, `
		local s = connected()
		local sess, err = s:sslhandshake()
		say(type(sess), err)
		assert(s:send("GET /x HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"))
		say(s:receive("*l"))
		local body
		while true do
			local line = s:receive("*l")
			if line == "" then break end
		end
		say(s:receive("*a"))
		s:close()
		-- reused_session == false asks for no session: plain true comes back
		local s2 = connected()
		say(s2:sslhandshake(false))
		-- a second handshake on the same connection succeeds at once
		say(type(s2:sslhandshake()), s2:sslhandshake(false))
		s2:close()
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "table nil\nHTTP/1.1 200 OK\nhello /x\ntrue\ntable true\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestSSLHandshakeVerify(t *testing.T) {
	_, host, port, certPath := tlsTestServer(t, nil)
	gls := newTLSState(t)

	// Self-signed server, system roots: verification fails and the socket
	// is closed like nginx
	out, err := runTLS(t, gls, host, port, `
		local s = connected()
		say(s:sslhandshake(nil, "example.com", true))
		say(s:send("x"))
		-- IP connect, no server_name: the chain alone is verified
		local s2 = connected()
		say(s2:sslhandshake(nil, nil, true))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "nil certificate verify failed: unable to get local issuer certificate\nnil closed\n" +
		"nil certificate verify failed: unable to get local issuer certificate\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}

	// Trusted bundle: verification passes for a covered name, fails for
	// another name
	if err := gls.SetSSLTrustedCertificate(certPath); err != nil {
		t.Fatal(err)
	}
	out, err = runTLS(t, gls, host, port, `
		local s = connected()
		say(type(s:sslhandshake(nil, "example.com", true)))
		assert(s:send("GET / HTTP/1.0\r\n\r\n"))
		say(s:receive("*l"))
		s:close()
		local s2 = connected()
		local ok, err = s2:sslhandshake(nil, "other.test", true)
		say(ok, err:match("^certificate verify failed: ") ~= nil, err:find("other.test", 1, true) ~= nil)
		-- verification off still works with any name
		local s3 = connected()
		say(type(s3:sslhandshake(nil, "other.test", false)))
		s3:close()
		-- IP connect with no server_name: chain verified, no hostname check
		local s4 = connected()
		say(type(s4:sslhandshake(nil, nil, true)))
		assert(s4:send("GET /ip HTTP/1.0\r\n\r\n"))
		say(s4:receive("*l"))
		s4:close()
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want = "table\nHTTP/1.0 200 OK\nnil true true\ntable\ntable\nHTTP/1.0 200 OK\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}

	// Bad bundle paths are reported
	if err := gls.SetSSLTrustedCertificate(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Error("expected error for a missing bundle")
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	os.WriteFile(empty, []byte("not a cert"), 0o600)
	if err := gls.SetSSLTrustedCertificate(empty); err == nil || !strings.Contains(err.Error(), "no certificates") {
		t.Errorf("expected 'no certificates' error, got %v", err)
	}
}

func TestSSLHandshakeErrors(t *testing.T) {
	_, host, port, _ := tlsTestServer(t, nil)
	gls := newTLSState(t)
	out, err := runTLS(t, gls, host, port, `
		local s = golapis.socket.tcp()
		say(s:sslhandshake())
		s:close()
		say(s:sslhandshake())
		local s2 = connected()
		say(pcall(s2.sslhandshake, s2, nil, 42))
		say(select(2, pcall(s2.sslhandshake, s2, 1, 2, 3, 4, 5)))
		-- plaintext read-ahead can't be upgraded
		assert(s2:send("GET / HTTP/1.0\r\n\r\n"))
		s2:close()
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	for _, want := range []string{
		"nil not connected\n",
		"nil closed\n",
		"bad argument #2 to 'sslhandshake' (string expected, got number)",
		"expecting 1 ~ 5 arguments (including the object), but seen 6",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestSSLHandshakeTimeoutAndAbort(t *testing.T) {
	// A server that accepts and never speaks TLS
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	gls := newTLSState(t)
	out, err := runTLS(t, gls, host, port, `
		local s = golapis.socket.tcp()
		s:settimeout(100)
		assert(s:connect(host, port))
		golapis.update_time()
		local start = golapis.now()
		say(s:sslhandshake())
		golapis.update_time()
		say(golapis.now() - start < 2)
		say(s:send("x"))
		-- a killed thread's pending handshake is cancelled
		local s2 = golapis.socket.tcp()
		s2:settimeout(5000)
		assert(s2:connect(host, port))
		local th = golapis.thread.spawn(function() say("spawned", s2:sslhandshake()) end)
		golapis.sleep(0.05)
		say(golapis.thread.kill(th))
		say(s2:send("x"))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "nil timeout\ntrue\nnil closed\n1\nnil closed\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestSSLCloseDoesNotBlock(t *testing.T) {
	// tls.Conn.Close would wait up to 5s to write close_notify to a peer
	// that isn't reading; the cosocket closes the transport directly
	_, host, port, _ := tlsTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// hold the connection without reading further
		hj, _ := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		time.Sleep(3 * time.Second)
		c.Close()
	}))
	gls := newTLSState(t)
	start := time.Now()
	out, err := runTLS(t, gls, host, port, `
		local s = connected()
		assert(s:sslhandshake())
		assert(s:send("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
		-- fill the peer's receive window so a close_notify could not be written
		local chunk = string.rep("x", 65536)
		s:settimeout(200)
		local stalled = false
		for i = 1, 4096 do
			local ok, err = s:send(chunk)
			if not ok then stalled = err break end
		end
		say("send stalled:", stalled)
		say(s:close())
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if out != "send stalled: timeout\n1\n" {
		t.Errorf("output = %q", out)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("close path took %v", d)
	}
}

func TestSSLHandshakeKeepalive(t *testing.T) {
	_, host, port, _ := tlsTestServer(t, nil)
	gls := newTLSState(t)
	out, err := runTLS(t, gls, host, port, `
		local s = connected()
		assert(s:sslhandshake())
		assert(s:send("GET /a HTTP/1.1\r\nHost: example.com\r\n\r\n"))
		say(s:receive("*l"))
		local len
		while true do
			local line = s:receive("*l")
			if line == "" then break end
			len = len or tonumber(line:match("^Content%-Length: (%d+)"))
		end
		say(s:receive(len))
		say(s:setkeepalive())
		-- the pooled connection is still TLS: sslhandshake succeeds at once
		-- and the next request goes over the same connection
		local s2 = connected()
		say(s2:getreusedtimes(), type(s2:sslhandshake()))
		assert(s2:send("GET /b HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"))
		say(s2:receive("*l"))
		s2:close()
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "HTTP/1.1 200 OK\nhello /a\n1\n1 table\nHTTP/1.1 200 OK\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestSSLHandshakeRequestSocket(t *testing.T) {
	// The request socket can't be upgraded; and golapis's own handler works
	// behind a TLS listener, so a Lua client can speak to a raw-socket echo
	// over TLS end to end
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	if err := gls.LoadEntryPoint(CodeEntryPoint{Code: `
		local sock = assert(golapis.req.socket(true))
		local ok, err = sock:sslhandshake()
		sock:send("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
		local line = sock:receive("*l")
		sock:send("echo " .. line .. " " .. tostring(err) .. "\n")
	`}); err != nil {
		t.Fatal(err)
	}
	gls.Start()
	defer gls.Close()
	defer gls.Stop()
	server, host, port, _ := tlsTestServer(t, gls.HTTPHandler(DefaultHTTPServerConfig()))
	_ = server

	out, err := runTLS(t, gls, host, port, `
		local s = connected()
		assert(s:sslhandshake())
		assert(s:send("GET /ws HTTP/1.1\r\nHost: x\r\n\r\n"))
		say(s:receive("*l"))
		s:receive("*l") s:receive("*l")
		assert(s:send("ping\n"))
		say(s:receive("*l"))
		s:close()
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "HTTP/1.1 200 OK\necho ping not supported on the request socket\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestSSLHandshakeStartTLS(t *testing.T) {
	// A plaintext greeting followed by an upgrade on the same connection,
	// like PostgreSQL or SMTP STARTTLS. The greeting is read with a short
	// timeout that has long expired by the time the handshake starts.
	server, _, _, _ := tlsTestServer(t, nil)
	cert := server.TLS.Certificates[0]
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				fmt.Fprint(c, "220 ready\n")
				tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}})
				if err := tc.Handshake(); err != nil {
					return
				}
				buf := make([]byte, 64)
				n, _ := tc.Read(buf)
				fmt.Fprintf(tc, "echo %s", buf[:n])
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	gls := newTLSState(t)
	out, err := runTLS(t, gls, host, port, `
		local s = golapis.socket.tcp()
		s:settimeout(50)
		assert(s:connect(host, port))
		say(s:receive("*l"))
		golapis.sleep(0.1)
		s:settimeout(3000)
		say(s:sslhandshake(false))
		assert(s:send("hi\n"))
		say(s:receive("*l"))
		s:close()
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	want := "220 ready\ntrue\necho hi\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}
