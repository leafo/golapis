package golapis

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// DefaultClientMaxBodySize is the default maximum request body size (1MB)
const DefaultClientMaxBodySize int64 = 1 * 1024 * 1024

// HTTPServerConfig holds configuration for the HTTP server
type HTTPServerConfig struct {
	ClientMaxBodySize int64 // max request body size in bytes (0 = unlimited)
	StatePoolSize     int   // number of Lua states in pool (0 = runtime.NumCPU())
}

// DefaultHTTPServerConfig returns the default HTTP server configuration.
func DefaultHTTPServerConfig() *HTTPServerConfig {
	return &HTTPServerConfig{
		ClientMaxBodySize: DefaultClientMaxBodySize,
		StatePoolSize:     0, // defaults to runtime.NumCPU()
	}
}

// StartHTTPServer starts an HTTP server that executes the given Lua script for each request
// Uses a pool of GolapisLuaState instances to handle requests concurrently
func StartHTTPServer(filename, port string, config *HTTPServerConfig) {
	fmt.Printf("Starting HTTP server on port %s with script: %s\n", port, filename)
	if config == nil {
		config = DefaultHTTPServerConfig()
	}

	// Create state pool
	pool, err := NewStatePool(filename, config.StatePoolSize)
	if err != nil {
		log.Fatalf("Failed to create state pool: %v", err)
	}
	fmt.Printf("Created state pool with %d workers\n", pool.Size())

	// Setup graceful shutdown
	setupGracefulShutdown(pool)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Get a state from the pool
		lua, err := pool.Get()
		if err != nil {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			logHTTPRequest(r, time.Now(), http.StatusServiceUnavailable, 0)
			return
		}
		defer pool.Put(lua) // Return state to pool when done

		// Create request context and wrap the response writer
		req := NewGolapisRequest(r)
		req.maxBodySize = config.ClientMaxBodySize
		wrappedWriter := req.WrapResponseWriter(w)

		// Create response channel for this request
		resp := make(chan *StateResponse, 1)

		// Send request to the state's event loop
		lua.eventChan <- &StateEvent{
			Type:         EventRunEntryPoint,
			OutputWriter: wrappedWriter,
			Request:      req,
			Response:     resp,
		}

		// Wait for request completion
		result := <-resp

		if result.Error != nil {
			http.Error(w, result.Error.Error(), http.StatusInternalServerError)
			logHTTPRequest(r, req.StartTime(), http.StatusInternalServerError, 0)
			return
		}

		// Apply response headers if not already sent (handles no-body case)
		req.FlushHeaders(w)

		// Log with actual status code
		status := req.ResponseStatus
		if status == 0 {
			status = http.StatusOK
		}
		logHTTPRequest(r, req.StartTime(), status, 0)
	})

	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// setupGracefulShutdown sets up signal handling for graceful shutdown
func setupGracefulShutdown(pool *StatePool) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Println("\nShutting down gracefully...")
		pool.Close()
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
