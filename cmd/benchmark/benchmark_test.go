package main

import (
	"testing"
)

// =============================================================================
// SECTION 1: PURE CGO OVERHEAD
// =============================================================================

// BenchmarkCGO_Baseline measures the cost of a single CGO call
func BenchmarkCGO_Baseline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		CBaselineAdd(1, 2)
	}
}

// BenchmarkCGO_StringArg measures CGO with string argument (CString allocation)
func BenchmarkCGO_StringArg(b *testing.B) {
	s := "hello world"
	for i := 0; i < b.N; i++ {
		cs := CString(s)
		CStringLength(cs)
		FreeCString(cs)
	}
}

// BenchmarkCGO_StringArgPrealloc measures CGO with pre-allocated C string
func BenchmarkCGO_StringArgPrealloc(b *testing.B) {
	s := "hello world"
	cs := CString(s)
	defer FreeCString(cs)
	for i := 0; i < b.N; i++ {
		CStringLength(cs)
	}
}

// =============================================================================
// SECTION 2: LUA C API FROM GO
// =============================================================================

// BenchmarkLuaAPI_CreateClose measures Lua state creation/destruction
func BenchmarkLuaAPI_CreateClose(b *testing.B) {
	for i := 0; i < b.N; i++ {
		L := NewState()
		L.Close()
	}
}

// BenchmarkLuaAPI_PushPopInteger measures stack push/pop for integers
func BenchmarkLuaAPI_PushPopInteger(b *testing.B) {
	L := NewState()
	defer L.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.PushInteger(i)
		L.Pop(1)
	}
}

// BenchmarkLuaAPI_PushPopString measures stack push/pop for strings
func BenchmarkLuaAPI_PushPopString(b *testing.B) {
	L := NewState()
	defer L.Close()
	s := CString("hello world")
	defer FreeCString(s)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.PushStringC(s)
		L.Pop(1)
	}
}

// BenchmarkLuaAPI_GetSetGlobal measures global variable access
func BenchmarkLuaAPI_GetSetGlobal(b *testing.B) {
	L := NewState()
	defer L.Close()

	name := CString("counter")
	defer FreeCString(name)

	// Initialize global
	L.PushInteger(0)
	L.SetGlobalC(name)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.GetGlobalC(name)
		val := L.ToInteger(-1)
		L.Pop(1)
		L.PushInteger(val + 1)
		L.SetGlobalC(name)
	}
}

// BenchmarkLuaAPI_CallCFunction measures calling a C function registered in Lua
func BenchmarkLuaAPI_CallCFunction(b *testing.B) {
	L := NewState()
	defer L.Close()
	L.RegisterCFunctions()

	name := CString("c_add")
	defer FreeCString(name)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.GetGlobalC(name)
		L.PushInteger(1)
		L.PushInteger(2)
		L.PCall(2, 1)
		L.Pop(1)
	}
}

// BenchmarkLuaAPI_CallNoopCFunction measures pure function call overhead
func BenchmarkLuaAPI_CallNoopCFunction(b *testing.B) {
	L := NewState()
	defer L.Close()
	L.RegisterCFunctions()

	name := CString("c_noop")
	defer FreeCString(name)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.GetGlobalC(name)
		L.PCall(0, 0)
	}
}

// BenchmarkLuaAPI_DoString measures luaL_dostring overhead (includes parsing)
func BenchmarkLuaAPI_DoString(b *testing.B) {
	L := NewState()
	defer L.Close()

	code := CString("local x = 1 + 2")
	defer FreeCString(code)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.DoStringC(code)
	}
}

// BenchmarkLuaAPI_CallPrecompiled measures calling precompiled Lua chunk
func BenchmarkLuaAPI_CallPrecompiled(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.LoadChunk("local x = 1 + 2", "test_chunk")
	name := CString("test_chunk")
	defer FreeCString(name)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkLuaAPI_CreateTable measures table creation
func BenchmarkLuaAPI_CreateTable(b *testing.B) {
	L := NewState()
	defer L.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.NewTable()
		L.Pop(1)
	}
}

// BenchmarkLuaAPI_TableInsert measures table insert operations
func BenchmarkLuaAPI_TableInsert(b *testing.B) {
	L := NewState()
	defer L.Close()
	L.NewTable()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.SetTableInt(-1, i%1000, i)
	}
}

// =============================================================================
// SECTION 3: FFI VS C API COMPARISON
// Uses precompiled chunks to avoid JIT compilation overhead during benchmark
// =============================================================================

// BenchmarkFFI_VsCAPI_100Calls_CAPI measures 100 C API calls from Lua
func BenchmarkFFI_VsCAPI_100Calls_CAPI(b *testing.B) {
	L := NewState()
	defer L.Close()
	L.RegisterCFunctions()

	L.LoadChunk(`
		local sum = 0
		for i = 1, 100 do
			sum = c_add(sum, i)
		end
		return sum
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup to trigger JIT
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkFFI_VsCAPI_100Calls_PureLua measures 100 iterations in pure Lua (JIT)
func BenchmarkFFI_VsCAPI_100Calls_PureLua(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.LoadChunk(`
		local sum = 0
		for i = 1, 100 do
			sum = sum + i  -- Pure Lua, JIT-compiled
		end
		return sum
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup to trigger JIT
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkFFI_VsCAPI_10000Calls_CAPI measures 10000 C API calls from Lua
func BenchmarkFFI_VsCAPI_10000Calls_CAPI(b *testing.B) {
	L := NewState()
	defer L.Close()
	L.RegisterCFunctions()

	L.LoadChunk(`
		local sum = 0
		for i = 1, 10000 do
			sum = c_add(sum, i)
		end
		return sum
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 10; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkFFI_VsCAPI_10000Calls_PureLua measures 10000 iterations in pure Lua (JIT)
func BenchmarkFFI_VsCAPI_10000Calls_PureLua(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.LoadChunk(`
		local sum = 0
		for i = 1, 10000 do
			sum = sum + i
		end
		return sum
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 10; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkFFI_NoopCall_CAPI measures noop C API calls
func BenchmarkFFI_NoopCall_CAPI(b *testing.B) {
	L := NewState()
	defer L.Close()
	L.RegisterCFunctions()

	L.LoadChunk(`
		for i = 1, 1000 do
			c_noop()
		end
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkFFI_NoopCall_PureLua measures equivalent pure Lua calls
func BenchmarkFFI_NoopCall_PureLua(b *testing.B) {
	L := NewState()
	defer L.Close()

	// Define and preload
	L.DoString(`local function lua_noop() end _G.lua_noop = lua_noop`)
	L.LoadChunk(`
		for i = 1, 1000 do
			lua_noop()
		end
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// =============================================================================
// SECTION 4: BATCHED VS INDIVIDUAL CGO CROSSINGS
// =============================================================================

// BenchmarkBatching_Individual100 measures 100 individual CGO crossings
func BenchmarkBatching_Individual100(b *testing.B) {
	L := NewState()
	defer L.Close()

	name := CString("counter")
	defer FreeCString(name)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 100 CGO crossings - this is the "bad" pattern
		for j := 0; j < 100; j++ {
			L.PushInteger(j)
			L.SetGlobalC(name)
		}
	}
}

// BenchmarkBatching_SingleLuaLoop100 measures single CGO call with Lua loop
func BenchmarkBatching_SingleLuaLoop100(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.LoadChunk(`
		for i = 0, 99 do
			counter = i
		end
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkBatching_Individual1000 measures 1000 individual CGO crossings
func BenchmarkBatching_Individual1000(b *testing.B) {
	L := NewState()
	defer L.Close()

	name := CString("counter")
	defer FreeCString(name)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 1000; j++ {
			L.PushInteger(j)
			L.SetGlobalC(name)
		}
	}
}

// BenchmarkBatching_SingleLuaLoop1000 measures single CGO call with Lua loop
func BenchmarkBatching_SingleLuaLoop1000(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.LoadChunk(`
		for i = 0, 999 do
			counter = i
		end
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkBatching_PureCLoop measures pure C loop (no CGO inside loop)
func BenchmarkBatching_PureCLoop(b *testing.B) {
	for i := 0; i < b.N; i++ {
		CSumLoop(1000)
	}
}

// =============================================================================
// SECTION 5: CALLBACK OVERHEAD (Lua -> C -> Go pattern simulation)
// =============================================================================

// BenchmarkCallback_PureLuaFunc measures pure Lua function calls
func BenchmarkCallback_PureLuaFunc(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.DoString(`
		local counter = 0
		function increment(val)
			counter = counter + 1
			return val + 1
		end
	`)
	L.LoadChunk(`
		local sum = 0
		for i = 1, 1000 do
			sum = increment(sum)
		end
		return sum
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkCallback_CAPIFunc measures C API function calls from Lua
func BenchmarkCallback_CAPIFunc(b *testing.B) {
	L := NewState()
	defer L.Close()
	L.RegisterCFunctions()

	L.LoadChunk(`
		local sum = 0
		for i = 1, 1000 do
			sum = c_increment(sum)
		end
		return sum
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// =============================================================================
// SECTION 6: STRING HANDLING PATTERNS
// =============================================================================

// BenchmarkStrings_GoToLua measures passing strings from Go to Lua
func BenchmarkStrings_GoToLua(b *testing.B) {
	L := NewState()
	defer L.Close()

	name := CString("teststr")
	defer FreeCString(name)

	testString := "Hello, this is a test string for benchmarking!"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cs := CString(testString)
		L.PushStringC(cs)
		L.SetGlobalC(name)
		FreeCString(cs)
	}
}

// BenchmarkStrings_GoToLuaPrealloc measures passing pre-allocated strings
func BenchmarkStrings_GoToLuaPrealloc(b *testing.B) {
	L := NewState()
	defer L.Close()

	name := CString("teststr")
	defer FreeCString(name)

	testString := "Hello, this is a test string for benchmarking!"
	cs := CString(testString)
	defer FreeCString(cs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.PushStringC(cs)
		L.SetGlobalC(name)
	}
}

// BenchmarkStrings_LuaToGo measures getting strings from Lua to Go
func BenchmarkStrings_LuaToGo(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.DoString(`teststr = "Hello, this is a test string for benchmarking!"`)

	name := CString("teststr")
	defer FreeCString(name)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.GetGlobalC(name)
		s := L.ToStringC(-1)
		_ = GoString(s) // Convert to Go string
		L.Pop(1)
	}
}

// BenchmarkStrings_LuaStringProcessing measures string processing in Lua
func BenchmarkStrings_LuaStringProcessing(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.LoadChunk(`
		local s = "Hello, World!"
		local result = ""
		for i = 1, 100 do
			result = s:upper():lower():gsub("hello", "hi")
		end
		return result
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// =============================================================================
// SECTION 7: TABLE/DATA STRUCTURE OPERATIONS
// =============================================================================

// BenchmarkTables_CreatePopulate100 measures creating and populating tables
func BenchmarkTables_CreatePopulate100(b *testing.B) {
	L := NewState()
	defer L.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.NewTable()
		for j := 0; j < 100; j++ {
			L.SetTableInt(-1, j, j*2)
		}
		L.Pop(1)
	}
}

// BenchmarkTables_CreatePopulate100_Lua measures same in pure Lua
func BenchmarkTables_CreatePopulate100_Lua(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.LoadChunk(`
		local t = {}
		for i = 0, 99 do
			t[i] = i * 2
		end
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkTables_DeepNesting measures accessing deeply nested tables
func BenchmarkTables_DeepNesting(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.DoString(`deep = {a = {b = {c = {d = {e = {value = 42}}}}}}`)
	L.LoadChunk(`
		local val = 0
		for i = 1, 1000 do
			val = deep.a.b.c.d.e.value
		end
		return val
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// =============================================================================
// SECTION 8: REAL-WORLD PATTERNS
// =============================================================================

// BenchmarkRealWorld_SingleRequestPattern simulates a single HTTP request handler
func BenchmarkRealWorld_SingleRequestPattern(b *testing.B) {
	L := NewState()
	defer L.Close()
	L.RegisterCFunctions()

	L.DoString(`
		request = {
			uri = "/api/users?id=123&name=test",
			method = "GET",
			headers = {
				["Content-Type"] = "application/json",
				["Accept"] = "*/*",
			}
		}
	`)
	L.LoadChunk(`
		-- Parse request
		local method = request.method
		local uri = request.uri
		local content_type = request.headers["Content-Type"]

		-- Simple routing
		local response = ""
		if method == "GET" and uri:find("/api/users") then
			response = '{"status": "ok", "data": []}'
		else
			response = '{"error": "not found"}'
		end

		return response
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkRealWorld_JSONLikeProcessing simulates JSON-like data processing
func BenchmarkRealWorld_JSONLikeProcessing(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.LoadChunk(`
		-- Build a response object
		local response = {
			status = 200,
			headers = {
				["Content-Type"] = "application/json",
				["X-Request-ID"] = "abc-123",
			},
			body = {
				success = true,
				data = {
					users = {},
				}
			}
		}

		-- Populate users
		for i = 1, 10 do
			response.body.data.users[i] = {
				id = i,
				name = "User " .. i,
				email = "user" .. i .. "@example.com",
			}
		end

		return response.status
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// BenchmarkRealWorld_PrecompiledChunk measures pre-compiled Lua chunk execution
func BenchmarkRealWorld_PrecompiledChunk(b *testing.B) {
	L := NewState()
	defer L.Close()

	L.LoadChunk(`
		local sum = 0
		for i = 1, 100 do
			sum = sum + i
		end
		return sum
	`, "bench")
	name := CString("bench")
	defer FreeCString(name)

	// Warmup
	for i := 0; i < 100; i++ {
		L.CallChunkC(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		L.CallChunkC(name)
	}
}

// Note: BenchmarkRealWorld_DoStringVsPrecompiled_DoString was removed because
// calling luaL_dostring() in a tight loop (100k+ iterations) causes LuaJIT
// SIGSEGV due to JIT code accumulation. Use BenchmarkLuaAPI_DoString instead
// which measures the same thing but with simpler code that completes faster.
