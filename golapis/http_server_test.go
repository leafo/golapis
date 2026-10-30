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

func TestParseHumanDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"30", 30 * time.Second, false},
		{"0", 0, false},
		{"500ms", 500 * time.Millisecond, false},
		{"45s", 45 * time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"2h", 2 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{" 10s ", 10 * time.Second, false},
		{"", 0, true},
		{"s", 0, true},
		{"abc", 0, true},
		{"-5s", 0, true},
		{"1.5s", 0, true},
		{"1m30s", 0, true},
		{"5M", 0, true},
		{"10us", 0, true},
		{"+5s", 0, true},
		{"9223372036854775807s", 0, true}, // overflow
	}
	for _, c := range cases {
		got, err := ParseHumanDuration(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseHumanDuration(%q) = %v, want error", c.in, got)
			}
		} else if err != nil || got != c.want {
			t.Errorf("ParseHumanDuration(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
}

func TestNormalizeHTTPServerConfigKeepsNoTimeout(t *testing.T) {
	config := normalizeHTTPServerConfig(&HTTPServerConfig{
		ReadHeaderTimeout: NoTimeout,
		WriteTimeout:      NoTimeout,
	})

	if config.ReadHeaderTimeout != NoTimeout || config.WriteTimeout != NoTimeout {
		t.Fatalf("NoTimeout was replaced: %+v", config)
	}
	if config.ReadTimeout != DefaultReadTimeout {
		t.Fatalf("ReadTimeout = %v, want default", config.ReadTimeout)
	}
	if again := normalizeHTTPServerConfig(config); again.WriteTimeout != NoTimeout {
		t.Fatal("second normalize replaced NoTimeout")
	}
}

func TestServerReadHeaderTimeoutDisable(t *testing.T) {
	if got := serverReadHeaderTimeout(NoTimeout); got != 0 {
		t.Fatalf("serverReadHeaderTimeout(NoTimeout) = %v, want 0", got)
	}
	if got := serverReadHeaderTimeout(5 * time.Second); got != 5*time.Second {
		t.Fatalf("serverReadHeaderTimeout(5s) = %v", got)
	}
}

func TestListenURL(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"[::1]:8080", "http://[::1]:8080"},
		{"0.0.0.0:8080", "http://localhost:8080 (all interfaces)"},
		{"[::]:8080", "http://localhost:8080 (all interfaces)"},
	}
	for _, tt := range tests {
		addr, err := net.ResolveTCPAddr("tcp", tt.addr)
		if err != nil {
			t.Fatalf("ResolveTCPAddr(%q): %v", tt.addr, err)
		}
		if got := listenURL(addr); got != tt.want {
			t.Errorf("listenURL(%q) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}
