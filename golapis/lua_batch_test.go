package golapis

import (
	"strings"
	"testing"
)

func TestBatchSimpleTable(t *testing.T) {
	// Test that batch-built tables work via location.capture
	code := `
		if golapis.var.request_uri == "/inner" then
			golapis.header["X-Test"] = "batch-value"
			golapis.print("hello batch")
			return
		end
		local res = golapis.location.capture("/inner")
		golapis.say("status:" .. res.status)
		golapis.say("body:" .. res.body)
		golapis.say("header:" .. tostring(res.header["X-Test"]))
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "status:200") {
		t.Errorf("expected status:200, got: %q", body)
	}
	if !strings.Contains(body, "body:hello batch") {
		t.Errorf("expected body:hello batch, got: %q", body)
	}
	if !strings.Contains(body, "header:batch-value") {
		t.Errorf("expected header:batch-value, got: %q", body)
	}
}

func TestBatchEmptyHeaders(t *testing.T) {
	code := `
		if golapis.var.request_uri == "/inner" then
			golapis.print("no headers")
			return
		end
		local res = golapis.location.capture("/inner")
		golapis.say(type(res.header))
		golapis.say(res.body)
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "table\n") {
		t.Errorf("expected header to be table, got: %q", body)
	}
}

func TestBatchMultipleHeaders(t *testing.T) {
	code := `
		if golapis.var.request_uri == "/inner" then
			golapis.header["X-One"] = "1"
			golapis.header["X-Two"] = "2"
			golapis.header["X-Three"] = "3"
			golapis.print("ok")
			return
		end
		local res = golapis.location.capture("/inner")
		golapis.say(res.header["X-One"])
		golapis.say(res.header["X-Two"])
		golapis.say(res.header["X-Three"])
	`

	w, err := runLuaEntryPointHTTP(t, "/test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "1\n") || !strings.Contains(body, "2\n") || !strings.Contains(body, "3\n") {
		t.Errorf("expected all three headers, got: %q", body)
	}
}

func TestBatchQueryArgs(t *testing.T) {
	code := `
		local args = golapis.req.get_uri_args()
		golapis.say(args.foo)
		golapis.say(args.bar)
	`

	w, err := runLuaWithQueryString(t, "foo=hello&bar=world", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "hello\n") {
		t.Errorf("expected foo=hello, got: %q", body)
	}
	if !strings.Contains(body, "world\n") {
		t.Errorf("expected bar=world, got: %q", body)
	}
}

func TestBatchQueryArgsBooleanArgs(t *testing.T) {
	code := `
		local args = golapis.req.get_uri_args()
		golapis.say(type(args.flag))
		golapis.say(tostring(args.flag))
		golapis.say(args.name)
	`

	w, err := runLuaWithQueryString(t, "flag&name=test", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "boolean\n") {
		t.Errorf("expected boolean type for flag, got: %q", body)
	}
	if !strings.Contains(body, "true\n") {
		t.Errorf("expected true for flag, got: %q", body)
	}
	if !strings.Contains(body, "test\n") {
		t.Errorf("expected name=test, got: %q", body)
	}
}

func TestBatchQueryArgsMultiValue(t *testing.T) {
	code := `
		local args = golapis.req.get_uri_args()
		if type(args.x) == "table" then
			for i, v in ipairs(args.x) do
				golapis.say("x[" .. i .. "]=" .. v)
			end
		else
			golapis.say("x=" .. tostring(args.x))
		end
	`

	w, err := runLuaWithQueryString(t, "x=a&x=b&x=c", code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "x[1]=a") {
		t.Errorf("expected x[1]=a, got: %q", body)
	}
	if !strings.Contains(body, "x[2]=b") {
		t.Errorf("expected x[2]=b, got: %q", body)
	}
	if !strings.Contains(body, "x[3]=c") {
		t.Errorf("expected x[3]=c, got: %q", body)
	}
}

func TestBatchBuilderEncoding(t *testing.T) {
	// Test that the builder produces expected opcodes
	b := NewLuaBatch()

	b.Nil()
	if b.buf[0] != batchOpNil {
		t.Errorf("expected NIL opcode, got 0x%02x", b.buf[0])
	}

	b.Reset()
	b.Bool(true)
	if b.buf[0] != batchOpTrue {
		t.Errorf("expected TRUE opcode, got 0x%02x", b.buf[0])
	}

	b.Reset()
	b.Bool(false)
	if b.buf[0] != batchOpFalse {
		t.Errorf("expected FALSE opcode, got 0x%02x", b.buf[0])
	}

	b.Reset()
	b.Int(42)
	if b.buf[0] != batchOpInt {
		t.Errorf("expected INT opcode, got 0x%02x", b.buf[0])
	}
	if len(b.buf) != 9 { // 1 opcode + 8 bytes
		t.Errorf("expected 9 bytes for INT, got %d", len(b.buf))
	}

	b.Reset()
	b.Table()
	if b.buf[0] != batchOpTable {
		t.Errorf("expected TABLE opcode, got 0x%02x", b.buf[0])
	}

	b.Reset()
	b.InlineString("test")
	if b.buf[0] != batchOpStrI {
		t.Errorf("expected STRI opcode, got 0x%02x", b.buf[0])
	}
	// 1 opcode + 4 length + 4 bytes "test"
	if len(b.buf) != 9 {
		t.Errorf("expected 9 bytes for STRI 'test', got %d", len(b.buf))
	}
}

func TestBatchPoolReuse(t *testing.T) {
	b1 := AcquireBatch()
	b1.Int(1).Int(2).Int(3)
	origLen := len(b1.buf)
	ReleaseBatch(b1)

	b2 := AcquireBatch()
	if len(b2.buf) != 0 {
		t.Errorf("expected reset batch, got buf len %d", len(b2.buf))
	}
	if len(b2.strings) != 0 {
		t.Errorf("expected reset strings, got len %d", len(b2.strings))
	}
	_ = origLen
	ReleaseBatch(b2)
}

func TestBatchPushPanicsOnInterpreterError(t *testing.T) {
	gls := NewGolapisLuaState()
	if gls == nil {
		t.Fatal("Failed to create Lua state")
	}
	defer gls.Close()

	b := NewLuaBatch()
	b.buf = append(b.buf, 0xFF) // invalid opcode

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from lua_batch_push failure")
		}
	}()

	b.Push(gls.luaState)
}

