package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golapis/golapis"
)

type headerFlags []string

func (h *headerFlags) String() string {
	return strings.Join(*h, ", ")
}

func (h *headerFlags) Set(value string) error {
	*h = append(*h, value)
	return nil
}

func main() {
	method := flag.String("method", "GET", "HTTP method (GET, POST, PUT, DELETE, etc.)")
	path := flag.String("path", "/", "Request path (e.g., /api/users?id=123)")
	body := flag.String("body", "", "Request body")
	bodyFile := flag.String("body-file", "", "Read request body from file")
	contentType := flag.String("content-type", "", "Content-Type header")
	execCode := flag.String("e", "", "Execute Lua code string instead of a file")
	ngxAlias := flag.Bool("ngx", false, "Alias golapis table to global ngx")
	showHeaders := flag.Bool("headers", false, "Show response headers")
	showStatus := flag.Bool("status", false, "Show response status code")
	quiet := flag.Bool("q", false, "Quiet mode - only show response body")
	timeout := flag.Duration("timeout", 30*time.Second, "Request timeout")

	var headers headerFlags
	flag.Var(&headers, "H", "Add header (can be used multiple times, format: 'Name: Value')")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [script.lua]\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Runs a Lua script through the HTTP handler and makes a single request to it.")
		fmt.Fprintln(os.Stderr, "Useful for testing HTTP-specific functionality without running a persistent server.\n")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nExamples:")
		fmt.Fprintln(os.Stderr, "  http-eval script.lua")
		fmt.Fprintln(os.Stderr, "  http-eval -e 'golapis.say(\"hello\")'")
		fmt.Fprintln(os.Stderr, "  echo 'golapis.say(\"hello\")' | http-eval -")
		fmt.Fprintln(os.Stderr, "  http-eval -method POST -body '{\"key\":\"value\"}' script.lua")
		fmt.Fprintln(os.Stderr, "  http-eval -path '/api/users?id=123' -H 'Authorization: Bearer token' script.lua")
	}

	flag.Parse()

	args := flag.Args()

	var filename string
	if len(args) > 0 {
		filename = args[0]
	}

	// Must have either -e or a filename
	if *execCode == "" && filename == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Check if file exists (when using file mode, skip for stdin)
	if filename != "" && filename != "-" {
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: script file not found: %s\n", filename)
			os.Exit(1)
		}
	}

	// Check for stdin conflict
	if filename == "-" && *body == "-" {
		fmt.Fprintln(os.Stderr, "Error: cannot read both script and body from stdin")
		os.Exit(1)
	}

	// Read body from stdin or file if specified
	requestBody := *body
	if *body == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading body from stdin: %v\n", err)
			os.Exit(1)
		}
		requestBody = string(data)
	} else if *bodyFile != "" {
		data, err := os.ReadFile(*bodyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading body file: %v\n", err)
			os.Exit(1)
		}
		requestBody = string(data)
	}

	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding available port: %v\n", err)
		os.Exit(1)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Create Lua state
	lua := golapis.NewGolapisLuaState()
	if lua == nil {
		fmt.Fprintln(os.Stderr, "Failed to create Lua state")
		os.Exit(1)
	}
	defer lua.Close()

	if *ngxAlias {
		lua.SetupNgxAlias()
	}

	// Preload the entrypoint
	if *execCode != "" {
		if err := lua.PreloadEntryPointString(*execCode); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load Lua code: %v\n", err)
			os.Exit(1)
		}
	} else if filename == "-" {
		stdinBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(1)
		}
		if err := lua.PreloadEntryPointString(string(stdinBytes)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load Lua code from stdin: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := lua.PreloadEntryPointFile(filename); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load Lua script %s: %v\n", filename, err)
			os.Exit(1)
		}
	}

	lua.Start()
	defer lua.Stop()

	// Start HTTP server in background
	serverReady := make(chan struct{})
	serverErr := make(chan error, 1)

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: lua.HTTPHandler(nil),
	}

	go func() {
		ln, err := net.Listen("tcp", server.Addr)
		if err != nil {
			serverErr <- err
			return
		}
		close(serverReady)
		if err := server.Serve(ln); err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for server to be ready or error
	select {
	case <-serverReady:
		// Server is ready
	case err := <-serverErr:
		fmt.Fprintf(os.Stderr, "Server failed to start: %v\n", err)
		os.Exit(1)
	case <-time.After(5 * time.Second):
		fmt.Fprintln(os.Stderr, "Timeout waiting for server to start")
		os.Exit(1)
	}

	// Build the request URL
	requestPath := *path
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, requestPath)

	// Create the HTTP request
	var bodyReader io.Reader
	if requestBody != "" {
		bodyReader = bytes.NewBufferString(requestBody)
	}

	req, err := http.NewRequest(strings.ToUpper(*method), url, bodyReader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating request: %v\n", err)
		os.Exit(1)
	}

	// Add content-type if specified
	if *contentType != "" {
		req.Header.Set("Content-Type", *contentType)
	}

	// Add custom headers
	for _, h := range headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		} else {
			fmt.Fprintf(os.Stderr, "Warning: invalid header format: %s (expected 'Name: Value')\n", h)
		}
	}

	// Make the request
	client := &http.Client{
		Timeout: *timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error making request: %v\n", err)
		server.Close()
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
		server.Close()
		os.Exit(1)
	}

	// Output results
	if !*quiet {
		if *showStatus {
			fmt.Printf("Status: %d %s\n", resp.StatusCode, resp.Status)
		}
		if *showHeaders {
			fmt.Println("Headers:")
			for name, values := range resp.Header {
				for _, v := range values {
					fmt.Printf("  %s: %s\n", name, v)
				}
			}
			if len(respBody) > 0 {
				fmt.Println()
			}
		}
	}

	// Always output body (unless empty)
	if len(respBody) > 0 {
		os.Stdout.Write(respBody)
	}

	// Shutdown server
	server.Close()
}
