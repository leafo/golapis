package golapis

import (
	"errors"
	"io"
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

// Write implements io.Writer, flushing headers before the first write.
func (w *headerFlushingWriter) Write(data []byte) (int, error) {
	w.request.FlushHeaders(w.ResponseWriter)
	return w.ResponseWriter.Write(data)
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
