package procapi

import "net/http"

// Router returns the HTTP handler for the scan-processor API surface.
// This is served over the Unix-domain socket described in design doc
// sec. 4.2, not TCP — main.go owns the listener.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /process", s.handleProcess)

	return mux
}
