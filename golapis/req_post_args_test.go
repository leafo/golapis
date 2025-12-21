package golapis

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// runLuaWithHTTPBody runs Lua code with a POST request body
func runLuaWithHTTPBody(t *testing.T, body string, code string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	gls.Start()
	defer gls.Stop()

	// Create mock HTTP POST request with body
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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

func TestGetPostArgsBasic(t *testing.T) {
	w, err := runLuaWithHTTPBody(t, "foo=bar&baz=qux", `
		golapis.req.read_body()
		local args, err = golapis.req.get_post_args()
		if not args then
			error("failed to get post args: " .. tostring(err))
		end
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

func TestGetPostArgsMultipleValues(t *testing.T) {
	w, err := runLuaWithHTTPBody(t, "bar=baz&bar=blah", `
		golapis.req.read_body()
		local args = golapis.req.get_post_args()
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

func TestGetPostArgsBooleanArgs(t *testing.T) {
	w, err := runLuaWithHTTPBody(t, "foo&bar", `
		golapis.req.read_body()
		local args = golapis.req.get_post_args()
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

func TestGetPostArgsEmptyValues(t *testing.T) {
	w, err := runLuaWithHTTPBody(t, "foo=&bar=", `
		golapis.req.read_body()
		local args = golapis.req.get_post_args()
		-- Empty string values should be empty strings, not booleans
		golapis.say("foo: [" .. tostring(args.foo) .. "]")
		golapis.say("bar: [" .. tostring(args.bar) .. "]")
		golapis.say("foo type: " .. type(args.foo))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "foo: []\nbar: []\nfoo type: string\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetPostArgsEmptyKeys(t *testing.T) {
	w, err := runLuaWithHTTPBody(t, "=hello&=world", `
		golapis.req.read_body()
		local args = golapis.req.get_post_args()
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

func TestGetPostArgsUrlDecoding(t *testing.T) {
	// Test from nginx docs: 'a%20b=1%61+2' should yield 'a b: 1a 2'
	w, err := runLuaWithHTTPBody(t, "a%20b=1%61+2", `
		golapis.req.read_body()
		local args = golapis.req.get_post_args()
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

func TestGetPostArgsMaxLimit(t *testing.T) {
	w, err := runLuaWithHTTPBody(t, "a=1&b=2&c=3&d=4&e=5", `
		golapis.req.read_body()
		local args, err = golapis.req.get_post_args(3)
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
	// Should have at most 3 args and return "truncated" error
	if body != "count: 3\nerr: truncated\n" {
		t.Errorf("Unexpected body: %q", body)
	}
}

func TestGetPostArgsUnlimited(t *testing.T) {
	w, err := runLuaWithHTTPBody(t, "a=1&b=2&c=3&d=4&e=5", `
		golapis.req.read_body()
		local args, err = golapis.req.get_post_args(0)
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
	// Should have all 5 args and no error
	if body != "count: 5\nerr: nil\n" {
		t.Errorf("Unexpected body: %q", body)
	}
}

func TestGetPostArgsBodyNotRead(t *testing.T) {
	w, err := runLuaWithHTTPBody(t, "foo=bar", `
		-- Don't call read_body() first
		local args, err = golapis.req.get_post_args()
		if args == nil then
			golapis.say("args: nil")
		else
			golapis.say("args: table")
		end
		golapis.say("err: " .. tostring(err))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "args: nil\nerr: request body not read\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetPostArgsEmptyBody(t *testing.T) {
	w, err := runLuaWithHTTPBody(t, "", `
		golapis.req.read_body()
		local args, err = golapis.req.get_post_args()
		if type(args) ~= "table" then
			error("expected table, got " .. type(args))
		end
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
	expected := "count: 0\nerr: nil\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetPostArgsReadBodyTwice(t *testing.T) {
	w, err := runLuaWithHTTPBody(t, "foo=bar", `
		-- Call read_body() multiple times - should be safe
		golapis.req.read_body()
		golapis.req.read_body()
		local args = golapis.req.get_post_args()
		golapis.say("foo: " .. tostring(args.foo))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	expected := "foo: bar\n"
	if body != expected {
		t.Errorf("Unexpected body: %q, want %q", body, expected)
	}
}

func TestGetPostArgsNotInHTTPContext(t *testing.T) {
	// When not in HTTP context, should return nil
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
			local args = golapis.req.get_post_args()
			if args ~= nil then
				error("expected nil, got " .. type(args))
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
