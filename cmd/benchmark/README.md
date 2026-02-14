# golapis benchmarks

Benchmark suite for measuring Go-C-LuaJIT calling conventions and comparing
batched vs individual CGO crossing patterns.

## Running

Run all benchmarks:

```bash
go test -bench=. -benchtime=1s -run=^$ ./cmd/benchmark
```

Or use the runner which prints section descriptions:

```bash
go run ./cmd/benchmark
```

Run a specific benchmark pattern:

```bash
go test -bench=CaptureResponse -benchtime=1s -run=^$ ./cmd/benchmark
```

## Benchmark sections

1. **Pure CGO overhead** -- baseline cost of Go-to-C boundary crossings
2. **Lua C API from Go** -- stack operations, table creation, function calls
3. **FFI vs C API** -- LuaJIT JIT-compiled Lua vs C API calls from Lua
4. **Batched vs individual CGO** -- single Lua loop vs many Go-to-C crossings
5. **Callback overhead** -- Lua-to-C function call patterns
6. **String handling** -- Go/Lua string passing and processing
7. **Table/data structure operations** -- table creation and population
8. **Real-world patterns** -- request handling and JSON-like processing
9. **Batch vs individual CGO (real tables)** -- `LuaBatch` compared against
   legacy individual-CGO push functions for HTTP capture responses and query
   arg tables
