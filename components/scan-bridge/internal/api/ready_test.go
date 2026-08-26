package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
)

const readyTestProfilesYAML = `
profiles:
  - name: receipts
    description: "Receipts, grayscale"
    source: "ADF Front"
    resolution: 200
    mode: "Gray"
    format: "pdf"
    page_size: "auto"
    timeout_seconds: 60
`

func readyTestProfiles(t *testing.T) *profiles.Set {
	t.Helper()
	set, err := profiles.Parse(strings.NewReader(readyTestProfilesYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return set
}

// TestReadyEndpoint covers the three states handleReady distinguishes
// (implementation brief for issue #9): profiles loaded and
// sane-runtime reachable (200), no profiles loaded (503, checked
// first so it never has to touch Dispatch), and profiles loaded but
// sane-runtime unreachable (503).
func TestReadyEndpoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		srv        *Server
		wantStatus int
		// wantError is empty for the 200 case, where we instead decode
		// a readyResponse below.
		wantError string
	}{
		{
			name: "profiles loaded and sane-runtime reachable returns 200",
			srv: &Server{
				Profiles: readyTestProfiles(t),
				Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
				Dispatch: &fakeDispatchClient{},
			},
			wantStatus: http.StatusOK,
		},
		{
			// Server{} zero value: Profiles and Dispatch are both nil.
			// profiles.Set.Len() is documented nil-safe (profiles.go),
			// and handleReady must check it BEFORE touching Dispatch —
			// this case would panic on a nil Dispatch.Ping call if the
			// ordering were reversed.
			name: "no profiles loaded returns 503 without touching a nil dispatch",
			srv: &Server{
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			},
			wantStatus: http.StatusServiceUnavailable,
			wantError:  "no_profiles_loaded",
		},
		{
			name: "profiles loaded but sane-runtime unreachable returns 503",
			srv: &Server{
				Profiles: readyTestProfiles(t),
				Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
				Dispatch: &fakeDispatchClient{
					pingFn: func(ctx context.Context) error {
						return errors.New("sane-runtime: connection refused")
					},
				},
			},
			wantStatus: http.StatusServiceUnavailable,
			wantError:  "sane_runtime_unreachable",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			rec := httptest.NewRecorder()
			tc.srv.Router().ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}

			if tc.wantError == "" {
				var body readyResponse
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body.Status != "ready" {
					t.Errorf("status = %q, want ready", body.Status)
				}
				return
			}

			var body errorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Error != tc.wantError {
				t.Errorf("error = %q, want %q", body.Error, tc.wantError)
			}
		})
	}
}
