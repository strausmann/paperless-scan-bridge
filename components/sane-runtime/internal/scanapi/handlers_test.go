package scanapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/strausmann/paperless-scan-bridge/components/sane-runtime/internal/scanner"
)

// fakeScanner is an in-memory scanner.Scanner test double. It never
// shells out, so handlers_test.go runs with no hardware and no exec
// dependency — only exec_scanner_test.go needs the fixture binary.
type fakeScanner struct {
	mu sync.Mutex

	pages   []scanner.Page
	err     error
	devices []string
	devErr  error

	// started/proceed gate Scan for the concurrency test: Scan closes
	// started as soon as it is entered (so the test knows the server
	// mutex is held) and blocks on proceed until the test releases it.
	started chan struct{}
	proceed chan struct{}

	calls int
}

func (f *fakeScanner) Scan(ctx context.Context, params scanner.Params) ([]scanner.Page, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	if f.started != nil {
		close(f.started)
	}
	if f.proceed != nil {
		<-f.proceed
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.pages, nil
}

func (f *fakeScanner) ListDevices(ctx context.Context) ([]string, error) {
	return f.devices, f.devErr
}

// fakePage builds a scanner.Page over an in-memory byte slice; Close
// is a no-op, matching a real Page's contract of "safe to Close once".
func fakePage(index int, data string) scanner.Page {
	return scanner.Page{Index: index, Data: io.NopCloser(strings.NewReader(data))}
}

func newTestServer(t *testing.T, sc scanner.Scanner) *Server {
	t.Helper()
	return &Server{
		Scanner: sc,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

const validScanBody = `{
  "request_id": "req-1",
  "device": "",
  "source": "ADF Duplex",
  "resolution": 300,
  "mode": "Color",
  "format": "tiff",
  "max_pages": 0,
  "timeout_seconds": 300
}`

func doPost(t *testing.T, srv *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func doGet(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

// parsedMultipart is the decoded shape of a successful /scan response,
// used by the happy-path tests below.
type parsedMultipart struct {
	meta  scanMetadata
	pages [][]byte
	types []string
}

func parseMultipartResponse(t *testing.T, rec *httptest.ResponseRecorder) parsedMultipart {
	t.Helper()
	_, params, err := mime.ParseMediaType(rec.Header().Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse Content-Type %q: %v", rec.Header().Get("Content-Type"), err)
	}
	boundary, ok := params["boundary"]
	if !ok {
		t.Fatalf("Content-Type %q has no boundary", rec.Header().Get("Content-Type"))
	}

	mr := multipart.NewReader(rec.Body, boundary)

	metaPart, err := mr.NextPart()
	if err != nil {
		t.Fatalf("read metadata part: %v", err)
	}
	metaBytes, err := io.ReadAll(metaPart)
	if err != nil {
		t.Fatalf("read metadata bytes: %v", err)
	}
	var meta scanMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}

	var out parsedMultipart
	out.meta = meta
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read page part: %v", err)
		}
		b, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read page bytes: %v", err)
		}
		out.pages = append(out.pages, b)
		out.types = append(out.types, part.Header.Get("Content-Type"))
	}
	return out
}

func TestScan_HappyPath_SinglePage(t *testing.T) {
	t.Parallel()

	sc := &fakeScanner{pages: []scanner.Page{fakePage(0, "PAGE-0")}}
	srv := newTestServer(t, sc)

	rec := doPost(t, srv, "/scan", validScanBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	parsed := parseMultipartResponse(t, rec)
	if parsed.meta.RequestID != "req-1" {
		t.Errorf("request_id = %q, want req-1", parsed.meta.RequestID)
	}
	if parsed.meta.PageCount != 1 {
		t.Errorf("page_count = %d, want 1", parsed.meta.PageCount)
	}
	if len(parsed.pages) != 1 || string(parsed.pages[0]) != "PAGE-0" {
		t.Errorf("pages = %v, want [PAGE-0]", parsed.pages)
	}
	if parsed.types[0] != "image/tiff" {
		t.Errorf("page content-type = %q, want image/tiff", parsed.types[0])
	}
}

func TestScan_HappyPath_MultiPage(t *testing.T) {
	t.Parallel()

	sc := &fakeScanner{pages: []scanner.Page{
		fakePage(0, "PAGE-0"),
		fakePage(1, "PAGE-1"),
		fakePage(2, "PAGE-2"),
	}}
	srv := newTestServer(t, sc)

	rec := doPost(t, srv, "/scan", validScanBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	parsed := parseMultipartResponse(t, rec)
	if parsed.meta.PageCount != 3 {
		t.Errorf("page_count = %d, want 3", parsed.meta.PageCount)
	}
	want := []string{"PAGE-0", "PAGE-1", "PAGE-2"}
	if len(parsed.pages) != len(want) {
		t.Fatalf("len(pages) = %d, want %d", len(parsed.pages), len(want))
	}
	for i, w := range want {
		if string(parsed.pages[i]) != w {
			t.Errorf("pages[%d] = %q, want %q (order must match scan order)", i, parsed.pages[i], w)
		}
	}
}

func TestScan_ValidationError_400(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{
			name: "bad mode",
			body: `{"source":"ADF Duplex","resolution":300,"mode":"Sepia","format":"tiff"}`,
		},
		{
			name: "unknown field",
			body: `{"source":"ADF Duplex","resolution":300,"mode":"Color","format":"tiff","totally_unknown":true}`,
		},
		{
			name: "bad source",
			body: `{"source":"Teleporter","resolution":300,"mode":"Color","format":"tiff"}`,
		},
		{
			name: "resolution too low",
			body: `{"source":"ADF Duplex","resolution":10,"mode":"Color","format":"tiff"}`,
		},
		{
			name: "resolution too high",
			body: `{"source":"ADF Duplex","resolution":9999,"mode":"Color","format":"tiff"}`,
		},
		{
			name: "malformed json",
			body: `{not json`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sc := &fakeScanner{pages: []scanner.Page{fakePage(0, "x")}}
			srv := newTestServer(t, sc)

			rec := doPost(t, srv, "/scan", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
			}
			var body errorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if body.Error != "invalid_request" {
				t.Errorf("error = %q, want invalid_request", body.Error)
			}
		})
	}
}

func TestScan_ErrorMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		scanErr   error
		wantCode  int
		wantError string
	}{
		{
			name:      "no scanner detected",
			scanErr:   scanner.ErrNoScannerDetected,
			wantCode:  http.StatusServiceUnavailable,
			wantError: "no_scanner_detected",
		},
		{
			name:      "ADF empty",
			scanErr:   scanner.ErrNoDocuments,
			wantCode:  http.StatusUnprocessableEntity,
			wantError: "no_documents",
		},
		{
			name:      "device error",
			scanErr:   scanner.ErrDeviceError,
			wantCode:  http.StatusUnprocessableEntity,
			wantError: "device_error",
		},
		{
			name:      "timeout",
			scanErr:   scanner.ErrTimeout,
			wantCode:  http.StatusGatewayTimeout,
			wantError: "scan_timeout",
		},
		{
			name:      "generic failure",
			scanErr:   errors.New("boom: exit status 2"),
			wantCode:  http.StatusInternalServerError,
			wantError: "scan_failed",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sc := &fakeScanner{err: tc.scanErr}
			srv := newTestServer(t, sc)

			rec := doPost(t, srv, "/scan", validScanBody)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			var body errorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if body.Error != tc.wantError {
				t.Errorf("error = %q, want %q", body.Error, tc.wantError)
			}
		})
	}
}

func TestScan_ConcurrentRequests_SecondGets409(t *testing.T) {
	t.Parallel()

	sc := &fakeScanner{
		pages:   []scanner.Page{fakePage(0, "PAGE-0")},
		started: make(chan struct{}),
		proceed: make(chan struct{}),
	}
	srv := newTestServer(t, sc)

	var rec1 *httptest.ResponseRecorder
	done := make(chan struct{})
	go func() {
		defer close(done)
		rec1 = doPost(t, srv, "/scan", validScanBody)
	}()

	<-sc.started // first request is now inside Scan, holding the server's scan slot

	rec2 := doPost(t, srv, "/scan", validScanBody)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second request status = %d, want 409; body = %s", rec2.Code, rec2.Body.String())
	}
	var body errorResponse
	if err := json.NewDecoder(rec2.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error != "scanner_busy" {
		t.Errorf("error = %q, want scanner_busy", body.Error)
	}

	close(sc.proceed)
	<-done

	if rec1.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200; body = %s", rec1.Code, rec1.Body.String())
	}
}

func TestHealth_AlwaysOK(t *testing.T) {
	t.Parallel()

	// Even a Scanner that would fail ListDevices must not affect
	// liveness: /health only answers "the process is up".
	sc := &fakeScanner{devErr: errors.New("boom")}
	srv := newTestServer(t, sc)

	rec := doGet(t, srv, "/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReady_NoScanner_503(t *testing.T) {
	t.Parallel()

	sc := &fakeScanner{devices: nil}
	srv := newTestServer(t, sc)

	rec := doGet(t, srv, "/ready")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestReady_ScannerDetected_200(t *testing.T) {
	t.Parallel()

	sc := &fakeScanner{devices: []string{"avision:libusb:001:002"}}
	srv := newTestServer(t, sc)

	rec := doGet(t, srv, "/ready")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReady_ListDevicesError_503(t *testing.T) {
	t.Parallel()

	sc := &fakeScanner{devErr: errors.New("scanimage -L failed")}
	srv := newTestServer(t, sc)

	rec := doGet(t, srv, "/ready")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestScan_MethodNotAllowed pins that GET /scan does not silently
// fall through to a 404 catch-all — Go 1.22's ServeMux method
// patterns return 405 for a matched path with the wrong method.
func TestScan_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	sc := &fakeScanner{pages: []scanner.Page{fakePage(0, "x")}}
	srv := newTestServer(t, sc)

	rec := doGet(t, srv, "/scan")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
