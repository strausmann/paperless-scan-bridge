package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
)

func TestVersionFlag(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.HasPrefix(stdout.String(), "scan-bridge ") {
		t.Errorf("version output = %q, want prefix 'scan-bridge '", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty, got %q", stderr.String())
	}
}

func TestUnknownFlagReturnsError(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"--bogus"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error from unknown flag")
	}
}

// TestNewHTTPServers_WiresReadTimeoutFromConfig covers issue #47's
// Read-Timeout hardening: cfg.Server.ReadTimeoutSeconds must reach
// BOTH constructed *http.Server values' ReadTimeout field (bounding
// the whole request -- headers AND body -- unlike ReadHeaderTimeout,
// which stays hardcoded at 5s and only bounds the header phase).
// run() never returns its *http.Server values (it blocks serving
// them until shutdown), so this test exercises newHTTPServers
// directly rather than needing a real network listener.
func TestNewHTTPServers_WiresReadTimeoutFromConfig(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Server.ReadTimeoutSeconds = 42
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	public, metricsSrv := newHTTPServers(cfg, http.NewServeMux(), http.NewServeMux())

	wantReadTimeout := 42 * time.Second
	if public.ReadTimeout != wantReadTimeout {
		t.Errorf("public.ReadTimeout = %s, want %s", public.ReadTimeout, wantReadTimeout)
	}
	if public.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("public.ReadHeaderTimeout = %s, want 5s", public.ReadHeaderTimeout)
	}
	if metricsSrv.ReadTimeout != wantReadTimeout {
		t.Errorf("metricsSrv.ReadTimeout = %s, want %s", metricsSrv.ReadTimeout, wantReadTimeout)
	}
	if public.Addr != cfg.Server.Listen {
		t.Errorf("public.Addr = %q, want %q", public.Addr, cfg.Server.Listen)
	}
	if metricsSrv.Addr != cfg.Server.MetricsListen {
		t.Errorf("metricsSrv.Addr = %q, want %q", metricsSrv.Addr, cfg.Server.MetricsListen)
	}
	if public.Handler == nil || metricsSrv.Handler == nil {
		t.Error("both servers must carry the handlers passed in, want non-nil")
	}
}

// TestNewHTTPServers_ZeroReadTimeoutSecondsMeansNoTimeout documents
// the (currently unreachable in production, since config.Validate
// rejects ReadTimeoutSeconds <= 0) zero-value behaviour: an
// unvalidated Config with ReadTimeoutSeconds == 0 produces
// ReadTimeout == 0, which net/http.Server treats as "no timeout" --
// distinct from, and not to be confused with, a deliberately large
// timeout.
func TestNewHTTPServers_ZeroReadTimeoutSecondsMeansNoTimeout(t *testing.T) {
	t.Parallel()

	var cfg config.Config // zero value, deliberately not Default()/Validate()d
	public, _ := newHTTPServers(cfg, http.NewServeMux(), http.NewServeMux())
	if public.ReadTimeout != 0 {
		t.Errorf("public.ReadTimeout = %s, want 0 for a zero-value config.Config", public.ReadTimeout)
	}
}
