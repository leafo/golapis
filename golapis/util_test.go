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
