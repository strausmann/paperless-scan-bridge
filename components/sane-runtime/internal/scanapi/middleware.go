package scanapi

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder wraps http.ResponseWriter so the logging middleware can
// observe the status and the response size without changing what the
// handler does.
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
	// A handler that writes without calling WriteHeader gets an implicit
	// 200 from net/http; record the same so the log never shows 0.
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

// levelForStatus keeps failures out of the noise floor: an operator
// filtering at warn level still sees every 4xx and 5xx.
func levelForStatus(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// loggingMiddleware records one structured line per request.
//
// Why sane-runtime needs this at all: until it existed, only failures
// were logged, so a successful request produced nothing. Reading the
// container's logs after a scan showed only the startup line, and there
// was no way to tell "the request never reached this container" (a
// socket or permissions problem) from "it arrived and the scanner is
// simply slow". The only evidence was scan-bridge's own duration figure
// on the other side of the socket, which is inference, not proof.
//
// The schema mirrors scan-bridge's loggingMiddleware deliberately, so a
// single scan reads as two corresponding lines across the two
// containers and the time spent in each is directly comparable.
//
// One field is deliberately NOT mirrored: source_ip. This server is
// reached only over a Unix-domain socket (ADR 0009), where RemoteAddr
// carries no meaningful peer address — it would log a constant or an
// empty string on every line and invite someone to treat it as an
// identity.
//
// Timing covers the whole handler, which for POST /scan includes
// streaming a multipart body of several megabytes. That is the point:
// stopping the timer when the header is written would report a few
// milliseconds for a twenty-second scan.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		s.logger().LogAttrs(r.Context(), levelForStatus(rec.status),
			"http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int("bytes", rec.bytes),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	})
}
