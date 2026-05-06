package golapis

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func captureStandardLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldPrefix := log.Prefix()

	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")

	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})

	return &buf
}

func TestLogConstants(t *testing.T) {
	code := `
		golapis.say(golapis.STDERR)
		golapis.say(golapis.EMERG)
		golapis.say(golapis.ALERT)
		golapis.say(golapis.CRIT)
		golapis.say(golapis.ERR)
		golapis.say(golapis.WARN)
		golapis.say(golapis.NOTICE)
		golapis.say(golapis.INFO)
		golapis.say(golapis.DEBUG)
	`
	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	got := strings.Fields(output)
	want := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8"}
	if len(got) != len(want) {
		t.Fatalf("got %d constants, want %d: %q", len(got), len(want), output)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("constant %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestLogFormatsValues(t *testing.T) {
	logs := captureStandardLog(t)

	_, err := runLuaAndCapture(t, `
		golapis.log(golapis.ERR, "failed: ", nil, " ", true, " ", false, " ", golapis.null)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	got := logs.String()
	if !strings.Contains(got, "[lua] [ERR] failed: nil true false null") {
		t.Fatalf("unexpected log output: %q", got)
	}
}

func TestLogNgxAlias(t *testing.T) {
	logs := captureStandardLog(t)

	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()
	gls.SetupNgxAlias()
	gls.Start()
	defer gls.Stop()

	err := gls.RunString(`ngx.log(ngx.WARN, "via ngx")`)
	gls.Wait()
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	if !strings.Contains(logs.String(), "[lua] [WARN] via ngx") {
		t.Fatalf("unexpected log output: %q", logs.String())
	}
}

func TestLogInvalidLevels(t *testing.T) {
	tests := []string{
		`golapis.log(-1, "bad")`,
		`golapis.log(9, "bad")`,
		`golapis.log()`,
		`golapis.log({}, "bad")`,
	}

	for _, code := range tests {
		_, err := runLuaAndCapture(t, code)
		if err == nil {
			t.Fatalf("expected error for %s", code)
		}
	}
}

func TestLogArgumentTypes(t *testing.T) {
	logs := captureStandardLog(t)

	_, err := runLuaAndCapture(t, `
		local t = setmetatable({}, {
			__tostring = function()
				return "table string"
			end
		})
		golapis.log(golapis.INFO, "value=", t)
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(logs.String(), "[lua] [INFO] value=table string") {
		t.Fatalf("unexpected log output: %q", logs.String())
	}

	_, err = runLuaAndCapture(t, `golapis.log(golapis.INFO, {})`)
	if err == nil {
		t.Fatal("expected table without __tostring to error")
	}

	_, err = runLuaAndCapture(t, `golapis.log(golapis.INFO, function() end)`)
	if err == nil {
		t.Fatal("expected function argument to error")
	}
}

func TestLogInHTTPCoroutineAndTimerContexts(t *testing.T) {
	logs := captureStandardLog(t)

	w, _, err := runLuaWithHTTP(t, `
		golapis.log(golapis.NOTICE, "http")
		local co = coroutine.create(function()
			golapis.log(golapis.INFO, "coroutine")
		end)
		assert(coroutine.resume(co))
		golapis.timer.at(0, function()
			golapis.log(golapis.DEBUG, "timer")
		end)
		golapis.say("done")
	`)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if body := w.Body.String(); body != "done\n" {
		t.Fatalf("unexpected response body: %q", body)
	}

	got := logs.String()
	for _, want := range []string{
		"[lua] [NOTICE] http",
		"[lua] [INFO] coroutine",
		"[lua] [DEBUG] timer",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in log output: %q", want, got)
		}
	}
}
