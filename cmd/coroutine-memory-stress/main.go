package main

import (
	"flag"
	"fmt"
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
	iterations := flag.Int("iterations", 10000, "number of coroutine test iterations")
	batchSize := flag.Int("batch", 500, "iterations per batch (memory sampled between batches)")
	warmup := flag.Int("warmup", 100, "warmup iterations before measuring")
	flag.Parse()

	fmt.Println("Coroutine Memory Stress Test")
	fmt.Println("============================")
	fmt.Printf("Iterations: %d, Batch size: %d, Warmup: %d\n\n", *iterations, *batchSize, *warmup)

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
		runBatch(gls, *warmup)
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
		runBatch(gls, currentBatch)
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

// runBatch schedules coroutine stress test iterations
func runBatch(gls *golapis.GolapisLuaState, count int) {
	for i := 0; i < count; i++ {
		// Test 1: Basic create/resume/destroy
		code1 := `
			for i = 1, 10 do
				local co = coroutine.create(function(x) return x * 2 end)
				coroutine.resume(co, i)
			end
		`
		if err := gls.RunString(code1); err != nil {
			fmt.Fprintf(os.Stderr, "Error in test 1: %v\n", err)
		}

		// Test 2: Coroutines with multiple yields
		code2 := `
			local co = coroutine.create(function()
				for i = 1, 5 do coroutine.yield(i) end
				return "done"
			end)
			while coroutine.status(co) ~= "dead" do
				coroutine.resume(co)
			end
		`
		if err := gls.RunString(code2); err != nil {
			fmt.Fprintf(os.Stderr, "Error in test 2: %v\n", err)
		}

		// Test 3: Nested coroutines
		code3 := `
			local outer = coroutine.create(function()
				local inner = coroutine.create(function()
					return 42
				end)
				return coroutine.resume(inner)
			end)
			coroutine.resume(outer)
		`
		if err := gls.RunString(code3); err != nil {
			fmt.Fprintf(os.Stderr, "Error in test 3: %v\n", err)
		}

		// Test 4: coroutine.wrap
		code4 := `
			local gen = coroutine.wrap(function()
				for i = 1, 5 do coroutine.yield(i) end
			end)
			for i = 1, 5 do gen() end
		`
		if err := gls.RunString(code4); err != nil {
			fmt.Fprintf(os.Stderr, "Error in test 4: %v\n", err)
		}

		// Test 5: Coroutine with async sleep
		code5 := `
			local co = coroutine.create(function()
				golapis.sleep(0)
				return "done"
			end)
			coroutine.resume(co)
		`
		if err := gls.RunString(code5); err != nil {
			fmt.Fprintf(os.Stderr, "Error in test 5: %v\n", err)
		}

		// Test 6: Error in coroutine
		code6 := `
			local co = coroutine.create(function()
				error("test error")
			end)
			local ok, err = coroutine.resume(co)
		`
		if err := gls.RunString(code6); err != nil {
			fmt.Fprintf(os.Stderr, "Error in test 6: %v\n", err)
		}

		// Test 7: Deeply nested coroutines
		code7 := `
			local function nest(depth)
				if depth <= 0 then return depth end
				local co = coroutine.create(function() return nest(depth - 1) end)
				local ok, r = coroutine.resume(co)
				return r
			end
			nest(10)
		`
		if err := gls.RunString(code7); err != nil {
			fmt.Fprintf(os.Stderr, "Error in test 7: %v\n", err)
		}

		// Test 8: Abandoned coroutine (yields but never resumed again)
		code8 := `
			local co = coroutine.create(function()
				coroutine.yield("first")
				error("unreachable: abandoned coroutine was resumed")
			end)
			coroutine.resume(co)
			-- co goes out of scope while still suspended
		`
		if err := gls.RunString(code8); err != nil {
			fmt.Fprintf(os.Stderr, "Error in test 8: %v\n", err)
		}

		// Test 9: Abandoned coroutine with captured upvalues/locals
		code9 := `
			local big_table = {}
			for i = 1, 100 do big_table[i] = string.rep("x", 100) end

			local co = coroutine.create(function()
				local captured = big_table
				coroutine.yield(1)
				error("unreachable: abandoned coroutine with upvalues was resumed")
			end)
			coroutine.resume(co)
			-- co and big_table should both be GC'd
		`
		if err := gls.RunString(code9); err != nil {
			fmt.Fprintf(os.Stderr, "Error in test 9: %v\n", err)
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
