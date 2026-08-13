package api

import (
	"context"
	"net/http"
	"time"
)

// readyPingTimeout bounds how long handleReady waits for sane-runtime
// to answer its own health check before treating it as unreachable.
// It stays well under typical client/orchestrator probe timeouts so
// /ready fails fast instead of hanging.
const readyPingTimeout = 3 * time.Second

// readyResponse is the 200 OK payload for GET /ready.
type readyResponse struct {
	Status string `json:"status"`
}

// handleReady reports whether scan-bridge is ready to serve POST
// /scan: profiles must be loaded AND sane-runtime must answer its
// health check within readyPingTimeout.
//
// The profiles check runs first and needs no network round trip — an
// empty profile set means every scan would 404 regardless of
// sane-runtime's state. Checking it first also means a nil/unwired
// Dispatch (e.g. before main.go finishes constructing the Server) is
// never dereferenced in that state; profiles.Set.Len() is documented
// nil-safe (internal/profiles/profiles.go), so this handler tolerates
// a nil Profiles too.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.Profiles.Len() == 0 {
		s.writeJSON(w, r, http.StatusServiceUnavailable, errorResponse{
			Error: "no_profiles_loaded",
			Hint:  "No scan profiles are configured; check internal/profiles/defaults.yaml.",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), readyPingTimeout)
	defer cancel()
	if err := s.Dispatch.Ping(ctx); err != nil {
		s.writeJSON(w, r, http.StatusServiceUnavailable, errorResponse{
			Error: "sane_runtime_unreachable",
			Hint:  "sane-runtime did not answer its health check in time.",
		})
		return
	}

	s.writeJSON(w, r, http.StatusOK, readyResponse{Status: "ready"})
}
