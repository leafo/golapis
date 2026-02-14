// Package main provides a benchmark runner for testing Go-C-LuaJIT calling conventions.
//
// Usage:
//
//	go run ./cmd/benchmark           # Run all benchmarks
//	go run ./cmd/benchmark -bench=.  # Run with go test benchmark flags
//
// Or use go test directly:
//
//	go test -bench=. ./cmd/benchmark
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	runAll := flag.Bool("all", false, "Run all benchmarks (default)")
	benchPattern := flag.String("bench", ".", "Benchmark pattern to match")
	benchTime := flag.String("benchtime", "1s", "Benchmark time per test")
	count := flag.Int("count", 1, "Number of times to run each benchmark")
	verbose := flag.Bool("v", false, "Verbose output")
	flag.Parse()

	// Default to running all benchmarks
	if !*runAll && *benchPattern == "." {
		*runAll = true
	}

	fmt.Println("Go-C-LuaJIT Calling Convention Benchmarks")
	fmt.Println("==========================================")
	fmt.Println()
	fmt.Println("This benchmark suite tests different calling conventions:")
	fmt.Println("  1. Pure CGO overhead (Go <-> C boundary)")
	fmt.Println("  2. Lua C API calls from Go")
	fmt.Println("  3. LuaJIT FFI vs C API comparison")
	fmt.Println("  4. Batched vs individual CGO crossings")
	fmt.Println("  5. Callback overhead (Lua -> C -> Go)")
	fmt.Println("  6. Shared memory patterns")
	fmt.Println()

	// Build the go test command
	args := []string{"test", "-bench=" + *benchPattern, "-benchtime=" + *benchTime}
	if *count > 1 {
		args = append(args, fmt.Sprintf("-count=%d", *count))
	}
	if *verbose {
		args = append(args, "-v")
	}
	args = append(args, "-run=^$") // Don't run unit tests, only benchmarks
	args = append(args, "./cmd/benchmark")

	fmt.Printf("Running: go %s\n\n", strings.Join(args, " "))

	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "Error running benchmarks: %v\n", err)
		os.Exit(1)
	}
}
