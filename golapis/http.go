package golapis

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// DefaultClientMaxBodySize is the default maximum request body size (1MB)
const DefaultClientMaxBodySize int64 = 1 * 1024 * 1024

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
}

// DefaultHTTPServerConfig returns the default HTTP server configuration.
func DefaultHTTPServerConfig() *HTTPServerConfig {
	return &HTTPServerConfig{
		ClientMaxBodySize: DefaultClientMaxBodySize,
	}
}

// HTTPHandler returns an http.Handler that executes the loaded entrypoint for each request.
// The GolapisLuaState must have Start() called and an entrypoint loaded before use.
func (gls *GolapisLuaState) HTTPHandler(config *HTTPServerConfig) http.Handler {
	if config == nil {
		config = DefaultHTTPServerConfig()
	}
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
	if config == nil {
		config = DefaultHTTPServerConfig()
	}

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

	// Setup graceful shutdown
	setupGracefulShutdown(lua)

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
		// Wrap with logging
		mux.Handle(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()
			fileHandler.ServeHTTP(w, r)
			logHTTPRequest(r, startTime, http.StatusOK, 0)
		}))
		fmt.Printf("Serving files from %s at %s\n", localPath, prefix)
	}

	// Create handler with logging wrapper
	handler := lua.HTTPHandler(config)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		handler.ServeHTTP(w, r)
		logHTTPRequest(r, startTime, http.StatusOK, 0)
	})

	// Store mux on state so location.capture can route through it
	lua.httpMux = mux

	fmt.Printf("Listening on http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// setupGracefulShutdown sets up signal handling for graceful shutdown
func setupGracefulShutdown(lua *GolapisLuaState) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Println("\nShutting down gracefully...")
		lua.Stop()
		lua.Close()
		os.Exit(0)
	}()
}

// logHTTPRequest logs HTTP requests in nginx "combined" log format
// Combined format: $remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
func logHTTPRequest(r *http.Request, startTime time.Time, status int, bodyBytes int64) {
	remoteAddr := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		remoteAddr = forwarded
	}

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
