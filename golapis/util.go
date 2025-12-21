package golapis

import (
	"net/url"
	"strings"
)

const upperhex = "0123456789ABCDEF"

// escapeURI percent-encodes a string according to nginx-lua escape_uri behavior.
// escapeType 0: Full URI - escapes space, #, %, ?, and bytes 0x00-0x1F, 0x7F-0xFF
// escapeType 2: URI component - escapes everything except A-Z, a-z, 0-9, -, ., _, ~
func escapeURI(s string, escapeType int) string {
	var sb strings.Builder
	sb.Grow(len(s) * 3) // Worst case: every byte gets escaped

	for i := 0; i < len(s); i++ {
		c := s[i]
		if shouldEscape(c, escapeType) {
			sb.WriteByte('%')
			sb.WriteByte(upperhex[c>>4])
			sb.WriteByte(upperhex[c&0x0F])
		} else {
			sb.WriteByte(c)
		}
	}

	return sb.String()
}

// shouldEscape returns true if the byte should be percent-encoded for the given escape type.
func shouldEscape(c byte, escapeType int) bool {
	if escapeType == 0 {
		// Type 0: Full URI escaping
		// Escape: space, #, %, ?, 0x00-0x1F, 0x7F-0xFF
		if c == ' ' || c == '#' || c == '%' || c == '?' {
			return true
		}
		if c <= 0x1F || c >= 0x7F {
			return true
		}
		return false
	}

	// Type 2 (default): URI component escaping
	// Keep only: A-Z, a-z, 0-9, -, ., _, ~
	if c >= 'A' && c <= 'Z' {
		return false
	}
	if c >= 'a' && c <= 'z' {
		return false
	}
	if c >= '0' && c <= '9' {
		return false
	}
	if c == '-' || c == '.' || c == '_' || c == '~' {
		return false
	}
	return true
}

// unescapeURI decodes a percent-encoded string with nginx-lua compatible behavior.
// - Decodes valid %XX sequences
// - Decodes + as space
// - Leaves invalid sequences unchanged (e.g., %zz, trailing %)
func unescapeURI(s string) string {
	// Fast path: no escapes
	if !strings.ContainsAny(s, "%+") {
		return s
	}

	var sb strings.Builder
	sb.Grow(len(s))

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '+':
			sb.WriteByte(' ')
		case '%':
			if i+2 < len(s) {
				if decoded, ok := unhex(s[i+1], s[i+2]); ok {
					sb.WriteByte(decoded)
					i += 2
					continue
				}
			}
			// Invalid sequence: keep the %
			sb.WriteByte('%')
		default:
			sb.WriteByte(s[i])
		}
	}

	return sb.String()
}

// unhex decodes two hex characters to a byte.
// Returns the decoded byte and true if both characters are valid hex digits.
func unhex(hi, lo byte) (byte, bool) {
	h, ok1 := hexValue(hi)
	l, ok2 := hexValue(lo)
	if !ok1 || !ok2 {
		return 0, false
	}
	return h<<4 | l, true
}

// hexValue returns the numeric value of a hex character and whether it's valid.
func hexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

// queryArg represents a parsed query argument value
// isBoolean=true means the arg had no "=" sign (e.g., ?foo)
type queryArg struct {
	key       string
	value     string
	isBoolean bool
}

// parseQueryString parses a raw query string with nginx-lua compatible behavior:
// - "?foo" (no =) returns foo=true
// - "?foo=" returns foo=""
// - "?foo=bar" returns foo="bar"
// - Multiple values for same key are returned as repeated tuples in order
func parseQueryString(rawQuery string, maxArgs int) ([]queryArg, bool) {
	result := make([]queryArg, 0)
	truncated := false

	if rawQuery == "" {
		return result, false
	}

	for _, part := range strings.Split(rawQuery, "&") {
		if part == "" {
			continue
		}

		var key, value string
		var isBoolean bool

		if idx := strings.Index(part, "="); idx >= 0 {
			// Has "=" sign: key=value or key=
			key = part[:idx]
			value = part[idx+1:]
			isBoolean = false
		} else {
			// No "=" sign: boolean arg
			key = part
			isBoolean = true
		}

		// URL-decode key and value
		if decodedKey, err := url.QueryUnescape(key); err == nil {
			key = decodedKey
		}
		if !isBoolean {
			if decodedValue, err := url.QueryUnescape(value); err == nil {
				value = decodedValue
			}
		}

		if maxArgs > 0 && len(result) >= maxArgs {
			truncated = true
			break
		}

		result = append(result, queryArg{key: key, value: value, isBoolean: isBoolean})
	}

	return result, truncated
}
