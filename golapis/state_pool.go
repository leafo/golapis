package golapis

import (
	"fmt"
	"runtime"
	"sync"
)

// StatePool manages a pool of GolapisLuaState instances for concurrent request handling
type StatePool struct {
	states   []*GolapisLuaState
	stateCh  chan *GolapisLuaState // channel for available states
	filename string                // entrypoint file to preload
	size     int                   // number of states in the pool
	mu       sync.Mutex            // protects pool operations
	closed   bool                  // true when pool is closed
}

// NewStatePool creates a pool of GolapisLuaState instances
// If size <= 0, defaults to runtime.NumCPU()
func NewStatePool(filename string, size int) (*StatePool, error) {
	if size <= 0 {
		size = runtime.NumCPU()
	}

	pool := &StatePool{
		states:   make([]*GolapisLuaState, size),
		stateCh:  make(chan *GolapisLuaState, size),
		filename: filename,
		size:     size,
	}

	// Create and initialize all states
	for i := 0; i < size; i++ {
		state := NewGolapisLuaState()
		if state == nil {
			// Clean up any states already created
			for j := 0; j < i; j++ {
				pool.states[j].Stop()
				pool.states[j].Close()
			}
			return nil, fmt.Errorf("failed to create Lua state %d", i)
		}

		// Preload the entrypoint file
		if err := state.PreloadEntryPointFile(filename); err != nil {
			// Clean up
			state.Close()
			for j := 0; j < i; j++ {
				pool.states[j].Stop()
				pool.states[j].Close()
			}
			return nil, fmt.Errorf("failed to preload entrypoint in state %d: %v", i, err)
		}

		// Start the event loop
		state.Start()

		pool.states[i] = state
		pool.stateCh <- state // make available
	}

	return pool, nil
}

// Get retrieves an available state from the pool
// Blocks until a state is available or pool is closed
func (p *StatePool) Get() (*GolapisLuaState, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("pool is closed")
	}
	p.mu.Unlock()

	state, ok := <-p.stateCh
	if !ok {
		return nil, fmt.Errorf("pool is closed")
	}
	return state, nil
}

// Put returns a state to the pool for reuse
func (p *StatePool) Put(state *GolapisLuaState) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		// Pool is closed, don't return the state
		return
	}

	// Return state to pool (non-blocking)
	select {
	case p.stateCh <- state:
	default:
		// This shouldn't happen if pool is used correctly
		// but handle gracefully
	}
}

// Close shuts down all states in the pool
func (p *StatePool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}
	p.closed = true

	// Close the channel to prevent new gets
	close(p.stateCh)

	// Stop and close all states
	for _, state := range p.states {
		if state != nil {
			state.Stop()
			state.Close()
		}
	}
}

// Size returns the number of states in the pool
func (p *StatePool) Size() int {
	return p.size
}
