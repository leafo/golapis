package golapis

import (
	"net/url"
	"strings"
)

// queryArg represents a parsed query argument value
// isBoolean=true means the arg had no "=" sign (e.g., ?foo)
type queryArg struct {
	value     string
	isBoolean bool
}

// parseQueryString parses a raw query string with nginx-lua compatible behavior:
// - "?foo" (no =) returns foo=true
// - "?foo=" returns foo=""
// - "?foo=bar" returns foo="bar"
// - Multiple values for same key are collected into a slice
func parseQueryString(rawQuery string) map[string][]queryArg {
	result := make(map[string][]queryArg)

	if rawQuery == "" {
		return result
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

		result[key] = append(result[key], queryArg{value: value, isBoolean: isBoolean})
	}

	return result
}
