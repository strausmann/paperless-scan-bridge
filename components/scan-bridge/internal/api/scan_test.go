package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/dispatch"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
)

// fakeDispatchClient is an in-memory dispatch.Client double for
// handler tests — the real transport is exercised in
// internal/dispatch/http_client_test.go.
type fakeDispatchClient struct {
	dispatchFn func(ctx context.Context, req dispatch.Request) (dispatch.Response, error)
}

func (f *fakeDispatchClient) Dispatch(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
	if f.dispatchFn == nil {
		return dispatch.Response{}, errors.New("fakeDispatchClient: dispatchFn not set")
	}
	return f.dispatchFn(ctx, req)
}

func (f *fakeDispatchClient) Cancel(ctx context.Context, jobID string) error {
	return dispatch.ErrNotImplemented
}

func (f *fakeDispatchClient) Ping(ctx context.Context) error { return nil }

func (f *fakeDispatchClient) Close() error { return nil }

const scanTestProfilesYAML = `
profiles:
  - name: receipts
    description: "Receipts, grayscale"
    source: "ADF"
    resolution: 200
    mode: "Gray"
    format: "pdf"
    page_size: "auto"
    timeout_seconds: 60
`

// newScanTestServer builds a Server wired for POST /scan tests: real
// profiles (parsed from scanTestProfilesYAML), the supplied auth
// config, and the supplied dispatch client (a fakeDispatchClient in
// every test here).
func newScanTestServer(t *testing.T, auth config.AuthConfig, client dispatch.Client) *Server {
	t.Helper()

	set, err := profiles.Parse(strings.NewReader(scanTestProfilesYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return &Server{
		Profiles: set,
		Build:    BuildInfo{Version: "0.1.0-test"},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:     auth,
		Dispatch: client,
	}
}

func tokenAuth(t *testing.T, plaintext string) config.AuthConfig {
	t.Helper()
	cfg := config.Default()
	cfg.Auth.Mode = config.AuthModeToken
	// config.applyEnv hashes a plaintext bearer token the same way
	// (SHA-256 hex) when it is supplied via SCAN_BRIDGE_API_TOKEN
	// (config.go); we are not going through Load here, so replicate
	// that exact transform rather than reimplementing/guessing at the
	// hash the middleware under test expects.
	sum := sha256.Sum256([]byte(plaintext))
	cfg.Auth.TokenHash = hex.EncodeToString(sum[:])
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return cfg.Auth
}

func postScan(t *testing.T, srv *Server, bearer string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
		reader = nil
	case []byte:
		reader = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(http.MethodPost, "/scan", reader)
	req.RemoteAddr = "10.0.0.9:5555"
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func TestScanNoAuthHeaderReturns401(t *testing.T) {
	t.Parallel()

	srv := newScanTestServer(t, tokenAuth(t, "correct-token"), &fakeDispatchClient{})
	rec := postScan(t, srv, "", map[string]string{"profile": "receipts"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "unauthorized" {
		t.Errorf("error = %q, want unauthorized", body.Error)
	}
}

func TestScanWrongTokenReturns401WithWWWAuthenticate(t *testing.T) {
	t.Parallel()

	srv := newScanTestServer(t, tokenAuth(t, "correct-token"), &fakeDispatchClient{})
	rec := postScan(t, srv, "wrong-token", map[string]string{"profile": "receipts"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Errorf("WWW-Authenticate = %q, want it to mention Bearer", got)
	}
}

func TestScanValidTokenUnknownProfileReturns404(t *testing.T) {
	t.Parallel()

	srv := newScanTestServer(t, tokenAuth(t, "correct-token"), &fakeDispatchClient{})
	rec := postScan(t, srv, "correct-token", map[string]string{"profile": "does-not-exist"})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (auth must have passed for us to get here)", rec.Code)
	}
	var body errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "profile_not_found" {
		t.Errorf("error = %q, want profile_not_found", body.Error)
	}
}

func TestScanUnknownJSONFieldReturns400(t *testing.T) {
	t.Parallel()

	srv := newScanTestServer(t, tokenAuth(t, "correct-token"), &fakeDispatchClient{})
	rec := postScan(t, srv, "correct-token", map[string]string{
		"profile":       "receipts",
		"correspondent": "not accepted yet",
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "invalid_body" {
		t.Errorf("error = %q, want invalid_body", body.Error)
	}
}

func TestScanHappyPathReturns200(t *testing.T) {
	t.Parallel()

	var gotReq dispatch.Request
	client := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			gotReq = req
			return dispatch.Response{
				JobID:          req.JobID,
				Pages:          []string{"/scans/" + req.JobID + "/page-1.tiff", "/scans/" + req.JobID + "/page-2.tiff"},
				DurationMillis: 4200,
			}, nil
		},
	}
	srv := newScanTestServer(t, tokenAuth(t, "correct-token"), client)
	rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var body scanResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Profile != "receipts" {
		t.Errorf("profile = %q, want receipts", body.Profile)
	}
	if body.PageCount != 2 {
		t.Errorf("page_count = %d, want 2", body.PageCount)
	}
	if len(body.Pages) != 2 {
		t.Errorf("len(pages) = %d, want 2", len(body.Pages))
	}
	if body.DurationMillis != 4200 {
		t.Errorf("duration_ms = %d, want 4200", body.DurationMillis)
	}
	if body.ScanID == "" {
		t.Error("scan_id must not be empty")
	}
	if gotReq.Profile.Name != "receipts" {
		t.Errorf("dispatch received profile %q, want receipts", gotReq.Profile.Name)
	}
	if gotReq.JobID != body.ScanID {
		t.Errorf("dispatch JobID %q != response scan_id %q", gotReq.JobID, body.ScanID)
	}
}

// TestScanTagMergeReflected pins the milestone default from the
// implementation brief (D-Tag-Merge): the profile carries no default
// tag IDs yet (Task 10), so tag.Merge(nil, "", callerTagIDs,
// callerStrategy) with an "add" (or empty, which behaves like "add")
// strategy simply returns the caller's tag IDs, deduplicated.
func TestScanTagMergeReflected(t *testing.T) {
	t.Parallel()

	client := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID}, nil
		},
	}
	srv := newScanTestServer(t, tokenAuth(t, "correct-token"), client)
	rec := postScan(t, srv, "correct-token", map[string]any{
		"profile": "receipts",
		"tag_ids": []int{21},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var body scanResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.EffectiveTagIDs) != 1 || body.EffectiveTagIDs[0] != 21 {
		t.Errorf("effective_tag_ids = %v, want [21]", body.EffectiveTagIDs)
	}
}

func TestScanDispatchErrorsMapToStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		dispatchFn func(ctx context.Context, req dispatch.Request) (dispatch.Response, error)
		wantStatus int
		wantError  string
	}{
		{
			name: "no scanner detected",
			dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
				return dispatch.Response{}, dispatch.ErrNoScannerDetected
			},
			wantStatus: http.StatusServiceUnavailable,
			wantError:  "no_scanner_detected",
		},
		{
			name: "no documents",
			dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
				return dispatch.Response{}, dispatch.ErrNoDocuments
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantError:  "no_documents",
		},
		{
			name: "busy",
			dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
				return dispatch.Response{}, dispatch.ErrBusy
			},
			wantStatus: http.StatusConflict,
			wantError:  "scanner_busy",
		},
		{
			name: "timeout",
			dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
				return dispatch.Response{}, dispatch.ErrTimeout
			},
			wantStatus: http.StatusGatewayTimeout,
			wantError:  "timeout",
		},
		{
			name: "context deadline exceeded maps to timeout too",
			dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
				return dispatch.Response{}, context.DeadlineExceeded
			},
			wantStatus: http.StatusGatewayTimeout,
			wantError:  "timeout",
		},
		{
			name: "unclassified error maps to bad gateway",
			dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
				return dispatch.Response{}, errors.New("boom")
			},
			wantStatus: http.StatusBadGateway,
			wantError:  "dispatch_failed",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := &fakeDispatchClient{dispatchFn: tc.dispatchFn}
			srv := newScanTestServer(t, tokenAuth(t, "correct-token"), client)
			rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
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

func TestScanAuthMiddlewareIPAllowlistMode(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Auth.Mode = config.AuthModeIPAllowlist
	cfg.Auth.AllowedCIDRs = []string{"10.0.0.0/24"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	client := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID}, nil
		},
	}
	srv := newScanTestServer(t, cfg.Auth, client)

	t.Run("allowed source IP needs no bearer token", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/scan", strings.NewReader(`{"profile":"receipts"}`))
		req.RemoteAddr = "10.0.0.42:1234"
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("disallowed source IP is rejected", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/scan", strings.NewReader(`{"profile":"receipts"}`))
		req.RemoteAddr = "192.168.1.5:1234"
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}
