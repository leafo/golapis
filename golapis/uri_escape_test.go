package golapis

import (
	"strings"
	"testing"
)

func TestEscapeURILua(t *testing.T) {
	code := `
		golapis.say(golapis.escape_uri("hello world"))
		golapis.say(golapis.escape_uri("a=b", 0))
		golapis.say(golapis.escape_uri("a=b", 2))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), output)
	}
	if lines[0] != "hello%20world" {
		t.Errorf("line 0: expected 'hello%%20world', got %q", lines[0])
	}
	if lines[1] != "a=b" {
		t.Errorf("line 1: expected 'a=b', got %q", lines[1])
	}
	if lines[2] != "a%3Db" {
		t.Errorf("line 2: expected 'a%%3Db', got %q", lines[2])
	}
}

func TestUnescapeURILua(t *testing.T) {
	code := `
		golapis.say(golapis.unescape_uri("hello%20world"))
		golapis.say(golapis.unescape_uri("hello+world"))
		golapis.say(golapis.unescape_uri("%zz"))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), output)
	}
	if lines[0] != "hello world" {
		t.Errorf("line 0: expected 'hello world', got %q", lines[0])
	}
	if lines[1] != "hello world" {
		t.Errorf("line 1: expected 'hello world', got %q", lines[1])
	}
	if lines[2] != "%zz" {
		t.Errorf("line 2: expected '%%zz', got %q", lines[2])
	}
}
