package golapis

import (
	"net/http/httptest"
	"testing"
)

// runLuaWithHTTPHeaders runs Lua code with custom request headers
func runLuaWithHTTPHeaders(t *testing.T, headers map[string][]string, code string) (*httptest.ResponseRecorder, error) {
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

	// Set custom headers
	for key, values := range headers {
		for _, v := range values {
			r.Header.Add(key, v)
		}
	}

	// Create GolapisRequest and wrap writer
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
	req.FlushHeaders(w)

	return w, result.Error
}

func TestGetHeadersBasic(t *testing.T) {
	headers := map[string][]string{
		"Content-Type": {"application/json"},
		"X-Custom":     {"hello"},
	}

	w, err := runLuaWithHTTPHeaders(t, headers, `
		local h = golapis.req.get_headers()
		golapis.say("content-type: " .. tostring(h["content-type"]))
		golapis.say("x-custom: " .. tostring(h["x-custom"]))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if body != "content-type: application/json\nx-custom: hello\n" {
		t.Errorf("Unexpected body: %q", body)
	}
}

func TestGetHeadersLowercaseKeys(t *testing.T) {
	headers := map[string][]string{
		"X-Mixed-Case": {"value"},
	}

	w, err := runLuaWithHTTPHeaders(t, headers, `
		local h = golapis.req.get_headers()
		-- Keys should be lowercase by default
		local found = false
		for k, v in pairs(h) do
			if k == "x-mixed-case" then
				found = true
				golapis.say("found: " .. v)
			end
		end
		if not found then
			golapis.say("not found")
		end
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if body != "found: value\n" {
		t.Errorf("Unexpected body: %q", body)
	}
}

func TestGetHeadersNormalizedLookup(t *testing.T) {
	headers := map[string][]string{
		"Content-Type": {"text/html"},
	}

	w, err := runLuaWithHTTPHeaders(t, headers, `
		local h = golapis.req.get_headers()
		-- All these lookups should work via metamethod normalization
		golapis.say(h["content-type"])
		golapis.say(h["Content-Type"])
		golapis.say(h.content_type)
		golapis.say(h["content_type"])
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "text/html\ntext/html\ntext/html\ntext/html\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetHeadersMultipleValues(t *testing.T) {
	headers := map[string][]string{
		"Set-Cookie": {"a=1", "b=2", "c=3"},
	}

	w, err := runLuaWithHTTPHeaders(t, headers, `
		local h = golapis.req.get_headers()
		local cookies = h["set-cookie"]
		if type(cookies) ~= "table" then
			error("expected table, got " .. type(cookies))
		end
		golapis.say("count: " .. #cookies)
		for i, v in ipairs(cookies) do
			golapis.say(i .. ": " .. v)
		end
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "count: 3\n1: a=1\n2: b=2\n3: c=3\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetHeadersHost(t *testing.T) {
	// Host header is handled specially by Go's http package
	w, err := runLuaWithHTTPHeaders(t, nil, `
		local h = golapis.req.get_headers()
		-- httptest.NewRequest sets Host from URL
		if h.host then
			golapis.say("host: " .. h.host)
		else
			golapis.say("no host")
		end
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	// httptest.NewRequest("GET", "/test", nil) sets Host to "example.com"
	if body != "host: example.com\n" {
		t.Errorf("Unexpected body: %q", body)
	}
}

func TestGetHeadersRaw(t *testing.T) {
	headers := map[string][]string{
		"Content-Type": {"text/plain"},
	}

	w, err := runLuaWithHTTPHeaders(t, headers, `
		local h = golapis.req.get_headers(100, true)
		-- With raw=true, keys should keep original casing (Go canonicalizes to Content-Type)
		local found = false
		for k, v in pairs(h) do
			if k == "Content-Type" then
				found = true
				golapis.say("found canonical: " .. v)
			end
		end
		if not found then
			golapis.say("not found")
		end
		-- Metamethod should NOT be added with raw=true
		-- So underscore lookup should return nil
		if h.content_type == nil then
			golapis.say("underscore lookup: nil")
		else
			golapis.say("underscore lookup: " .. tostring(h.content_type))
		end
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "found canonical: text/plain\nunderscore lookup: nil\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetHeadersMaxLimit(t *testing.T) {
	headers := map[string][]string{
		"H1": {"v1"},
		"H2": {"v2"},
		"H3": {"v3"},
		"H4": {"v4"},
		"H5": {"v5"},
	}

	w, err := runLuaWithHTTPHeaders(t, headers, `
		local h, err = golapis.req.get_headers(3)
		local count = 0
		for k, v in pairs(h) do
			count = count + 1
		end
		golapis.say("count: " .. count)
		golapis.say("err: " .. tostring(err))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	// Should have at most 3 headers and return "truncated" error
	if body != "count: 3\nerr: truncated\n" {
		t.Errorf("Unexpected body: %q", body)
	}
}

func TestGetHeadersUnlimited(t *testing.T) {
	headers := map[string][]string{
		"H1": {"v1"},
		"H2": {"v2"},
		"H3": {"v3"},
	}

	w, err := runLuaWithHTTPHeaders(t, headers, `
		local h, err = golapis.req.get_headers(0)
		local count = 0
		for k, v in pairs(h) do
			count = count + 1
		end
		golapis.say("count: " .. count)
		golapis.say("err: " .. tostring(err))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	// 3 custom headers + Host = 4, no truncation
	if body != "count: 4\nerr: nil\n" {
		t.Errorf("Unexpected body: %q", body)
	}
}

func TestGetHeadersEmptyInCLI(t *testing.T) {
	// When not in HTTP context, should return empty table (not error)
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	gls.Start()
	defer gls.Stop()

	resp := make(chan *StateResponse, 1)
	gls.eventChan <- &StateEvent{
		Type: EventRunString,
		Code: `
			local h = golapis.req.get_headers()
			if type(h) ~= "table" then
				error("expected table, got " .. type(h))
			end
			local c = 0
			for _ in pairs(h) do c = c + 1 end
			if c ~= 0 then
				error("expected empty table, got " .. c .. " entries")
			end
		`,
		Response: resp,
	}

	result := <-resp
	gls.Wait()

	if result.Error != nil {
		t.Fatalf("Lua error: %v", result.Error)
	}
}
