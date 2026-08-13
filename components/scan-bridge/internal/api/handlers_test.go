package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	set, err := profiles.Parse(strings.NewReader(`
profiles:
  - name: receipts
    description: "Receipts, grayscale"
    source: "ADF"
    resolution: 200
    mode: "Gray"
    format: "pdf"
    page_size: "auto"
    timeout_seconds: 60
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return &Server{
		Profiles: set,
		Build: BuildInfo{
			Version:   "0.1.0-test",
			Commit:    "deadbeef",
			BuildDate: "2026-04-30",
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func do(t *testing.T, srv *Server, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
}

func TestVersionEndpoint(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/version")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body versionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version != "0.1.0-test" {
		t.Errorf("version = %q, want 0.1.0-test", body.Version)
	}
	if body.Commit != "deadbeef" {
		t.Errorf("commit = %q, want deadbeef", body.Commit)
	}
}

func TestProfilesListEndpoint(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/profiles")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Profiles []profileSummary `json:"profiles"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Profiles) != 1 || body.Profiles[0].Name != "receipts" {
		t.Errorf("profiles = %+v, want one entry named receipts", body.Profiles)
	}
}

func TestProfileDetailEndpoint(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/profiles/receipts")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var p profiles.Profile
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Resolution != 200 {
		t.Errorf("resolution = %d, want 200", p.Resolution)
	}
}

func TestProfileDetailUnknown(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/profiles/does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "profile_not_found" {
		t.Errorf("error = %q, want profile_not_found", body.Error)
	}
}

// TestNotImplementedEndpoints covers the endpoints still stubbed at
// 501. POST /scan is real as of Phase 1.2 (ADR 0005) and is instead
// covered by internal/api/scan_test.go; GET /ready is real as of
// Phase 1.2h (issue #9) and is covered by internal/api/ready_test.go.
func TestNotImplementedEndpoints(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/jobs"},
		{http.MethodGet, "/jobs/01HJ"},
		{http.MethodPost, "/jobs/01HJ/cancel"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			rec := do(t, srv, tc.method, tc.path)
			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("status = %d, want 501", rec.Code)
			}
			var body errorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Error != "not_implemented" {
				t.Errorf("error = %q, want not_implemented", body.Error)
			}
		})
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/bogus")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestClientIPIgnoresUntrustedHeaders pins the security property that
// clientIP never honours caller-controlled forwarding headers — they
// are spoofable and would compromise the future ip_allowlist auth
// mode. Header-aware behaviour will return in Phase 1.4 alongside a
// trusted_proxies config option.
func TestClientIPIgnoresUntrustedHeaders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			name:       "remote addr only",
			remoteAddr: "10.0.0.5:5432",
			want:       "10.0.0.5",
		},
		{
			name:       "x-real-ip is ignored",
			remoteAddr: "10.0.0.5:5432",
			headers:    map[string]string{"X-Real-IP": "192.168.1.10"},
			want:       "10.0.0.5",
		},
		{
			name:       "x-forwarded-for is ignored even if x-real-ip is also set",
			remoteAddr: "10.0.0.5:5432",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.7, 10.0.0.1",
				"X-Real-IP":       "192.168.1.10",
			},
			want: "10.0.0.5",
		},
		{
			name:       "remote addr without port falls through unchanged",
			remoteAddr: "unix",
			want:       "unix",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			req.RemoteAddr = tc.remoteAddr
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if got := clientIP(req); got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
