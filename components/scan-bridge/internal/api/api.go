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
}
