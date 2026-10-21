package golapis

/*
#include <lua.h>
#include <lauxlib.h>
#include "lua_helpers.h"
*/
import "C"
import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"sync"
	"unicode/utf8"
	"unsafe"
)

// =============================================================================
// Regex primitives (ngx.re)
// =============================================================================
//
// Two exports back the ngx.re API that bootstrap.lua builds in Lua:
// _exec runs one match from a byte offset and _exec_all collects every
// non-overlapping match. Both hand back raw byte offsets; capture tables,
// ctx.pos, find's nth, gmatch's iterator and sub/gsub's templates are all
// Lua-side.
//
// Go's regexp is RE2: no backreferences, no lookaround, no possessive or
// atomic groups. Such patterns fail to compile and surface as the same
// (nil, "failed to compile regex ...") nginx returns for a bad pattern.
// PCRE's (?<name>...) is accepted as-is.

type compiledRegex struct {
	re       *regexp.Regexp
	ngroups  int      // capturing groups, excluding group 0
	names    []string // per group index (1-based), "" when unnamed
	anyNamed bool

	// needsContext is set when the pattern has ^, \A, \b or \B, whose
	// meaning at a start offset depends on the preceding text; see execAt
	needsContext bool
	offsetOnce   sync.Once
	offsetRe     *regexp.Regexp
	offsetSrc    string
}

const regexCacheMax = 1024

var (
	regexCache   = make(map[string]*compiledRegex)
	regexCacheMu sync.Mutex
)

// parseRegexFlags maps nginx's option letters. i, m, s become RE2 inline
// flags; a anchors the match at the start position. o (compile once), j
// (JIT), d (DFA), D (dup names), u/U (UTF-8) change nothing here: every
// pattern is cached, and RE2 is always UTF-8. x (extended syntax) has no
// RE2 equivalent and is refused.
func parseRegexFlags(flags string) (prefix string, anchored bool, err error) {
	var i, m, s bool
	for k := 0; k < len(flags); k++ {
		switch c := flags[k]; c {
		case 'i':
			i = true
		case 'm':
			m = true
		case 's':
			s = true
		case 'a':
			anchored = true
		case 'o', 'j', 'd', 'D', 'u', 'U':
		case 'x':
			return "", false, fmt.Errorf(`unsupported flag "x" (flags "%s"): extended syntax is not available with RE2`, flags)
		default:
			return "", false, fmt.Errorf(`unknown flag "%c" (flags "%s")`, c, flags)
		}
	}
	if i || m || s {
		prefix = "(?"
		if i {
			prefix += "i"
		}
		if m {
			prefix += "m"
		}
		if s {
			prefix += "s"
		}
		prefix += ")"
	}
	return prefix, anchored, nil
}

// compileRegex returns the cached compilation of pattern with flags. A flag
// error is a usage error (raised); a pattern error is returned as a message
// for the caller to hand back as (nil, err).
func compileRegex(pattern, flags string) (c *compiledRegex, flagErr error, compileErr string) {
	prefix, anchored, err := parseRegexFlags(flags)
	if err != nil {
		return nil, err, ""
	}
	// Keyed on the effective flags so "jo", "oj" and "" share an entry
	key := prefix
	if anchored {
		key += "a"
	}
	key += "\x00" + pattern

	regexCacheMu.Lock()
	c = regexCache[key]
	regexCacheMu.Unlock()
	if c != nil {
		return c, nil, ""
	}

	src := prefix + pattern
	offsetSrc := prefix + `\A(?s:.)(?s:.*?)(` + pattern + `)`
	if anchored {
		src = prefix + `\A(?:` + pattern + `)`
		offsetSrc = prefix + `\A(?s:.)(` + pattern + `)`
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, nil, fmt.Sprintf("failed to compile regex \"%s\": %v", pattern, err)
	}

	names := re.SubexpNames()
	c = &compiledRegex{re: re, ngroups: re.NumSubexp(), names: names, offsetSrc: offsetSrc}
	if tree, err := syntax.Parse(src, syntax.Perl); err == nil {
		c.needsContext = hasContextAssertion(tree)
	} else {
		c.needsContext = true
	}
	for _, n := range names {
		if n != "" {
			c.anyNamed = true
			break
		}
	}

	regexCacheMu.Lock()
	if len(regexCache) >= regexCacheMax {
		regexCache = make(map[string]*compiledRegex)
	}
	regexCache[key] = c
	regexCacheMu.Unlock()
	return c, nil, ""
}

// hasContextAssertion reports whether the parsed pattern contains an
// assertion that looks at the character before the current position
func hasContextAssertion(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpBeginLine, syntax.OpBeginText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return true
	}
	for _, sub := range re.Sub {
		if hasContextAssertion(sub) {
			return true
		}
	}
	return false
}

// execAt finds the leftmost match starting at or after byte offset init,
// returning offsets relative to subject (group 0 first), or nil.
//
// Go's regexp has no start-offset search. Patterns without context
// assertions simply search subject[init:]. For the others, matching
// subject[init:] would let ^, \A and \b treat init as the start of the
// text where PCRE sees the preceding character, so the search runs on the
// text from one byte before init with a derived pattern,
// \A(?s:.)(?s:.*?)(P), that consumes that byte of context and captures P
// as group 1; the lazy gap keeps leftmost-first semantics. When init is in
// the middle of a multi-byte character the preceding byte would make (?s:.)
// swallow the whole character, so a neutral byte stands in for it.
func (c *compiledRegex) execAt(subject []byte, init int) []int {
	if init <= 0 {
		return c.re.FindSubmatchIndex(subject)
	}
	if init > len(subject) {
		return nil
	}
	if !c.needsContext {
		m := c.re.FindSubmatchIndex(subject[init:])
		return rebase(m, init)
	}

	c.offsetOnce.Do(func() {
		// The derivation only wraps and prefixes an already valid pattern
		c.offsetRe = regexp.MustCompile(c.offsetSrc)
	})
	base := init - 1
	text := subject[base:]
	if _, size := utf8.DecodeRune(text); size != 1 {
		// Mid-character: \x01 is one byte, not a word char, not a newline
		buf := make([]byte, 0, len(text))
		buf = append(buf, 1)
		text = append(buf, subject[init:]...)
	}
	m := c.offsetRe.FindSubmatchIndex(text)
	if m == nil {
		return nil
	}
	return rebase(m[2:], base) // drop the derived group 0; group 1 is P
}

func rebase(m []int, base int) []int {
	for i := range m {
		if m[i] >= 0 {
			m[i] += base
		}
	}
	return m
}

// execAll collects every match the way nginx's gsub does: search from the
// end of the previous match, and after an empty match step one byte
// forward so the search makes progress
func (c *compiledRegex) execAll(subject []byte) [][]int {
	var matches [][]int
	pos := 0
	for pos <= len(subject) {
		m := c.execAt(subject, pos)
		if m == nil {
			break
		}
		matches = append(matches, m)
		pos = m[1]
		if m[0] == m[1] {
			pos++
		}
	}
	return matches
}

// regexArgs reads (subject, regex, flags) from the stack; nameIdx holds the
// calling function's name for error messages. The subject is converted
// with tostring semantics for numbers; the regex must be a string. Returns
// the C-wrapper result to use when the arguments are rejected: -1 (raise,
// message pushed) for a usage error, 2 for (nil, err) on a bad pattern; 0
// when ok.
func regexArgs(L *C.lua_State, nameIdx C.int) (subject []byte, c *compiledRegex, rc C.int) {
	fname := "match"
	if C.lua_gettop(L) >= nameIdx && C.lua_type(L, nameIdx) == C.LUA_TSTRING {
		fname = string(luaStringBytes(L, nameIdx))
	}
	usage := func(msg string) ([]byte, *compiledRegex, C.int) {
		C.lua_pushnil(L)
		pushGoString(L, msg)
		C.lua_pushboolean(L, 1)
		return nil, nil, 3
	}
	if C.lua_isstring(L, 1) == 0 {
		return usage(fmt.Sprintf("bad argument #1 to '%s' (string expected, got %s)", fname, luaTypeName(L, 1)))
	}
	if C.lua_type(L, 2) != C.LUA_TSTRING {
		return usage(fmt.Sprintf("bad argument #2 to '%s' (string expected, got %s)", fname, luaTypeName(L, 2)))
	}
	flags := ""
	if C.lua_gettop(L) >= 3 && C.lua_type(L, 3) != C.LUA_TNIL {
		if C.lua_type(L, 3) != C.LUA_TSTRING {
			return usage(fmt.Sprintf("bad argument #3 to '%s' (string expected, got %s)", fname, luaTypeName(L, 3)))
		}
		flags = string(luaStringBytes(L, 3))
	}
	c, flagErr, compileErr := compileRegex(string(luaStringBytes(L, 2)), flags)
	if flagErr != nil {
		return usage(flagErr.Error())
	}
	if compileErr != "" {
		C.lua_pushnil(L)
		pushGoString(L, compileErr)
		return nil, nil, 2
	}
	return luaStringView(L, 1), c, 0
}

// luaStringView returns the Lua string at idx without copying. The slice
// aliases Lua memory: it is valid only while the value stays on the stack,
// i.e. for the duration of the binding, and must not be retained.
func luaStringView(L *C.lua_State, idx C.int) []byte {
	var n C.size_t
	p := C.lua_tolstring_wrapper(L, idx, &n)
	if p == nil || n == 0 {
		return []byte{}
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(p)), int(n))
}

func luaTypeName(L *C.lua_State, idx C.int) string {
	return C.GoString(C.lua_typename(L, C.lua_type(L, idx)))
}

// pushOffsets pushes the match locations as a flat array: for each match,
// start and end (0-based, end exclusive) for group 0..ngroups, -1 for a
// group that did not participate
func pushOffsets(L *C.lua_State, matches [][]int) {
	n := 0
	for _, m := range matches {
		n += len(m)
	}
	C.lua_createtable(L, C.int(n), 0)
	i := 1
	for _, m := range matches {
		for _, off := range m {
			C.lua_pushinteger(L, C.lua_Integer(off))
			C.lua_rawseti(L, -2, C.int(i))
			i++
		}
	}
}

// pushNames pushes {[group index] = name} or nil when no group is named
func pushNames(L *C.lua_State, c *compiledRegex) {
	if !c.anyNamed {
		C.lua_pushnil(L)
		return
	}
	C.lua_newtable_wrapper(L)
	for i, name := range c.names {
		if name == "" {
			continue
		}
		pushGoString(L, name)
		C.lua_rawseti(L, -2, C.int(i))
	}
}

// _exec(subject, regex, flags, init, fname) -> offsets, ngroups, names
//
//	| false, ngroups, names (no match)
//	| nil, err              (bad pattern)
//
// init is a 0-based byte offset; the match starts at or after it with the
// preceding text as context (see execAt). fname is only for error messages.
//
//export golapis_re_exec
func golapis_re_exec(L *C.lua_State) C.int {
	subject, c, rc := regexArgs(L, 5)
	if rc != 0 {
		return rc
	}
	init := 0
	if C.lua_gettop(L) >= 4 && C.lua_type(L, 4) != C.LUA_TNIL {
		init = int(C.lua_tointeger(L, 4))
	}

	m := c.execAt(subject, init)
	if m == nil {
		C.lua_pushboolean(L, 0)
	} else {
		pushOffsets(L, [][]int{m})
	}
	C.lua_pushinteger(L, C.lua_Integer(c.ngroups))
	pushNames(L, c)
	return 3
}

// _exec_all(subject, regex, flags, fname) -> offsets, nmatches, ngroups, names
//
//	| nil, err
//
//export golapis_re_exec_all
func golapis_re_exec_all(L *C.lua_State) C.int {
	subject, c, rc := regexArgs(L, 4)
	if rc != 0 {
		return rc
	}
	matches := c.execAll(subject)
	pushOffsets(L, matches)
	C.lua_pushinteger(L, C.lua_Integer(len(matches)))
	C.lua_pushinteger(L, C.lua_Integer(c.ngroups))
	pushNames(L, c)
	return 4
}
