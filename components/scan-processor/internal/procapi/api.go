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

// defaultMaxRequestBytes bounds an inbound POST /process request body
// via http.MaxBytesReader (handlers.go's decodeProcessRequest) when
// Server.MaxRequestBytes is unset -- a hardening backstop (issue #47):
// decodeProcessRequest reads the whole multipart body via
// io.ReadAll(part) before the per-request timeout context is even
// constructed, so an unbounded body is both a memory-exhaustion and a
// slow-client vector.
//
// Sized against a REAL page, not a hypothetical small one: the
// repo's own deploy/profiles/default.yaml scans at 300 DPI, Color,
// A4 -- uncompressed TIFF at that resolution/depth is
// ~2481x3507px * 3 bytes/px ≈ 25 MiB *per page*, and procclient (the
// scan-bridge-side client, internal/procclient/http_client.go)
// sends every page of a scan in ONE /process POST, never
// paginated. 512 MiB covers a ~19-20 page duplex/color batch with
// that profile -- the same order of magnitude CONTAINER_SUITE.md
// sec. 8.1 uses for the (superseded, never-built) shared raw-page
// tmpfs ("512 MB is comfortable [...] raise for high-volume
// setups"), which this constant is now sized to match rather than
// contradict. compose.yaml's scan-processor `/tmp` tmpfs mount
// (exec_pipeline.go's scratch directory, which holds this many
// bytes again per intermediate processing stage) is sized with
// headroom above this constant for the same reason -- see that
// file's tmpfs comment.
const defaultMaxRequestBytes int64 = 512 << 20 // 512 MiB

// allowedOCRLanguagesDefault mirrors the tessdata packs installed in
// scan-processor's runtime image (../../Dockerfile:
// tesseract-ocr-deu, tesseract-ocr-eng) and matches
// exec_pipeline.go's defaultOCRLanguages. validateProcessRequest
// rejects any ocr.languages entry outside this set *before* the
// pipeline ever runs (issue #47): without it, an unsupported language
// collapses into exactly one argv slot after tesseract's `-l` flag
// (see pipeline.buildOCRPDFArgs) -- not exploitable, but a wasted
// OCR round-trip ending in a 422 instead of a cheap, immediate 400.
var allowedOCRLanguagesDefault = map[string]bool{"deu": true, "eng": true}

// Server bundles the collaborators the HTTP handlers need. Create it
// with &Server{...} and call Router() once; Server must not be
// copied after first use because processMu guards the single
// processing slot.
type Server struct {
	Pipeline pipeline.Pipeline
	Logger   *slog.Logger

	// MaxRequestBytes overrides defaultMaxRequestBytes when positive.
	// Zero/negative (including a Server built directly by a test or
	// caller that never set it) falls back to the default -- a
	// zero-value Server must still enforce SOME bound rather than
	// silently disabling the limit.
	MaxRequestBytes int64

	// AllowedOCRLanguages overrides allowedOCRLanguagesDefault when
	// non-empty -- e.g. a deployment whose runtime image installs
	// additional tessdata packs.
	AllowedOCRLanguages map[string]bool

	processMu sync.Mutex
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Server) maxRequestBytes() int64 {
	if s.MaxRequestBytes > 0 {
		return s.MaxRequestBytes
	}
	return defaultMaxRequestBytes
}

func (s *Server) allowedOCRLanguages() map[string]bool {
	if len(s.AllowedOCRLanguages) > 0 {
		return s.AllowedOCRLanguages
	}
	return allowedOCRLanguagesDefault
}
