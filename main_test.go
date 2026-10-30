package main

import "testing"

func TestHTTPAddrFlag(t *testing.T) {
	valid := map[string]string{
		"8080":           ":8080",
		":8080":          ":8080",
		"127.0.0.1:8080": "127.0.0.1:8080",
		"localhost:8080": "localhost:8080",
		"[::1]:8080":     "[::1]:8080",
	}
	for input, want := range valid {
		var f httpAddrFlag
		if err := f.Set(input); err != nil {
			t.Errorf("Set(%q) error: %v", input, err)
		} else if string(f) != want {
			t.Errorf("Set(%q) = %q, want %q", input, f, want)
		}
	}

	for _, input := range []string{"", "app.lua", "-e", "::1", "127.0.0.1", "127.0.0.1:", "host:http", "70000"} {
		var f httpAddrFlag
		if err := f.Set(input); err == nil {
			t.Errorf("Set(%q) = %q, want error", input, f)
		}
	}
}
