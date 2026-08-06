// Package healthcheck owns the daemon's liveness and readiness logic
// plus the synthetic-scan worker described in CONTAINER_SUITE.md
// sec. 4.1.
//
// Phase 1.1 wires only the liveness endpoint (always 200 once main.go
// has finished startup); the readiness probe and the synthetic-scan
// worker land with the dispatch subsystem and the
// scan-bridge / sane-runtime integration test.
package healthcheck

import (
	"context"
	"errors"
)

// ErrNotReady is returned by Ready when a required collaborator is
// missing — typically sane-runtime is unreachable or no profiles are
// loaded. The /ready handler turns this into a 503.
var ErrNotReady = errors.New("scan-bridge is not ready")

// Live is the liveness check that backs GET /health. It returns nil
// once the daemon has finished initialisation; main.go decides when
// to register the handler so this remains a constant-time success.
func Live(_ context.Context) error {
	return nil
}

// Ready will return nil once the dispatch subsystem can ping
// sane-runtime and the profile set is non-empty. The current stub
// returns ErrNotReady so the /ready endpoint can already respond
// honestly to monitoring scrapes — even a 503 is more useful than
// a 200 that lies.
func Ready(_ context.Context) error {
	return ErrNotReady
}

// TODO(phase 1.4): synthetic-scan worker — once a configured
// interval, the worker dispatches a no-op profile against
// sane-runtime, observes the round-trip, and updates
// scan_bridge_synthetic_check_total{outcome=...}.
