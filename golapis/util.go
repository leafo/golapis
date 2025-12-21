package golapis

import (
	"net/url"
	"strings"
)

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
