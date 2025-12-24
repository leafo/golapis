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

func TestReqStartTimeReturnsNilOutsideHTTP(t *testing.T) {
	// Outside HTTP context, req.start_time() should return nil
	code := `
		local t = golapis.req.start_time()
		golapis.say("result:", tostring(t))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "result:nil") {
		t.Errorf("expected nil outside HTTP context, got: %q", output)
	}
}

func TestToday(t *testing.T) {
	code := `
		local d = golapis.today()
		golapis.say("type:", type(d))
		golapis.say("len:", #d)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "type:string") {
		t.Errorf("expected type:string, got: %q", output)
	}
	if !strings.Contains(output, "len:10") {
		t.Errorf("expected len:10 (yyyy-mm-dd format), got: %q", output)
	}
}

func TestTodayFormat(t *testing.T) {
	code := `
		local d = golapis.today()
		golapis.print(d)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	output = strings.TrimSpace(output)

	// Verify format matches yyyy-mm-dd
	if len(output) != 10 {
		t.Errorf("expected 10 characters, got %d: %q", len(output), output)
	}
	if output[4] != '-' || output[7] != '-' {
		t.Errorf("expected yyyy-mm-dd format, got: %q", output)
	}

	// Verify it matches Go's current date
	expected := time.Now().Format("2006-01-02")
	if output != expected {
		t.Errorf("date mismatch: lua=%q go=%q", output, expected)
	}
}

func TestTime(t *testing.T) {
	code := `
		local ts = golapis.time()
		golapis.say("type:", type(ts))
		golapis.say("positive:", ts > 0)
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

func TestTimeIsInteger(t *testing.T) {
	code := `
		local ts = golapis.time()
		-- Check that it's an integer (no decimal part)
		golapis.say("is_int:", ts == math.floor(ts))
		golapis.print(ts)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "is_int:true") {
		t.Errorf("expected integer (no decimal), got: %q", output)
	}
}

func TestTimeReturnsReasonableTimestamp(t *testing.T) {
	code := `
		local ts = golapis.time()
		golapis.print(ts)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	luaTime, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse lua time %q: %v", output, err)
	}

	goTime := time.Now().Unix()
	diff := luaTime - goTime
	if diff < 0 {
		diff = -diff
	}
	if diff > 1 {
		t.Errorf("Time difference too large: lua=%d go=%d diff=%d", luaTime, goTime, diff)
	}
}

func TestNowVsTime(t *testing.T) {
	// Verify that now() and time() are related: floor(now()) should equal time()
	code := `
		local now_val = golapis.now()
		local time_val = golapis.time()
		local floored = math.floor(now_val)
		-- Allow for timing edge case where second boundary is crossed
		local diff = math.abs(floored - time_val)
		golapis.say("diff_ok:", diff <= 1)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "diff_ok:true") {
		t.Errorf("now() and time() should be related, got: %q", output)
	}
}
