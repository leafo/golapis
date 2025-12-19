package golapis

import (
	"net/http/httptest"
	"testing"
)

// runLuaWithHTTP runs Lua code in an HTTP context and returns the response recorder
func runLuaWithHTTP(t *testing.T, code string) (*httptest.ResponseRecorder, *GolapisRequest, error) {
	t.Helper()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	gls.Start()
	defer gls.Stop()

	// Create mock HTTP request/response
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)

	// Create GolapisRequest and wrap writer (like http.go does)
	req := NewGolapisRequest(r)
	wrappedWriter := req.WrapResponseWriter(w)

	// Send event to Lua state
	resp := make(chan *StateResponse, 1)
	gls.eventChan <- &StateEvent{
		Type:         EventRunString,
		Code:         code,
		OutputWriter: wrappedWriter,
		Request:      req,
		Response:     resp,
	}

	result := <-resp
	gls.Wait()

	// Flush headers if not already sent (like http.go does for no-body case)
	req.FlushHeaders(w)

	return w, req, result.Error
}

func TestHeaderBasicSet(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.header["X-Custom"] = "hello world"
		golapis.say("ok")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Header().Get("X-Custom"); got != "hello world" {
		t.Errorf("X-Custom header = %q, want %q", got, "hello world")
	}
}

func TestHeaderUnderscoreConversion(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.header.content_type = "text/plain"
		golapis.say("ok")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Header().Get("Content-Type"); got != "text/plain" {
		t.Errorf("Content-Type header = %q, want %q", got, "text/plain")
	}
}

func TestHeaderCaseInsensitive(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.header["x-foo-bar"] = "test"
		golapis.say("ok")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	// Header should be canonicalized
	if got := w.Header().Get("X-Foo-Bar"); got != "test" {
		t.Errorf("X-Foo-Bar header = %q, want %q", got, "test")
	}
}

func TestHeaderMultiValue(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.header["Set-Cookie"] = {"session=abc123", "theme=dark"}
		golapis.say("ok")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	cookies := w.Header()["Set-Cookie"]
	if len(cookies) != 2 {
		t.Fatalf("Set-Cookie header count = %d, want 2", len(cookies))
	}
	if cookies[0] != "session=abc123" {
		t.Errorf("Set-Cookie[0] = %q, want %q", cookies[0], "session=abc123")
	}
	if cookies[1] != "theme=dark" {
		t.Errorf("Set-Cookie[1] = %q, want %q", cookies[1], "theme=dark")
	}
}

func TestHeaderClearWithNil(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.header["X-Remove"] = "temporary"
		golapis.header["X-Remove"] = nil
		golapis.say("ok")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Header().Get("X-Remove"); got != "" {
		t.Errorf("X-Remove header = %q, want empty (deleted)", got)
	}
}

func TestHeaderClearWithEmptyTable(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.header["X-Remove"] = "temporary"
		golapis.header["X-Remove"] = {}
		golapis.say("ok")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Header().Get("X-Remove"); got != "" {
		t.Errorf("X-Remove header = %q, want empty (deleted)", got)
	}
}

func TestHeaderSingleValueWithTable(t *testing.T) {
	// For single-value headers like Content-Type, only last value should be used
	w, _, err := runLuaWithHTTP(t, `
		golapis.header.content_type = {"text/html", "text/plain"}
		golapis.say("ok")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Header().Get("Content-Type"); got != "text/plain" {
		t.Errorf("Content-Type header = %q, want %q (last value)", got, "text/plain")
	}
}

func TestHeaderNoBody(t *testing.T) {
	// Headers should be flushed even without body output
	w, _, err := runLuaWithHTTP(t, `
		golapis.header["X-No-Body"] = "still works"
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Header().Get("X-No-Body"); got != "still works" {
		t.Errorf("X-No-Body header = %q, want %q", got, "still works")
	}
}

func TestHeaderReadBack(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.header["X-Test"] = "readable"
		local val = golapis.header["X-Test"]
		if val ~= "readable" then
			error("header read failed: got " .. tostring(val))
		end
		golapis.say("ok")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Header().Get("X-Test"); got != "readable" {
		t.Errorf("X-Test header = %q, want %q", got, "readable")
	}
}

func TestHeaderSetAfterWrite(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `
		golapis.say("body first")
		golapis.header["X-Too-Late"] = "should fail"
	`)
	if err == nil {
		t.Fatal("Expected error when setting header after body write, got nil")
	}
}
