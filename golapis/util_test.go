package golapis

import (
	"reflect"
	"testing"
)

func TestParseQueryString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string][]queryArg
	}{
		{
			name:     "empty string",
			input:    "",
			expected: map[string][]queryArg{},
		},
		{
			name:  "single boolean arg",
			input: "foo",
			expected: map[string][]queryArg{
				"foo": {{value: "", isBoolean: true}},
			},
		},
		{
			name:  "multiple boolean args",
			input: "hello&world",
			expected: map[string][]queryArg{
				"hello": {{value: "", isBoolean: true}},
				"world": {{value: "", isBoolean: true}},
			},
		},
		{
			name:  "single key=value",
			input: "foo=bar",
			expected: map[string][]queryArg{
				"foo": {{value: "bar", isBoolean: false}},
			},
		},
		{
			name:  "empty value with equals",
			input: "foo=",
			expected: map[string][]queryArg{
				"foo": {{value: "", isBoolean: false}},
			},
		},
		{
			name:  "mixed boolean and value",
			input: "bool&empty=&value=foo",
			expected: map[string][]queryArg{
				"bool":  {{value: "", isBoolean: true}},
				"empty": {{value: "", isBoolean: false}},
				"value": {{value: "foo", isBoolean: false}},
			},
		},
		{
			name:  "multiple values same key",
			input: "x=a&x=b&x=c",
			expected: map[string][]queryArg{
				"x": {
					{value: "a", isBoolean: false},
					{value: "b", isBoolean: false},
					{value: "c", isBoolean: false},
				},
			},
		},
		{
			name:  "mixed types same key",
			input: "x&x=a&x",
			expected: map[string][]queryArg{
				"x": {
					{value: "", isBoolean: true},
					{value: "a", isBoolean: false},
					{value: "", isBoolean: true},
				},
			},
		},
		{
			name:  "url encoded value",
			input: "name=hello%20world",
			expected: map[string][]queryArg{
				"name": {{value: "hello world", isBoolean: false}},
			},
		},
		{
			name:  "url encoded key",
			input: "hello%20world=value",
			expected: map[string][]queryArg{
				"hello world": {{value: "value", isBoolean: false}},
			},
		},
		{
			name:  "special characters encoded",
			input: "tag=%26special&other=%3D%3D",
			expected: map[string][]queryArg{
				"tag":   {{value: "&special", isBoolean: false}},
				"other": {{value: "==", isBoolean: false}},
			},
		},
		{
			name:  "empty parts ignored",
			input: "foo&&bar&",
			expected: map[string][]queryArg{
				"foo": {{value: "", isBoolean: true}},
				"bar": {{value: "", isBoolean: true}},
			},
		},
		{
			name:  "value with equals sign",
			input: "equation=1%2B1=2",
			expected: map[string][]queryArg{
				"equation": {{value: "1+1=2", isBoolean: false}},
			},
		},
		{
			name:  "plus decoded as space",
			input: "msg=hello+world",
			expected: map[string][]queryArg{
				"msg": {{value: "hello world", isBoolean: false}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseQueryString(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseQueryString(%q)\ngot:  %+v\nwant: %+v", tt.input, result, tt.expected)
			}
		})
	}
}
