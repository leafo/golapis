package golapis

// Old implementations preserved as baselines for benchmarking.
// These use individual CGO calls (the approach before batching).

// pushCaptureResponseIndividual is the old CaptureResponse.PushToLua implementation.
func pushCaptureResponseIndividual(L cLuaState, cr CaptureResponse) {
	luaNewTable(L)

	luaPushInteger(L, cr.Status)
	luaSetFieldCString(L, "status")

	pushGoString(L, cr.Body)
	luaSetFieldCString(L, "body")

	luaNewTable(L)
	for key, values := range cr.Headers {
		if len(values) > 0 {
			pushGoString(L, key)
			pushGoString(L, values[0])
			luaSetTable(L)
		}
	}
	luaSetFieldCString(L, "header")
}

// pushQueryArgsIndividual is the old pushQueryArgsToLuaTable implementation.
func pushQueryArgsIndividual(L cLuaState, args []queryArg) {
	luaNewTable(L)

	argBuckets := make(map[string][]queryArg)
	for _, arg := range args {
		if arg.key == "" {
			continue
		}
		argBuckets[arg.key] = append(argBuckets[arg.key], arg)
	}

	for key, keyArgs := range argBuckets {
		if len(keyArgs) == 1 {
			pushGoString(L, key)
			if keyArgs[0].isBoolean {
				luaPushBoolean(L, true)
			} else {
				pushGoString(L, keyArgs[0].value)
			}
			luaSetTable(L)
		} else {
			pushGoString(L, key)
			luaNewTable(L)
			for i, arg := range keyArgs {
				luaPushInteger(L, i+1)
				if arg.isBoolean {
					luaPushBoolean(L, true)
				} else {
					pushGoString(L, arg.value)
				}
				luaSetTable(L)
			}
			luaSetTable(L)
		}
	}
}
