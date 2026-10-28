package golapis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DefaultClientMaxBodySize is the default maximum request body size (1MB)
const DefaultClientMaxBodySize int64 = 1 * 1024 * 1024

const (
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 30 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultShutdownTimeout   = 10 * time.Second
	DefaultMaxHeaderBytes    = 1 << 20
)

// NoTimeout disables a timeout in HTTPServerConfig, where 0 means "use the
// default".
const NoTimeout time.Duration = -1

// FileServerMapping maps a local directory to a URL prefix for static file serving
type FileServerMapping struct {
	LocalPath string // local filesystem path (e.g., "./static")
	URLPrefix string // URL prefix (e.g., "/static/")
}

// HTTPServerConfig holds configuration for the HTTP server
type HTTPServerConfig struct {
	ClientMaxBodySize int64               // max request body size in bytes (0 = unlimited)
	NgxAlias          bool                // alias golapis table to global ngx
	SSLTrustedCert    string              // PEM bundle for cosocket ssl_verify (lua_ssl_trusted_certificate)
	FileServers       []FileServerMapping // static file server mappings
	ReadHeaderTimeout time.Duration       // max time to read request headers (NoTimeout to disable)
	ReadTimeout       time.Duration       // max time to read the full request (NoTimeout to disable)
	WriteTimeout      time.Duration       // max time to write the response (NoTimeout to disable)
	IdleTimeout       time.Duration       // max keep-alive idle time (NoTimeout to disable)
	ShutdownTimeout   time.Duration       // max graceful shutdown wait (NoTimeout to disable)
	MaxHeaderBytes    int                 // max request header size
	TrustProxyHeaders bool                // trust X-Forwarded-For for request logs
	Workers           int                 // Lua states serving requests; 1 by default, 0 = one per CPU
	InitWorker        EntryPoint          // run once per worker after start, before it serves (init_worker_by_lua)
}

// DefaultHTTPServerConfig returns the default HTTP server configuration.
func DefaultHTTPServerConfig() *HTTPServerConfig {
	return &HTTPServerConfig{
		Workers:           1,
		ClientMaxBodySize: DefaultClientMaxBodySize,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		ReadTimeout:       DefaultReadTimeout,
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,
		ShutdownTimeout:   DefaultShutdownTimeout,
		MaxHeaderBytes:    DefaultMaxHeaderBytes,
	}
}

func normalizeHTTPServerConfig(config *HTTPServerConfig) *HTTPServerConfig {
	if config == nil {
		return DefaultHTTPServerConfig()
	}

	normalized := *config
	defaults := DefaultHTTPServerConfig()
	if normalized.Workers == 0 {
		normalized.Workers = AutoWorkers()
	} else if normalized.Workers < 0 {
		normalized.Workers = defaults.Workers
	}
	if normalized.ReadHeaderTimeout == 0 {
		normalized.ReadHeaderTimeout = defaults.ReadHeaderTimeout
	}
	if normalized.ReadTimeout == 0 {
		normalized.ReadTimeout = defaults.ReadTimeout
	}
	if normalized.WriteTimeout == 0 {
		normalized.WriteTimeout = defaults.WriteTimeout
	}
	if normalized.IdleTimeout == 0 {
		normalized.IdleTimeout = defaults.IdleTimeout
	}
	if normalized.ShutdownTimeout == 0 {
		normalized.ShutdownTimeout = defaults.ShutdownTimeout
	}
	if normalized.MaxHeaderBytes == 0 {
		normalized.MaxHeaderBytes = defaults.MaxHeaderBytes
	}
	return &normalized
}

// ParseHumanDuration parses a non-negative integer duration with an optional
// ms/s/m/h/d suffix, nginx style. A bare number is seconds, e.g. "30", "500ms",
// "5m", "1d".
func ParseHumanDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	unit := time.Second
	num := s
	switch {
	case strings.HasSuffix(s, "ms"):
		unit, num = time.Millisecond, s[:len(s)-2]
	case strings.HasSuffix(s, "s"):
		unit, num = time.Second, s[:len(s)-1]
	case strings.HasSuffix(s, "m"):
		unit, num = time.Minute, s[:len(s)-1]
	case strings.HasSuffix(s, "h"):
		unit, num = time.Hour, s[:len(s)-1]
	case strings.HasSuffix(s, "d"):
		unit, num = 24*time.Hour, s[:len(s)-1]
	}

	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || num[0] < '0' || num[0] > '9' {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("duration must not be negative, got %q", s)
	}
	if n > math.MaxInt64/int64(unit) {
		return 0, fmt.Errorf("duration %q is too large", s)
	}
	return time.Duration(n) * unit, nil
}

// http.Server treats a negative timeout as disabled, but a zero
// ReadHeaderTimeout falls back to ReadTimeout. Without that fallback, disabling
// the header timeout would leave headers unbounded even with ReadTimeout set,
// since http.Server only applies ReadTimeout once headers are read.
func serverReadHeaderTimeout(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

// HTTPHandler returns an http.Handler that executes the loaded entrypoint for each request.
// The GolapisLuaState must have Start() called and an entrypoint loaded before use.
func (gls *GolapisLuaState) HTTPHandler(config *HTTPServerConfig) http.Handler {
	config = normalizeHTTPServerConfig(config)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := NewGolapisRequest(r)
		req.maxBodySize = config.ClientMaxBodySize
		wrappedWriter := req.WrapResponseWriter(w)

		// Rejected with ErrStateStopped if Stop raced with dispatch; the
		// event is never queued in that case, so a later Start can't run it.
		result := gls.submit(&StateEvent{
			Type:         EventRunEntryPoint,
			OutputWriter: wrappedWriter,
			Request:      req,
		})
		if req.hijackedConn != nil {
			if result.Error != nil {
				log.Printf("error after raw socket acquired %s %s: %v", r.Method, r.URL.RequestURI(), result.Error)
			}
			// The request owns the connection now; net/http won't close it
			// when the handler returns, so close it once the thread is done.
			req.hijackedConn.Close()
			return
		}
		if result.Error != nil {
			status := http.StatusInternalServerError
			if errors.Is(result.Error, ErrStateStopped) {
				status = http.StatusServiceUnavailable
			}
			log.Printf("%d %s %s: %v", status, r.Method, r.URL.RequestURI(), result.Error)
			// Once headers are committed the status can't change; sending
			// http.Error would just append the message to the response body.
			if !req.HeadersSent {
				http.Error(w, result.Error.Error(), status)
			}
			return
		}
		req.FlushHeaders(w)
	})
}

// StartHTTPServer starts an HTTP server that executes the given Lua script
// for each request, spread round-robin over config.Workers Lua states.
func StartHTTPServer(entry EntryPoint, port string, config *HTTPServerConfig) {
	config = normalizeHTTPServerConfig(config)
	fmt.Printf("Starting HTTP server on port %s with script: %s (%d workers)\n", port, entry, config.Workers)

	pool, err := NewWorkerPool(entry, config)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	for i, r := range buildFileRoutes(config) {
		fmt.Printf("Serving files from %s at %s\n", config.FileServers[i].LocalPath, r.prefix)
	}

	var requestWg sync.WaitGroup
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           logHTTPRequests(pool.Handler(), config.TrustProxyHeaders, &requestWg),
		ReadHeaderTimeout: serverReadHeaderTimeout(config.ReadHeaderTimeout),
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		MaxHeaderBytes:    config.MaxHeaderBytes,
	}
	shutdownStarted, shutdownDone := setupGracefulShutdown(server, config.ShutdownTimeout)

	fmt.Printf("Listening on http://localhost:%s\n", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	select {
	case <-shutdownStarted:
		pool.BeginShutdown()
		if err := <-shutdownDone; err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
		// Shutdown doesn't track hijacked connections (raw request sockets).
		// Give their threads the same grace period, then close them so the
		// request wait can finish.
		deadline := time.Now().Add(config.ShutdownTimeout)
		waitWithTimeout(pool.Wait, config.ShutdownTimeout)
		pool.CloseHijackedConns()
		requestWg.Wait()
		// Threads whose sockets just closed finish promptly; give them what
		// is left of the grace period, but never wait without bound: a
		// timer that ignores worker.exiting() must not pin the process.
		remaining := config.ShutdownTimeout
		if remaining >= 0 {
			remaining = max(time.Until(deadline), time.Second)
		}
		if !waitWithTimeout(pool.Wait, remaining) {
			log.Printf("shutdown timeout: stopping with Lua threads still running")
		}
	default:
	}
	pool.Stop()
}

// waitWithTimeout runs wait and returns false if it hasn't finished after
// timeout. A negative timeout (NoTimeout) waits indefinitely.
func waitWithTimeout(wait func(), timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	if timeout < 0 {
		<-done
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// setupGracefulShutdown sets up signal handling for graceful shutdown
func setupGracefulShutdown(server *http.Server, timeout time.Duration) (<-chan struct{}, <-chan error) {
	shutdownStarted := make(chan struct{})
	shutdownDone := make(chan error, 1)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		signal.Stop(c)
		close(shutdownStarted)
		fmt.Println("\nShutting down gracefully...")
		ctx := context.Background()
		if timeout >= 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		shutdownDone <- server.Shutdown(ctx)
	}()

	return shutdownStarted, shutdownDone
}

type responseStatsWriter struct {
	http.ResponseWriter
	status    int
	bodyBytes int64
}

func newResponseStatsWriter(w http.ResponseWriter) *responseStatsWriter {
	return &responseStatsWriter{ResponseWriter: w}
}

func (w *responseStatsWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseStatsWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bodyBytes += int64(n)
	return n, err
}

func (w *responseStatsWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseStatsWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *responseStatsWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *responseStatsWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseStatsWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func logHTTPRequests(next http.Handler, trustProxyHeaders bool, requestWg *sync.WaitGroup) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestWg != nil {
			requestWg.Add(1)
			defer requestWg.Done()
		}
		startTime := time.Now()
		statsWriter := newResponseStatsWriter(w)
		next.ServeHTTP(statsWriter, r)
		logHTTPRequest(r, startTime, statsWriter.Status(), statsWriter.bodyBytes, trustProxyHeaders)
	})
}

// logHTTPRequest logs HTTP requests in nginx "combined" log format
// Combined format: $remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
func logHTTPRequest(r *http.Request, startTime time.Time, status int, bodyBytes int64, trustProxyHeaders bool) {
	remoteAddr := requestRemoteAddr(r, trustProxyHeaders)

	remoteUser := "-"
	if r.URL.User != nil && r.URL.User.Username() != "" {
		remoteUser = r.URL.User.Username()
	}

	timeLocal := startTime.Format("02/Jan/2006:15:04:05 -0700")
	duration := time.Since(startTime)
	request := fmt.Sprintf("%s %s %s", r.Method, r.URL.RequestURI(), r.Proto)
	referer := r.Header.Get("Referer")
	if referer == "" {
		referer = "-"
	}
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		userAgent = "-"
	}

	log.Printf("%s - %s [%s] \"%s\" %d %d \"%s\" \"%s\" %v",
		remoteAddr, remoteUser, timeLocal, request, status, bodyBytes, referer, userAgent, duration)
}

func requestRemoteAddr(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			if first, _, ok := strings.Cut(forwarded, ","); ok {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(forwarded)
		}
	}
	return r.RemoteAddr
}
