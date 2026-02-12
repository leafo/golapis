//go:build (darwin || linux || windows) && (amd64 || arm64)

package golapis

/*
#include "lua_helpers.h"
*/
import "C"
import (
	"encoding/binary"
	"math"
	"sync"
	"unsafe"
)

const luaBatchPtrSize = unsafe.Sizeof(uintptr(0))

// Compile-time guard: batch encoding stores string pointers as u64, so uintptr must be 8 bytes.
var _ [8 - luaBatchPtrSize]byte
var _ [luaBatchPtrSize - 8]byte

// Type alias so test files can reference the Lua state type without importing C.
type cLuaState = *C.lua_State

// Thin wrappers for individual CGO calls, used by benchmarks to compare
// old (per-call) approach against the new batched approach.

func luaPop(L *C.lua_State, n int) {
	C.lua_pop_wrapper(L, C.int(n))
}

func luaNewTable(L *C.lua_State) {
	C.lua_newtable_wrapper(L)
}

func luaPushInteger(L *C.lua_State, v int) {
	C.lua_pushinteger(L, C.lua_Integer(v))
}

func luaPushBoolean(L *C.lua_State, v bool) {
	if v {
		C.lua_pushboolean(L, 1)
	} else {
		C.lua_pushboolean(L, 0)
	}
}

func luaSetTable(L *C.lua_State) {
	C.lua_settable(L, -3)
}

func luaSetFieldCString(L *C.lua_State, name string) {
	cname := C.CString(name)
	C.lua_setfield(L, -2, cname)
	C.free(unsafe.Pointer(cname))
}

// Opcode constants matching lua_helpers.h
const (
	batchOpNil    = 0x01
	batchOpTrue   = 0x02
	batchOpFalse  = 0x03
	batchOpInt    = 0x04
	batchOpNum    = 0x05
	batchOpStr    = 0x06
	batchOpStrI   = 0x07
	batchOpTable  = 0x08
	batchOpTableA = 0x09
	batchOpSet    = 0x0A
	batchOpSetF   = 0x0B
	batchOpSetFI  = 0x0C
	batchOpSetI   = 0x0D
	batchOpPop    = 0x0E
)

// LuaBatch encodes a sequence of Lua stack operations into a byte buffer
// that can be executed with a single CGO call via lua_batch_push in lua_helpers.h.
//
// Building Lua tables from Go normally requires many individual CGO calls —
// each costing ~100-200ns of overhead (goroutine→C transition, stack switching).
// For example, pushing an HTTP response with 15 headers needs ~46 CGO crossings.
// LuaBatch encodes all the operations into a flat byte buffer, then executes them
// with a single CGO call where a C-side interpreter loop runs the Lua stack ops.
//
// String data uses embedded pointers: the Go string's data pointer is written
// directly into the byte buffer as raw bytes. This avoids needing a separate
// pointer array passed to C (which would trigger CGO's pointer-checking rules
// and require runtime.Pinner). The strings slice keeps the Go strings alive
// for the GC; the pointers in buf are invisible to the GC since they're stored
// as opaque bytes, not pointer-typed values. This is safe because Push() is
// synchronous and Go's GC is non-moving.
type LuaBatch struct {
	buf     []byte   // instruction buffer (contains opcodes, inline data, and embedded pointers)
	strings []string // GC anchor: keeps referenced Go strings alive during Push()
}

// NewLuaBatch creates a new LuaBatch with pre-allocated buffers.
func NewLuaBatch() *LuaBatch {
	return &LuaBatch{
		buf:     make([]byte, 0, 256),
		strings: make([]string, 0, 16),
	}
}

var batchPool = sync.Pool{
	New: func() interface{} { return NewLuaBatch() },
}

// AcquireBatch gets a LuaBatch from the pool, reset and ready to use.
func AcquireBatch() *LuaBatch {
	b := batchPool.Get().(*LuaBatch)
	b.Reset()
	return b
}

// ReleaseBatch returns a LuaBatch to the pool for reuse.
func ReleaseBatch(b *LuaBatch) {
	b.Reset()
	batchPool.Put(b)
}

// Reset clears the batch for reuse without freeing the underlying buffers.
func (b *LuaBatch) Reset() {
	for i := range b.strings {
		b.strings[i] = ""
	}
	b.buf = b.buf[:0]
	b.strings = b.strings[:0]
}

func (b *LuaBatch) appendU32(v uint32) {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], v)
	b.buf = append(b.buf, tmp[:]...)
}

func (b *LuaBatch) appendU64(v uint64) {
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], v)
	b.buf = append(b.buf, tmp[:]...)
}

func (b *LuaBatch) appendI64(v int64) {
	b.appendU64(uint64(v))
}

func (b *LuaBatch) appendF64(v float64) {
	b.appendU64(math.Float64bits(v))
}

// appendStringPtr embeds a Go string's data pointer and length directly in the buffer,
// and keeps the string alive in b.strings so the GC won't collect its backing data.
func (b *LuaBatch) appendStringPtr(s string) {
	b.strings = append(b.strings, s) // GC anchor
	if len(s) == 0 {
		b.appendU64(0) // nil pointer
	} else {
		b.appendU64(uint64(uintptr(unsafe.Pointer(unsafe.StringData(s)))))
	}
	b.appendU32(uint32(len(s)))
}

// Nil pushes nil onto the Lua stack.
func (b *LuaBatch) Nil() *LuaBatch {
	b.buf = append(b.buf, batchOpNil)
	return b
}

// Bool pushes a boolean onto the Lua stack.
func (b *LuaBatch) Bool(v bool) *LuaBatch {
	if v {
		b.buf = append(b.buf, batchOpTrue)
	} else {
		b.buf = append(b.buf, batchOpFalse)
	}
	return b
}

// Int pushes an integer onto the Lua stack.
func (b *LuaBatch) Int(v int) *LuaBatch {
	b.buf = append(b.buf, batchOpInt)
	b.appendI64(int64(v))
	return b
}

// Int64 pushes an int64 onto the Lua stack.
func (b *LuaBatch) Int64(v int64) *LuaBatch {
	b.buf = append(b.buf, batchOpInt)
	b.appendI64(v)
	return b
}

// Number pushes a float64 onto the Lua stack.
func (b *LuaBatch) Number(v float64) *LuaBatch {
	b.buf = append(b.buf, batchOpNum)
	b.appendF64(v)
	return b
}

// String pushes a Go string onto the Lua stack via an embedded pointer (zero-copy on Go side).
func (b *LuaBatch) String(s string) *LuaBatch {
	b.buf = append(b.buf, batchOpStr)
	b.appendStringPtr(s)
	return b
}

// InlineString pushes a string with bytes embedded directly in the instruction buffer.
// Good for short constant strings like field names.
func (b *LuaBatch) InlineString(s string) *LuaBatch {
	b.buf = append(b.buf, batchOpStrI)
	b.appendU32(uint32(len(s)))
	b.buf = append(b.buf, s...)
	return b
}

// Table pushes a new empty table onto the Lua stack.
func (b *LuaBatch) Table() *LuaBatch {
	b.buf = append(b.buf, batchOpTable)
	return b
}

// TableSized pushes a new table with array/hash size hints.
func (b *LuaBatch) TableSized(narr, nrec int) *LuaBatch {
	b.buf = append(b.buf, batchOpTableA)
	b.appendU32(uint32(narr))
	b.appendU32(uint32(nrec))
	return b
}

// Set pops key and value from the stack and sets them on the table at -3.
func (b *LuaBatch) Set() *LuaBatch {
	b.buf = append(b.buf, batchOpSet)
	return b
}

// SetField pops value from the stack and sets it as a named field on the table at -2.
// Uses an embedded pointer for the field name.
func (b *LuaBatch) SetField(name string) *LuaBatch {
	b.buf = append(b.buf, batchOpSetF)
	b.appendStringPtr(name)
	return b
}

// SetFieldInline pops value from the stack and sets it as a named field on the table at -2.
// Embeds the field name directly in the instruction buffer.
func (b *LuaBatch) SetFieldInline(name string) *LuaBatch {
	b.buf = append(b.buf, batchOpSetFI)
	b.appendU32(uint32(len(name)))
	b.buf = append(b.buf, name...)
	return b
}

// SetIndex pops value from the stack and sets it at the given integer index
// in the table at -2 (uses lua_rawseti).
func (b *LuaBatch) SetIndex(idx int) *LuaBatch {
	b.buf = append(b.buf, batchOpSetI)
	b.appendU32(uint32(idx))
	return b
}

// Pop pops n values from the Lua stack.
func (b *LuaBatch) Pop(n int) *LuaBatch {
	b.buf = append(b.buf, batchOpPop)
	b.buf = append(b.buf, byte(n))
	return b
}

// -- Convenience methods --

// StringField pushes a string value and sets it as a named field (inline field name).
func (b *LuaBatch) StringField(name, val string) *LuaBatch {
	return b.String(val).SetFieldInline(name)
}

// IntField pushes an integer value and sets it as a named field (inline field name).
func (b *LuaBatch) IntField(name string, val int) *LuaBatch {
	return b.Int(val).SetFieldInline(name)
}

// StringEntry pushes key and value strings, then calls Set (for table[key] = val).
func (b *LuaBatch) StringEntry(key, val string) *LuaBatch {
	return b.String(key).String(val).Set()
}

// Push executes all encoded instructions on the given Lua state with a single CGO call.
// String pointers embedded in the buffer remain valid because b.strings keeps them alive.
func (b *LuaBatch) Push(L *C.lua_State) {
	if len(b.buf) == 0 {
		return
	}
	ret := C.lua_batch_push(L,
		(*C.uchar)(unsafe.Pointer(&b.buf[0])), C.size_t(len(b.buf)))
	if ret != 0 {
		panic("golapis: lua_batch_push failed")
	}
}
