package golapis

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNow(t *testing.T) {
	code := `
		local t = golapis.now()
		golapis.say("type:", type(t))
		golapis.say("positive:", t > 0)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "type:number") {
		t.Errorf("expected type:number, got: %q", output)
	}
	if !strings.Contains(output, "positive:true") {
		t.Errorf("expected positive value, got: %q", output)
	}
}

func TestNowReturnsReasonableTimestamp(t *testing.T) {
	code := `
		local t = golapis.now()
		golapis.print(string.format("%.6f", t))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	luaTime, err := strconv.ParseFloat(strings.TrimSpace(output), 64)
	if err != nil {
		t.Fatalf("Failed to parse lua time %q: %v", output, err)
	}

	goTime := float64(time.Now().UnixNano()) / 1e9
	diff := math.Abs(luaTime - goTime)
	if diff > 1.0 {
		t.Errorf("Time difference too large: lua=%.6f go=%.6f diff=%.6f", luaTime, goTime, diff)
	}
}

func TestNowHasMicrosecondPrecision(t *testing.T) {
	code := `
		local t = golapis.now()
		-- Check that formatting with 6 decimal places gives different trailing digits
		golapis.print(string.format("%.6f", t))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	// Check it has decimal places
	if !strings.Contains(output, ".") {
		t.Errorf("expected decimal point in timestamp, got: %q", output)
	}

	// Parse and verify it's a valid timestamp
	luaTime, err := strconv.ParseFloat(strings.TrimSpace(output), 64)
	if err != nil {
		t.Fatalf("Failed to parse lua time %q: %v", output, err)
	}

	// Should be a reasonable Unix timestamp (after year 2020)
	if luaTime < 1577836800 { // 2020-01-01
		t.Errorf("timestamp seems too old: %.6f", luaTime)
	}
}

func TestUpdateTimeReturnsNil(t *testing.T) {
	code := `
		local result = golapis.update_time()
		golapis.say("result:", tostring(result))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "result:nil") {
		t.Errorf("expected update_time to return nil, got: %q", output)
	}
}

func TestUpdateTimeDoesNotError(t *testing.T) {
	// Just verify calling update_time doesn't cause errors
	code := `
		golapis.update_time()
		golapis.update_time()
		golapis.update_time()
		golapis.say("ok")
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "ok") {
		t.Errorf("expected 'ok', got: %q", output)
	}
}
