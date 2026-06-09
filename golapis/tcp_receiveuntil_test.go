package golapis

import (
	"net"
	"strings"
	"testing"
	"time"
)

// startTCPWriteServer starts a server that writes each entry of writes (with
// delay between them) to every accepted connection, then either closes the
// connection or holds it open until cleanup.
func startTCPChunkedWriteServer(t *testing.T, writes []string, delay time.Duration, closeAfter bool) (*net.TCPAddr, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	done := make(chan struct{})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				for i, w := range writes {
					if i > 0 && delay > 0 {
						time.Sleep(delay)
					}
					c.Write([]byte(w))
				}
				if closeAfter {
					c.Close()
				} else {
					<-done
					c.Close()
				}
			}(conn)
		}
	}()

	cleanup := func() {
		close(done)
		listener.Close()
	}
	return listener.Addr().(*net.TCPAddr), cleanup
}

func connectSnippet(addr *net.TCPAddr) string {
	return `
		local sock = golapis.socket.tcp()
		sock:settimeout(1000)
		local ok, err = sock:connect("` + addr.IP.String() + `", ` + itoa(addr.Port) + `)
		if not ok then
			golapis.say("connect error: ", err)
			return
		end
	`
}

func TestReceiveuntilBasicSections(t *testing.T) {
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"hello--world--"}, 0, false)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader, err = sock:receiveuntil("--")
		if not reader then
			golapis.say("receiveuntil error: ", err)
			return
		end
		golapis.say("first: ", reader())
		golapis.say("second: ", reader())
		sock:close()
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "first: hello\n") || !strings.Contains(output, "second: world\n") {
		t.Errorf("unexpected output: %q", output)
	}
}

func TestReceiveuntilInclusive(t *testing.T) {
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"hello--rest"}, 0, false)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader = sock:receiveuntil("--", { inclusive = true })
		golapis.say("data: ", reader())
		sock:close()
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "data: hello--\n") {
		t.Errorf("expected inclusive pattern in result, got: %q", output)
	}
}

func TestReceiveuntilPatternAcrossPackets(t *testing.T) {
	// The pattern "--" is split across two TCP writes.
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"hello-", "-world--"}, 50*time.Millisecond, false)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader = sock:receiveuntil("--")
		golapis.say("first: ", reader())
		golapis.say("second: ", reader())
		sock:close()
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "first: hello\n") || !strings.Contains(output, "second: world\n") {
		t.Errorf("unexpected output: %q", output)
	}
}

func TestReceiveuntilSizedChunks(t *testing.T) {
	// lua-resty-http style drain loop: read the section in chunks of at most
	// 4 bytes; nil signals the end of the section; the next section follows.
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"chunk1data--next--"}, 0, false)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader = sock:receiveuntil("--")
		local parts = {}
		while true do
			local chunk, err = reader(4)
			if err then
				golapis.say("error: ", err)
				return
			end
			if not chunk then break end
			parts[#parts + 1] = chunk
		end
		golapis.say("parts: ", table.concat(parts, "|"))
		golapis.say("next: ", reader())
		sock:close()
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "parts: chun|k1da|ta\n") {
		t.Errorf("unexpected sized chunks: %q", output)
	}
	if !strings.Contains(output, "next: next\n") {
		t.Errorf("expected next section to read correctly, got: %q", output)
	}
}

func TestReceiveuntilSizedExactBudgetThenMatch(t *testing.T) {
	// Budget is exhausted exactly at the pattern boundary: the next sized
	// call consumes only the pattern and returns "" before the nil marker.
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"abc--Xrest"}, 0, false)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader = sock:receiveuntil("--X")
		golapis.say("one: ", reader(3))
		local two = reader(3)
		golapis.say("two: [", tostring(two), "] empty=", tostring(two == ""))
		golapis.say("marker: ", tostring(reader(3)))
		golapis.say("rest: ", sock:receive(4))
		sock:close()
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "one: abc\n") {
		t.Errorf("expected first chunk 'abc', got: %q", output)
	}
	if !strings.Contains(output, "two: [] empty=true\n") {
		t.Errorf("expected empty final chunk at pattern, got: %q", output)
	}
	if !strings.Contains(output, "marker: nil\n") {
		t.Errorf("expected nil end-of-section marker, got: %q", output)
	}
	if !strings.Contains(output, "rest: rest\n") {
		t.Errorf("expected remaining stream readable, got: %q", output)
	}
}

func TestReceiveuntilSelfOverlappingPattern(t *testing.T) {
	// Mirrors ngx_lua t/066-socket-receiveuntil.t TEST 5: pattern "aa" on
	// "abcabcaad" reads "abcabc", then EOF with partial "d".
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"abcabcaad"}, 0, true)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader = sock:receiveuntil("aa")
		golapis.say("data: [", reader(), "]")
		local data, err, partial = reader()
		golapis.say("err: ", tostring(err), " partial: [", tostring(partial), "]")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "data: [abcabc]\n") {
		t.Errorf("expected 'abcabc' before first match, got: %q", output)
	}
	if !strings.Contains(output, "err: closed partial: [d]\n") {
		t.Errorf("expected EOF with partial 'd', got: %q", output)
	}
}

func TestReceiveuntilEOFWithheldPartialMatch(t *testing.T) {
	// "abab" matches at offset 1 of "aababab", leaving "ab" — which is a
	// partial match of the pattern at EOF. Per ngx_lua those withheld bytes
	// are NOT flushed into the partial data (the EOF error path never
	// emits the in-progress match prefix).
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"aababab"}, 0, true)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader = sock:receiveuntil("abab")
		golapis.say("data: [", reader(), "]")
		local data, err, partial = reader()
		golapis.say("err: ", tostring(err), " partial: [", tostring(partial), "]")
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "data: [a]\n") {
		t.Errorf("expected 'a' before first match, got: %q", output)
	}
	if !strings.Contains(output, "err: closed partial: []\n") {
		t.Errorf("expected EOF with withheld partial-match bytes excluded, got: %q", output)
	}
}

func TestReceiveuntilEOFClosesSocket(t *testing.T) {
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"partial-data"}, 0, true)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader = sock:receiveuntil("--")
		local data, err, partial = reader()
		golapis.say("err: ", tostring(err), " partial: ", tostring(partial))
		local ok, err2 = sock:send("x")
		golapis.say("send err: ", tostring(err2))
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "err: closed partial: partial-data\n") {
		t.Errorf("expected closed with partial data, got: %q", output)
	}
	if !strings.Contains(output, "send err: closed") {
		t.Errorf("expected socket in closed state after EOF, got: %q", output)
	}
}

func TestReceiveuntilTimeoutPreservesState(t *testing.T) {
	// A timeout mid-pattern must not lose matcher state: the withheld "-"
	// is not flushed into the partial, and the retry completes the match.
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"ab-", "-Xtail"}, 300*time.Millisecond, false)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader = sock:receiveuntil("--X")
		sock:settimeout(100)
		local data, err, partial = reader()
		golapis.say("err: ", tostring(err), " partial: [", tostring(partial), "]")
		sock:settimeout(1000)
		local data2 = reader()
		golapis.say("data2: [", tostring(data2), "]")
		golapis.say("tail: ", sock:receive(4))
		sock:close()
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "err: timeout partial: [ab]\n") {
		t.Errorf("expected timeout with partial 'ab' (held '-' withheld), got: %q", output)
	}
	if !strings.Contains(output, "data2: []\n") {
		t.Errorf("expected retry to complete the match with no extra data, got: %q", output)
	}
	if !strings.Contains(output, "tail: tail\n") {
		t.Errorf("expected stream readable after match, got: %q", output)
	}
}

func TestReceiveuntilInterleavedWithReceive(t *testing.T) {
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"head--tail\nmore--"}, 0, false)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader = sock:receiveuntil("--")
		golapis.say("head: ", reader())
		golapis.say("line: ", sock:receive("*l"))
		golapis.say("more: ", reader())
		sock:close()
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "head: head\n") ||
		!strings.Contains(output, "line: tail\n") ||
		!strings.Contains(output, "more: more\n") {
		t.Errorf("unexpected output: %q", output)
	}
}

func TestReceiveuntilArgumentErrors(t *testing.T) {
	code := `
		local sock = golapis.socket.tcp()

		local reader, err = sock:receiveuntil("")
		golapis.say("empty: ", tostring(reader), " ", tostring(err))

		local ok, err = pcall(function() return sock:receiveuntil("x", "notatable") end)
		golapis.say("badopts: ", tostring(ok))

		local ok, err = pcall(function() return sock:receiveuntil("x", { inclusive = "yes" }) end)
		golapis.say("badinclusive: ", tostring(ok), " ", tostring(err))

		local ok, err = pcall(function() return sock:receiveuntil() end)
		golapis.say("noargs: ", tostring(ok))

		local reader = sock:receiveuntil("x")
		local ok, err = pcall(function() return reader(1, 2) end)
		golapis.say("iterargs: ", tostring(ok))
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "empty: nil pattern is empty\n") {
		t.Errorf("expected 'pattern is empty', got: %q", output)
	}
	if !strings.Contains(output, "badopts: false\n") {
		t.Errorf("expected error for non-table options, got: %q", output)
	}
	if !strings.Contains(output, "badinclusive: false") || !strings.Contains(output, "inclusive") {
		t.Errorf("expected error for bad inclusive type, got: %q", output)
	}
	if !strings.Contains(output, "noargs: false\n") {
		t.Errorf("expected error for missing pattern, got: %q", output)
	}
	if !strings.Contains(output, "iterargs: false\n") {
		t.Errorf("expected error for too many iterator args, got: %q", output)
	}
}

func TestReceiveuntilOnUnconnectedSocket(t *testing.T) {
	code := `
		local sock = golapis.socket.tcp()
		local reader = sock:receiveuntil("--")
		local data, err = reader()
		golapis.say("err: ", tostring(err))
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "err: closed\n") {
		t.Errorf("expected 'closed' on unconnected socket (OpenResty parity), got: %q", output)
	}
}

func TestReceiveuntilPendingRestoredForReceive(t *testing.T) {
	// After a reader times out holding partial-match bytes ("ab" of "abc"),
	// switching to a plain receive must restore those bytes to the buffer
	// front, like ngx_http_lua_socket_tcp_read_prepare does when the input
	// filter changes.
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"ab", "X\n"}, 300*time.Millisecond, false)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader = sock:receiveuntil("abc")
		sock:settimeout(100)
		local data, err, partial = reader()
		golapis.say("err: ", tostring(err), " partial: [", tostring(partial), "]")
		sock:settimeout(1000)
		golapis.say("recv: [", sock:receive(3), "]")
		sock:close()
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "err: timeout partial: []\n") {
		t.Errorf("expected timeout with empty partial, got: %q", output)
	}
	if !strings.Contains(output, "recv: [abX]\n") {
		t.Errorf("expected withheld 'ab' restored before receive, got: %q", output)
	}
}

func TestReceiveuntilPendingRestoredForOtherReader(t *testing.T) {
	// Same as above but switching to a different receiveuntil reader: the
	// first reader's withheld bytes must be visible to the second reader.
	addr, cleanup := startTCPChunkedWriteServer(t, []string{"ab", "X\n"}, 300*time.Millisecond, false)
	defer cleanup()

	code := connectSnippet(addr) + `
		local reader1 = sock:receiveuntil("abc")
		sock:settimeout(100)
		local data, err = reader1()
		golapis.say("err: ", tostring(err))
		sock:settimeout(1000)
		local reader2 = sock:receiveuntil("\n")
		golapis.say("line: [", reader2(), "]")
		sock:close()
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "err: timeout\n") {
		t.Errorf("expected first reader to time out, got: %q", output)
	}
	if !strings.Contains(output, "line: [abX]\n") {
		t.Errorf("expected second reader to see restored 'ab', got: %q", output)
	}
}

func TestReceiveuntilBadSelfRaises(t *testing.T) {
	// OpenResty raises a Lua argument error (luaL_checktype) for a bad self,
	// e.g. sock.receiveuntil(32, "ab") called with dot instead of colon.
	code := `
		local sock = golapis.socket.tcp()
		local ok, err = pcall(function() return sock.receiveuntil(32, "ab") end)
		golapis.say("ok: ", tostring(ok), " err: ", tostring(err))
	`
	output, err := runTCPTest(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if !strings.Contains(output, "ok: false") || !strings.Contains(output, "bad argument #1") {
		t.Errorf("expected bad-self to raise an argument error, got: %q", output)
	}
}
