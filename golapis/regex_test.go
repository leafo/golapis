package golapis

import (
	"strings"
	"testing"
)

// runRe runs code with golapis.re bound as `re` and returns say() output
func runRe(t *testing.T, code string) string {
	t.Helper()
	// say joins its arguments with spaces, print-style
	prelude := `local re = golapis.re
local function say(...)
	local t = {}
	for i = 1, select("#", ...) do t[i] = tostring((select(i, ...))) end
	golapis.say(table.concat(t, " "))
end
`
	out, err := runLuaCLI(t, prelude+code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	return out
}

func expectOut(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestReMatch(t *testing.T) {
	out := runRe(t, `
		local m = re.match("hello, 1234", "([0-9])[0-9]+")
		say(m[0], m[1], tostring(m[2]))
		m = re.match("hello, 1234", "(?<first>[0-9])([0-9]+)", "jo")
		say(m[0], m[1], m[2], m.first)
		-- unset optional groups are false, by index and by name
		m = re.match("abc", "(a)(?<x>x)?(c)?")
		say(tostring(m[1]), tostring(m[2]), tostring(m[3]), tostring(m.x))
		say(tostring(re.match("hello", "world")))
		-- numbers are converted
		say(re.match(12345, "3(4)")[1])
		-- res table reuse
		local res = {}
		say(tostring(re.match("ab", "(b)", nil, nil, res) == res), res[1])
	`)
	expectOut(t, out, "1234 1 nil\n1234 1 234 1\na false false false\nnil\n4\ntrue b\n")
}

func TestReMatchFlags(t *testing.T) {
	out := runRe(t, `
		say(re.match("Hello", "hello", "i")[0])
		say(tostring(re.match("a\nb", "^b")), re.match("a\nb", "^b", "m")[0])
		say(tostring(re.match("a\nb", "a.b")), re.match("a\nb", "a.b", "s")[0])
		-- anchored
		say(tostring(re.match("xab", "ab", "a")), re.match("ab", "ab", "a")[0])
		-- no-op flags accepted
		say(re.match("ab", "ab", "jodDuU")[0])
		say(select(2, pcall(re.match, "ab", "ab", "q")))
		say(select(2, pcall(re.match, "ab", "ab", "x")))
	`)
	expectOut(t, out, "Hello\nnil b\nnil a\nb\nnil ab\nab\n"+
		`unknown flag "q" (flags "q")`+"\n"+
		`unsupported flag "x" (flags "x"): extended syntax is not available with RE2`+"\n")
}

func TestReCompileErrors(t *testing.T) {
	out := runRe(t, `
		local m, err = re.match("hello", "(world")
		say(tostring(m), err)
		-- RE2 has no backreferences: reported as a compile error, not raised
		local m, err = re.match("aa", [[(a)\1]])
		say(tostring(m), err:match("^failed to compile regex") ~= nil)
		local from, to, err = re.find("hello", "(world")
		say(tostring(from), tostring(to), err:match("^failed to compile") ~= nil)
		local it, err = re.gmatch("hello", "(world")
		say(tostring(it), err:match("^failed to compile") ~= nil)
		local s, n, err = re.gsub("hello", "(world", "x")
		say(tostring(s), tostring(n), err:match("^failed to compile") ~= nil)
		say(select(2, pcall(re.match, "ab", nil)))
		say(select(2, pcall(re.find, "ab", nil)))
		say(select(2, pcall(re.gmatch, "ab", nil)))
		say(select(2, pcall(re.gsub, "ab", nil, "")))
	`)
	expectOut(t, out, "nil failed to compile regex \"(world\": error parsing regexp: missing closing ): `(world`\n"+
		"nil true\nnil nil true\nnil true\nnil nil true\n"+
		"bad argument #2 to 'match' (string expected, got nil)\n"+
		"bad argument #2 to 'find' (string expected, got nil)\n"+
		"bad argument #2 to 'gmatch' (string expected, got nil)\n"+
		"bad argument #2 to 'gsub' (string expected, got nil)\n")
}

func TestReFindAndCtx(t *testing.T) {
	out := runRe(t, `
		local from, to = re.find("hello, world", "o")
		say(from, to)
		local ctx = { pos = 6 }
		from, to = re.find("hello, world", "o", "jo", ctx)
		say(from, to, ctx.pos)
		-- ctx.pos advances past each match; no match leaves it alone
		ctx = {}
		local n = 0
		while re.find("a1b22c333", "[0-9]+", "jo", ctx) do n = n + 1 end
		say(n, ctx.pos)
		-- nth selects a group; unset group gives nil, nil
		say(re.find("hello", "(h)(e)", nil, nil, 2))
		say(tostring(re.find("hello", "(h)(x)?", nil, nil, 2)))
		say(select(3, re.find("hello", "(h)", nil, nil, 3)))
		-- match also honours ctx
		ctx = { pos = 3 }
		local m = re.match("abcabc", "abc", nil, ctx)
		say(m[0], ctx.pos)
		-- pos beyond the subject never matches
		say(tostring(re.match("abc", "", nil, { pos = 10 })))
		-- an nth error does not consume input
		ctx = { pos = 1 }
		re.find("hello", "(h)", nil, ctx, 3)
		say(ctx.pos)
	`)
	expectOut(t, out, "5 5\n9 9 10\n3 10\n2 2\nnil\nnth out of bound\nabc 7\nnil\n1\n")
}

func TestReOffsetKeepsContext(t *testing.T) {
	// With ctx.pos set, ^ \A and \b still see the text before pos, as in
	// nginx; and the match may not start before pos
	out := runRe(t, `
		say(tostring(re.find("ab", "^b", nil, { pos = 2 })))
		say(tostring(re.find("ab", "\\Ab", nil, { pos = 2 })))
		say(tostring(re.find("ab", "\\bb", nil, { pos = 2 })))
		say(re.find("a b", "\\bb", nil, { pos = 2 }))
		say(re.find("a\nb", "^b", "m", { pos = 3 }))
		-- searching from the newline still finds b after it
		say(re.find("a\nb", "^b", "m", { pos = 2 }))
		-- the match must start at or after pos, even if an earlier one overlaps
		say(re.find("aaa", "aa", nil, { pos = 2 }))
		-- anchored at pos
		say(re.find("xab", "ab", "a", { pos = 2 }))
		say(tostring(re.find("xab", "ab", "a", { pos = 1 })))
		-- a ctx-driven tokenizer sees exactly nginx's sequence
		local ctx, toks = {}, {}
		while true do
			local m = re.match("foo bar  baz", "^\\s*(\\w+)", "jo", ctx)
			if not m then break end
			toks[#toks + 1] = m[1]
		end
		say(#toks, ctx.pos)
		-- multi-byte text before pos
		say(re.find("日本語abc", "\\babc", nil, { pos = 10 }))
		say(tostring(re.find("日本語abc", "\\bbc", nil, { pos = 11 })))
		-- pos inside a multi-byte character: the search starts at that byte
		say(re.find("\230\151\165x", ".", nil, { pos = 2 }))
		say(tostring(re.find("\230\151\165x", "^x", nil, { pos = 2 })))
		say(re.find("\230\151\165x", "x", "a", { pos = 4 }))
		say(tostring(re.find("\230\151\165x", "x", "a", { pos = 2 })))
	`)
	expectOut(t, out, "nil\nnil\nnil\n3 3\n3 3\n3 3\n2 3\n2 3\nnil\n1 4\n10 12\nnil\n2 2\nnil\n4 4\nnil\n")
}

func TestReGmatch(t *testing.T) {
	out := runRe(t, `
		local parts = {}
		for m in re.gmatch("hello, world", "([a-z])([a-z]+)", "j") do
			parts[#parts + 1] = m[0] .. ":" .. m[1] .. ":" .. m[2]
		end
		say(table.concat(parts, " "))
		local it = re.gmatch("abc", "x")
		say(tostring(it()))
		-- lazy: context before each match is kept, ^ matches only once
		local n = 0
		for m in re.gmatch("aaa", "^a") do n = n + 1 end
		say(n)
		n = 0
		for m in re.gmatch("ab ab", "\\bab") do n = n + 1 end
		say(n)
		-- empty matches advance
		local n = 0
		for m in re.gmatch("abc", "x*") do n = n + 1 end
		say(n)
		for m in re.gmatch("k1=v1", "(?<k>[a-z0-9]+)=(?<v>[a-z0-9]+)") do say(m.k, "=", m.v) end
	`)
	expectOut(t, out, "hello:h:ello world:w:orld\nnil\n1\n2\n4\nk1 = v1\n")
}

func TestReSubGsub(t *testing.T) {
	out := runRe(t, `
		say(re.sub("hello, world", "([a-z]+)", "[$0,$1]"))
		say(re.gsub("hello, world", "([a-z])[a-z]+", "[$0,$1]", "i"))
		say(re.gsub("hello, world", "[a-z]+", function(m) return "[" .. m[0] .. "]" end))
		say(re.gsub("hello, world", "(?<w>[a-z]+)", "<${w}>${0}"))
		say(re.gsub("abc", "x*", "-"))
		-- ^ only matches at the start, even when replacing globally
		say(re.gsub("aaa", "^a", "b"))
		say(re.gsub("a$b", "\\$", "$$"))
		say(re.sub("abc", "(b)(x)?", "$1$2!"))
		-- no match returns the subject untouched
		say(re.gsub("abc", "x", "y"))
		-- function results are converted; numbers work as subjects
		say(re.gsub(1234, "[0-9]", function(m) return m[0] + 1 end))
		say(re.sub("abc", "b", "$"))
		say(re.sub("abc", "(b)", "$2"))
		say(re.sub("abc", "(b)", "${nope}"))
		say(re.sub("abc", "(b)", "${"))
		say(re.sub("abc", "(b)", 42))
		-- template errors are reported even without a match
		say(re.sub("abc", "x", "${"))
		-- names that look numeric to tonumber are still names
		say(re.sub("abc", "(?<nan>b)", "[$nan]"))
		say(re.sub("abc", "(?<inf>b)", "[${inf}]"))
		-- nginx stepping: an empty match right after a match still counts
		say(re.gsub("aaa", "a*", "-"))
		say(re.gsub("baaa", "a*", "-"))
		-- anchored gsub anchors at each successive position
		say(re.gsub("aaa", "a", "b", "a"))
		say(re.gsub("xaa", "a", "b", "a"))
		-- gsub and gmatch agree on the match sequence
		local n = 0
		for m in re.gmatch("baaa", "a*") do n = n + 1 end
		say(n, select(2, re.gsub("baaa", "a*", "")))
	`)
	expectOut(t, out, "[hello,hello], world 1\n[hello,h], [world,w] 2\n[hello], [world] 2\n<hello>hello, <world>world 2\n-a-b-c- 4\nbaa 1\na$b 1\nab!c 1\nabc 0\n2345 4\n"+
		"nil nil invalid capturing variable name found in \"$\"\n"+
		"nil nil the capture $2 is out of range (1 capturing groups)\n"+
		"nil nil unknown named capture \"nope\" in the replacement template\n"+
		"nil nil invalid capturing variable name found in \"${\"\n"+
		"a42c 1\n"+
		"nil nil invalid capturing variable name found in \"${\"\n"+
		"a[b]c 1\na[b]c 1\n-- 2\n-b-- 3\nbbb 3\nxaa 0\n3 3\n")
}

func TestReUsageErrorsPointAtCaller(t *testing.T) {
	// Argument and flag errors are raised at the ngx.re caller's level, not
	// inside bootstrap.lua
	out := runRe(t, `
		local function try(f, ...)
			local ok, err = pcall(function(...)
				local r = f(...) -- not a tail call, so this frame is the caller
				return r
			end, ...)
			say((err:gsub("^%[string \"[^\"]*\"%]:(%d+): ", "line %1: ")))
		end
		try(re.match, "ab", "ab", "q")
		try(re.find, "ab", nil)
		try(re.gmatch, "ab", "ab", false)
		try(re.sub, "ab", "ab", "", "q")
		try(re.gsub, "ab", "ab", "", "q")
	`)
	if strings.Contains(out, "bootstrap") {
		t.Errorf("error position points into bootstrap.lua: %q", out)
	}
	if n := strings.Count(out, "line 10: "); n != 5 {
		t.Errorf("want all 5 errors at the caller's line, got %d: %q", n, out)
	}
	for _, want := range []string{
		`line 10: unknown flag "q" (flags "q")`,
		`line 10: bad argument #2 to 'find' (string expected, got nil)`,
		`line 10: bad argument #3 to 'gmatch' (string expected, got boolean)`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestReContextFastPath(t *testing.T) {
	// Patterns without ^ \A \b \B search the subject from the offset
	// directly; the rest go through the context-preserving derived pattern
	for pat, want := range map[string]bool{
		"abc": false, "a$": false, "(?m)x$": false, "[^a]": false,
		"^a": true, `\Aa`: true, `\ba`: true, `\Ba`: true, "(?m)^a": true, "x(?:^|y)": true,
	} {
		c, _, cerr := compileRegex(pat, "")
		if cerr != "" {
			t.Fatalf("%s: %s", pat, cerr)
		}
		if c.needsContext != want {
			t.Errorf("needsContext(%q) = %v, want %v", pat, c.needsContext, want)
		}
	}
}

func TestReRequireAndCache(t *testing.T) {
	out := runRe(t, `
		say(tostring(require("ngx.re").match == re.match))
		-- the same pattern under different flags compiles separately
		say(tostring(re.match("A", "a")), re.match("A", "a", "i")[0])
		-- binary subjects and patterns work bytewise
		say(#re.match("a\0b", "\0(.)")[0], re.match("a\0b", "\0(.)")[1])
	`)
	expectOut(t, out, "true\nnil A\n2 b\n")
	if n := len(regexCache); n < 2 {
		t.Errorf("regex cache has %d entries, want at least 2", n)
	}
	// flag spellings with the same effect share one entry
	before := len(regexCache)
	runRe(t, `re.match("a", "zz", "jo") re.match("a", "zz", "oj") re.match("a", "zz") re.match("a", "zz", "o")`)
	if n := len(regexCache) - before; n != 1 {
		t.Errorf("4 spellings of the same flags added %d cache entries, want 1", n)
	}
}

func TestReWebsocketClientPatterns(t *testing.T) {
	// The three patterns resty.websocket.client uses, with its "jo" flags
	out := runRe(t, `
		local m = re.match("wss://example.com:8443/chat?x=1", [[^(wss?)://([^:/]+)(?::(\d+))?(.*)]], "jo")
		say(m[1], m[2], m[3], m[4])
		m = re.match("ws://example.com/", [[^(wss?)://([^:/]+)(?::(\d+))?(.*)]], "jo")
		say(m[1], m[2], tostring(m[3]), m[4])
		say(tostring(re.find("ws://unix:/tmp/x.sock:/path", "unix:")))
		m = re.match("ws://unix:/tmp/x.sock:/path", [[^(wss?)://(unix:[^:]+):()(.*)]], "jo")
		say(m[2], m[4])
		m = re.match("HTTP/1.1 101 Switching Protocols", [[^\s*HTTP/1\.1\s+(\d+)]], "jo")
		say(m[1])
	`)
	expectOut(t, out, "wss example.com 8443 /chat?x=1\nws example.com false /\n6\nunix:/tmp/x.sock /path\n101\n")
}
