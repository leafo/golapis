package golapis

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// StartHTTPServer starts an HTTP server that executes the given Lua script for each request
func StartHTTPServer(filename, port string) {
	fmt.Printf("Starting HTTP server on port %s with script: %s\n", port, filename)

	// Load the Lua file once at startup to validate it
	templateLua := NewLuaState()
	if templateLua == nil {
		log.Fatal("Failed to create template Lua state")
	}
	defer templateLua.Close()

	if err := templateLua.LoadFile(filename); err != nil {
		log.Fatal(fmt.Sprintf("Error loading Lua file: %v", err))
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lua := NewLuaState()
		if lua == nil {
			http.Error(w, "Failed to create Lua state", http.StatusInternalServerError)
			logHTTPRequest(r, http.StatusInternalServerError, 0, time.Since(start))
			return
		}
		defer lua.Close()

		// Set up output writer to send response to HTTP client
		lua.SetOutputWriter(w)

		// Load and execute the Lua file
		if err := lua.LoadFile(filename); err != nil {
			http.Error(w, fmt.Sprintf("Error loading Lua file: %v", err), http.StatusInternalServerError)
			logHTTPRequest(r, http.StatusInternalServerError, 0, time.Since(start))
			return
		}

		if err := lua.CallLoadedAsCoroutine(); err != nil {
			http.Error(w, fmt.Sprintf("Error executing Lua code: %v", err), http.StatusInternalServerError)
			logHTTPRequest(r, http.StatusInternalServerError, 0, time.Since(start))
			return
		}

		logHTTPRequest(r, http.StatusOK, 0, time.Since(start))
	})

	log.Fatal(http.ListenAndServe(":"+port, nil))
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
