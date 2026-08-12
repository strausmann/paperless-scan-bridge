// Package dispatch is the client to the sane-runtime container.
//
// It speaks HTTP-over-Unix-socket (ADR 0009) so the scan-bridge daemon
// can trigger scans without touching the SANE backends directly. The
// production implementation (httpUnixClient, see http_client.go)
// lands in Phase 1.2, alongside the in-package sentinel errors below
// that classify sane-runtime's error envelope so internal/api can map
// them to HTTP status codes without depending on this package's wire
// format.
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
	JobID          string
	Pages          []string
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

// ErrNotImplemented is returned by Cancel, which sane-runtime does not
// yet expose an endpoint for (CONTAINER_SUITE.md sec. 5.4 lists
// `/scan/{id}/cancel` as a future addition).
var ErrNotImplemented = errors.New("dispatch: not implemented (phase 1.4)")

// Sentinel errors classifying a failed Dispatch call. httpUnixClient
// wraps one of these (via %w) so callers can use errors.Is regardless
// of the underlying sane-runtime status code or transport error;
// internal/api.mapDispatchError is the primary consumer.
var (
	// ErrNoScannerDetected means sane-runtime has not detected any
	// attached scanner (sane-runtime HTTP 503).
	ErrNoScannerDetected = errors.New("dispatch: no scanner detected")
	// ErrNoDocuments means the scan ran but the feeder was empty
	// (sane-runtime HTTP 422).
	ErrNoDocuments = errors.New("dispatch: no documents in feeder")
	// ErrBusy means sane-runtime is already processing another scan
	// (sane-runtime HTTP 409).
	ErrBusy = errors.New("dispatch: scanner busy")
	// ErrTimeout means the scan did not finish within the caller's
	// deadline, either because sane-runtime itself reported a timeout
	// (HTTP 504) or because the request's context deadline expired
	// client-side.
	ErrTimeout = errors.New("dispatch: timed out")
)

// TODO(phase 1.4): publish scan_bridge_dispatch_duration_seconds and
// scan_bridge_scan_duration_seconds histograms from this layer.
