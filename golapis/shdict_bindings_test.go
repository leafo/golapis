package golapis

import (
	"strings"
	"sync"
	"testing"
)

// The registry is process-global, so dicts for these tests are registered
// once with names unlikely to collide with other tests.
var shdictBindingsTestSetup sync.Once

func runShdictLua(t *testing.T, code string) error {
	t.Helper()

	shdictBindingsTestSetup.Do(func() {
		if err := RegisterSharedDict("bind_test", 1<<20); err != nil {
			t.Fatalf("register bind_test: %v", err)
		}
		if err := RegisterSharedDict("bind_tiny", 8192); err != nil {
			t.Fatalf("register bind_tiny: %v", err)
		}
	})

	// shared assert helper prepended to every test chunk
	_, err := runLuaAndCapture(t, `
		local function check(cond, msg)
			if not cond then error(msg or "check failed", 2) end
		end
	`+code)
	return err
}

func TestShdictLuaBasic(t *testing.T) {
	err := runShdictLua(t, `
		local d = golapis.shared.bind_test

		check(golapis.shared.no_such_dict == nil, "undeclared dict should be nil")

		-- set returns exactly (true, nil, false)
		check(select("#", d:set("basic_k", "hello")) == 3, "set should return 3 values")
		local ok, e, forcible = d:set("basic_k", "hello")
		check(ok == true and e == nil and forcible == false, "set tuple")

		-- get with zero flags returns a single value
		check(select("#", d:get("basic_k")) == 1, "get should return 1 value when flags == 0")
		check(d:get("basic_k") == "hello", "get value")

		-- value types round-trip
		d:set("basic_num", 3.5)
		check(d:get("basic_num") == 3.5, "number value")
		d:set("basic_int", 42)
		check(d:get("basic_int") == 42, "integer value")
		d:set("basic_true", true)
		check(d:get("basic_true") == true, "boolean true")
		d:set("basic_false", false)
		check(d:get("basic_false") == false, "boolean false")

		-- missing key is a single nil
		check(select("#", d:get("basic_missing")) == 1, "miss should return 1 value")
		check(d:get("basic_missing") == nil, "miss is nil")

		-- non-string keys go through tostring()
		d:set(123, "numkey")
		check(d:get("123") == "numkey", "number key conversion")
		d:set(true, "boolkey")
		check(d:get("true") == "boolkey", "boolean key conversion")

		-- delete
		local ok2, e2, f2 = d:delete("basic_k")
		check(ok2 == true and e2 == nil and f2 == false, "delete tuple")
		check(d:get("basic_k") == nil, "deleted")
		check(d:delete("basic_never_existed") == true, "delete missing is true")

		-- set(key, nil) also deletes
		d:set("basic_num", nil)
		check(d:get("basic_num") == nil, "set nil deletes")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestShdictLuaFlags(t *testing.T) {
	err := runShdictLua(t, `
		local d = golapis.shared.bind_test

		d:set("flags_k", "v", 0, 7)
		check(select("#", d:get("flags_k")) == 2, "get should return 2 values when flags ~= 0")
		local v, flags = d:get("flags_k")
		check(v == "v" and flags == 7, "flags returned")

		-- get_stale always returns 3 values when found
		check(select("#", d:get_stale("flags_k")) == 3, "get_stale returns 3 values")
		local v2, flags2, stale = d:get_stale("flags_k")
		check(v2 == "v" and flags2 == 7 and stale == false, "get_stale tuple")

		d:set("flags_zero", "v")
		local v3, flags3, stale3 = d:get_stale("flags_zero")
		check(v3 == "v" and flags3 == nil and stale3 == false, "get_stale with zero flags")

		check(d:get_stale("flags_missing") == nil, "get_stale miss is nil")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestShdictLuaAddReplaceIncr(t *testing.T) {
	err := runShdictLua(t, `
		local d = golapis.shared.bind_test

		check(d:add("ari_k", "first") == true, "add new")
		local ok, e = d:add("ari_k", "second")
		check(ok == false and e == "exists", "add existing")

		check(d:replace("ari_k", "replaced") == true, "replace existing")
		local ok2, e2 = d:replace("ari_missing", "v")
		check(ok2 == false and e2 == "not found", "replace missing")

		-- incr without init
		local n, e3 = d:incr("ari_counter", 1)
		check(n == nil and e3 == "not found", "incr missing")

		d:set("ari_counter", 10)
		check(select("#", d:incr("ari_counter", 5)) == 1, "incr no-init returns 1 value")
		check(d:incr("ari_counter", 5) == 20, "incr value")

		-- incr with init returns 3 values
		check(select("#", d:incr("ari_counter2", 2, 100)) == 3, "incr init returns 3 values")
		local n2, e4, f = d:incr("ari_counter2", 2, 100)
		-- first call created 102, second call increments to 104
		check(n2 == 104 and e4 == nil and f == false, "incr init tuple")

		-- incr on non-number
		d:set("ari_str", "x")
		local n3, e5 = d:incr("ari_str", 1)
		check(n3 == nil and e5 == "not a number", "incr on string")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestShdictLuaKeyAndValueErrors(t *testing.T) {
	err := runShdictLua(t, `
		local d = golapis.shared.bind_test

		local v, e = d:get(nil)
		check(v == nil and e == "nil key", "nil key")
		local v2, e2 = d:get("")
		check(v2 == nil and e2 == "empty key", "empty key")
		local v3, e3 = d:get(string.rep("x", 65536))
		check(v3 == nil and e3 == "key too long", "key too long")

		local ok, e4 = d:set("err_k", {})
		check(ok == nil and e4 == "bad value type", "table value")

		local ok2, e5, f = d:add("err_k", nil)
		check(ok2 == false and e5 == "attempt to add or replace nil values" and f == false,
			"add nil value")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestShdictLuaArgumentErrors(t *testing.T) {
	err := runShdictLua(t, `
		local d = golapis.shared.bind_test

		local ok, e = pcall(function() return d:set("ae_k", "v", -1) end)
		check(not ok and e:find('bad "exptime" argument', 1, true), "negative exptime: " .. tostring(e))

		local ok2, e2 = pcall(function() return d:incr("ae_k", 1, nil, 10) end)
		check(not ok2 and e2:find('must provide "init" when providing "init_ttl"', 1, true),
			"init_ttl without init: " .. tostring(e2))

		local ok3, e3 = pcall(function() return d:incr("ae_k", 1, 0, -1) end)
		check(not ok3 and e3:find('bad "init_ttl" argument', 1, true), "negative init_ttl")

		local ok4, e4 = pcall(function() return d:expire("ae_k") end)
		check(not ok4 and e4:find('bad "exptime" argument', 1, true), "expire without exptime")

		-- method called on a non-dict
		local ok5, e5 = pcall(d.get, {}, "k")
		check(not ok5 and e5:find('bad "zone" argument', 1, true), "bad zone table")
		local ok6, e6 = pcall(d.get, 42, "k")
		check(not ok6 and e6:find('bad "zone" argument', 1, true), "bad zone number")

		local ok7, e7 = pcall(function() return d:incr("ae_k", 1, "abc") end)
		check(not ok7 and e7:find("bad init arg", 1, true), "bad init type")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestShdictLuaTTLExpire(t *testing.T) {
	err := runShdictLua(t, `
		local d = golapis.shared.bind_test

		local ttl, e = d:ttl("ttl_missing")
		check(ttl == nil and e == "not found", "ttl missing")
		local ok, e2 = d:expire("ttl_missing", 5)
		check(ok == nil and e2 == "not found", "expire missing")

		d:set("ttl_forever", "v")
		check(d:ttl("ttl_forever") == 0, "ttl never-expire is 0")

		d:set("ttl_k", "v", 5)
		local remaining = d:ttl("ttl_k")
		check(remaining > 4.9 and remaining <= 5, "ttl close to 5, got " .. tostring(remaining))

		check(d:expire("ttl_k", 100) == true, "expire ok")
		check(d:ttl("ttl_k") > 99, "ttl updated")
		check(d:expire("ttl_k", 0) == true, "expire 0 ok")
		check(d:ttl("ttl_k") == 0, "expiry removed")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestShdictLuaLists(t *testing.T) {
	err := runShdictLua(t, `
		local d = golapis.shared.bind_test

		check(d:llen("list_k") == 0, "llen missing is 0")

		check(d:rpush("list_k", "a") == 1, "rpush 1")
		check(d:rpush("list_k", 2) == 2, "rpush 2")
		check(d:lpush("list_k", "front") == 3, "lpush")

		check(d:llen("list_k") == 3, "llen")
		check(d:lpop("list_k") == "front", "lpop")
		check(d:rpop("list_k") == 2, "rpop preserves numbers")
		check(d:lpop("list_k") == "a", "lpop last")
		check(d:lpop("list_k") == nil, "pop empty is nil")
		check(d:llen("list_k") == 0, "list removed when emptied")

		-- type interactions
		d:rpush("list_k2", "x")
		local v, e = d:get("list_k2")
		check(v == nil and e == "value is a list", "get on list")
		local n, e2 = d:incr("list_k2", 1)
		check(n == nil and e2 == "not a number", "incr on list")

		d:set("list_scalar", "v")
		local n2, e3 = d:lpush("list_scalar", "x")
		check(n2 == nil and e3 == "value not a list", "push on scalar")
		local v2, e4 = d:lpop("list_scalar")
		check(v2 == nil and e4 == "value not a list", "pop on scalar")
		local l, e5 = d:llen("list_scalar")
		check(l == nil and e5 == "value not a list", "llen on scalar")

		-- set over a list destroys it
		d:set("list_k2", "scalar now")
		check(d:get("list_k2") == "scalar now", "set over list")

		-- bad list value types
		local n3, e6 = d:rpush("list_k3", true)
		check(n3 == nil and e6 == "bad value type", "boolean list value")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestShdictLuaGetKeysCapacity(t *testing.T) {
	err := runShdictLua(t, `
		local d = golapis.shared.bind_tiny
		d:flush_all()
		d:flush_expired()

		d:set("gk_1", "v")
		d:set("gk_2", "v")
		d:set("gk_3", "v")

		local keys = d:get_keys()
		check(type(keys) == "table" and #keys == 3, "get_keys count, got " .. #keys)
		check(keys[1] == "gk_1" and keys[3] == "gk_3", "get_keys oldest first")

		local keys2 = d:get_keys(2)
		check(#keys2 == 2, "get_keys max_count")

		check(d:capacity() == 8192, "capacity")
		local free = d:free_space()
		check(type(free) == "number" and free > 0 and free < 8192, "free_space")

		check(d:flush_expired() == 0, "flush_expired none")
		d:flush_all()
		check(#d:get_keys() == 0, "get_keys empty after flush_all")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestShdictLuaForcibleEviction(t *testing.T) {
	err := runShdictLua(t, `
		local d = golapis.shared.bind_tiny
		d:flush_all()
		d:flush_expired()

		local big = string.rep("x", 1500)
		local count = 0
		local forced = false
		-- fill the 8k dict until eviction kicks in
		for i = 1, 10 do
			local ok, e, forcible = d:set("ev_" .. i, big)
			check(ok == true, "set should succeed via eviction: " .. tostring(e))
			if forcible then forced = true end
			count = i
		end
		check(forced, "expected at least one forcible eviction")
		check(d:get("ev_1") == nil, "oldest entry evicted")
		check(d:get("ev_" .. count) ~= nil, "newest entry present")

		-- safe_set refuses to evict
		local ok2, e2, f2 = d:safe_set("ev_safe", big)
		check(ok2 == false and e2 == "no memory" and f2 == false, "safe_set no memory")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

// Shared dicts are process-global: values written in one Lua state are
// visible from a freshly created state.
func TestShdictLuaSharedAcrossStates(t *testing.T) {
	if err := runShdictLua(t, `
		golapis.shared.bind_test:set("cross_state", "written in state 1")
	`); err != nil {
		t.Fatal(err)
	}

	out, err := runLuaAndCapture(t, `
		golapis.print(golapis.shared.bind_test:get("cross_state"))
	`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "written in state 1") {
		t.Errorf("value not shared across states, got: %q", out)
	}
}
