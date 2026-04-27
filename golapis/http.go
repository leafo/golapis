package golapis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
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

// FileServerMapping maps a local directory to a URL prefix for static file serving
type FileServerMapping struct {
	LocalPath string // local filesystem path (e.g., "./static")
	URLPrefix string // URL prefix (e.g., "/static/")
}

// HTTPServerConfig holds configuration for the HTTP server
type HTTPServerConfig struct {
	ClientMaxBodySize int64               // max request body size in bytes (0 = unlimited)
	NgxAlias          bool                // alias golapis table to global ngx
	FileServers       []FileServerMapping // static file server mappings
	ReadHeaderTimeout time.Duration       // max time to read request headers
	ReadTimeout       time.Duration       // max time to read the full request
	WriteTimeout      time.Duration       // max time to write the response
	IdleTimeout       time.Duration       // max keep-alive idle time
	ShutdownTimeout   time.Duration       // max graceful shutdown wait
	MaxHeaderBytes    int                 // max request header size
	TrustProxyHeaders bool                // trust X-Forwarded-For for request logs
}

// DefaultHTTPServerConfig returns the default HTTP server configuration.
func DefaultHTTPServerConfig() *HTTPServerConfig {
	return &HTTPServerConfig{
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

// HTTPHandler returns an http.Handler that executes the loaded entrypoint for each request.
// The GolapisLuaState must have Start() called and an entrypoint loaded before use.
func (gls *GolapisLuaState) HTTPHandler(config *HTTPServerConfig) http.Handler {
	config = normalizeHTTPServerConfig(config)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := NewGolapisRequest(r)
		req.maxBodySize = config.ClientMaxBodySize
		wrappedWriter := req.WrapResponseWriter(w)

		resp := make(chan *StateResponse, 1)
		gls.eventChan <- &StateEvent{
			Type:         EventRunEntryPoint,
			OutputWriter: wrappedWriter,
			Request:      req,
			Response:     resp,
		}

		result := <-resp
		if result.Error != nil {
			http.Error(w, result.Error.Error(), http.StatusInternalServerError)
			return
		}
		req.FlushHeaders(w)
	})
}

// StartHTTPServer starts an HTTP server that executes the given Lua script for each request
// Uses a single shared GolapisLuaState for all requests with cooperative scheduling
func StartHTTPServer(entry EntryPoint, port string, config *HTTPServerConfig) {
	fmt.Printf("Starting HTTP server on port %s with script: %s\n", port, entry)
	config = normalizeHTTPServerConfig(config)

	// Create single shared Lua state at server startup
	lua := NewGolapisLuaState()
	if lua == nil {
		log.Fatal("Failed to create Lua state")
	}

	if config.NgxAlias {
		lua.SetupNgxAlias()
	}

	// Load the entrypoint at startup
	if err := lua.LoadEntryPoint(entry); err != nil {
		lua.Close()
		log.Fatalf("Failed to load Lua script %s: %v", entry, err)
	}

	// Start the event loop (runs for lifetime of server)
	lua.Start()
	defer lua.Close()
	defer lua.Stop()

	// Create custom mux so location.capture can route through it
	mux := http.NewServeMux()

	// Register static file servers first (more specific routes take precedence)
	for _, fs := range config.FileServers {
		prefix := fs.URLPrefix
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		localPath := fs.LocalPath // capture for closure
		fileHandler := http.StripPrefix(prefix, http.FileServer(http.Dir(localPath)))
		mux.Handle(prefix, fileHandler)
		fmt.Printf("Serving files from %s at %s\n", localPath, prefix)
	}

	handler := lua.HTTPHandler(config)
	mux.Handle("/", handler)

	// Store mux on state so location.capture can route through it
	lua.httpMux = mux

	var requestWg sync.WaitGroup
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           logHTTPRequests(mux, config.TrustProxyHeaders, &requestWg),
		ReadHeaderTimeout: config.ReadHeaderTimeout,
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
		if err := <-shutdownDone; err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
		requestWg.Wait()
	default:
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
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
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
