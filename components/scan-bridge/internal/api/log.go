package api

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// attr is a tiny adapter so handlers can compose slog.Attrs with less
// noise than calling slog.Any/slog.Int/... per field.
func attr(key string, value any) slog.Attr {
	return slog.Any(key, value)
}

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

// clientIP extracts the best available source IP from the request.
//
// Prefers the leftmost X-Forwarded-For value when running behind a
// reverse proxy, then X-Real-IP, then RemoteAddr. The result is a
// bare IP string, suitable for log fields.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if comma := strings.IndexByte(xff, ','); comma >= 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return strings.TrimSpace(real)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
