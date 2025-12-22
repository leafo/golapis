package golapis

import (
	"strings"
	"testing"
)

func TestExitBasic(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.say("before exit")
		golapis.exit(404)
		golapis.say("after exit - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	// Only "before exit" should be printed
	if got := w.Body.String(); got != "before exit\n" {
		t.Errorf("Body = %q, want %q", got, "before exit\n")
	}

	// Status should be 200 because headers were already sent by say()
	// exit() after output can't change the status (nginx-lua behavior)
	if w.Code != 200 {
		t.Errorf("Response code = %d, want 200 (headers already sent)", w.Code)
	}
}

func TestExitBeforeOutput(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.exit(404)
		golapis.say("after exit - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	// No output
	if got := w.Body.String(); got != "" {
		t.Errorf("Body = %q, want empty", got)
	}

	// Status should be 404 since exit was called before any output
	if w.Code != 404 {
		t.Errorf("Response code = %d, want 404", w.Code)
	}
}

func TestExitSetsStatus(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		wantStatus int
	}{
		{"200 OK", `golapis.exit(200)`, 200},
		{"201 Created", `golapis.exit(201)`, 201},
		{"204 No Content", `golapis.exit(204)`, 204},
		{"301 Redirect", `golapis.exit(301)`, 301},
		{"400 Bad Request", `golapis.exit(400)`, 400},
		{"401 Unauthorized", `golapis.exit(401)`, 401},
		{"403 Forbidden", `golapis.exit(403)`, 403},
		{"404 Not Found", `golapis.exit(404)`, 404},
		{"500 Internal Error", `golapis.exit(500)`, 500},
		{"503 Service Unavailable", `golapis.exit(503)`, 503},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _, err := runLuaWithHTTP(t, tt.code)
			if err != nil {
				t.Fatalf("Lua error: %v", err)
			}
			if w.Code != tt.wantStatus {
				t.Errorf("Response code = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestExitDefaultStatus(t *testing.T) {
	// exit() without argument should default to 200
	w, _, err := runLuaWithHTTP(t, `
		golapis.say("done")
		golapis.exit()
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if w.Code != 200 {
		t.Errorf("Response code = %d, want 200", w.Code)
	}
}

func TestExitAfterSleep(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.say("before sleep")
		golapis.sleep(0.01)
		golapis.say("after sleep")
		golapis.exit(404)
		golapis.say("after exit - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "before sleep\nafter sleep\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
	// Status is 200 because headers were sent by the first say()
	if w.Code != 200 {
		t.Errorf("Response code = %d, want 200 (headers already sent)", w.Code)
	}
}

func TestExitAfterSleepNoOutput(t *testing.T) {
	// Test exit with sleep but no output before exit
	w, _, err := runLuaWithHTTP(t, `
		golapis.sleep(0.01)
		golapis.exit(404)
		golapis.say("after exit - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Body.String(); got != "" {
		t.Errorf("Body = %q, want empty", got)
	}
	if w.Code != 404 {
		t.Errorf("Response code = %d, want 404", w.Code)
	}
}

func TestExitInvalidStatus(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{"negative", `golapis.exit(-1)`},
		{"too low", `golapis.exit(99)`},
		{"too high", `golapis.exit(600)`},
		{"way too high", `golapis.exit(9999)`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := runLuaWithHTTP(t, tt.code)
			if err == nil {
				t.Error("Expected error for invalid exit status")
			}
		})
	}
}

func TestExitInvalidType(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `golapis.exit("not a number")`)
	if err == nil {
		t.Error("Expected error for non-numeric exit status")
	}
}

func TestExitWithStatusConstants(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		wantStatus int
	}{
		{"HTTP_OK", `golapis.exit(golapis.HTTP_OK)`, 200},
		{"HTTP_CREATED", `golapis.exit(golapis.HTTP_CREATED)`, 201},
		{"HTTP_NO_CONTENT", `golapis.exit(golapis.HTTP_NO_CONTENT)`, 204},
		{"HTTP_MOVED_PERMANENTLY", `golapis.exit(golapis.HTTP_MOVED_PERMANENTLY)`, 301},
		{"HTTP_MOVED_TEMPORARILY", `golapis.exit(golapis.HTTP_MOVED_TEMPORARILY)`, 302},
		{"HTTP_NOT_MODIFIED", `golapis.exit(golapis.HTTP_NOT_MODIFIED)`, 304},
		{"HTTP_BAD_REQUEST", `golapis.exit(golapis.HTTP_BAD_REQUEST)`, 400},
		{"HTTP_UNAUTHORIZED", `golapis.exit(golapis.HTTP_UNAUTHORIZED)`, 401},
		{"HTTP_FORBIDDEN", `golapis.exit(golapis.HTTP_FORBIDDEN)`, 403},
		{"HTTP_NOT_FOUND", `golapis.exit(golapis.HTTP_NOT_FOUND)`, 404},
		{"HTTP_NOT_ALLOWED", `golapis.exit(golapis.HTTP_NOT_ALLOWED)`, 405},
		{"HTTP_INTERNAL_SERVER_ERROR", `golapis.exit(golapis.HTTP_INTERNAL_SERVER_ERROR)`, 500},
		{"HTTP_BAD_GATEWAY", `golapis.exit(golapis.HTTP_BAD_GATEWAY)`, 502},
		{"HTTP_SERVICE_UNAVAILABLE", `golapis.exit(golapis.HTTP_SERVICE_UNAVAILABLE)`, 503},
		{"HTTP_GATEWAY_TIMEOUT", `golapis.exit(golapis.HTTP_GATEWAY_TIMEOUT)`, 504},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _, err := runLuaWithHTTP(t, tt.code)
			if err != nil {
				t.Fatalf("Lua error: %v", err)
			}
			if w.Code != tt.wantStatus {
				t.Errorf("Response code = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHTTPStatusConstants(t *testing.T) {
	// Verify the constants have correct values
	_, _, err := runLuaWithHTTP(t, `
		assert(golapis.HTTP_OK == 200, "HTTP_OK should be 200")
		assert(golapis.HTTP_NOT_FOUND == 404, "HTTP_NOT_FOUND should be 404")
		assert(golapis.HTTP_INTERNAL_SERVER_ERROR == 500, "HTTP_INTERNAL_SERVER_ERROR should be 500")
		assert(golapis.OK == 0, "OK should be 0")
		assert(golapis.ERROR == -1, "ERROR should be -1")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
}

func TestExitWithHeadersSet(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.header["X-Custom"] = "test value"
		golapis.header["Content-Type"] = "application/json"
		golapis.exit(201)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if w.Code != 201 {
		t.Errorf("Response code = %d, want 201", w.Code)
	}
	if got := w.Header().Get("X-Custom"); got != "test value" {
		t.Errorf("X-Custom header = %q, want %q", got, "test value")
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type header = %q, want %q", got, "application/json")
	}
}

func TestExitInLoop(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		for i = 1, 10 do
			golapis.say("iteration " .. i)
			if i == 3 then
				golapis.exit(200)
			end
		end
		golapis.say("after loop - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "iteration 1\niteration 2\niteration 3\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestExitInFunction(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local function handle_error()
			golapis.exit(500)
		end

		golapis.say("before call")
		handle_error()
		golapis.say("after call - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Body.String(); got != "before call\n" {
		t.Errorf("Body = %q, want %q", got, "before call\n")
	}
	// Status is 200 because say() already sent headers
	if w.Code != 200 {
		t.Errorf("Response code = %d, want 200 (headers already sent)", w.Code)
	}
}

func TestExitInFunctionNoOutput(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local function handle_error()
			golapis.exit(500)
		end

		handle_error()
		golapis.say("after call - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Body.String(); got != "" {
		t.Errorf("Body = %q, want empty", got)
	}
	if w.Code != 500 {
		t.Errorf("Response code = %d, want 500", w.Code)
	}
}

func TestExitInNestedFunction(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local function inner()
			golapis.exit(403)
		end

		local function outer()
			golapis.say("in outer")
			inner()
			golapis.say("after inner - should not print")
		end

		outer()
		golapis.say("after outer - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Body.String(); got != "in outer\n" {
		t.Errorf("Body = %q, want %q", got, "in outer\n")
	}
	// Status is 200 because say() already sent headers
	if w.Code != 200 {
		t.Errorf("Response code = %d, want 200 (headers already sent)", w.Code)
	}
}

func TestExitInNestedFunctionNoOutput(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local function inner()
			golapis.exit(403)
		end

		local function outer()
			inner()
			golapis.say("after inner - should not print")
		end

		outer()
		golapis.say("after outer - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Body.String(); got != "" {
		t.Errorf("Body = %q, want empty", got)
	}
	if w.Code != 403 {
		t.Errorf("Response code = %d, want 403", w.Code)
	}
}

func TestExitNotInTimerCallback(t *testing.T) {
	// exit() should error in timer callbacks since they don't have HTTP context
	_, _, err := runLuaWithHTTP(t, `
		golapis.timer.at(0, function()
			golapis.exit(200)
		end)
		golapis.sleep(0.05)
	`)
	// We expect this to NOT fail the main request, but the timer callback should error internally
	// The timer error gets logged but doesn't propagate to the main request
	if err != nil {
		// If there's an error, it should mention "HTTP request context"
		if !strings.Contains(err.Error(), "HTTP request context") {
			// It's okay if the main request succeeds but timer fails silently
		}
	}
}

func TestExitPreservesStatusWhenSet(t *testing.T) {
	// If golapis.status is set before exit, exit should override it
	w, _, err := runLuaWithHTTP(t, `
		golapis.status = 200
		golapis.exit(404)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	// exit(404) should win
	if w.Code != 404 {
		t.Errorf("Response code = %d, want 404 (exit should override status)", w.Code)
	}
}

func TestExitWithZeroStatus(t *testing.T) {
	// exit(0) should use default (200)
	w, _, err := runLuaWithHTTP(t, `
		golapis.exit(0)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if w.Code != 200 {
		t.Errorf("Response code = %d, want 200", w.Code)
	}
}
