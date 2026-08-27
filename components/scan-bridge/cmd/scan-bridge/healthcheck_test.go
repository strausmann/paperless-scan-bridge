package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRunHealthcheck covers the container probe. The case that matters
// most is the last one: before this subcommand existed, the compose
// healthcheck ran `/scan-bridge healthcheck`, the argument was ignored,
// and the binary started a second daemon on every probe. A test that
// only asserted "exit 0 when ready" would have passed against that
// behaviour too -- the daemon starts fine. So the failure cases are
// what actually pin it down.
func TestRunHealthcheck(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     int
		body       string
		wantErr    bool
		wantInText string
	}{
		{
			name:       "ready",
			status:     http.StatusOK,
			body:       `{"status":"ready"}`,
			wantErr:    false,
			wantInText: "ready",
		},
		{
			name:       "scanner backend down",
			status:     http.StatusServiceUnavailable,
			body:       `{"error":"sane_runtime_unreachable"}`,
			wantErr:    true,
			wantInText: "sane_runtime_unreachable",
		},
		{
			name:       "no profiles",
			status:     http.StatusServiceUnavailable,
			body:       `{"error":"no_profiles_loaded"}`,
			wantErr:    true,
			wantInText: "no_profiles_loaded",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			var out bytes.Buffer
			err := runHealthcheck([]string{"--url", srv.URL + "/ready"}, &out)

			if tc.wantErr && err == nil {
				t.Fatalf("runHealthcheck() = nil, want error for status %d", tc.status)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("runHealthcheck() = %v, want nil", err)
			}

			// The reason has to survive into whatever the operator
			// sees, whether that is the error or stdout. A probe that
			// says only "unhealthy" sends someone to the logs for
			// something the probe already knew.
			got := out.String()
			if err != nil {
				got = err.Error()
			}
			if !strings.Contains(got, tc.wantInText) {
				t.Fatalf("output %q does not mention %q", got, tc.wantInText)
			}
		})
	}
}

// TestRunHealthcheckUnreachable is the "the daemon is not listening"
// case -- what the probe actually meets while the container is still
// starting, and the reason compose needs a start_period.
func TestRunHealthcheckUnreachable(t *testing.T) {
	t.Parallel()

	// Port 1 on loopback: privileged, and nothing in this test binary
	// binds it, so the connection is refused rather than timing out.
	var out bytes.Buffer
	err := runHealthcheck([]string{"--url", "http://127.0.0.1:1/ready"}, &out)
	if err == nil {
		t.Fatal("runHealthcheck() against a closed port = nil, want error")
	}
	if !strings.Contains(err.Error(), "healthcheck:") {
		t.Fatalf("error %q is not attributed to the healthcheck", err)
	}
}

// TestRunDispatchesHealthcheck guards the wiring in run(): the bare
// subcommand has to be recognised BEFORE flag parsing, or it falls
// through and starts the daemon -- which is exactly the bug this
// subcommand was written to fix.
func TestRunDispatchesHealthcheck(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"healthcheck", "--url", srv.URL + "/ready"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(healthcheck) = %v, want nil", err)
	}
	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("run(healthcheck) stdout = %q, want it to report readiness", stdout.String())
	}
}
