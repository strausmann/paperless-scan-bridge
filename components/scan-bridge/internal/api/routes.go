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

	// Readiness — real as of Phase 1.2h (issue #9): reports whether
	// profiles are loaded and sane-runtime answers its own health
	// check. Like /health, it is NOT behind requireBearer — a
	// monitoring probe should not need a bearer token.
	mux.HandleFunc("GET /ready", s.handleReady)

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

	// Panel firmware, mirrored from GitHub Releases (ADR 0024 decides
	// that updates come from the bridge, ADR 0025 that the bridge
	// mirrors them from GitHub Releases; issue #111).
	//
	// Deliberately NOT behind requireBearer, for two reasons:
	// the panel fetches the manifest before an operator has entered a
	// token (and would otherwise be unable to update its way out of a
	// broken configuration), and the content is a public release
	// binary anyone can download from GitHub with no credential at
	// all. There is nothing here a token would protect.
	//
	// The manifest gets its own handler because the bridge does not
	// serve it byte-for-byte: each build's `ota.path` is rewritten to
	// the generation-qualified route below, so the binary a panel
	// downloads is the one the manifest it read describes, even if a
	// newer release landed in between. The `md5` next to it is never
	// touched (ADR 0024).
	//
	// `GET /firmware/{name}` stays for "give me the newest", which is
	// what an operator with curl wants. Both bare-name paths resolve
	// through internal/firmware, which matches the name against the
	// cached release's own file list rather than joining it onto a
	// path — so there is exactly one place where a request-supplied
	// name meets the filesystem, and it is an allowlist.
	if s.Firmware != nil {
		mux.HandleFunc("GET /firmware/manifest.json", s.handleFirmwareManifest)
		mux.HandleFunc("GET /firmware/{name}", s.handleFirmwareFile)
		mux.HandleFunc("GET /firmware/{generation}/{name}", s.handleFirmwareVersionedFile)
		mux.HandleFunc("POST /firmware/refresh", s.handleFirmwareRefresh)
	} else {
		mux.Handle("GET /firmware/manifest.json", s.notImplemented("firmware mirror is disabled"))
		mux.Handle("GET /firmware/{name}", s.notImplemented("firmware mirror is disabled"))
		mux.Handle("GET /firmware/{generation}/{name}", s.notImplemented("firmware mirror is disabled"))
		mux.Handle("POST /firmware/refresh", s.notImplemented("firmware mirror is disabled"))
	}

	// Endpoints whose backing subsystem has not landed yet. They
	// return a uniform 501 envelope so clients see a stable schema.
	mux.Handle("GET /jobs", s.notImplemented("job store has not landed yet"))
	mux.Handle("GET /jobs/{id}", s.notImplemented("job store has not landed yet"))
	mux.Handle("POST /jobs/{id}/cancel", s.notImplemented("job store has not landed yet"))

	return s.loggingMiddleware(mux)
}
