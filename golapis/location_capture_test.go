package golapis

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runLuaEntryPointHTTP loads code as an entrypoint, then runs it via EventRunEntryPoint
// with a mock HTTP request. This is needed for location.capture since it dispatches
// EventRunEntryPoint internally for the subrequest.
func runLuaEntryPointHTTP(t *testing.T, uri string, code string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	// Load the code as an entrypoint so location.capture can re-invoke it
	if err := gls.LoadEntryPoint(CodeEntryPoint{Code: code}); err != nil {
		return nil, err
	}

	gls.Start()
	defer gls.Stop()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", uri, nil)
	req := NewGolapisRequest(r)
	wrappedWriter := req.WrapResponseWriter(w)

	resp := make(chan *StateResponse, 1)
	gls.eventChan <- &StateEvent{
		Type:         EventRunEntryPoint,
		OutputWriter: wrappedWriter,
		Request:      req,
		Response:     resp,
	}

	result := <-resp
	gls.Wait()
	req.FlushHeaders(w)

	return w, result.Error
}

func TestLocationCaptureBasic(t *testing.T) {
	code := `
		if golapis.var.request_uri == "/inner" then
			golapis.print("hello from inner")
			return
		end
		local res = golapis.location.capture("/inner")
		golapis.print("status:" .. res.status)
		golapis.print(" body:" .. res.body)
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "status:200") {
		t.Errorf("expected status:200 in body, got: %q", body)
	}
	if !strings.Contains(body, "body:hello from inner") {
		t.Errorf("expected body:hello from inner in body, got: %q", body)
	}
}

func TestLocationCaptureStatus(t *testing.T) {
	code := `
		if golapis.var.request_uri == "/inner" then
			golapis.status = 404
			golapis.print("not found")
			return
		end
		local res = golapis.location.capture("/inner")
		golapis.say(res.status)
		golapis.say(res.body)
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "404\n") {
		t.Errorf("expected 404 status in body, got: %q", body)
	}
	if !strings.Contains(body, "not found\n") {
		t.Errorf("expected 'not found' in body, got: %q", body)
	}
}

func TestLocationCaptureHeaders(t *testing.T) {
	code := `
		if golapis.var.request_uri == "/inner" then
			golapis.header["X-Custom"] = "test-value"
			golapis.print("ok")
			return
		end
		local res = golapis.location.capture("/inner")
		golapis.say(res.status)
		if res.header then
			golapis.say("header:" .. tostring(res.header["X-Custom"]))
		else
			golapis.say("no headers")
		end
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "200\n") {
		t.Errorf("expected 200 status in body, got: %q", body)
	}
	if !strings.Contains(body, "header:test-value") {
		t.Errorf("expected custom header in body, got: %q", body)
	}
}

func TestLocationCaptureWithQueryString(t *testing.T) {
	code := `
		if golapis.var.request_uri == "/inner?key=value" then
			golapis.print("got query")
			return
		end
		local res = golapis.location.capture("/inner?key=value")
		golapis.say(res.status)
		golapis.say(res.body)
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "200\n") {
		t.Errorf("expected 200 status, got: %q", body)
	}
	if !strings.Contains(body, "got query\n") {
		t.Errorf("expected 'got query' in body, got: %q", body)
	}
}

func TestLocationCaptureOutputIsolation(t *testing.T) {
	// Verify the subrequest output doesn't leak into the main request
	code := `
		if golapis.var.request_uri == "/inner" then
			golapis.print("INNER OUTPUT")
			return
		end
		golapis.print("BEFORE|")
		local res = golapis.location.capture("/inner")
		golapis.print("AFTER|")
		golapis.print("captured:" .. res.body)
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "BEFORE|AFTER|captured:INNER OUTPUT"
	if body != expected {
		t.Errorf("expected %q, got %q", expected, body)
	}
}

func TestLocationCaptureMultiple(t *testing.T) {
	// Test making multiple captures in sequence
	code := `
		if golapis.var.request_uri == "/a" then
			golapis.print("response-a")
			return
		end
		if golapis.var.request_uri == "/b" then
			golapis.print("response-b")
			return
		end
		local res1 = golapis.location.capture("/a")
		local res2 = golapis.location.capture("/b")
		golapis.say(res1.body)
		golapis.say(res2.body)
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "response-a\n") {
		t.Errorf("expected response-a in body, got: %q", body)
	}
	if !strings.Contains(body, "response-b\n") {
		t.Errorf("expected response-b in body, got: %q", body)
	}
}

func TestLocationCaptureWithSleep(t *testing.T) {
	// Test that capture works alongside other async operations
	code := `
		if golapis.var.request_uri == "/inner" then
			golapis.sleep(0.01)
			golapis.print("after-sleep")
			return
		end
		local res = golapis.location.capture("/inner")
		golapis.say(res.status)
		golapis.say(res.body)
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "200\n") {
		t.Errorf("expected 200 status, got: %q", body)
	}
	if !strings.Contains(body, "after-sleep\n") {
		t.Errorf("expected 'after-sleep' in body, got: %q", body)
	}
}

func TestLocationCaptureWithExit(t *testing.T) {
	// Test that golapis.exit() in a subrequest is properly captured
	code := `
		if golapis.var.request_uri == "/inner" then
			golapis.print("before exit")
			golapis.exit(403)
			golapis.print("after exit - should not appear")
			return
		end
		local res = golapis.location.capture("/inner")
		golapis.say(res.status)
		golapis.say(res.body)
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "403\n") {
		t.Errorf("expected 403 status, got: %q", body)
	}
	if !strings.Contains(body, "before exit\n") {
		t.Errorf("expected 'before exit' in body, got: %q", body)
	}
	if strings.Contains(body, "after exit") {
		t.Errorf("should not contain 'after exit', got: %q", body)
	}
}

func TestLocationCaptureEmptyBody(t *testing.T) {
	code := `
		if golapis.var.request_uri == "/inner" then
			return
		end
		local res = golapis.location.capture("/inner")
		golapis.say(res.status)
		golapis.say("body-len:" .. #res.body)
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "200\n") {
		t.Errorf("expected 200 status, got: %q", body)
	}
	if !strings.Contains(body, "body-len:0\n") {
		t.Errorf("expected empty body, got: %q", body)
	}
}

func TestLocationCaptureErrorNoArgs(t *testing.T) {
	// Test that calling with no arguments returns nil + error
	code := `
		if golapis.var.request_uri ~= "/test" then
			return
		end
		local res, err = golapis.location.capture()
		assert(res == nil, "should return nil for no arguments, got: " .. tostring(res))
		assert(type(err) == "string", "should return error string, got: " .. type(err))
		golapis.say("OK")
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(w.Body.String(), "OK") {
		t.Errorf("test failed: %s", w.Body.String())
	}
}

func TestLocationCaptureErrorNonString(t *testing.T) {
	// Test that calling with non-string argument returns nil + error
	code := `
		if golapis.var.request_uri ~= "/test" then
			return
		end
		local res, err = golapis.location.capture(123)
		assert(res == nil, "should return nil for non-string, got: " .. tostring(res))
		assert(type(err) == "string", "should return error string, got: " .. type(err))
		golapis.say("OK")
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(w.Body.String(), "OK") {
		t.Errorf("test failed: %s", w.Body.String())
	}
}

func TestLocationCaptureResultTable(t *testing.T) {
	// Verify the result table has the expected fields
	code := `
		if golapis.var.request_uri == "/inner" then
			golapis.print("test body")
			return
		end
		local res = golapis.location.capture("/inner")
		assert(type(res) == "table", "result should be a table, got: " .. type(res))
		assert(type(res.status) == "number", "status should be a number, got: " .. type(res.status))
		assert(type(res.body) == "string", "body should be a string, got: " .. type(res.body))
		assert(type(res.header) == "table", "header should be a table, got: " .. type(res.header))
		golapis.say("OK")
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if !strings.Contains(w.Body.String(), "OK") {
		t.Errorf("assertions failed: %s", w.Body.String())
	}
}

func TestLocationCaptureErrorNonHTTPContext(t *testing.T) {
	output, err := runLuaAndCapture(t, `
		local res, captureErr = golapis.location.capture("/inner")
		assert(res == nil, "expected nil result outside HTTP context")
		assert(type(captureErr) == "string", "expected error string")
		assert(captureErr:find("HTTP request context", 1, true), "unexpected error: " .. tostring(captureErr))
		golapis.say("OK")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK\n") {
		t.Errorf("test failed: %q", output)
	}
}

func TestLocationCaptureFileServer(t *testing.T) {
	// Create a temp directory with a test file
	tmpDir := t.TempDir()
	testContent := "hello from static file"
	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	code := `
		local res = golapis.location.capture("/static/test.txt")
		golapis.say(res.status)
		golapis.say(res.body)
	`

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	if err := gls.LoadEntryPoint(CodeEntryPoint{Code: code}); err != nil {
		t.Fatal(err)
	}

	gls.Start()
	defer gls.Stop()

	// Build a mux with a file-server handler and the Lua handler
	mux := http.NewServeMux()
	fileHandler := http.StripPrefix("/static/", http.FileServer(http.Dir(tmpDir)))
	mux.Handle("/static/", fileHandler)

	config := DefaultHTTPServerConfig()
	luaHandler := gls.HTTPHandler(config)
	mux.Handle("/", luaHandler)

	gls.httpMux = mux

	// Dispatch the outer request through the mux
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	mux.ServeHTTP(w, r)
	gls.Wait()

	body := w.Body.String()
	if !strings.Contains(body, "200\n") {
		t.Errorf("expected 200 status, got: %q", body)
	}
	if !strings.Contains(body, testContent) {
		t.Errorf("expected file content %q in body, got: %q", testContent, body)
	}
}
