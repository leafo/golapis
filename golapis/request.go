package golapis

import (
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// ErrBodyTooLarge is returned when the request body exceeds the maximum size
var ErrBodyTooLarge = errors.New("request body too large")

// GolapisRequest wraps an HTTP request and holds request processing state
type GolapisRequest struct {
	Request         *http.Request // The underlying HTTP request
	ResponseHeaders http.Header   // Accumulated response headers
	ResponseStatus  int           // HTTP status code (0 = not set, defaults to 200)
	HeadersSent     bool          // True after first body write
	startTime       time.Time     // When request was created

	// Body caching (body can only be read once from Go's Request.Body)
	bodyRead bool   // Whether body has been read
	bodyData []byte // Cached body content
	bodyErr  error  // Error from reading body (if any)

	// Configuration
	maxBodySize int64 // max body size in bytes (0 = unlimited)

	hijackedConn net.Conn // connection taken over by req.socket(true), if any
}

// Hijacked reports whether req.socket(true) has taken over the connection
func (r *GolapisRequest) Hijacked() bool {
	return r.hijackedConn != nil
}

// NewGolapisRequest creates a new GolapisRequest from an http.Request
func NewGolapisRequest(r *http.Request) *GolapisRequest {
	return &GolapisRequest{
		Request:         r,
		ResponseHeaders: make(http.Header),
		HeadersSent:     false,
		startTime:       time.Now(),
	}
}

// StartTime returns the timestamp when the request was created
func (r *GolapisRequest) StartTime() time.Time {
	return r.startTime
}

// FlushHeaders writes accumulated response headers and status to the given ResponseWriter
// if they haven't been sent yet. Returns true if headers were flushed.
func (r *GolapisRequest) FlushHeaders(w http.ResponseWriter) bool {
	if r.HeadersSent {
		return false
	}
	for key, values := range r.ResponseHeaders {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	// Write status code (default to 200 if not set)
	status := r.ResponseStatus
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	r.HeadersSent = true
	return true
}

// headerFlushingWriter wraps an http.ResponseWriter to automatically flush
// accumulated response headers on the first write.
type headerFlushingWriter struct {
	http.ResponseWriter
	request *GolapisRequest
}

// ErrRawSocketAcquired is returned by response output functions once
// req.socket(true) has taken over the connection
var ErrRawSocketAcquired = errors.New("raw request socket acquired")

// ErrHijackNotSupported is returned by req.socket(true) when there is no
// underlying TCP connection to take over (HTTP/2, captured subrequests, or
// a ResponseWriter without http.Hijacker)
var ErrHijackNotSupported = errors.New("connection does not support hijacking")

// Write implements io.Writer, flushing headers before the first write.
func (w *headerFlushingWriter) Write(data []byte) (int, error) {
	if w.request.Hijacked() {
		return 0, ErrRawSocketAcquired
	}
	w.request.FlushHeaders(w.ResponseWriter)
	return w.ResponseWriter.Write(data)
}

// Hijack takes over the underlying TCP connection. Like ngx.req.socket(true),
// nothing is sent on the caller's behalf: if the response headers haven't
// gone out, the socket is bare and the script writes the whole response
// itself. If they have, any buffered body is flushed first. The returned
// bytes are request data the HTTP server had already buffered and must be
// consumed before reading from conn.
func (w *headerFlushingWriter) Hijack() (net.Conn, []byte, error) {
	if w.request.Hijacked() {
		return nil, nil, errors.New("duplicate call")
	}
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	// Wrappers may forward Hijack and only fail at call time; HTTP/2 is the
	// common case and can be rejected up front
	if !ok || w.request.Request.ProtoMajor >= 2 {
		return nil, nil, ErrHijackNotSupported
	}

	if w.request.HeadersSent {
		// net/http chunk-frames a body-bearing HTTP/1.1 response that has no
		// Content-Length; raw writes can't continue that stream
		if w.request.responseIsChunked() {
			return nil, nil, errors.New("response headers already sent with chunked encoding; set Content-Length or Transfer-Encoding: identity before sending them")
		}
		w.Flush()
	}

	conn, brw, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	w.request.hijackedConn = conn
	// Suppress the automatic response; the script owns the wire now
	w.request.HeadersSent = true

	var buffered []byte
	if n := brw.Reader.Buffered(); n > 0 {
		peeked, _ := brw.Reader.Peek(n)
		buffered = append([]byte(nil), peeked...)
	}
	return conn, buffered, nil
}

// SendHeaders flushes the accumulated headers to the client without writing
// any body.
func (w *headerFlushingWriter) SendHeaders() {
	w.keepUnreadBody()
	w.request.FlushHeaders(w.ResponseWriter)
}

// keepUnreadBody stops net/http from discarding an unread request body when
// the response headers are written, so it stays readable afterwards (by
// read_body, or from a raw socket). The cost is that the connection isn't
// kept alive unless the body does get read.
func (w *headerFlushingWriter) keepUnreadBody() {
	r := w.request
	if r.HeadersSent || r.bodyRead || r.Request.ContentLength == 0 {
		return
	}
	// Not supported by every writer (e.g. httptest.ResponseRecorder); then
	// there is nothing to protect
	http.NewResponseController(w.ResponseWriter).EnableFullDuplex()
}

// Flush sends headers if needed and pushes any buffered response data to
// the client, if the underlying writer supports it.
func (w *headerFlushingWriter) Flush() {
	w.SendHeaders()
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// responseIsChunked reports whether net/http will apply chunked
// Transfer-Encoding to this response (mirrors its bodyAllowedForStatus rule).
func (r *GolapisRequest) responseIsChunked() bool {
	status := r.ResponseStatus
	if status == 0 {
		status = http.StatusOK
	}
	bodyAllowed := !(status >= 100 && status <= 199) &&
		status != http.StatusNoContent && status != http.StatusNotModified
	if !bodyAllowed || !r.Request.ProtoAtLeast(1, 1) || r.Request.Method == "HEAD" {
		return false
	}
	h := r.ResponseHeaders
	return h.Get("Content-Length") == "" && h.Get("Transfer-Encoding") != "identity"
}

// WrapResponseWriter creates a headerFlushingWriter that will apply
// accumulated headers from the GolapisRequest on first write.
func (r *GolapisRequest) WrapResponseWriter(w http.ResponseWriter) *headerFlushingWriter {
	return &headerFlushingWriter{
		ResponseWriter: w,
		request:        r,
	}
}

// readBodyLimited reads the full request body, enforcing maxBodySize
// (0 = unlimited) and closing the body when done. It is called from a
// goroutine off the event loop, so it must not touch GolapisRequest fields;
// results are stored on the request by the caller's resume handler.
func readBodyLimited(body io.ReadCloser, maxBodySize int64) ([]byte, error) {
	defer body.Close()

	// Use LimitedReader to cap the amount we read
	var reader io.Reader = body
	if maxBodySize > 0 {
		// Read up to maxBodySize + 1 to detect overflow
		reader = io.LimitReader(body, maxBodySize+1)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	// Check if we hit the limit (read more than maxBodySize)
	if maxBodySize > 0 && int64(len(data)) > maxBodySize {
		return nil, ErrBodyTooLarge
	}

	return data, nil
}

// BodyWasRead returns true if ReadBody has been called
func (r *GolapisRequest) BodyWasRead() bool {
	return r.bodyRead
}

// GetBody returns the cached body data (nil if not yet read)
func (r *GolapisRequest) GetBody() []byte {
	return r.bodyData
}
