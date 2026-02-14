package main

import (
	"golapis/golapis"
	"net/http"
	"testing"
	"unsafe"
)

// =============================================================================
// SECTION 9: BATCH VS INDIVIDUAL CGO — REAL TABLE STRUCTURES
// =============================================================================

func benchCaptureResponseData() (int, string, http.Header) {
	return 200, "hello world response body", http.Header{
		"Content-Type":   {"text/html"},
		"X-Request-Id":   {"abc123"},
		"Cache-Control":  {"no-cache"},
		"Content-Length": {"25"},
		"Server":         {"golapis"},
	}
}

func benchQueryArgsData() []benchQueryArg {
	return []benchQueryArg{
		{key: "page", value: "1"},
		{key: "sort", value: "created"},
		{key: "order", value: "desc"},
		{key: "limit", value: "20"},
		{key: "filter", value: "active"},
		{key: "q", value: "search term"},
	}
}

// BenchmarkCaptureResponse_Batch uses golapis.LuaBatch to push a capture response table.
func BenchmarkCaptureResponse_Batch(b *testing.B) {
	L := NewState()
	defer L.Close()

	status, body, headers := benchCaptureResponseData()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nHeaders := 0
		for _, values := range headers {
			if len(values) > 0 {
				nHeaders++
			}
		}

		batch := golapis.AcquireBatch()

		batch.Table()
		batch.Int(status).SetFieldInline("status")
		batch.String(body).SetFieldInline("body")

		batch.TableSized(0, nHeaders)
		for key, values := range headers {
			if len(values) > 0 {
				batch.StringEntry(key, values[0])
			}
		}
		batch.SetFieldInline("header")

		batch.PushUnsafe(unsafe.Pointer(L.L))
		golapis.ReleaseBatch(batch)
		L.Pop(1)
	}
}

// BenchmarkCaptureResponse_Individual uses legacy individual CGO calls to push
// the same capture response table.
func BenchmarkCaptureResponse_Individual(b *testing.B) {
	L := NewState()
	defer L.Close()

	status, body, headers := benchCaptureResponseData()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pushCaptureResponseLegacy(L, status, body, headers)
		L.Pop(1)
	}
}

// BenchmarkQueryArgs_Batch uses golapis.LuaBatch to push a query args table.
func BenchmarkQueryArgs_Batch(b *testing.B) {
	L := NewState()
	defer L.Close()

	args := benchQueryArgsData()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Bucket args inside the measured loop to capture end-to-end cost.
		argBuckets := make(map[string][]benchQueryArg)
		for _, arg := range args {
			if arg.key == "" {
				continue
			}
			argBuckets[arg.key] = append(argBuckets[arg.key], arg)
		}

		batch := golapis.AcquireBatch()

		batch.TableSized(0, len(argBuckets))
		for key, keyArgs := range argBuckets {
			if len(keyArgs) == 1 {
				batch.String(key)
				if keyArgs[0].isBoolean {
					batch.Bool(true)
				} else {
					batch.String(keyArgs[0].value)
				}
				batch.Set()
			} else {
				batch.String(key)
				batch.TableSized(len(keyArgs), 0)
				for j, arg := range keyArgs {
					if arg.isBoolean {
						batch.Bool(true)
					} else {
						batch.String(arg.value)
					}
					batch.SetIndex(j + 1)
				}
				batch.Set()
			}
		}

		batch.PushUnsafe(unsafe.Pointer(L.L))
		golapis.ReleaseBatch(batch)
		L.Pop(1)
	}
}

// BenchmarkQueryArgs_Individual uses legacy individual CGO calls to push
// the same query args table.
func BenchmarkQueryArgs_Individual(b *testing.B) {
	L := NewState()
	defer L.Close()

	args := benchQueryArgsData()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pushQueryArgsLegacy(L, args)
		L.Pop(1)
	}
}
