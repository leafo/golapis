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

func TestHashWithNullBytesLua(t *testing.T) {
	code := `
		local input = "a\0b"

		-- md5: should hash all 3 bytes
		golapis.say(golapis.md5(input))

		-- md5_bin: verify length and first byte
		local md5bin = golapis.md5_bin(input)
		golapis.say(#md5bin)
		golapis.say(string.byte(md5bin, 1))

		-- sha1_bin: verify length and first byte
		local sha1bin = golapis.sha1_bin(input)
		golapis.say(#sha1bin)
		golapis.say(string.byte(sha1bin, 1))

		-- hmac_sha1: test with null bytes in both key and data
		local hmac = golapis.hmac_sha1("key\0secret", input)
		golapis.say(#hmac)
		golapis.say(string.byte(hmac, 1))

		-- encode_base64: should encode all 3 bytes including null
		golapis.say(golapis.encode_base64(input))

		-- decode_base64: should decode to 3 bytes including null
		local decoded = golapis.decode_base64("YQBi")
		golapis.say(#decoded)
		golapis.say(string.byte(decoded, 1))
		golapis.say(string.byte(decoded, 2))
		golapis.say(string.byte(decoded, 3))

		-- decode_base64mime: should handle null bytes in output
		local decoded_mime = golapis.decode_base64mime("YQBi")
		golapis.say(#decoded_mime)
		golapis.say(string.byte(decoded_mime, 2))

		-- escape_uri: null byte should be escaped as %00
		golapis.say(golapis.escape_uri(input))

		-- unescape_uri: %00 should decode to null byte
		local unescaped = golapis.unescape_uri("a%00b")
		golapis.say(#unescaped)
		golapis.say(string.byte(unescaped, 1))
		golapis.say(string.byte(unescaped, 2))
		golapis.say(string.byte(unescaped, 3))
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	expected := []string{
		"70350f6027bce3713f6b76473084309b", // md5("a\0b")
		"16",                               // md5_bin length
		"112",                              // md5_bin first byte
		"20",                               // sha1_bin length
		"74",                               // sha1_bin first byte
		"20",                               // hmac_sha1 length
		"58",                               // hmac_sha1 first byte
		"YQBi",                             // encode_base64("a\0b")
		"3",                                // decode_base64 length
		"97",                               // decode_base64 byte 1 ('a')
		"0",                                // decode_base64 byte 2 (null)
		"98",                               // decode_base64 byte 3 ('b')
		"3",                                // decode_base64mime length
		"0",                                // decode_base64mime byte 2 (null)
		"a%00b",                            // escape_uri("a\0b")
		"3",                                // unescape_uri length
		"97",                               // unescape_uri byte 1 ('a')
		"0",                                // unescape_uri byte 2 (null)
		"98",                               // unescape_uri byte 3 ('b')
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
