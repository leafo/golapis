package golapis

import (
	"io"
	"net"
	"testing"
)

// Benchmarks for the Lua<->Go yield/resume path. Uses only API that exists
// before and after the websocket patches so the same file runs on both.

func benchState(b *testing.B) *GolapisLuaState {
	gls := NewGolapisLuaState()
	if gls == nil {
		b.Fatal("Failed to create Lua state")
	}
	gls.Start()
	b.Cleanup(func() { gls.Stop(); gls.Close() })
	return gls
}

func echoServer(b *testing.B) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	b.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

// One TCP send + one receive per iteration: two async yields through the
// event loop
func BenchmarkTCPRoundtrip(b *testing.B) {
	gls := benchState(b)
	addr := echoServer(b)
	host, port, _ := net.SplitHostPort(addr)
	b.ResetTimer()
	err := gls.RunString(`
		local n = ` + benchItoa(b.N) + `
		local sock = golapis.socket.tcp()
		assert(sock:connect("` + host + `", ` + port + `))
		for i = 1, n do
			assert(sock:send("ping\n"))
			assert(sock:receive("*l"))
		end
		sock:close()
	`)
	if err != nil {
		b.Fatal(err)
	}
}

// Yield into the timer scheduler and resume, once per iteration
func BenchmarkSleepZero(b *testing.B) {
	gls := benchState(b)
	b.ResetTimer()
	err := gls.RunString(`
		for i = 1, ` + benchItoa(b.N) + ` do golapis.sleep(0) end
	`)
	if err != nil {
		b.Fatal(err)
	}
}

// Pure Lua coroutine ping-pong: resume and yield both go through the Go
// scheduler but never leave the event loop
func BenchmarkCoroutinePingPong(b *testing.B) {
	gls := benchState(b)
	b.ResetTimer()
	err := gls.RunString(`
		local co = coroutine.create(function()
			while true do coroutine.yield(1) end
		end)
		for i = 1, ` + benchItoa(b.N) + ` do
			assert(coroutine.resume(co))
		end
	`)
	if err != nil {
		b.Fatal(err)
	}
}

// Whole-request cost: run a trivial script as a fresh thread per iteration
func BenchmarkRunStringTrivial(b *testing.B) {
	gls := benchState(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := gls.RunString(`local x = 1`); err != nil {
			b.Fatal(err)
		}
	}
}

func benchItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
