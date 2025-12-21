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

// FlushHeaders writes accumulated response headers to the given ResponseWriter
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

// ReadBody reads and caches the request body. Safe to call multiple times.
// Returns the cached body data and any error from the initial read.
// If the body exceeds maxBodySize, returns ErrBodyTooLarge.
func (r *GolapisRequest) ReadBody() ([]byte, error) {
	if r.bodyRead {
		return r.bodyData, r.bodyErr
	}

	r.bodyRead = true
	if r.Request.Body == nil {
		r.bodyData = nil
		r.bodyErr = nil
		return nil, nil
	}

	defer r.Request.Body.Close()

	// Check Content-Length header first for early rejection
	if r.maxBodySize > 0 && r.Request.ContentLength > r.maxBodySize {
		r.bodyData = nil
		r.bodyErr = ErrBodyTooLarge
		return nil, r.bodyErr
	}

	// Use LimitedReader to cap the amount we read
	var reader io.Reader = r.Request.Body
	if r.maxBodySize > 0 {
		// Read up to maxBodySize + 1 to detect overflow
		reader = io.LimitReader(r.Request.Body, r.maxBodySize+1)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		r.bodyData = nil
		r.bodyErr = err
		return nil, err
	}

	// Check if we hit the limit (read more than maxBodySize)
	if r.maxBodySize > 0 && int64(len(data)) > r.maxBodySize {
		r.bodyData = nil
		r.bodyErr = ErrBodyTooLarge
		return nil, r.bodyErr
	}

	r.bodyData = data
	r.bodyErr = nil
	return r.bodyData, nil
}

// BodyWasRead returns true if ReadBody has been called
func (r *GolapisRequest) BodyWasRead() bool {
	return r.bodyRead
}

// GetBody returns the cached body data (nil if not yet read)
func (r *GolapisRequest) GetBody() []byte {
	return r.bodyData
}
