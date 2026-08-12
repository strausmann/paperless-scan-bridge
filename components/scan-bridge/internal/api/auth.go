package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
)

// requireBearer wraps next with the daemon auth check (ADR 0006): in
// AuthModeIPAllowlist, the caller's source IP must fall into
// Auth.AllowedCIDRs; otherwise the caller must present a bearer token
// whose SHA-256 hex digest matches Auth.TokenHash. It currently guards
// only POST /scan (routes.go) per D5 in the Phase 1.2 implementation
// brief — the rest of the surface (health, version, profiles) stays
// open, matching Phase 1.1 behaviour.
func (s *Server) requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Auth.Mode == config.AuthModeIPAllowlist {
			if s.sourceIPAllowed(r) {
				next.ServeHTTP(w, r)
				return
			}
			s.writeUnauthorized(w, r)
			return
		}

		token, ok := bearerToken(r)
		if !ok || !tokenMatches(token, s.Auth.TokenHash) {
			s.writeUnauthorized(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sourceIPAllowed re-parses clientIP's string form (log.go) into a
// net.IP so it can be checked against Auth.AllowedCIDRs. clientIP is
// deliberately not header-aware (see log.go's comment on
// X-Forwarded-For) — the same "only the TCP peer is authenticated"
// property that makes it safe for logging makes it the only source of
// truth this middleware can trust for the ip_allowlist auth mode.
func (s *Server) sourceIPAllowed(r *http.Request) bool {
	ip := net.ParseIP(clientIP(r))
	if ip == nil {
		return false
	}
	return s.Auth.IPAllowed(ip)
}

// bearerToken extracts the token from a "Bearer <token>"
// Authorization header. It reports false if the header is absent, has
// a different scheme, or the token is empty after trimming.
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}

// tokenMatches reports whether token's SHA-256 hex digest equals
// wantHash, using a constant-time comparison so response timing does
// not leak how many hex characters matched. An empty wantHash (auth
// configured, but no token ever set) never matches — there is no
// "any token is fine" state.
func tokenMatches(token, wantHash string) bool {
	if wantHash == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	got := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(wantHash)) == 1
}

// writeUnauthorized sends the 401 response with the WWW-Authenticate
// challenge RFC 7235 sec. 4.1 requires for a Bearer-scheme resource.
func (s *Server) writeUnauthorized(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="scan-bridge"`)
	s.writeJSON(w, r, http.StatusUnauthorized, errorResponse{Error: "unauthorized"})
}
