package api

import "net/http"

// Router returns the HTTP handler for the public API surface.
//
// /metrics is intentionally NOT registered here; main.go exposes the
// Prometheus collectors on the dedicated metrics listener so a
// reverse-proxy / firewall can keep that surface internal.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	// Liveness and version — always real, always cheap.
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /version", s.handleVersion)

	// Profiles — read-only, served straight out of the in-memory Set.
	mux.HandleFunc("GET /profiles", s.handleProfilesList)
	mux.HandleFunc("GET /profiles/{name}", s.handleProfileDetail)

	// POST /scan (ADR 0005) is real as of Phase 1.2: it dispatches to
	// sane-runtime via s.Dispatch and returns the finished result
	// inline (see scan.go for the documented deviation from the
	// originally planned async 202 flow). It is the only endpoint
	// behind the bearer/IP-allowlist auth middleware (ADR 0006, D5 in
	// the implementation brief) — the rest of the surface stays open.
	mux.Handle("POST /scan", s.requireBearer(http.HandlerFunc(s.handleScan)))

	// Endpoints whose backing subsystem has not landed yet. They
	// return a uniform 501 envelope so clients see a stable schema.
	mux.Handle("GET /ready", s.notImplemented("readiness probe needs dispatch subsystem"))
	mux.Handle("GET /jobs", s.notImplemented("job store has not landed yet"))
	mux.Handle("GET /jobs/{id}", s.notImplemented("job store has not landed yet"))
	mux.Handle("POST /jobs/{id}/cancel", s.notImplemented("job store has not landed yet"))

	return s.loggingMiddleware(mux)
}
