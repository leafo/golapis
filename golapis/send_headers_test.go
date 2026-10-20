package golapis

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHeadersSentDefault(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local before = golapis.headers_sent
		golapis.say("x")
		local after = golapis.headers_sent
		golapis.say(tostring(before), ",", tostring(after))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if got := w.Body.String(); got != "x\nfalse,true\n" {
		t.Errorf("body = %q", got)
	}
}

func TestSendHeadersWithoutBody(t *testing.T) {
	w, req, err := runLuaWithHTTP(t, `
		golapis.status = 101
		golapis.header["Upgrade"] = "websocket"
		local ok, err = golapis.send_headers()
		assert(ok == 1, err)
		assert(golapis.headers_sent == true)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !req.HeadersSent {
		t.Error("HeadersSent should be true")
	}
	if w.Code != 101 {
		t.Errorf("Response code = %d, want 101", w.Code)
	}
	if got := w.Header().Get("Upgrade"); got != "websocket" {
		t.Errorf("Upgrade header = %q", got)
	}
}

func TestSendHeadersThenStatusFails(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `
		golapis.send_headers()
		golapis.status = 500
	`)
	if err == nil || !strings.Contains(err.Error(), "after response headers have been sent") {
		t.Fatalf("expected headers-sent error, got: %v", err)
	}
}

func TestFlushSendsHeadersAndFlushes(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.status = 202
		golapis.say("partial")
		local ok, err = golapis.flush(true)
		assert(ok == 1, err)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if w.Code != 202 {
		t.Errorf("Response code = %d, want 202", w.Code)
	}
	if !w.Flushed {
		t.Error("expected ResponseRecorder to be flushed")
	}
}

func TestSendHeadersOutsideRequest(t *testing.T) {
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()
	gls.Start()
	defer gls.Stop()

	err := gls.RunString(`
		local ok, err = golapis.send_headers()
		assert(ok == nil and err == "no request found", tostring(err))
		ok, err = golapis.flush()
		assert(ok == nil and err == "no request found", tostring(err))
		assert(golapis.headers_sent == false)
		assert(golapis.req.http_version() == nil)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
}

func TestReqHttpVersion(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `golapis.say(golapis.req.http_version())`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	// httptest.NewRequest defaults to HTTP/1.1
	if got := w.Body.String(); got != "1.1\n" {
		t.Errorf("body = %q, want 1.1", got)
	}
}

func TestReqHttpVersionHTTP2(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Proto, r.ProtoMajor, r.ProtoMinor = "HTTP/2.0", 2, 0
	w, _, err := runLuaWithHTTPRequest(t, r, `golapis.say(golapis.req.http_version())`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if got := w.Body.String(); got != "2\n" {
		t.Errorf("body = %q, want 2", got)
	}
}

func TestHeadersSentReadOnly(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `golapis.headers_sent = false`)
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only error, got: %v", err)
	}
}

func TestConfigTable(t *testing.T) {
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()
	gls.Start()
	defer gls.Stop()

	err := gls.RunString(`
		assert(type(golapis.config) == "table")
		assert(golapis.config.debug == false)
		assert(golapis.config.subsystem == "http")
		assert(type(golapis.config.ngx_lua_version) == "number")
		assert(golapis.config.ngx_lua_version >= 9011)
		assert(type(golapis.config.nginx_version) == "number")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
}
