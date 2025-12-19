package golapis

import (
	"bytes"
	"strings"
	"testing"
)

func TestSay(t *testing.T) {
	tests := []struct {
		name     string
		luaCode  string
		expected string
	}{
		// Basic types
		{"string", `golapis.say("hello")`, "hello\n"},
		{"integer", `golapis.say(42)`, "42\n"},
		{"float", `golapis.say(3.14159)`, "3.14159\n"},
		{"boolean true", `golapis.say(true)`, "true\n"},
		{"boolean false", `golapis.say(false)`, "false\n"},
		{"nil", `golapis.say(nil)`, "nil\n"},
		{"null", `golapis.say(golapis.null)`, "null\n"},

		// Multiple args (no separator)
		{"multi args", `golapis.say("a", "b", "c")`, "abc\n"},
		{"mixed types", `golapis.say("n=", 42, " ok=", true)`, "n=42 ok=true\n"},

		// Arrays
		{"simple array", `golapis.say({"a", "b", "c"})`, "abc\n"},
		{"nested array", `golapis.say({"a", {"b", "c"}, "d"})`, "abcd\n"},
		{"sparse array", `golapis.say({[1]="a", [3]="c"})`, "anilc\n"},
		{"empty table", `golapis.say({})`, "\n"},

		// Edge cases
		{"no args", `golapis.say()`, "\n"},
		{"empty string", `golapis.say("")`, "\n"},
		{"number formatting int32 max", `golapis.say(2147483647)`, "2147483647\n"},
		{"number formatting outside int32", `golapis.say(2147483648)`, "2147483648\n"},
		{"negative number", `golapis.say(-42)`, "-42\n"},
		{"scientific notation", `golapis.say(1e15)`, "1e+15\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runLuaAndCapture(t, tt.luaCode)
			if err != nil {
				t.Fatalf("Lua error: %v", err)
			}
			if output != tt.expected {
				t.Errorf("output mismatch\ngot:  %q\nwant: %q", output, tt.expected)
			}
		})
	}
}

func TestPrint(t *testing.T) {
	tests := []struct {
		name     string
		luaCode  string
		expected string
	}{
		{"string no newline", `golapis.print("hello")`, "hello"},
		{"multiple prints", `golapis.print("a"); golapis.print("b")`, "ab"},
		{"no args", `golapis.print()`, ""},
		{"integer", `golapis.print(42)`, "42"},
		{"array", `golapis.print({"x", "y"})`, "xy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runLuaAndCapture(t, tt.luaCode)
			if err != nil {
				t.Fatalf("Lua error: %v", err)
			}
			if output != tt.expected {
				t.Errorf("output mismatch\ngot:  %q\nwant: %q", output, tt.expected)
			}
		})
	}
}

func TestSayReturnValue(t *testing.T) {
	// Test that say returns 1 on success
	code := `
		local ok = golapis.say("test")
		golapis.say("ok=", ok)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "ok=1") {
		t.Errorf("expected return value 1, got output: %q", output)
	}
}

func TestPrintReturnValue(t *testing.T) {
	// Test that print returns 1 on success
	code := `
		local ok = golapis.print("test")
		golapis.say("ok=", ok)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "ok=1") {
		t.Errorf("expected return value 1, got output: %q", output)
	}
}

func TestSayErrors(t *testing.T) {
	tests := []struct {
		name        string
		luaCode     string
		errContains string
	}{
		{"function arg", `golapis.say(function() end)`, "function"},
		{"non-array table", `golapis.say({foo="bar"})`, "non-array"},
		{"thread arg", `golapis.say(coroutine.create(function() end))`, "thread"},
		{"negative key", `golapis.say({[-1]="x"})`, "non-array"},
		{"zero key", `golapis.say({[0]="x"})`, "non-array"},
		{"float key", `golapis.say({[1.5]="x"})`, "non-array"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Code that captures error and outputs it
			code := `
				local ok, err = ` + tt.luaCode + `
				golapis.say("err=", tostring(err))
			`
			output, luaErr := runLuaAndCapture(t, code)
			if luaErr != nil {
				t.Fatalf("Lua error: %v", luaErr)
			}
			if !strings.Contains(output, tt.errContains) {
				t.Errorf("expected error containing %q, got: %q", tt.errContains, output)
			}
		})
	}
}

func TestSayAndPrintCombined(t *testing.T) {
	// Test combining say and print in sequence
	code := `
		golapis.print("start:")
		golapis.say("middle")
		golapis.print("end")
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	expected := "start:middle\nend"
	if output != expected {
		t.Errorf("output mismatch\ngot:  %q\nwant: %q", output, expected)
	}
}

func TestOutputShortWrites(t *testing.T) {
	code := `
		golapis.print("short")
		golapis.say("write")
	`
	expected := "shortwrite\n"

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	buf := &bytes.Buffer{}
	gls.SetOutputWriter(&shortWriter{buf: buf, max: 1})

	gls.Start()
	defer gls.Stop()

	err := gls.RunString(code)
	gls.Wait()
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if buf.String() != expected {
		t.Errorf("output mismatch\ngot:  %q\nwant: %q", buf.String(), expected)
	}
}

// runLuaAndCapture creates a Lua state, runs code, and returns captured output
func runLuaAndCapture(t *testing.T, code string) (string, error) {
	t.Helper()

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	buf := &bytes.Buffer{}
	gls.SetOutputWriter(buf)

	gls.Start()
	defer gls.Stop()

	err := gls.RunString(code)
	gls.Wait()

	return buf.String(), err
}

type shortWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if w.max > 0 && len(p) > w.max {
		p = p[:w.max]
	}
	return w.buf.Write(p)
}
