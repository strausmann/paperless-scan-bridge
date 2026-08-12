package scanapi

import "net/http"

// Router returns the HTTP handler for the sane-runtime API surface.
// This is served over the Unix-domain socket described in ADR 0009,
// not TCP — main.go owns the listener.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("POST /scan", s.handleScan)

	return mux
}
