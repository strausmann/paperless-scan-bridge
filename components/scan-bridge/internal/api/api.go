// Package api wires the HTTP surface of the scan-bridge daemon.
//
// The route table mirrors CONTAINER_SUITE.md sec. 4.4. Endpoints whose
// real behaviour depends on the sane-runtime / scan-processor
// containers (or on the not-yet-implemented job store) currently
// answer 501 Not Implemented. They are present in the route table so
// callers see a consistent surface and so the OpenAPI document stays
// honest about what exists; their bodies will be filled in by the
// dispatch and jobs subsystems in subsequent Phase 1 sessions.
package api

import (
	"log/slog"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/dispatch"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/procclient"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
)

// BuildInfo captures the immutable identity of this binary as
// stamped in by main.go via -ldflags.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// Server bundles the long-lived collaborators the HTTP handlers need.
//
// It is intentionally a small value type — handlers reach into it for
// dependencies, but the Server itself does not own goroutines or
// resources beyond the supplied Logger. Lifecycle (Listen / Shutdown)
// is owned by main.go.
type Server struct {
	Profiles *profiles.Set
	Build    BuildInfo
	Logger   *slog.Logger

	// Auth configures the requireBearer middleware (auth.go) guarding
	// POST /scan, per ADR 0006. The zero value (empty Mode, empty
	// TokenHash) rejects every request — there is no "auth disabled"
	// state.
	Auth config.AuthConfig
	// Dispatch is the client to sane-runtime (ADR 0009) that
	// handleScan (scan.go) calls to actually run a scan. Tests supply
	// an in-memory fake; main.go wires dispatch.NewHTTPUnixClient.
	Dispatch dispatch.Client
	// ProcClient is the client to scan-processor (design doc
	// docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
	// sec. 4.2/9 Task 5/7) that handleScan calls after a successful
	// dispatch to run OCR/deskew/blank-removal/rotation/format-
	// conversion/assembly on the scanned pages. Tests supply an
	// in-memory fake; main.go wires procclient.NewHTTPUnixClient.
	ProcClient procclient.Client
	// Secrets resolves named secrets (e.g. a Paperless API token) for
	// destination modules built via destinations.Build (design doc
	// sec. 5.3). main.go wires config.NewSecretResolver over
	// /run/secrets and os.LookupEnv; tests supply a resolver backed by
	// an in-memory map.
	Secrets config.SecretResolver
	// OutputDir is where the dispatch client writes completed scan
	// pages, and where the ProcClient writes the assembled documents
	// scan-processor returns (config.PathsConfig.OutputDir). handleScan
	// (scan.go) removes OutputDir/<scan_id> after every request unless
	// KeepScanOutput is set (issue #49 point 1) — both dispatch and
	// procclient key their subdirectory off the same scan_id.
	OutputDir string
	// KeepScanOutput disables that post-request cleanup. false (the
	// zero value, matching config.Default()'s paths.keep_scan_output)
	// means "clean up" — the pipeline processes receipts/invoices
	// (PII), so leaving them on disk indefinitely after every request
	// is an unbounded local-accumulation risk.
	KeepScanOutput bool
	// MaxRequestBytes bounds the size of an inbound POST /scan request
	// body via http.MaxBytesReader (handleScan, issue #47 hardening).
	// Zero/negative falls back to config.DefaultMaxRequestBytes — a
	// Server built directly (as most tests in this package do) rather
	// than through config.Load must still enforce SOME bound.
	MaxRequestBytes int64
}

// maxRequestBytes returns the effective POST /scan request-body limit
// handleScan enforces via http.MaxBytesReader.
func (s *Server) maxRequestBytes() int64 {
	if s.MaxRequestBytes > 0 {
		return s.MaxRequestBytes
	}
	return config.DefaultMaxRequestBytes
}
