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

// StartHTTPServer starts an HTTP server that executes the given Lua script for each request
// Uses a single shared GolapisLuaState for all requests with cooperative scheduling
func StartHTTPServer(filename, port string) {
	fmt.Printf("Starting HTTP server on port %s with script: %s\n", port, filename)

	// Create single shared Lua state at server startup
	lua := NewGolapisLuaState()
	if lua == nil {
		log.Fatal("Failed to create Lua state")
	}

	// Preload the entrypoint file at startup
	if err := lua.PreloadEntryPointFile(filename); err != nil {
		lua.Close()
		log.Fatalf("Failed to load Lua script %s: %v", filename, err)
	}

	// Start the event loop (runs for lifetime of server)
	lua.Start()

	// Setup graceful shutdown
	setupGracefulShutdown(lua)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create request context and wrap the response writer
		req := NewGolapisRequest(r)
		wrappedWriter := req.WrapResponseWriter(w)

		// Create response channel for this request
		resp := make(chan *StateResponse, 1)

		// Send request to shared Lua state's event loop
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
			logHTTPRequest(r, http.StatusInternalServerError, 0, time.Since(start))
			return
		}

		// Apply response headers if not already sent (handles no-body case)
		req.FlushHeaders(w)

		logHTTPRequest(r, http.StatusOK, 0, time.Since(start))
	})

	log.Fatal(http.ListenAndServe(":"+port, nil))
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
func logHTTPRequest(r *http.Request, status int, bodyBytes int64, duration time.Duration) {
	remoteAddr := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		remoteAddr = forwarded
	}

	remoteUser := "-"
	if r.URL.User != nil && r.URL.User.Username() != "" {
		remoteUser = r.URL.User.Username()
	}

	timeLocal := time.Now().Format("02/Jan/2006:15:04:05 -0700")
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
