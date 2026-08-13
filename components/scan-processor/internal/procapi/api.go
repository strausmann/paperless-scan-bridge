// Package procapi wires the HTTP surface scan-processor exposes to
// scan-bridge over the Unix-domain socket described in design doc
// sec. 4.2: POST /process, GET /health.
//
// The wire contract (multipart/mixed request and response shape,
// JSON field names, and the four documented error status codes) is
// frozen by components/scan-bridge/internal/procclient — that
// package's doc comment states scan-processor "must implement" it,
// and its http_client_test.go's fake server is this handler's
// specification. This package intentionally does not import
// procclient (or anything under components/scan-bridge): the
// contract is duplicated here as independently declared types with
// identical JSON tags, matching the same dependency-direction rule
// internal/pipeline's doc comment states.
//
// The /process path is single-flight, mirroring
// components/sane-runtime/internal/scanapi's POST /scan: a second
// concurrent request while one is already running gets 409
// scanner_busy immediately (via sync.Mutex.TryLock) rather than
// queuing.
package procapi

import (
	"log/slog"
	"sync"

	"github.com/strausmann/paperless-scan-bridge/components/scan-processor/internal/pipeline"
)

// Server bundles the collaborators the HTTP handlers need. Create it
// with &Server{...} and call Router() once; Server must not be
// copied after first use because processMu guards the single
// processing slot.
type Server struct {
	Pipeline pipeline.Pipeline
	Logger   *slog.Logger

	processMu sync.Mutex
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
