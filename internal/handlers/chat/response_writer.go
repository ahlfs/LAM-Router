package chat

import (
	"net/http"
	"sync/atomic"
)

// committedResponseWriter wraps http.ResponseWriter to track whether
// response headers have been sent to the client (i.e., WriteHeader or
// first Write has been called). This is used by fallback logic to
// determine if a retry is safe.
type committedResponseWriter struct {
	http.ResponseWriter
	committed int32 // atomic: 1 if headers have been sent
	recordedBody   []byte
	recordedChunks [][]byte
}

func newCommittedResponseWriter(w http.ResponseWriter) *committedResponseWriter {
	return &committedResponseWriter{ResponseWriter: w}
}

func (cw *committedResponseWriter) WriteHeader(code int) {
	atomic.StoreInt32(&cw.committed, 1)
	cw.ResponseWriter.WriteHeader(code)
}

func (cw *committedResponseWriter) Write(b []byte) (int, error) {
	atomic.StoreInt32(&cw.committed, 1)
	cw.recordedBody = append(cw.recordedBody, b...)
	chunkCopy := make([]byte, len(b))
	copy(chunkCopy, b)
	cw.recordedChunks = append(cw.recordedChunks, chunkCopy)
	return cw.ResponseWriter.Write(b)
}

// IsCommitted returns true if response headers have been sent.
func (cw *committedResponseWriter) IsCommitted() bool {
	return atomic.LoadInt32(&cw.committed) == 1
}

// Flush implements http.Flusher if the underlying writer supports it.
func (cw *committedResponseWriter) Flush() {
	if f, ok := cw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
