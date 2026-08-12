// Package scanapi wires the HTTP surface sane-runtime exposes to
// scan-bridge over the Unix-domain socket described in ADR 0009:
// POST /scan, GET /health, GET /ready.
//
// The scan path is intentionally single-flight: sane-runtime drives
// exactly one physical scanner, so a second concurrent POST /scan
// gets 409 scanner_busy immediately (via sync.Mutex.TryLock) rather
// than queuing — job queuing is Task 8's concern, deferred here.
package scanapi

import (
	"log/slog"
	"sync"

	"github.com/strausmann/paperless-scan-bridge/components/sane-runtime/internal/scanner"
)

// Server bundles the collaborators the HTTP handlers need. Create it
// with &Server{...} and call Router() once; Server must not be copied
// after first use because scanMu guards the single scan slot.
type Server struct {
	Scanner scanner.Scanner
	Logger  *slog.Logger

	scanMu sync.Mutex
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
