package golapis

import (
	"bufio"
	"bytes"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogHTTPRequestsCapturesStatusAndBytes(t *testing.T) {
	var logBuf bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	}()

	handler := logHTTPRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	}), false, nil)

	req := httptest.NewRequest("GET", "/coffee", nil)
	req.RemoteAddr = "198.51.100.10:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.20")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	logLine := logBuf.String()
	if !strings.Contains(logLine, `198.51.100.10:12345`) {
		t.Fatalf("log line should use direct remote address, got: %q", logLine)
	}
	if strings.Contains(logLine, `203.0.113.20`) {
		t.Fatalf("log line should not trust X-Forwarded-For by default, got: %q", logLine)
	}
	if !strings.Contains(logLine, `"GET /coffee HTTP/1.1" 418 5`) {
		t.Fatalf("log line should include actual status and bytes, got: %q", logLine)
	}
}

func TestRequestRemoteAddrTrustProxyHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.10:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.20, 203.0.113.21")

	if got := requestRemoteAddr(req, false); got != "198.51.100.10:12345" {
		t.Fatalf("requestRemoteAddr without proxy trust = %q", got)
	}
	if got := requestRemoteAddr(req, true); got != "203.0.113.20" {
		t.Fatalf("requestRemoteAddr with proxy trust = %q", got)
	}
}

func TestDefaultHTTPServerConfigHasProductionTimeouts(t *testing.T) {
	config := DefaultHTTPServerConfig()

	if config.ReadHeaderTimeout <= 0 {
		t.Fatal("ReadHeaderTimeout should be enabled by default")
	}
	if config.ReadTimeout <= 0 {
		t.Fatal("ReadTimeout should be enabled by default")
	}
	if config.WriteTimeout <= 0 {
		t.Fatal("WriteTimeout should be enabled by default")
	}
	if config.IdleTimeout <= 0 {
		t.Fatal("IdleTimeout should be enabled by default")
	}
	if config.ShutdownTimeout <= 0 {
		t.Fatal("ShutdownTimeout should be enabled by default")
	}
	if config.MaxHeaderBytes <= 0 {
		t.Fatal("MaxHeaderBytes should be enabled by default")
	}
}

type hijackableRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (r *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	r.hijacked = true
	return nil, nil, nil
}

func TestResponseStatsWriterHijackerPassthrough(t *testing.T) {
	recorder := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	statsWriter := newResponseStatsWriter(recorder)

	_, _, err := statsWriter.Hijack()
	if err != nil {
		t.Fatalf("Hijack returned error: %v", err)
	}
	if !recorder.hijacked {
		t.Fatal("underlying writer was not hijacked")
	}
}

func TestResponseStatsWriterHijackerUnsupported(t *testing.T) {
	statsWriter := newResponseStatsWriter(httptest.NewRecorder())

	_, _, err := statsWriter.Hijack()
	if !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("Hijack error = %v, want http.ErrNotSupported", err)
	}
}

func TestLogHTTPRequestsWaitGroupTracksActiveRequest(t *testing.T) {
	var requestWg sync.WaitGroup
	started := make(chan struct{})
	release := make(chan struct{})
	handler := logHTTPRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}), false, &requestWg)

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	<-started

	go func() {
		requestWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("request WaitGroup completed while handler was still active")
	case <-time.After(10 * time.Millisecond):
	}

	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request WaitGroup did not complete after handler returned")
	}
}
