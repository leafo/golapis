package golapis

import (
	"strings"
	"testing"
)

func TestMD5Lua(t *testing.T) {
	code := `
		golapis.say(golapis.md5("hello"))
		golapis.say(golapis.md5(""))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	expected := []string{
		"5d41402abc4b2a76b9719d911017c592",
		"d41d8cd98f00b204e9800998ecf8427e",
	}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), output)
	}
	for i, exp := range expected {
		if lines[i] != exp {
			t.Errorf("line %d: expected %q, got %q", i, exp, lines[i])
		}
	}
}

func TestMD5BinLua(t *testing.T) {
	code := `
		local bin = golapis.md5_bin("hello")
		golapis.say(#bin)
		-- Verify first byte matches expected (0x5d = 93)
		golapis.say(string.byte(bin, 1))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	expected := []string{
		"16",
		"93", // 0x5d in decimal
	}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), output)
	}
	for i, exp := range expected {
		if lines[i] != exp {
			t.Errorf("line %d: expected %q, got %q", i, exp, lines[i])
		}
	}
}

func TestSHA1BinLua(t *testing.T) {
	code := `
		local bin = golapis.sha1_bin("hello")
		golapis.say(#bin)
		-- Verify first byte matches expected (0xaa = 170)
		golapis.say(string.byte(bin, 1))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	expected := []string{
		"20",
		"170", // 0xaa in decimal
	}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), output)
	}
	for i, exp := range expected {
		if lines[i] != exp {
			t.Errorf("line %d: expected %q, got %q", i, exp, lines[i])
		}
	}
}

func TestHMACSHA1Lua(t *testing.T) {
	// Test using the example from nginx-lua docs
	// HMAC-SHA1("thisisverysecretstuff", "some string we want to sign")
	// base64 encoded = "R/pvxzHC4NLtj7S+kXFg/NePTmk="
	code := `
		local key = "thisisverysecretstuff"
		local src = "some string we want to sign"
		local digest = golapis.hmac_sha1(key, src)
		golapis.say(#digest)
		-- First byte of the digest (0x47 = 71 = 'G' which is base64 'R')
		golapis.say(string.byte(digest, 1))
		-- Use golapis.encode_base64 to verify full result
		golapis.say(golapis.encode_base64(digest))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	expected := []string{
		"20",                           // HMAC-SHA1 produces 20 bytes
		"71",                           // First byte 0x47 = 71
		"R/pvxzHC4NLtj7S+kXFg/NePTmk=", // Expected base64 from nginx docs
	}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), output)
	}
	for i, exp := range expected {
		if lines[i] != exp {
			t.Errorf("line %d: expected %q, got %q", i, exp, lines[i])
		}
	}
}

func TestEncodeBase64Lua(t *testing.T) {
	code := `
		golapis.say(golapis.encode_base64("hello"))
		golapis.say(golapis.encode_base64("hello", true))
		golapis.say(golapis.encode_base64("hello", false))
		golapis.say(#golapis.encode_base64(""))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	expected := []string{
		"aGVsbG8=", // with padding (default)
		"aGVsbG8",  // no padding
		"aGVsbG8=", // with padding (explicit false)
		"0",        // empty string encodes to empty string (length 0)
	}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), output)
	}
	for i, exp := range expected {
		if lines[i] != exp {
			t.Errorf("line %d: expected %q, got %q", i, exp, lines[i])
		}
	}
}

func TestDecodeBase64Lua(t *testing.T) {
	code := `
		golapis.say(golapis.decode_base64("aGVsbG8="))
		golapis.say(golapis.decode_base64("aGVsbG8"))
		golapis.say(tostring(golapis.decode_base64("!!!invalid!!!")))
		local empty = golapis.decode_base64("")
		golapis.say(type(empty) .. ":" .. #empty)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	expected := []string{
		"hello",    // with padding
		"hello",    // without padding
		"nil",      // invalid input returns nil
		"string:0", // empty string decodes to empty string
	}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), output)
	}
	for i, exp := range expected {
		if lines[i] != exp {
			t.Errorf("line %d: expected %q, got %q", i, exp, lines[i])
		}
	}
}

func TestDecodeBase64MimeLua(t *testing.T) {
	code := `
		golapis.say(golapis.decode_base64mime("aGVs\nbG8="))
		golapis.say(golapis.decode_base64mime("aGVs bG8="))
		golapis.say(golapis.decode_base64mime("aGVsbG8="))
		local empty = golapis.decode_base64mime("")
		golapis.say(type(empty) .. ":" .. #empty)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	expected := []string{
		"hello",    // ignores newline
		"hello",    // ignores space
		"hello",    // normal decode
		"string:0", // empty string
	}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), output)
	}
	for i, exp := range expected {
		if lines[i] != exp {
			t.Errorf("line %d: expected %q, got %q", i, exp, lines[i])
		}
	}
}
