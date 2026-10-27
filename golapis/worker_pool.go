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
// startup near single-state time. On error every state created is closed.
func NewWorkerPool(entry EntryPoint, config *HTTPServerConfig) (*WorkerPool, error) {
	config = normalizeHTTPServerConfig(config)
	fileRoutes := buildFileRoutes(config)
	pool := &WorkerPool{workers: make([]*GolapisLuaState, config.Workers)}
	errs := make([]error, config.Workers)
	var wg sync.WaitGroup
	for i := range pool.workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pool.workers[i], errs[i] = newWorker(i, config.Workers, entry, config, fileRoutes)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("worker %d: %w", i, err)
		}
	}
	return pool, nil
}

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

func newWorker(id, count int, entry EntryPoint, config *HTTPServerConfig, fileRoutes []fileRoute) (*GolapisLuaState, error) {
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
	// Static routes first: more specific patterns win over "/"
	mux := http.NewServeMux()
	for _, r := range fileRoutes {
		mux.Handle(r.prefix, r.handler)
	}
	mux.Handle("/", gls.HTTPHandler(config))
	gls.httpMux = mux
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

// BeginShutdown flips golapis.worker.exiting() to true on every worker so
// long-running timers can wind down while in-flight requests finish.
func (p *WorkerPool) BeginShutdown() {
	for _, gls := range p.workers {
		gls.exiting.Store(true)
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
