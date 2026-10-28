package golapis

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// WorkerPool runs one GolapisLuaState per worker and spreads HTTP requests
// across them, the single-process counterpart of nginx's worker processes.
// Each worker owns its own mux so a location.capture subrequest is served
// by the state that issued it.
type WorkerPool struct {
	workers []*GolapisLuaState
	next    atomic.Uint32
	closed  atomic.Bool
}

// NewWorkerPool creates and starts config.Workers states with the entrypoint
// loaded. Workers are independent, so they are built in parallel to keep
// startup near single-state time. The first failure cancels the others:
// a worker still in its init hook is stopped (which interrupts a parked
// init) and every state is closed before the error is returned.
func NewWorkerPool(entry EntryPoint, config *HTTPServerConfig) (*WorkerPool, error) {
	config = normalizeHTTPServerConfig(config)
	fileRoutes := buildFileRoutes(config)
	pool := &WorkerPool{workers: make([]*GolapisLuaState, config.Workers)}

	type result struct {
		id  int
		gls *GolapisLuaState
		err error
	}
	results := make(chan result, config.Workers)
	cancel := make(chan struct{})
	for i := range pool.workers {
		go func(i int) {
			gls, err := newWorker(i, config.Workers, entry, config, fileRoutes, cancel)
			results <- result{i, gls, err}
		}(i)
	}

	var firstErr error
	for range pool.workers {
		r := <-results
		if r.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("worker %d: %w", r.id, r.err)
			close(cancel)
		}
		pool.workers[r.id] = r.gls
	}
	if firstErr != nil {
		pool.Close()
		return nil, firstErr
	}
	return pool, nil
}

// errWorkerCanceled is returned by a worker whose startup was cut short
// because another worker failed; its own error is never the one reported.
var errWorkerCanceled = fmt.Errorf("startup canceled")

// fileRoute is a static file handler at a normalized "/prefix/" path.
type fileRoute struct {
	prefix  string
	handler http.Handler
}

// buildFileRoutes normalizes the configured file servers once; the handlers
// are stateless and shared by every worker's mux.
func buildFileRoutes(config *HTTPServerConfig) []fileRoute {
	routes := make([]fileRoute, 0, len(config.FileServers))
	for _, fs := range config.FileServers {
		prefix := fs.URLPrefix
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		routes = append(routes, fileRoute{
			prefix:  prefix,
			handler: http.StripPrefix(prefix, http.FileServer(http.Dir(fs.LocalPath))),
		})
	}
	return routes
}

func newWorker(id, count int, entry EntryPoint, config *HTTPServerConfig, fileRoutes []fileRoute, cancel <-chan struct{}) (*GolapisLuaState, error) {
	gls := NewGolapisLuaState()
	if gls == nil {
		return nil, fmt.Errorf("failed to create Lua state")
	}
	gls.SetWorkerInfo(id, count)
	if config.NgxAlias {
		gls.SetupNgxAlias()
	}
	if config.SSLTrustedCert != "" {
		if err := gls.SetSSLTrustedCertificate(config.SSLTrustedCert); err != nil {
			gls.Close()
			return nil, fmt.Errorf("ssl trusted certificate: %w", err)
		}
	}
	if err := gls.LoadEntryPoint(entry); err != nil {
		gls.Close()
		return nil, fmt.Errorf("failed to load Lua script %s: %w", entry, err)
	}
	gls.Start()
	// Static routes first: more specific patterns win over "/". The mux is
	// in place before the init hook so a location.capture from init routes
	// exactly as it would from a request.
	mux := http.NewServeMux()
	for _, r := range fileRoutes {
		mux.Handle(r.prefix, r.handler)
	}
	mux.Handle("/", gls.HTTPHandler(config))
	gls.httpMux = mux
	// The init hook runs to completion on the loop before the worker takes
	// traffic; timers it starts keep running. A failure aborts startup.
	// Only this goroutine may stop the state, so the init runs on a helper
	// and a cancel from the pool is acted on here.
	if config.InitWorker != nil {
		initErr := make(chan error, 1)
		go func() { initErr <- config.InitWorker.run(gls) }()
		select {
		case err := <-initErr:
			if err != nil {
				gls.Close()
				return nil, fmt.Errorf("init worker %s: %w", config.InitWorker, err)
			}
		case <-cancel:
			gls.Stop() // interrupts a parked init; its submit returns ErrStateStopped
			<-initErr
			gls.Close()
			return nil, errWorkerCanceled
		}
	}
	return gls, nil
}

// Workers returns the pool's states in id order.
func (p *WorkerPool) Workers() []*GolapisLuaState {
	return p.workers
}

// Pick returns the next worker, round-robin, or nil once the pool is closed.
func (p *WorkerPool) Pick() *GolapisLuaState {
	if p.closed.Load() {
		return nil
	}
	n := p.next.Add(1) - 1
	return p.workers[n%uint32(len(p.workers))]
}

// Handler dispatches each request to a worker's mux. A request that
// arrives after Close gets a 503 rather than a freed state.
func (p *WorkerPool) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gls := p.Pick()
		if gls == nil {
			http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
			return
		}
		gls.httpMux.ServeHTTP(w, r)
	})
}

// BeginShutdown starts the graceful exit on every worker the way an nginx
// worker exits: golapis.worker.exiting() turns true, every pending timer
// fires at once with premature=true, and timer.at refuses new timers. A
// recurring timer therefore winds down on its own and Wait can finish.
func (p *WorkerPool) BeginShutdown() {
	for _, gls := range p.workers {
		gls.exiting.Store(true)
		gls.CancelAllTimers()
	}
}

// Wait blocks until every worker's threads have completed.
func (p *WorkerPool) Wait() {
	p.each(func(gls *GolapisLuaState) { gls.Wait() })
}

// CloseHijackedConns closes raw request sockets on every worker.
func (p *WorkerPool) CloseHijackedConns() {
	p.each(func(gls *GolapisLuaState) { gls.CloseHijackedConns() })
}

// Stop shuts down every worker's event loop and stops admitting requests.
func (p *WorkerPool) Stop() {
	p.closed.Store(true)
	p.each(func(gls *GolapisLuaState) { gls.Stop() })
}

// Close stops and frees every worker.
func (p *WorkerPool) Close() {
	p.closed.Store(true)
	p.each(func(gls *GolapisLuaState) {
		if gls != nil {
			gls.Close()
		}
	})
}

// each runs fn on every worker concurrently; a worker draining slowly must
// not hold up the others.
func (p *WorkerPool) each(fn func(*GolapisLuaState)) {
	var wg sync.WaitGroup
	for _, gls := range p.workers {
		wg.Add(1)
		go func(gls *GolapisLuaState) {
			defer wg.Done()
			fn(gls)
		}(gls)
	}
	wg.Wait()
}

// AutoWorkers is the worker count for Workers == 0: the CPUs Go will
// actually schedule on, which honors container quotas.
func AutoWorkers() int {
	return runtime.GOMAXPROCS(0)
}
