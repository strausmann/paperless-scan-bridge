package api

import (
	"net/http"
	"time"
)

// statusRecorder wraps http.ResponseWriter so the logging middleware
// can observe the response status without changing handler behaviour.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

// loggingMiddleware records one structured log line per request.
// The schema mirrors CONTAINER_SUITE.md sec. 4.9 (subset — trace_id
// and job_id propagation arrive with the dispatch subsystem).
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		s.Logger.LogAttrs(r.Context(), levelForStatus(rec.status),
			"http request",
			attr("method", r.Method),
			attr("path", r.URL.Path),
			attr("status", rec.status),
			attr("bytes", rec.bytes),
			attr("source_ip", clientIP(r)),
			attr("duration_ms", time.Since(start).Milliseconds()),
		)
	})
}
