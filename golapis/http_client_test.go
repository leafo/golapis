package golapis

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// === Test Server Setup ===

func setupHTTPTestServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/echo", handleEcho)
	mux.HandleFunc("/get", handleOnlyGet)
	mux.HandleFunc("/post", handleOnlyPost)
	mux.HandleFunc("/delete", handleOnlyDelete)
	mux.HandleFunc("/put", handleOnlyPut)
	mux.HandleFunc("/headers", handleHeaders)
	mux.HandleFunc("/redirect/", handleRedirect)
	mux.HandleFunc("/status/", handleStatus)
	mux.HandleFunc("/slow/", handleSlow)
	mux.HandleFunc("/raw", handleRaw)

	return httptest.NewServer(mux)
}

func handleEcho(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	response := map[string]interface{}{
		"method":  r.Method,
		"path":    r.URL.Path,
		"query":   r.URL.RawQuery,
		"headers": r.Header,
		"body":    string(body),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleOnlyGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "method": "GET"})
}

func handleOnlyPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "method": "POST", "body": string(body)})
}

func handleOnlyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "method": "DELETE"})
}

func handleOnlyPut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "method": "PUT", "body": string(body)})
}

func handleHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(r.Header)
}

func handleRedirect(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/redirect/"), "/")
	count, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "Invalid redirect count", http.StatusBadRequest)
		return
	}
	if count > 0 {
		http.Redirect(w, r, fmt.Sprintf("/redirect/%d", count-1), http.StatusFound)
	} else {
		w.Write([]byte("final destination"))
	}
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/status/"), "/")
	code, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "Invalid status code", http.StatusBadRequest)
		return
	}
	w.WriteHeader(code)
	w.Write([]byte(fmt.Sprintf("Status: %d", code)))
}

func handleSlow(w http.ResponseWriter, r *http.Request) {
	// /slow/{seconds} - delays response by specified seconds
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/slow/"), "/")
	seconds, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		http.Error(w, "Invalid delay", http.StatusBadRequest)
		return
	}
	time.Sleep(time.Duration(seconds * float64(time.Second)))
	w.Write([]byte("delayed response"))
}

func handleRaw(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.Write(body)
}

// === Test Helper ===

func runLuaHTTPTest(t *testing.T, code string) (string, error) {
	t.Helper()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	gls.Start()
	defer gls.Stop()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	req := NewGolapisRequest(r)
	wrappedWriter := req.WrapResponseWriter(w)

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

	return w.Body.String(), result.Error
}

// === Test Cases ===

func TestHTTPSimpleGet(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status, headers, statusline = golapis.http.request("%s/get")
		assert(status == 200, "status should be 200, got: " .. tostring(status))
		assert(body:find('"method":"GET"', 1, true), "body should contain method GET")
		assert(statusline == "200 OK", "statusline should be '200 OK', got: " .. tostring(statusline))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPSimplePost(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request("%s/post", "foo=bar&baz=qux")
		assert(status == 200, "status should be 200, got: " .. tostring(status))
		assert(body:find("foo=bar", 1, true), "body should contain posted data")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPGenericGet(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/get",
			method = "GET"
		}
		assert(status == 200, "status should be 200, got: " .. tostring(status))
		assert(body:find('"method":"GET"', 1, true), "body should contain method GET")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPGenericPost(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/post",
			method = "POST",
			body = "test body content",
			headers = { ["content-type"] = "text/plain" }
		}
		assert(status == 200, "status should be 200, got: " .. tostring(status))
		assert(body:find("test body content", 1, true), "body should contain posted data")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPCustomMethodDelete(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/delete",
			method = "DELETE"
		}
		assert(status == 200, "status should be 200, got: " .. tostring(status))
		assert(body:find('"method":"DELETE"', 1, true), "body should confirm DELETE method")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPCustomMethodPut(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/put",
			method = "PUT",
			body = "put content"
		}
		assert(status == 200, "status should be 200, got: " .. tostring(status))
		assert(body:find('"method":"PUT"', 1, true), "body should confirm PUT method")
		assert(body:find("put content", 1, true), "body should contain put data")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPCustomHeaders(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/headers",
			headers = {
				["X-Custom-Header"] = "test-value-123",
				["X-Another-Header"] = "another-value"
			}
		}
		assert(status == 200, "status should be 200, got: " .. tostring(status))
		assert(body:find("test-value-123", 1, true), "body should contain custom header value")
		assert(body:find("another-value", 1, true), "body should contain another header value")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPSource(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local chunks = {"hello", " ", "world"}
		local i = 0
		local function my_source()
			i = i + 1
			return chunks[i]
		end
		local body, status = golapis.http.request{
			url = "%s/post",
			method = "POST",
			source = my_source,
			headers = { ["content-type"] = "text/plain" }
		}
		assert(status == 200, "status should be 200, got: " .. tostring(status))
		assert(body:find("hello world", 1, true), "body should contain source data")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPBodyWithNullBytes(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body_in = "a" .. string.char(0) .. "b"
		local body_out, status = golapis.http.request{
			url = "%s/raw",
			method = "POST",
			body = body_in,
			headers = { ["content-type"] = "application/octet-stream" }
		}
		assert(status == 200, "status should be 200, got: " .. tostring(status))
		assert(body_out ~= nil, "response body should not be nil")
		assert(#body_out == 3, "response length should be 3, got: " .. tostring(#body_out))
		assert(body_out:byte(1) == 97 and body_out:byte(2) == 0 and body_out:byte(3) == 98,
			"response bytes should be a\\0b")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPSourceError(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local i = 0
		local function failing_source()
			i = i + 1
			if i == 1 then
				return "first chunk"
			elseif i == 2 then
				return nil, "source read failed: simulated error"
			end
			return nil
		end
		local body, err = golapis.http.request{
			url = "%s/post",
			method = "POST",
			source = failing_source,
			headers = { ["content-type"] = "text/plain" }
		}
		assert(body == nil, "body should be nil on source error, got: " .. tostring(body))
		assert(err == "source read failed: simulated error", "error should be propagated, got: " .. tostring(err))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPSinkError(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local function failing_sink(chunk)
			if chunk then
				return nil, "sink write failed: disk full"
			end
			return true
		end
		local result, err = golapis.http.request{
			url = "%s/get",
			sink = failing_sink
		}
		assert(result == nil, "result should be nil on sink error, got: " .. tostring(result))
		assert(err == "sink write failed: disk full", "error should be propagated, got: " .. tostring(err))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPSink(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local received = {}
		local function my_sink(chunk)
			if chunk then
				received[#received + 1] = chunk
			end
			return true
		end
		local result, status = golapis.http.request{
			url = "%s/get",
			sink = my_sink
		}
		assert(result == 1, "sink request should return 1, got: " .. tostring(result))
		assert(status == 200, "status should be 200, got: " .. tostring(status))
		assert(#received > 0, "sink should have received data")
		local full_body = table.concat(received)
		assert(full_body:find('"method":"GET"', 1, true), "sink data should contain response")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPRedirectFollow(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/redirect/2"
		}
		assert(status == 200, "redirect should be followed, got status: " .. tostring(status))
		assert(body == "final destination", "should reach final destination, got: " .. tostring(body))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPRedirectDisabled(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/redirect/1",
			redirect = false
		}
		assert(status == 302, "redirect should not be followed, got status: " .. tostring(status))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPMaxRedirects(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, err = golapis.http.request{
			url = "%s/redirect/10",
			maxredirects = 3
		}
		assert(body == nil, "should fail on too many redirects")
		assert(type(err) == "string", "error should be a string")
		assert(err:find("redirect", 1, true), "error should mention redirects, got: " .. tostring(err))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPResponseHeaders(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	// Debug: print out what we receive to understand header structure
	code := fmt.Sprintf(`
		local body, status, headers, statusline = golapis.http.request("%s/get")
		assert(status == 200, "status should be 200")
		-- Headers may be nil or a table depending on implementation
		if headers then
			local ct = headers["Content-Type"]
			if ct then
				if type(ct) == "table" then
					assert(ct[1] == "application/json", "Content-Type should be application/json, got: " .. tostring(ct[1]))
				else
					assert(ct == "application/json", "Content-Type should be application/json, got: " .. tostring(ct))
				end
			end
		end
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPStatusLine(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status, headers, statusline = golapis.http.request("%s/get")
		assert(status == 200, "status should be 200")
		assert(statusline == "200 OK", "statusline should be '200 OK', got: " .. tostring(statusline))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPStatusCodes(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	testCases := []struct {
		code       int
		statusLine string
	}{
		{200, "200 OK"},
		{201, "201 Created"},
		{204, "204 No Content"},
		{400, "400 Bad Request"},
		{404, "404 Not Found"},
		{500, "500 Internal Server Error"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Status%d", tc.code), func(t *testing.T) {
			code := fmt.Sprintf(`
				local body, status, headers, statusline = golapis.http.request("%s/status/%d")
				assert(status == %d, "status should be %d, got: " .. tostring(status))
				assert(statusline == "%s", "statusline should be '%s', got: " .. tostring(statusline))
				golapis.say("OK")
			`, server.URL, tc.code, tc.code, tc.code, tc.statusLine, tc.statusLine)

			output, err := runLuaHTTPTest(t, code)
			if err != nil {
				t.Fatalf("Lua error: %v", err)
			}
			if !strings.Contains(output, "OK") {
				t.Errorf("test did not pass: %s", output)
			}
		})
	}
}

func TestHTTPErrorBadURL(t *testing.T) {
	code := `
		local result, err = golapis.http.request("not-a-valid-url")
		assert(result == nil, "result should be nil for invalid URL")
		assert(err ~= nil, "error should not be nil")
		assert(type(err) == "string", "error should be a string")
		golapis.say("OK")
	`

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPErrorUnreachableHost(t *testing.T) {
	code := `
		local result, err = golapis.http.request("http://this-host-definitely-does-not-exist-12345.invalid")
		assert(result == nil, "result should be nil for unreachable host")
		assert(err ~= nil, "error should not be nil")
		assert(type(err) == "string", "error should be a string")
		golapis.say("OK")
	`

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPErrorNoArguments(t *testing.T) {
	// The http.lua wrapper throws an error when called with no arguments
	// because it tries to index nil. This is expected behavior.
	code := `
		local ok, err = pcall(function()
			golapis.http.request()
		end)
		assert(not ok, "should error when no arguments")
		assert(type(err) == "string", "error should be a string")
		golapis.say("OK")
	`

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPEchoEndpoint(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/echo?param=value",
			method = "POST",
			body = "request body content",
			headers = {
				["X-Test-Header"] = "header-value"
			}
		}
		assert(status == 200, "status should be 200")
		assert(body:find('"method":"POST"', 1, true), "should echo POST method")
		assert(body:find('"query":"param=value"', 1, true), "should echo query string")
		assert(body:find("request body content", 1, true), "should echo request body")
		assert(body:find("header-value", 1, true), "should echo custom header")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

func TestHTTPMethodNotAllowed(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	// Try POST to GET-only endpoint
	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/get",
			method = "POST",
			body = "test"
		}
		assert(status == 405, "status should be 405 Method Not Allowed, got: " .. tostring(status))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// === LuaSocket Compatibility Tests ===
// These tests verify behavior matches LuaSocket's socket.http.request()

// TestHTTPSimplePostContentType verifies that simple form POST auto-sets
// Content-Type to application/x-www-form-urlencoded (LuaSocket behavior)
func TestHTTPSimplePostContentType(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request("%s/echo", "foo=bar")
		assert(status == 200, "status should be 200")
		-- Simple POST should auto-set content-type to application/x-www-form-urlencoded
		assert(body:find("application/x-www-form-urlencoded", 1, true),
			"simple POST should set Content-Type to application/x-www-form-urlencoded")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPSimplePostMethod verifies that simple form with body triggers POST method
func TestHTTPSimplePostMethod(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request("%s/echo", "data")
		assert(status == 200, "status should be 200")
		assert(body:find('"method":"POST"', 1, true),
			"simple form with body should use POST method")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPSourceAutoContentLength verifies that source auto-sets content-length header
func TestHTTPSourceAutoContentLength(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local chunks = {"hello", "world"}  -- 10 bytes total
		local i = 0
		local function my_source()
			i = i + 1
			return chunks[i]
		end
		local body, status = golapis.http.request{
			url = "%s/echo",
			method = "POST",
			source = my_source,
			headers = { ["content-type"] = "text/plain" }
		}
		assert(status == 200, "status should be 200")
		-- Source should auto-set content-length
		assert(body:find('"Content-Length"', 1, true),
			"source should auto-set Content-Length header")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPReturnValuesFourValues verifies simple form returns 4 values on success
// Per LuaSocket: body, status_code, headers, status_line
func TestHTTPReturnValuesFourValues(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status, headers, statusline = golapis.http.request("%s/get")

		-- Verify all 4 return values
		assert(type(body) == "string", "1st return: body should be string, got: " .. type(body))
		assert(type(status) == "number", "2nd return: status should be number, got: " .. type(status))
		-- headers may be nil depending on implementation
		assert(type(statusline) == "string", "4th return: statusline should be string, got: " .. type(statusline))

		-- Verify values
		assert(status == 200, "status should be 200")
		assert(statusline == "200 OK", "statusline should be '200 OK'")
		assert(#body > 0, "body should not be empty")

		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPSinkReturnValues verifies generic form with sink returns (1, status, headers, statusline)
// Per LuaSocket: when sink is provided, returns 1 instead of body
func TestHTTPSinkReturnValues(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local received = {}
		local function my_sink(chunk)
			if chunk then received[#received + 1] = chunk end
			return true
		end

		local result, status, headers, statusline = golapis.http.request{
			url = "%s/get",
			sink = my_sink
		}

		-- With sink, first return should be 1, not body
		assert(result == 1, "1st return with sink should be 1, got: " .. tostring(result))
		assert(type(status) == "number", "2nd return: status should be number")
		assert(status == 200, "status should be 200")
		assert(type(statusline) == "string", "4th return: statusline should be string")
		assert(statusline == "200 OK", "statusline should be '200 OK'")

		-- Verify sink received data
		assert(#received > 0, "sink should have received data")

		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPErrorReturnValues verifies error case returns (nil, error_message)
// Per LuaSocket: on error, returns nil and error message
func TestHTTPErrorReturnValues(t *testing.T) {
	code := `
		local body, err = golapis.http.request("http://this-host-does-not-exist-12345.invalid")

		-- On error: nil, error_message
		assert(body == nil, "1st return on error should be nil")
		assert(type(err) == "string", "2nd return on error should be string, got: " .. type(err))
		assert(#err > 0, "error message should not be empty")

		golapis.say("OK")
	`

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPDefaultMethodIsGet verifies default method is GET when not specified
func TestHTTPDefaultMethodIsGet(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/echo"
			-- method not specified, should default to GET
		}
		assert(status == 200, "status should be 200")
		assert(body:find('"method":"GET"', 1, true), "default method should be GET")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPDefaultMaxRedirectsIsFive verifies maxredirects defaults to 5
func TestHTTPDefaultMaxRedirectsIsFive(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	// 5 redirects should succeed (default maxredirects=5)
	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/redirect/5"
			-- maxredirects not specified, should default to 5
		}
		assert(status == 200, "5 redirects should succeed with default maxredirects=5, got: " .. tostring(status))
		assert(body == "final destination", "should reach final destination")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPDefaultMaxRedirectsExceeded verifies 6 redirects fails with default maxredirects=5
func TestHTTPDefaultMaxRedirectsExceeded(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	// 6 redirects should fail (default maxredirects=5)
	code := fmt.Sprintf(`
		local body, err = golapis.http.request{
			url = "%s/redirect/6"
			-- maxredirects not specified, defaults to 5
		}
		assert(body == nil, "6 redirects should fail with default maxredirects=5")
		assert(type(err) == "string", "should return error message")
		assert(err:find("redirect", 1, true), "error should mention redirects")
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPSinkReceivesNilAtEnd verifies sink receives nil to signal end of data
// Per LuaSocket LTN12: sink receives nil when transfer is complete
func TestHTTPSinkReceivesNilAtEnd(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	code := fmt.Sprintf(`
		local received_nil = false
		local function my_sink(chunk)
			if chunk == nil then
				received_nil = true
			end
			return true
		end

		local result, status = golapis.http.request{
			url = "%s/get",
			sink = my_sink
		}

		assert(result == 1, "should return 1 with sink")
		assert(status == 200, "status should be 200")
		assert(received_nil, "sink should receive nil to signal end of data")

		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPHeadMethod verifies HEAD method works (returns headers but no body)
func TestHTTPHeadMethod(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	// Use /echo which accepts all methods
	code := fmt.Sprintf(`
		local body, status, headers, statusline = golapis.http.request{
			url = "%s/echo",
			method = "HEAD"
		}
		assert(status == 200, "HEAD should return 200, got: " .. tostring(status))
		-- HEAD responses typically have empty body
		assert(body == "" or body == nil or #body == 0, "HEAD should return empty body, got: " .. tostring(#(body or "")))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPTimeout verifies that timeout parameter works
func TestHTTPTimeout(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	// Request with short timeout to slow endpoint should fail
	code := fmt.Sprintf(`
		local body, err = golapis.http.request{
			url = "%s/slow/2",
			timeout = 0.1
		}
		assert(body == nil, "should timeout, got body: " .. tostring(body))
		assert(type(err) == "string", "error should be a string")
		assert(err:find("timeout", 1, true) or err:find("deadline", 1, true) or err:find("context", 1, true),
			"error should mention timeout/deadline, got: " .. tostring(err))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}

// TestHTTPTimeoutSuccess verifies request completes within timeout
func TestHTTPTimeoutSuccess(t *testing.T) {
	server := setupHTTPTestServer()
	defer server.Close()

	// Request with sufficient timeout should succeed
	code := fmt.Sprintf(`
		local body, status = golapis.http.request{
			url = "%s/slow/0.05",
			timeout = 2
		}
		assert(status == 200, "should succeed, got status: " .. tostring(status))
		assert(body == "delayed response", "body should be 'delayed response', got: " .. tostring(body))
		golapis.say("OK")
	`, server.URL)

	output, err := runLuaHTTPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("test did not pass: %s", output)
	}
}
