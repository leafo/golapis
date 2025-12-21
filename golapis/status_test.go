package golapis

import (
	"testing"
)

func TestStatusDefaultValue(t *testing.T) {
	w, req, err := runLuaWithHTTP(t, `
		local status = golapis.status
		golapis.say("status=" .. tostring(status))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	// Default status should be 0 from Lua perspective
	if req.ResponseStatus != 0 {
		t.Errorf("ResponseStatus = %d, want 0", req.ResponseStatus)
	}

	// But response should use 200
	if w.Code != 200 {
		t.Errorf("Response code = %d, want 200", w.Code)
	}
}

func TestStatusSet(t *testing.T) {
	w, req, err := runLuaWithHTTP(t, `
		golapis.status = 404
		golapis.say("not found")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if w.Code != 404 {
		t.Errorf("Response code = %d, want 404", w.Code)
	}
	if req.ResponseStatus != 404 {
		t.Errorf("ResponseStatus = %d, want 404", req.ResponseStatus)
	}
}

func TestStatusSetFromString(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.status = "201"
		golapis.say("created")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if w.Code != 201 {
		t.Errorf("Response code = %d, want 201", w.Code)
	}
}

func TestStatusReadBack(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `
		golapis.status = 403
		if golapis.status ~= 403 then
			error("status read back failed: got " .. tostring(golapis.status))
		end
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
}

func TestStatusInvalidRangeTooLow(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `golapis.status = 99`)
	if err == nil {
		t.Error("Expected error for status 99")
	}
}

func TestStatusInvalidRangeTooHigh(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `golapis.status = 1000`)
	if err == nil {
		t.Error("Expected error for status 1000")
	}
}

func TestStatusInvalidRangeNegative(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `golapis.status = -1`)
	if err == nil {
		t.Error("Expected error for negative status")
	}
}

func TestStatusAfterHeadersSent(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `
		golapis.say("body first")
		golapis.status = 500
	`)
	if err == nil {
		t.Fatal("Expected error when setting status after body write")
	}
}

func TestStatusNoBodyCase(t *testing.T) {
	// Status should work even without body output
	w, _, err := runLuaWithHTTP(t, `
		golapis.status = 204
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if w.Code != 204 {
		t.Errorf("Response code = %d, want 204", w.Code)
	}
}

func TestStatusMultipleSets(t *testing.T) {
	// Setting status multiple times before headers sent should work
	w, _, err := runLuaWithHTTP(t, `
		golapis.status = 404
		golapis.status = 500
		golapis.status = 201
		golapis.say("final")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if w.Code != 201 {
		t.Errorf("Response code = %d, want 201 (last set value)", w.Code)
	}
}

func TestStatusInvalidStringValue(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `golapis.status = "not a number"`)
	if err == nil {
		t.Error("Expected error for non-numeric string status")
	}
}

func TestStatusInvalidType(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `golapis.status = {}`)
	if err == nil {
		t.Error("Expected error for table status")
	}
}

func TestExistingTableKeysStillWork(t *testing.T) {
	// Ensure the metatable doesn't break existing functionality
	_, _, err := runLuaWithHTTP(t, `
		-- Test that regular keys still work
		if golapis.version ~= "1.0.0" then
			error("version check failed: got " .. tostring(golapis.version))
		end
		if type(golapis.say) ~= "function" then
			error("say not a function")
		end
		if type(golapis.header) ~= "table" then
			error("header not a table")
		end
		if type(golapis.var) ~= "table" then
			error("var not a table")
		end
		if type(golapis.req) ~= "table" then
			error("req not a table")
		end
		if type(golapis.http) ~= "table" then
			error("http not a table")
		end

		golapis.say("all checks passed")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
}

func TestCustomKeysStillWork(t *testing.T) {
	// Ensure we can still set/get custom keys via rawset/rawget fallback
	_, _, err := runLuaWithHTTP(t, `
		golapis.custom_key = "test_value"
		if golapis.custom_key ~= "test_value" then
			error("custom key set/get failed: got " .. tostring(golapis.custom_key))
		end

		golapis.say("custom key works")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
}

func TestStatusWithHeaders(t *testing.T) {
	// Status and headers should work together
	w, _, err := runLuaWithHTTP(t, `
		golapis.status = 301
		golapis.header["Location"] = "https://example.com"
		golapis.say("redirecting")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if w.Code != 301 {
		t.Errorf("Response code = %d, want 301", w.Code)
	}
	if got := w.Header().Get("Location"); got != "https://example.com" {
		t.Errorf("Location header = %q, want %q", got, "https://example.com")
	}
}
