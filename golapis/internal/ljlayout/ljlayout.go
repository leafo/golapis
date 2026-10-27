// Package ljlayout exposes field offsets of LuaJIT's lua_State that golapis
// reads directly. It lives in its own package so that lj_obj.h, which
// defines the full struct, never enters the main package's cgo preamble:
// once cgo sees lua_State as a complete struct with pointer fields, the
// runtime's pointer check on every C call taking a lua_State* gets
// measurably slower.
package ljlayout

/*
#cgo CFLAGS: -I../../../luajit/src
#include <stddef.h>
#include "lj_obj.h"

static size_t exdata_offset(void) { return offsetof(struct lua_State, exdata); }
static size_t exdata2_offset(void) { return offsetof(struct lua_State, exdata2); }
*/
import "C"

// Offsets in bytes from the start of a lua_State to its exdata slots.
var (
	Exdata  = uintptr(C.exdata_offset())
	Exdata2 = uintptr(C.exdata2_offset())
)
