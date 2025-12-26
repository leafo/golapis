package golapis

import (
	"strings"
	"testing"
)

// =============================================================================
// Basic coroutine.create and coroutine.resume
// =============================================================================

func TestCoroutineBasicCreateResume(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			return 42
		end)
		local ok, result = coroutine.resume(co)
		golapis.say("ok=" .. tostring(ok) .. " result=" .. tostring(result))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "ok=true result=42\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineEmptyFunction(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function() end)
		local ok, result = coroutine.resume(co)
		golapis.say("ok=" .. tostring(ok) .. " result=" .. tostring(result))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "ok=true result=nil\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineWithArguments(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function(a, b, c)
			return a + b + c
		end)
		local ok, result = coroutine.resume(co, 10, 20, 30)
		golapis.say("result=" .. tostring(result))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "result=60\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineMultipleReturnValues(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			return 1, 2, 3
		end)
		local ok, a, b, c = coroutine.resume(co)
		golapis.say("ok=" .. tostring(ok) .. " a=" .. tostring(a) .. " b=" .. tostring(b) .. " c=" .. tostring(c))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "ok=true a=1 b=2 c=3\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineResumeWithNoArguments(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function(...)
			return select("#", ...)
		end)
		local ok, count = coroutine.resume(co)
		golapis.say("arg_count=" .. tostring(count))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "arg_count=0\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

// =============================================================================
// coroutine.yield
// =============================================================================

func TestCoroutineYieldBasic(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function(x)
			local y = coroutine.yield("yielded:" .. x)
			return "final:" .. y
		end)
		local ok1, v1 = coroutine.resume(co, "arg1")
		golapis.say("resume1: " .. tostring(ok1) .. " " .. tostring(v1))
		local ok2, v2 = coroutine.resume(co, "arg2")
		golapis.say("resume2: " .. tostring(ok2) .. " " .. tostring(v2))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "resume1: true yielded:arg1\nresume2: true final:arg2\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineYieldMultipleValues(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			coroutine.yield(1, 2, 3)
			return 4, 5
		end)
		local ok1, a, b, c = coroutine.resume(co)
		golapis.say("yield: " .. a .. "," .. b .. "," .. c)
		local ok2, d, e = coroutine.resume(co)
		golapis.say("return: " .. d .. "," .. e)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "yield: 1,2,3\nreturn: 4,5\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineMultipleYields(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			coroutine.yield(1)
			coroutine.yield(2)
			coroutine.yield(3)
			return 4
		end)
		local results = {}
		for i = 1, 4 do
			local ok, val = coroutine.resume(co)
			table.insert(results, tostring(val))
		end
		golapis.say(table.concat(results, ","))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "1,2,3,4\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineYieldReceivesResumeArgs(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function(x)
			local y = coroutine.yield("first:" .. x)
			local z = coroutine.yield("second:" .. y)
			return "final:" .. z
		end)
		local ok1, v1 = coroutine.resume(co, "a")
		golapis.say(v1)
		local ok2, v2 = coroutine.resume(co, "b")
		golapis.say(v2)
		local ok3, v3 = coroutine.resume(co, "c")
		golapis.say(v3)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "first:a\nsecond:b\nfinal:c\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineYieldFromEntryCoroutineFails(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `
		coroutine.yield()
	`)
	if err == nil {
		t.Error("Expected error when yielding from entry coroutine")
	}
	if !strings.Contains(err.Error(), "yield") {
		t.Errorf("Error should mention yield: %v", err)
	}
}

// =============================================================================
// coroutine.status
// =============================================================================

func TestCoroutineStatusSuspended(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function() end)
		golapis.say("status=" .. coroutine.status(co))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "status=suspended\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineStatusRunning(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co
		co = coroutine.create(function()
			golapis.say("status=" .. coroutine.status(co))
		end)
		coroutine.resume(co)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "status=running\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineStatusDead(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function() end)
		coroutine.resume(co)
		golapis.say("status=" .. coroutine.status(co))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "status=dead\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineStatusNormal(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local outer, inner
		outer = coroutine.create(function()
			inner = coroutine.create(function()
				golapis.say("outer_status=" .. coroutine.status(outer))
			end)
			coroutine.resume(inner)
		end)
		coroutine.resume(outer)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "outer_status=normal\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineStatusAfterYield(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			coroutine.yield()
		end)
		coroutine.resume(co)
		golapis.say("status=" .. coroutine.status(co))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "status=suspended\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

// =============================================================================
// coroutine.running
// =============================================================================

func TestCoroutineRunningInMain(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co, is_main = coroutine.running()
		golapis.say("is_main=" .. tostring(is_main))
		golapis.say("has_co=" .. tostring(co ~= nil))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "is_main=true") {
		t.Errorf("Entry coroutine should be main: %q", body)
	}
}

func TestCoroutineRunningInUserCoroutine(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co
		co = coroutine.create(function()
			local running, is_main = coroutine.running()
			golapis.say("is_main=" .. tostring(is_main))
			golapis.say("is_same=" .. tostring(running == co))
		end)
		coroutine.resume(co)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "is_main=false") {
		t.Errorf("User coroutine should not be main: %q", body)
	}
	if !strings.Contains(body, "is_same=true") {
		t.Errorf("Running should return the current coroutine: %q", body)
	}
}

// =============================================================================
// coroutine.wrap
// =============================================================================

func TestCoroutineWrapBasic(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local wrapped = coroutine.wrap(function(x)
			coroutine.yield("yield:" .. x)
			return "return:" .. x
		end)
		golapis.say("call1:" .. wrapped("a"))
		golapis.say("call2:" .. wrapped("b"))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "call1:yield:a\ncall2:return:a\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineWrapThrowsError(t *testing.T) {
	_, _, err := runLuaWithHTTP(t, `
		local wrapped = coroutine.wrap(function()
			error("wrap error")
		end)
		wrapped()
	`)
	if err == nil {
		t.Error("Expected error from wrapped coroutine")
	}
	if !strings.Contains(err.Error(), "wrap error") {
		t.Errorf("Error should contain 'wrap error': %v", err)
	}
}

func TestCoroutineWrapMultipleReturnValues(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local wrapped = coroutine.wrap(function()
			coroutine.yield(1, 2, 3)
			return 4, 5, 6
		end)
		local a, b, c = wrapped()
		golapis.say("yield: " .. a .. "," .. b .. "," .. c)
		local d, e, f = wrapped()
		golapis.say("return: " .. d .. "," .. e .. "," .. f)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "yield: 1,2,3\nreturn: 4,5,6\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

// =============================================================================
// Error handling
// =============================================================================

func TestCoroutineErrorInCoroutine(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			error("test error")
		end)
		local ok, err = coroutine.resume(co)
		golapis.say("ok=" .. tostring(ok))
		golapis.say("has_error=" .. tostring(string.find(err, "test error") ~= nil))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "ok=false\nhas_error=true\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineResumeDeadCoroutine(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			return "done"
		end)
		coroutine.resume(co)
		local ok, err = coroutine.resume(co)
		golapis.say("ok=" .. tostring(ok))
		golapis.say("err_contains_dead=" .. tostring(string.find(err, "dead") ~= nil))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "ok=false\nerr_contains_dead=true\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineErrorInNestedCoroutine(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local outer = coroutine.create(function()
			local inner = coroutine.create(function()
				error("inner error")
			end)
			local ok, err = coroutine.resume(inner)
			golapis.say("inner_ok=" .. tostring(ok))
			golapis.say("inner_has_error=" .. tostring(string.find(err, "inner error") ~= nil))
			return "outer_done"
		end)
		local ok, result = coroutine.resume(outer)
		golapis.say("outer_ok=" .. tostring(ok))
		golapis.say("outer_result=" .. tostring(result))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "inner_ok=false\ninner_has_error=true\nouter_ok=true\nouter_result=outer_done\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

// =============================================================================
// Golapis context access from coroutines
// =============================================================================

func TestCoroutineAccessGolapisVar(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			golapis.say("method=" .. golapis.var.request_method)
		end)
		local ok, err = coroutine.resume(co)
		if not ok then
			golapis.say("error: " .. tostring(err))
		end
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "method=GET\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineAccessGolapisCtx(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		golapis.ctx.value = "test123"
		local co = coroutine.create(function()
			golapis.say("ctx.value=" .. tostring(golapis.ctx.value))
		end)
		local ok, err = coroutine.resume(co)
		if not ok then
			golapis.say("error: " .. tostring(err))
		end
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "ctx.value=test123\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineAccessGolapisHeader(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			golapis.header["X-From-Coroutine"] = "yes"
		end)
		coroutine.resume(co)
		golapis.say("done")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if got := w.Header().Get("X-From-Coroutine"); got != "yes" {
		t.Errorf("Header X-From-Coroutine = %q, want %q", got, "yes")
	}
}

// =============================================================================
// Async operations in coroutines
// =============================================================================

func TestCoroutineSleepBasic(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			golapis.say("before")
			golapis.sleep(0.01)
			golapis.say("after")
			return "done"
		end)
		local ok, result = coroutine.resume(co)
		golapis.say("ok=" .. tostring(ok) .. " result=" .. tostring(result))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "before\nafter\nok=true result=done\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineMultipleSleepsInSequence(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			golapis.say("1")
			golapis.sleep(0.01)
			golapis.say("2")
			golapis.sleep(0.01)
			golapis.say("3")
			golapis.sleep(0.01)
			golapis.say("4")
			return "done"
		end)
		local ok, result = coroutine.resume(co)
		golapis.say("result=" .. tostring(result))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "1\n2\n3\n4\nresult=done\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineTimerFromCoroutine(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local timer_fired = false
		local co = coroutine.create(function()
			golapis.timer.at(0, function()
				timer_fired = true
			end)
		end)
		coroutine.resume(co)
		golapis.sleep(0.05)
		golapis.say("timer_fired=" .. tostring(timer_fired))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "timer_fired=true\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

// =============================================================================
// Nested coroutines
// =============================================================================

func TestCoroutineNestedBasic(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local outer = coroutine.create(function()
			golapis.say("outer:start")
			local inner = coroutine.create(function()
				golapis.say("inner:run")
				return "inner_done"
			end)
			local ok, result = coroutine.resume(inner)
			golapis.say("outer:inner_result=" .. tostring(result))
			return "outer_done"
		end)
		local ok, result = coroutine.resume(outer)
		golapis.say("main:outer_result=" .. tostring(result))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "outer:start\ninner:run\nouter:inner_result=inner_done\nmain:outer_result=outer_done\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineNestedWithAsync(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local outer = coroutine.create(function()
			golapis.say("outer:start")
			local inner = coroutine.create(function()
				golapis.say("inner:before_sleep")
				golapis.sleep(0.01)
				golapis.say("inner:after_sleep")
				return "inner_done"
			end)
			local ok, result = coroutine.resume(inner)
			golapis.say("outer:inner_result=" .. tostring(result))
			golapis.sleep(0.01)
			golapis.say("outer:after_sleep")
			return "outer_done"
		end)
		local ok, result = coroutine.resume(outer)
		golapis.say("main:outer_result=" .. tostring(result))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "outer:start\ninner:before_sleep\ninner:after_sleep\nouter:inner_result=inner_done\nouter:after_sleep\nmain:outer_result=outer_done\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineDeeplyNested(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local function nest(depth, max)
			if depth >= max then
				golapis.say("reached depth " .. depth)
				golapis.sleep(0.01)
				return "done"
			end
			golapis.say("at depth " .. depth)
			local co = coroutine.create(function()
				return nest(depth + 1, max)
			end)
			local ok, result = coroutine.resume(co)
			if not ok then
				error(result)
			end
			return result
		end
		golapis.say("result=" .. nest(1, 3))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "at depth 1\nat depth 2\nreached depth 3\nresult=done\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

// =============================================================================
// Interleaved coroutines
// =============================================================================

func TestCoroutineInterleavedExecution(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co1 = coroutine.create(function()
			golapis.say("co1:a")
			coroutine.yield()
			golapis.say("co1:b")
			coroutine.yield()
			golapis.say("co1:c")
		end)
		local co2 = coroutine.create(function()
			golapis.say("co2:a")
			coroutine.yield()
			golapis.say("co2:b")
			coroutine.yield()
			golapis.say("co2:c")
		end)
		coroutine.resume(co1)
		coroutine.resume(co2)
		coroutine.resume(co1)
		coroutine.resume(co2)
		coroutine.resume(co1)
		coroutine.resume(co2)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "co1:a\nco2:a\nco1:b\nco2:b\nco1:c\nco2:c\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineResumeFromDifferentParent(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local child
		local parent1 = coroutine.create(function()
			child = coroutine.create(function()
				golapis.say("child:first")
				coroutine.yield()
				golapis.say("child:second")
			end)
			coroutine.resume(child)
			golapis.say("parent1:done")
		end)
		local parent2 = coroutine.create(function()
			coroutine.resume(child)
			golapis.say("parent2:done")
		end)
		coroutine.resume(parent1)
		coroutine.resume(parent2)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "child:first\nparent1:done\nchild:second\nparent2:done\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

// =============================================================================
// golapis.exit from coroutine
// =============================================================================

func TestCoroutineExitFromCoroutine(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local co = coroutine.create(function()
			golapis.say("before exit")
			golapis.exit(201)
			golapis.say("after exit - should not print")
		end)
		coroutine.resume(co)
		golapis.say("after resume - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "before exit\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
	// Note: status might be 200 if headers already sent
}

func TestCoroutineExitFromNestedCoroutine(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local outer = coroutine.create(function()
			local inner = coroutine.create(function()
				golapis.say("inner:before exit")
				golapis.exit(201)
				golapis.say("inner:after exit - should not print")
			end)
			coroutine.resume(inner)
			golapis.say("outer:after inner - should not print")
		end)
		coroutine.resume(outer)
		golapis.say("main:after outer - should not print")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "inner:before exit\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

// =============================================================================
// Real-world patterns
// =============================================================================

func TestCoroutineCaptureErrorsPattern(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local function capture_errors(fn, ...)
			local co = coroutine.create(fn)
			local success, err = coroutine.resume(co, ...)
			if not success then
				error(debug.traceback(co, err))
			end
		end

		capture_errors(function()
			golapis.say("capture ctx: " .. tostring(golapis.ctx))
			golapis.say("capture method: " .. golapis.var.request_method)
		end)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "capture ctx: table:") {
		t.Errorf("Body should contain ctx table: %q", body)
	}
	if !strings.Contains(body, "capture method: GET") {
		t.Errorf("Body should contain method GET: %q", body)
	}
}

func TestCoroutineProducerConsumerPattern(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local producer = coroutine.wrap(function()
			for i = 1, 3 do
				coroutine.yield(i * 10)
			end
		end)

		local results = {}
		for i = 1, 3 do
			table.insert(results, producer())
		end
		golapis.say(table.concat(results, ","))
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "10,20,30\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}

func TestCoroutineIteratorPattern(t *testing.T) {
	w, _, err := runLuaWithHTTP(t, `
		local function range(n)
			return coroutine.wrap(function()
				for i = 1, n do
					coroutine.yield(i)
				end
			end)
		end

		local sum = 0
		for v in range(5) do
			sum = sum + v
		end
		golapis.say("sum=" .. sum)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	expected := "sum=15\n"
	if got := w.Body.String(); got != expected {
		t.Errorf("Body = %q, want %q", got, expected)
	}
}
