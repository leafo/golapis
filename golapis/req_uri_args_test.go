package golapis

import (
	"net/http/httptest"
	"testing"
)

// runLuaWithQueryString runs Lua code with a GET request containing query parameters
func runLuaWithQueryString(t *testing.T, queryString string, code string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	gls.Start()
	defer gls.Stop()

	// Create mock HTTP GET request with query string
	w := httptest.NewRecorder()
	url := "/test"
	if queryString != "" {
		url = "/test?" + queryString
	}
	r := httptest.NewRequest("GET", url, nil)

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

func TestGetUriArgsBasic(t *testing.T) {
	w, err := runLuaWithQueryString(t, "foo=bar&baz=qux", `
		local args = golapis.req.get_uri_args()
		golapis.say("foo: " .. tostring(args.foo))
		golapis.say("baz: " .. tostring(args.baz))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "foo: bar\nbaz: qux\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetUriArgsMultipleValues(t *testing.T) {
	w, err := runLuaWithQueryString(t, "bar=baz&bar=blah", `
		local args = golapis.req.get_uri_args()
		local vals = args.bar
		if type(vals) ~= "table" then
			error("expected table, got " .. type(vals))
		end
		golapis.say("bar: " .. table.concat(vals, ", "))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "bar: baz, blah\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetUriArgsBooleanArgs(t *testing.T) {
	w, err := runLuaWithQueryString(t, "foo&bar", `
		local args = golapis.req.get_uri_args()
		golapis.say("foo: " .. tostring(args.foo))
		golapis.say("bar: " .. tostring(args.bar))
		golapis.say("foo type: " .. type(args.foo))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "foo: true\nbar: true\nfoo type: boolean\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetUriArgsUrlDecoding(t *testing.T) {
	// Test URL decoding: 'a%20b=1%61+2' should yield 'a b: 1a 2'
	w, err := runLuaWithQueryString(t, "a%20b=1%61+2", `
		local args = golapis.req.get_uri_args()
		for k, v in pairs(args) do
			golapis.say(k .. ": " .. tostring(v))
		end
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "a b: 1a 2\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetUriArgsMaxLimit(t *testing.T) {
	w, err := runLuaWithQueryString(t, "a=1&b=2&c=3&d=4&e=5", `
		local args, err = golapis.req.get_uri_args(3)
		local count = 0
		for k, v in pairs(args) do
			count = count + 1
		end
		golapis.say("count: " .. count)
		golapis.say("err: " .. tostring(err))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "count: 3\nerr: truncated\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetUriArgsEmptyKeys(t *testing.T) {
	w, err := runLuaWithQueryString(t, "=hello&=world", `
		local args = golapis.req.get_uri_args()
		-- Empty keys should be discarded
		local count = 0
		for k, v in pairs(args) do
			count = count + 1
			golapis.say(k .. ": " .. tostring(v))
		end
		golapis.say("count: " .. count)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "count: 0\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}
