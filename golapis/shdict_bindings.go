package golapis

/*
#include "lua_helpers.h"
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// Lua bindings for shared dicts (golapis.shared.<name>). Return tuples and
// error strings mirror lua-resty-core's resty/core/shdict.lua. Operations are
// synchronous (no coroutine yield): they briefly lock the dict's Go mutex.
//
// Convention: a negative return tells the C wrapper to raise a Lua error with
// the message on top of the stack (used for argument misuse, matching the
// error() calls in resty/core).

var cStrShdictMetatable = C.CString("golapis.shdict") // allocated once, never freed

// setupSharedDicts populates golapis.shared with one dict object per
// registered shared dict. Each object is a userdata holding the dict's
// registry index, with the golapis.shdict metatable for methods.
func (gls *GolapisLuaState) setupSharedDicts() {
	L := gls.luaState

	C.lua_rawgeti_wrapper(L, C.LUA_REGISTRYINDEX, gls.golapisRef)

	cShared := C.CString("shared")
	defer C.free(unsafe.Pointer(cShared))
	C.lua_getfield(L, -1, cShared)

	for id, d := range registeredSharedDicts() {
		ptr := C.lua_newuserdata(L, C.size_t(unsafe.Sizeof(uint64(0))))
		*(*uint64)(ptr) = uint64(id)
		C.luaL_getmetatable_wrapper(L, cStrShdictMetatable)
		C.lua_setmetatable(L, -2)

		cName := C.CString(d.name)
		C.lua_setfield(L, -2, cName)
		C.free(unsafe.Pointer(cName))
	}

	C.lua_pop_wrapper(L, 2) // pop shared table and golapis table
}

// shdictRaise pushes msg and returns -1 so the C wrapper raises a Lua error.
func shdictRaise(L *C.lua_State, msg string) C.int {
	pushGoString(L, msg)
	return -1
}

// shdictZone extracts the SharedDict from the userdata at stack index 1,
// verifying its metatable so foreign userdata can't be misinterpreted.
func shdictZone(L *C.lua_State) *SharedDict {
	if C.lua_getmetatable(L, 1) == 0 {
		return nil
	}
	C.luaL_getmetatable_wrapper(L, cStrShdictMetatable)
	eq := C.lua_rawequal(L, -1, -2)
	C.lua_pop_wrapper(L, 2)
	if eq == 0 {
		return nil
	}

	ptr := C.lua_touserdata_wrapper(L, 1)
	if ptr == nil {
		return nil
	}
	return getSharedDictByID(*(*uint64)(ptr))
}

// shdictKey extracts and validates the key at stack idx, applying tostring()
// conversion for non-string keys like resty/core does. A non-empty errStr
// means the caller should return (nil, errStr) to Lua.
func shdictKey(L *C.lua_State, idx C.int) (key string, errStr string) {
	switch C.lua_type(L, idx) {
	case C.LUA_TNONE, C.LUA_TNIL:
		return "", "nil key"
	case C.LUA_TSTRING, C.LUA_TNUMBER:
		// lua_tolstring converts number slots to their tostring() form
		key = string(luaStringBytes(L, idx))
	case C.LUA_TBOOLEAN:
		if C.lua_toboolean_wrapper(L, idx) != 0 {
			key = "true"
		} else {
			key = "false"
		}
	default:
		typeName := C.GoString(C.lua_typename(L, C.lua_type(L, idx)))
		key = fmt.Sprintf("%s: %p", typeName, C.lua_topointer(L, idx))
	}

	if len(key) == 0 {
		return "", "empty key"
	}
	if len(key) > shdictMaxKeyLen {
		return "", "key too long"
	}
	return key, ""
}

// shdictGetHelper implements get (stale=false) and get_stale (stale=true).
func shdictGetHelper(L *C.lua_State, stale bool) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	key, errStr := shdictKey(L, 2)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	val, flags, isStale, errStr := dict.Get(key, stale)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	if val.Type == shdictTNil {
		C.lua_pushnil(L)
		return 1
	}

	pushShdictValue(L, val)

	if !stale {
		if flags != 0 {
			C.lua_pushnumber(L, C.lua_Number(flags))
			return 2
		}
		return 1
	}

	// get_stale always returns 3 values when found
	if flags != 0 {
		C.lua_pushnumber(L, C.lua_Number(flags))
	} else {
		C.lua_pushnil(L)
	}
	if isStale {
		C.lua_pushboolean(L, 1)
	} else {
		C.lua_pushboolean(L, 0)
	}
	return 3
}

func pushShdictValue(L *C.lua_State, val ShdictValue) {
	switch val.Type {
	case shdictTString:
		pushGoString(L, val.Str)
	case shdictTNumber:
		C.lua_pushnumber(L, C.lua_Number(val.Num))
	case shdictTBool:
		if val.Num != 0 {
			C.lua_pushboolean(L, 1)
		} else {
			C.lua_pushboolean(L, 0)
		}
	default:
		C.lua_pushnil(L)
	}
}

//export golapis_shdict_get
func golapis_shdict_get(L *C.lua_State) C.int {
	return shdictGetHelper(L, false)
}

//export golapis_shdict_get_stale
func golapis_shdict_get_stale(L *C.lua_State) C.int {
	return shdictGetHelper(L, true)
}

// shdictStoreHelper implements set/safe_set/add/safe_add/replace/delete.
// Stack: 1=zone, 2=key, 3=value, 4=exptime, 5=flags.
func shdictStoreHelper(L *C.lua_State, op int) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	// exptime is validated before the key, matching shdict_store
	exptime := float64(0)
	switch C.lua_type(L, 4) {
	case C.LUA_TNONE, C.LUA_TNIL:
	default:
		if C.lua_isnumber(L, 4) == 0 {
			return shdictRaise(L, `bad "exptime" argument`)
		}
		exptime = float64(C.lua_tonumber(L, 4))
		if exptime < 0 {
			return shdictRaise(L, `bad "exptime" argument`)
		}
	}

	var flags uint32
	switch C.lua_type(L, 5) {
	case C.LUA_TNONE, C.LUA_TNIL:
	default:
		if C.lua_isnumber(L, 5) == 0 {
			return shdictRaise(L, `bad "flags" argument`)
		}
		flags = uint32(int64(C.lua_tonumber(L, 5)))
	}

	key, errStr := shdictKey(L, 2)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	var val ShdictValue
	switch C.lua_type(L, 3) {
	case C.LUA_TSTRING:
		val = ShdictValue{Type: shdictTString, Str: string(luaStringBytes(L, 3))}
	case C.LUA_TNUMBER:
		val = ShdictValue{Type: shdictTNumber, Num: float64(C.lua_tonumber(L, 3))}
	case C.LUA_TBOOLEAN:
		val = ShdictValue{Type: shdictTBool}
		if C.lua_toboolean_wrapper(L, 3) != 0 {
			val.Num = 1
		}
	case C.LUA_TNONE, C.LUA_TNIL:
		if op&(shdictOpAdd|shdictOpReplace) != 0 {
			C.lua_pushboolean(L, 0)
			pushGoString(L, "attempt to add or replace nil values")
			C.lua_pushboolean(L, 0)
			return 3
		}
		val = ShdictValue{Type: shdictTNil}
	default:
		C.lua_pushnil(L)
		pushGoString(L, "bad value type")
		return 2
	}

	ok, errStr, forcible := dict.Store(op, key, val, int64(exptime*1000), flags)

	if ok {
		C.lua_pushboolean(L, 1)
		C.lua_pushnil(L)
	} else {
		C.lua_pushboolean(L, 0)
		pushGoString(L, errStr)
	}
	if forcible {
		C.lua_pushboolean(L, 1)
	} else {
		C.lua_pushboolean(L, 0)
	}
	return 3
}

//export golapis_shdict_set
func golapis_shdict_set(L *C.lua_State) C.int {
	return shdictStoreHelper(L, 0)
}

//export golapis_shdict_safe_set
func golapis_shdict_safe_set(L *C.lua_State) C.int {
	return shdictStoreHelper(L, shdictOpSafe)
}

//export golapis_shdict_add
func golapis_shdict_add(L *C.lua_State) C.int {
	return shdictStoreHelper(L, shdictOpAdd)
}

//export golapis_shdict_safe_add
func golapis_shdict_safe_add(L *C.lua_State) C.int {
	return shdictStoreHelper(L, shdictOpAdd|shdictOpSafe)
}

//export golapis_shdict_replace
func golapis_shdict_replace(L *C.lua_State) C.int {
	return shdictStoreHelper(L, shdictOpReplace)
}

//export golapis_shdict_delete
func golapis_shdict_delete(L *C.lua_State) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	key, errStr := shdictKey(L, 2)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	dict.Store(0, key, ShdictValue{Type: shdictTNil}, 0, 0)

	C.lua_pushboolean(L, 1)
	C.lua_pushnil(L)
	C.lua_pushboolean(L, 0)
	return 3
}

//export golapis_shdict_incr
func golapis_shdict_incr(L *C.lua_State) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	key, errStr := shdictKey(L, 2)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	if C.lua_isnumber(L, 3) == 0 {
		typeName := C.GoString(C.lua_typename(L, C.lua_type(L, 3)))
		return shdictRaise(L, "bad value arg: number expected, got "+typeName)
	}
	delta := float64(C.lua_tonumber(L, 3))

	hasInit := false
	var init float64
	switch C.lua_type(L, 4) {
	case C.LUA_TNONE, C.LUA_TNIL:
	default:
		if C.lua_isnumber(L, 4) == 0 {
			typeName := C.GoString(C.lua_typename(L, C.lua_type(L, 4)))
			return shdictRaise(L, "bad init arg: number expected, got "+typeName)
		}
		hasInit = true
		init = float64(C.lua_tonumber(L, 4))
	}

	var initTTL float64
	switch C.lua_type(L, 5) {
	case C.LUA_TNONE, C.LUA_TNIL:
	default:
		if C.lua_isnumber(L, 5) == 0 {
			typeName := C.GoString(C.lua_typename(L, C.lua_type(L, 5)))
			return shdictRaise(L, "bad init_ttl arg: number expected, got "+typeName)
		}
		initTTL = float64(C.lua_tonumber(L, 5))
		if initTTL < 0 {
			return shdictRaise(L, `bad "init_ttl" argument`)
		}
		if !hasInit {
			return shdictRaise(L, `must provide "init" when providing "init_ttl"`)
		}
	}

	newVal, errStr, forcible := dict.Incr(key, delta, hasInit, init, int64(initTTL*1000))
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	C.lua_pushnumber(L, C.lua_Number(newVal))
	if !hasInit {
		return 1
	}
	C.lua_pushnil(L)
	if forcible {
		C.lua_pushboolean(L, 1)
	} else {
		C.lua_pushboolean(L, 0)
	}
	return 3
}

//export golapis_shdict_ttl
func golapis_shdict_ttl(L *C.lua_State) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	key, errStr := shdictKey(L, 2)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	ms, found := dict.TTLms(key)
	if !found {
		C.lua_pushnil(L)
		pushGoString(L, "not found")
		return 2
	}

	C.lua_pushnumber(L, C.lua_Number(float64(ms)/1000))
	return 1
}

//export golapis_shdict_expire
func golapis_shdict_expire(L *C.lua_State) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	// exptime is validated before the key, matching shdict_expire
	switch C.lua_type(L, 3) {
	case C.LUA_TNONE, C.LUA_TNIL:
		return shdictRaise(L, `bad "exptime" argument`)
	default:
		if C.lua_isnumber(L, 3) == 0 {
			return shdictRaise(L, `bad "exptime" argument`)
		}
	}
	exptime := float64(C.lua_tonumber(L, 3))

	key, errStr := shdictKey(L, 2)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	if !dict.Expire(key, int64(exptime*1000)) {
		C.lua_pushnil(L)
		pushGoString(L, "not found")
		return 2
	}

	C.lua_pushboolean(L, 1)
	return 1
}

//export golapis_shdict_get_keys
func golapis_shdict_get_keys(L *C.lua_State) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	maxCount := 1024
	switch C.lua_type(L, 2) {
	case C.LUA_TNONE, C.LUA_TNIL:
	default:
		if C.lua_isnumber(L, 2) == 0 {
			return shdictRaise(L, `bad "max_count" argument`)
		}
		maxCount = int(C.lua_tonumber(L, 2))
	}

	keys := dict.GetKeys(maxCount)
	C.lua_createtable(L, C.int(len(keys)), 0)
	for i, k := range keys {
		pushGoString(L, k)
		C.lua_rawseti(L, -2, C.int(i+1))
	}
	return 1
}

//export golapis_shdict_flush_all
func golapis_shdict_flush_all(L *C.lua_State) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}
	dict.FlushAll()
	return 0
}

//export golapis_shdict_flush_expired
func golapis_shdict_flush_expired(L *C.lua_State) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	attempts := 0
	switch C.lua_type(L, 2) {
	case C.LUA_TNONE, C.LUA_TNIL:
	default:
		if C.lua_isnumber(L, 2) == 0 {
			return shdictRaise(L, `bad "max_count" argument`)
		}
		attempts = int(C.lua_tonumber(L, 2))
	}

	C.lua_pushnumber(L, C.lua_Number(dict.FlushExpired(attempts)))
	return 1
}

//export golapis_shdict_capacity
func golapis_shdict_capacity(L *C.lua_State) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}
	C.lua_pushnumber(L, C.lua_Number(dict.Capacity()))
	return 1
}

//export golapis_shdict_free_space
func golapis_shdict_free_space(L *C.lua_State) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}
	C.lua_pushnumber(L, C.lua_Number(dict.FreeSpace()))
	return 1
}

// shdictPushHelper implements lpush (left=true) and rpush (left=false).
func shdictPushHelper(L *C.lua_State, left bool) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	key, errStr := shdictKey(L, 2)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	var node shdictListNode
	switch C.lua_type(L, 3) {
	case C.LUA_TSTRING:
		node.str = string(luaStringBytes(L, 3))
	case C.LUA_TNUMBER:
		node.isNumber = true
		node.num = float64(C.lua_tonumber(L, 3))
	default:
		C.lua_pushnil(L)
		pushGoString(L, "bad value type")
		return 2
	}

	newLen, errStr, headFail := dict.Push(key, left, node)
	if errStr != "" {
		// list-head allocation failure returns false, node failures nil
		if headFail {
			C.lua_pushboolean(L, 0)
		} else {
			C.lua_pushnil(L)
		}
		pushGoString(L, errStr)
		return 2
	}

	C.lua_pushnumber(L, C.lua_Number(newLen))
	return 1
}

//export golapis_shdict_lpush
func golapis_shdict_lpush(L *C.lua_State) C.int {
	return shdictPushHelper(L, true)
}

//export golapis_shdict_rpush
func golapis_shdict_rpush(L *C.lua_State) C.int {
	return shdictPushHelper(L, false)
}

// shdictPopHelper implements lpop (left=true) and rpop (left=false).
func shdictPopHelper(L *C.lua_State, left bool) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	key, errStr := shdictKey(L, 2)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	node, found, errStr := dict.Pop(key, left)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}
	if !found {
		C.lua_pushnil(L)
		return 1
	}

	if node.isNumber {
		C.lua_pushnumber(L, C.lua_Number(node.num))
	} else {
		pushGoString(L, node.str)
	}
	return 1
}

//export golapis_shdict_lpop
func golapis_shdict_lpop(L *C.lua_State) C.int {
	return shdictPopHelper(L, true)
}

//export golapis_shdict_rpop
func golapis_shdict_rpop(L *C.lua_State) C.int {
	return shdictPopHelper(L, false)
}

//export golapis_shdict_llen
func golapis_shdict_llen(L *C.lua_State) C.int {
	dict := shdictZone(L)
	if dict == nil {
		return shdictRaise(L, `bad "zone" argument`)
	}

	key, errStr := shdictKey(L, 2)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	n, errStr := dict.Llen(key)
	if errStr != "" {
		C.lua_pushnil(L)
		pushGoString(L, errStr)
		return 2
	}

	C.lua_pushnumber(L, C.lua_Number(n))
	return 1
}
