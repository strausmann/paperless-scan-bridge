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
	// scan-processor returns (config.PathsConfig.OutputDir). Server
	// does not use it directly today — it is threaded through so
	// main.go has one place to wire dispatch.NewHTTPUnixClient's and
	// procclient.NewHTTPUnixClient's outputDir argument from the same
	// config value the rest of the daemon reports in Description() —
	// but a future job-store/cleanup subsystem will.
	OutputDir string
}
