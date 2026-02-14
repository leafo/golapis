package main

import "net/http"

// Legacy push functions use individual CGO calls for each Lua stack operation.
// These are the old implementations preserved as baselines for benchmarking
// against the new batched approach (golapis.LuaBatch).

// pushCaptureResponseLegacy pushes a CaptureResponse-like table using individual CGO calls.
func pushCaptureResponseLegacy(L *LuaState, status int, body string, headers http.Header) {
	L.NewTable()

	L.PushInteger(status)
	L.SetField("status")

	L.PushGoString(body)
	L.SetField("body")

	L.NewTable()
	for key, values := range headers {
		if len(values) > 0 {
			L.PushGoString(key)
			L.PushGoString(values[0])
			L.SetTable()
		}
	}
	L.SetField("header")
}

// benchQueryArg mirrors golapis's unexported queryArg type for benchmark use.
type benchQueryArg struct {
	key       string
	value     string
	isBoolean bool
}

// pushQueryArgsLegacy pushes query args as a Lua table using individual CGO calls.
func pushQueryArgsLegacy(L *LuaState, args []benchQueryArg) {
	L.NewTable()

	argBuckets := make(map[string][]benchQueryArg)
	for _, arg := range args {
		if arg.key == "" {
			continue
		}
		argBuckets[arg.key] = append(argBuckets[arg.key], arg)
	}

	for key, keyArgs := range argBuckets {
		if len(keyArgs) == 1 {
			L.PushGoString(key)
			if keyArgs[0].isBoolean {
				L.PushBoolean(true)
			} else {
				L.PushGoString(keyArgs[0].value)
			}
			L.SetTable()
		} else {
			L.PushGoString(key)
			L.NewTable()
			for i, arg := range keyArgs {
				L.PushInteger(i + 1)
				if arg.isBoolean {
					L.PushBoolean(true)
				} else {
					L.PushGoString(arg.value)
				}
				L.SetTable()
			}
			L.SetTable()
		}
	}
}
