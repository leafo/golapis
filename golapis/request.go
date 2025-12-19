package golapis

import "net/http"

// GolapisRequest wraps an HTTP request and holds request processing state
type GolapisRequest struct {
	Request         *http.Request // The underlying HTTP request
	ResponseHeaders http.Header   // Accumulated response headers
	HeadersSent     bool          // True after first body write
}

// NewGolapisRequest creates a new GolapisRequest from an http.Request
func NewGolapisRequest(r *http.Request) *GolapisRequest {
	return &GolapisRequest{
		Request:         r,
		ResponseHeaders: make(http.Header),
		HeadersSent:     false,
	}
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
