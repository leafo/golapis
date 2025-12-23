package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"time"

	"golapis/golapis"
)

type MemorySample struct {
	Time      time.Duration
	Iteration int
	GoHeapMB  float64
	GoSysMB   float64
	LuaMemKB  int
}

func main() {
	iterations := flag.Int("iterations", 10000, "number of UDP request iterations")
	batchSize := flag.Int("batch", 500, "iterations per batch (memory sampled between batches)")
	warmup := flag.Int("warmup", 100, "warmup iterations before measuring")
	flag.Parse()

	fmt.Println("UDP Memory Stress Test")
	fmt.Println("======================")
	fmt.Printf("Iterations: %d, Batch size: %d, Warmup: %d\n\n", *iterations, *batchSize, *warmup)

	// Start UDP echo server
	server, err := startEchoServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start echo server: %v\n", err)
		os.Exit(1)
	}
	defer server.Close()
	serverPort := server.LocalAddr().(*net.UDPAddr).Port

	// Create Lua state
	gls := golapis.NewGolapisLuaState()
	if gls == nil {
		fmt.Fprintln(os.Stderr, "Failed to create Lua state")
		os.Exit(1)
	}
	defer gls.Close()

	gls.Start()
	defer gls.Stop()

	startTime := time.Now()
	var samples []MemorySample

	// Print header
	fmt.Printf("%-8s %10s %12s %12s %12s\n", "Time(s)", "Iteration", "Go Heap(MB)", "Lua Mem(KB)", "Go Sys(MB)")
	fmt.Println("-------- ---------- ------------ ------------ ------------")

	// Initial sample (state is idle)
	samples = append(samples, takeSample(gls, startTime, 0))
	printSample(samples[len(samples)-1])

	// Warmup phase
	if *warmup > 0 {
		fmt.Printf("\nRunning %d warmup iterations...\n", *warmup)
		runBatch(gls, serverPort, *warmup)
		gls.Wait() // Wait for all to complete - state is now idle
		gls.ForceLuaGC()
		runtime.GC()
		fmt.Println("Warmup complete.\n")
	}

	// Baseline sample (after warmup, state is idle)
	baseline := takeSample(gls, startTime, 0)
	samples = append(samples, baseline)
	printSample(baseline)

	// Main test - run in batches, sample between batches
	completed := 0
	for completed < *iterations {
		// Determine batch size for this iteration
		remaining := *iterations - completed
		currentBatch := *batchSize
		if remaining < currentBatch {
			currentBatch = remaining
		}

		// Run batch
		runBatch(gls, serverPort, currentBatch)
		gls.Wait() // State is now idle - safe to sample

		completed += currentBatch

		// Sample memory (state is idle after Wait)
		sample := takeSample(gls, startTime, completed)
		samples = append(samples, sample)
		printSample(sample)
	}

	// Final cleanup and sample
	gls.ForceLuaGC()
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	final := takeSample(gls, startTime, completed)
	samples = append(samples, final)
	fmt.Println("\nAfter GC:")
	printSample(final)

	// Analyze and report
	fmt.Println("\n" + analyzeSamples(baseline, final, *iterations))
}

func runBatch(gls *golapis.GolapisLuaState, port, count int) {
	// Schedule all iterations via timers - they run concurrently within Lua's event model
	for i := 0; i < count; i++ {
		code := fmt.Sprintf(`
			golapis.timer.at(0, function()
				local sock = golapis.socket.udp()
				sock:settimeout(1000)
				local ok, err = sock:setpeername("127.0.0.1", %d)
				if ok then
					sock:send("ping")
					sock:receive()
				end
				sock:close()
			end)
		`, port)

		if err := gls.RunString(code); err != nil {
			fmt.Fprintf(os.Stderr, "Error scheduling iteration: %v\n", err)
		}
	}
}

func takeSample(gls *golapis.GolapisLuaState, startTime time.Time, iteration int) MemorySample {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return MemorySample{
		Time:      time.Since(startTime),
		Iteration: iteration,
		GoHeapMB:  float64(m.Alloc) / 1024 / 1024,
		GoSysMB:   float64(m.Sys) / 1024 / 1024,
		LuaMemKB:  gls.GetLuaMemoryKB(),
	}
}

func printSample(s MemorySample) {
	fmt.Printf("%-8.1f %10d %12.2f %12d %12.2f\n",
		s.Time.Seconds(), s.Iteration, s.GoHeapMB, s.LuaMemKB, s.GoSysMB)
}

func analyzeSamples(baseline, final MemorySample, iterations int) string {
	goHeapDelta := final.GoHeapMB - baseline.GoHeapMB
	luaMemDelta := final.LuaMemKB - baseline.LuaMemKB
	goSysDelta := final.GoSysMB - baseline.GoSysMB

	result := fmt.Sprintf(`Summary:
  Total iterations: %d
  Duration: %.1fs
  Go Heap: %.2fMB → %.2fMB (Δ %+.2fMB)
  Lua Mem: %dKB → %dKB (Δ %+dKB)
  Go Sys:  %.2fMB → %.2fMB (Δ %+.2fMB)
`,
		iterations,
		final.Time.Seconds(),
		baseline.GoHeapMB, final.GoHeapMB, goHeapDelta,
		baseline.LuaMemKB, final.LuaMemKB, luaMemDelta,
		baseline.GoSysMB, final.GoSysMB, goSysDelta)

	// Determine verdict
	// Consider it a potential leak if memory grew more than 20% from baseline
	goHeapGrowthPct := (goHeapDelta / baseline.GoHeapMB) * 100
	luaMemGrowthPct := float64(luaMemDelta) / float64(baseline.LuaMemKB) * 100

	verdict := "✓ No significant memory growth detected"
	if goHeapGrowthPct > 20 {
		verdict = fmt.Sprintf("⚠ Go heap grew %.1f%% - possible leak", goHeapGrowthPct)
	} else if luaMemGrowthPct > 20 {
		verdict = fmt.Sprintf("⚠ Lua memory grew %.1f%% - possible leak", luaMemGrowthPct)
	}

	result += fmt.Sprintf("\nVerdict: %s\n", verdict)
	return result
}

// UDP echo server
type EchoServer struct {
	conn *net.UDPConn
	done chan struct{}
}

func startEchoServer() (*EchoServer, error) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	server := &EchoServer{
		conn: conn,
		done: make(chan struct{}),
	}

	go func() {
		buf := make([]byte, 65536)
		for {
			select {
			case <-server.done:
				return
			default:
				conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				n, remoteAddr, err := conn.ReadFromUDP(buf)
				if err != nil {
					continue
				}
				conn.WriteToUDP(buf[:n], remoteAddr)
			}
		}
	}()

	return server, nil
}

func (s *EchoServer) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}

func (s *EchoServer) Close() error {
	close(s.done)
	return s.conn.Close()
}
