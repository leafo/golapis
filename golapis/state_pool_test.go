package golapis

import (
	"sync"
	"testing"
	"time"
)

func TestStatePoolCreation(t *testing.T) {
	pool, err := NewStatePool("../examples/print.lua", 2)
	if err != nil {
		t.Fatalf("Failed to create state pool: %v", err)
	}
	defer pool.Close()

	if pool.Size() != 2 {
		t.Errorf("Expected pool size 2, got %d", pool.Size())
	}
}

func TestStatePoolGetPut(t *testing.T) {
	pool, err := NewStatePool("../examples/print.lua", 2)
	if err != nil {
		t.Fatalf("Failed to create state pool: %v", err)
	}
	defer pool.Close()

	// Get a state
	state1, err := pool.Get()
	if err != nil {
		t.Fatalf("Failed to get state from pool: %v", err)
	}
	if state1 == nil {
		t.Fatal("Got nil state from pool")
	}

	// Get another state
	state2, err := pool.Get()
	if err != nil {
		t.Fatalf("Failed to get second state from pool: %v", err)
	}
	if state2 == nil {
		t.Fatal("Got nil second state from pool")
	}

	// Verify they are different states
	if state1 == state2 {
		t.Error("Got the same state twice, expected different states")
	}

	// Return states to pool
	pool.Put(state1)
	pool.Put(state2)

	// Get a state again - should get one of the returned states
	state3, err := pool.Get()
	if err != nil {
		t.Fatalf("Failed to get state after return: %v", err)
	}
	if state3 == nil {
		t.Fatal("Got nil state after return")
	}

	// Should be one of the original states
	if state3 != state1 && state3 != state2 {
		t.Error("Got a new state instead of reused state")
	}

	pool.Put(state3)
}

func TestStatePoolConcurrentAccess(t *testing.T) {
	pool, err := NewStatePool("../examples/print.lua", 4)
	if err != nil {
		t.Fatalf("Failed to create state pool: %v", err)
	}
	defer pool.Close()

	var wg sync.WaitGroup
	concurrency := 10
	iterations := 5

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				state, err := pool.Get()
				if err != nil {
					t.Errorf("Goroutine %d: Failed to get state: %v", id, err)
					return
				}

				// Simulate some work
				time.Sleep(time.Millisecond)

				pool.Put(state)
			}
		}(i)
	}

	wg.Wait()
}

func TestStatePoolCloseBlocksGet(t *testing.T) {
	pool, err := NewStatePool("../examples/print.lua", 2)
	if err != nil {
		t.Fatalf("Failed to create state pool: %v", err)
	}

	// Get both states
	state1, err := pool.Get()
	if err != nil {
		t.Fatalf("Failed to get first state: %v", err)
	}
	state2, err := pool.Get()
	if err != nil {
		t.Fatalf("Failed to get second state: %v", err)
	}

	// Close the pool
	pool.Close()

	// Attempt to get should fail
	_, err = pool.Get()
	if err == nil {
		t.Error("Expected error when getting from closed pool")
	}

	// Put should not panic or block
	pool.Put(state1)
	pool.Put(state2)
}

func TestStatePoolDefaultSize(t *testing.T) {
	pool, err := NewStatePool("../examples/print.lua", 0)
	if err != nil {
		t.Fatalf("Failed to create state pool: %v", err)
	}
	defer pool.Close()

	// Should default to runtime.NumCPU()
	if pool.Size() <= 0 {
		t.Errorf("Expected pool size > 0, got %d", pool.Size())
	}
}

func TestStatePoolInvalidFile(t *testing.T) {
	_, err := NewStatePool("nonexistent_file.lua", 2)
	if err == nil {
		t.Error("Expected error when creating pool with invalid file")
	}
}
