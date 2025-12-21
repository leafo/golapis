package golapis

import (
	"reflect"
	"testing"
)

func TestParseQueryString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []queryArg
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []queryArg{},
		},
		{
			name:  "single boolean arg",
			input: "foo",
			expected: []queryArg{
				{key: "foo", value: "", isBoolean: true},
			},
		},
		{
			name:  "multiple boolean args",
			input: "hello&world",
			expected: []queryArg{
				{key: "hello", value: "", isBoolean: true},
				{key: "world", value: "", isBoolean: true},
			},
		},
		{
			name:  "single key=value",
			input: "foo=bar",
			expected: []queryArg{
				{key: "foo", value: "bar", isBoolean: false},
			},
		},
		{
			name:  "empty value with equals",
			input: "foo=",
			expected: []queryArg{
				{key: "foo", value: "", isBoolean: false},
			},
		},
		{
			name:  "mixed boolean and value",
			input: "bool&empty=&value=foo",
			expected: []queryArg{
				{key: "bool", value: "", isBoolean: true},
				{key: "empty", value: "", isBoolean: false},
				{key: "value", value: "foo", isBoolean: false},
			},
		},
		{
			name:  "multiple values same key",
			input: "x=a&x=b&x=c",
			expected: []queryArg{
				{key: "x", value: "a", isBoolean: false},
				{key: "x", value: "b", isBoolean: false},
				{key: "x", value: "c", isBoolean: false},
			},
		},
		{
			name:  "mixed types same key",
			input: "x&x=a&x",
			expected: []queryArg{
				{key: "x", value: "", isBoolean: true},
				{key: "x", value: "a", isBoolean: false},
				{key: "x", value: "", isBoolean: true},
			},
		},
		{
			name:  "url encoded value",
			input: "name=hello%20world",
			expected: []queryArg{
				{key: "name", value: "hello world", isBoolean: false},
			},
		},
		{
			name:  "url encoded key",
			input: "hello%20world=value",
			expected: []queryArg{
				{key: "hello world", value: "value", isBoolean: false},
			},
		},
		{
			name:  "special characters encoded",
			input: "tag=%26special&other=%3D%3D",
			expected: []queryArg{
				{key: "tag", value: "&special", isBoolean: false},
				{key: "other", value: "==", isBoolean: false},
			},
		},
		{
			name:  "empty parts ignored",
			input: "foo&&bar&",
			expected: []queryArg{
				{key: "foo", value: "", isBoolean: true},
				{key: "bar", value: "", isBoolean: true},
			},
		},
		{
			name:  "value with equals sign",
			input: "equation=1%2B1=2",
			expected: []queryArg{
				{key: "equation", value: "1+1=2", isBoolean: false},
			},
		},
		{
			name:  "plus decoded as space",
			input: "msg=hello+world",
			expected: []queryArg{
				{key: "msg", value: "hello world", isBoolean: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, truncated := parseQueryString(tt.input, 0)
			if truncated {
				t.Errorf("parseQueryString(%q) unexpectedly truncated", tt.input)
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseQueryString(%q)\ngot:  %+v\nwant: %+v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseQueryStringMaxArgs(t *testing.T) {
	result, truncated := parseQueryString("a=1&b=2&c=3", 2)
	expected := []queryArg{
		{key: "a", value: "1", isBoolean: false},
		{key: "b", value: "2", isBoolean: false},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("parseQueryString max args\ngot:  %+v\nwant: %+v", result, expected)
	}
	if !truncated {
		t.Errorf("parseQueryString expected truncated=true")
	}
}

func TestEscapeURIType2(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "alphanumeric unchanged",
			input:    "ABCxyz123",
			expected: "ABCxyz123",
		},
		{
			name:     "unreserved chars unchanged",
			input:    "hello-world_test.txt~",
			expected: "hello-world_test.txt~",
		},
		{
			name:     "space escaped",
			input:    "hello world",
			expected: "hello%20world",
		},
		{
			name:     "special chars escaped",
			input:    "a=b&c=d",
			expected: "a%3Db%26c%3Dd",
		},
		{
			name:     "slash escaped",
			input:    "path/to/file",
			expected: "path%2Fto%2Ffile",
		},
		{
			name:     "unicode escaped",
			input:    "café",
			expected: "caf%C3%A9",
		},
		{
			name:     "percent escaped",
			input:    "100%",
			expected: "100%25",
		},
		{
			name:     "question mark escaped",
			input:    "what?",
			expected: "what%3F",
		},
		{
			name:     "hash escaped",
			input:    "#anchor",
			expected: "%23anchor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeURI(tt.input, 2)
			if result != tt.expected {
				t.Errorf("escapeURI(%q, 2) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEscapeURIType0(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "alphanumeric unchanged",
			input:    "ABCxyz123",
			expected: "ABCxyz123",
		},
		{
			name:     "space escaped",
			input:    "hello world",
			expected: "hello%20world",
		},
		{
			name:     "special chars NOT escaped",
			input:    "a=b&c/d:e@f!g",
			expected: "a=b&c/d:e@f!g",
		},
		{
			name:     "question mark escaped",
			input:    "what?",
			expected: "what%3F",
		},
		{
			name:     "hash escaped",
			input:    "#anchor",
			expected: "%23anchor",
		},
		{
			name:     "percent escaped",
			input:    "100%",
			expected: "100%25",
		},
		{
			name:     "control chars escaped",
			input:    "hello\tworld\n",
			expected: "hello%09world%0A",
		},
		{
			name:     "high bytes escaped",
			input:    "café",
			expected: "caf%C3%A9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeURI(tt.input, 0)
			if result != tt.expected {
				t.Errorf("escapeURI(%q, 0) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestUnescapeURI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no escapes",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "space from %20",
			input:    "hello%20world",
			expected: "hello world",
		},
		{
			name:     "plus as space",
			input:    "hello+world",
			expected: "hello world",
		},
		{
			name:     "mixed %20 and plus",
			input:    "b%20r56+7",
			expected: "b r56 7",
		},
		{
			name:     "lowercase hex",
			input:    "%2f%2F",
			expected: "//",
		},
		{
			name:     "invalid sequence - single %",
			input:    "100%",
			expected: "100%",
		},
		{
			name:     "invalid sequence - non-hex",
			input:    "%zz",
			expected: "%zz",
		},
		{
			name:     "invalid sequence - partial hex",
			input:    "%2",
			expected: "%2",
		},
		{
			name:     "mixed valid and invalid",
			input:    "try %search%%20%again%",
			expected: "try %search% %again%",
		},
		{
			name:     "all special chars",
			input:    "%3F%23%26%3D",
			expected: "?#&=",
		},
		{
			name:     "null byte",
			input:    "%00",
			expected: "\x00",
		},
		{
			name:     "high bytes (unicode)",
			input:    "caf%C3%A9",
			expected: "café",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unescapeURI(tt.input)
			if result != tt.expected {
				t.Errorf("unescapeURI(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
