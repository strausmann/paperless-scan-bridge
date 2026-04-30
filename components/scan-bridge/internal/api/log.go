package api

import (
	"log/slog"
	"net"
	"net/http"
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

// clientIP returns the source IP from the TCP connection.
//
// X-Forwarded-For and X-Real-IP are deliberately not consulted here:
// they are caller-controlled and trusting them without a configured
// list of trusted proxies turns IP-based authentication (the
// `ip_allowlist` mode in CONTAINER_SUITE.md sec. 4.5) into trivially
// spoofable access. The auth middleware that consumes IPs lands in
// Phase 1.4 together with a `trusted_proxies` config option; only
// then does it become safe to honour the forwarded headers, and only
// when the immediate peer is in that allowlist.
//
// Until then this function returns RemoteAddr's host part for every
// caller — the only IP value that has been TLS/TCP-authenticated by
// the kernel.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
