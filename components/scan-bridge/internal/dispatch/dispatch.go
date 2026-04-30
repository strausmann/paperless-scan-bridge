// Package dispatch is the client to the sane-runtime container.
//
// It speaks HTTP-over-Unix-socket per CONTAINER_SUITE.md sec. 7
// (inter-container communication) so the scan-bridge daemon can
// trigger scans, cancel them, and observe progress without ever
// touching the SANE backends directly. The implementation lands in
// Phase 1.4 once sane-runtime exposes the agreed JSON contract; this
// file only declares the surface other packages will program against.
package dispatch

import (
	"context"
	"errors"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
)

// Request is a single scan dispatch sent to sane-runtime.
type Request struct {
	JobID    string
	TraceID  string
	Profile  profiles.Profile
	Metadata map[string]string
}

// Response carries the result of a completed scan from sane-runtime
// back to the bridge daemon. Page paths point at the raw image batch
// the scan-processor will consume.
type Response struct {
	JobID    string
	Pages    []string
	DurationMillis int64
}

// Client is the contract scan-bridge programs against. Implementations
// are in-package (production HTTP-over-Unix client) or in tests
// (in-memory fakes).
type Client interface {
	Dispatch(ctx context.Context, req Request) (Response, error)
	Cancel(ctx context.Context, jobID string) error
	Ping(ctx context.Context) error
	Close() error
}

// ErrNotImplemented is returned by the placeholder client until the
// real HTTP-over-Unix transport lands.
var ErrNotImplemented = errors.New("dispatch: not implemented (phase 1.4)")

// TODO(phase 1.4): real Client backed by an HTTP-over-Unix-socket
// transport at the path configured via PathsConfig.SaneSocket.
// Should propagate TraceID via a request header so sane-runtime can
// log it and echo it back in the Response.
//
// TODO(phase 1.4): publish scan_bridge_dispatch_duration_seconds and
// scan_bridge_scan_duration_seconds histograms from this layer.
