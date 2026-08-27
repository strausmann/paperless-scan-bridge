package scanapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/sane-runtime/internal/scanner"
)

// capturingServer returns a Server whose logger writes JSON into buf, so
// a test can assert on the emitted fields rather than on a substring.
func capturingServer(t *testing.T, sc scanner.Scanner) (*Server, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	return &Server{
		Scanner: sc,
		Logger:  slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}, buf
}

// requestLines returns every "http request" record the middleware wrote.
func requestLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		if rec["msg"] == "http request" {
			out = append(out, rec)
		}
	}
	return out
}

// TestRequestLoggingLogsSuccessfulRequests is the whole point of this
// middleware: before it, a successful request produced no log line at
// all, so there was no way to tell "the request never arrived" from
// "the scanner is slow" when reading sane-runtime's logs.
func TestRequestLoggingLogsSuccessfulRequests(t *testing.T) {
	t.Parallel()

	srv, buf := capturingServer(t, &fakeScanner{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	srv.Router().ServeHTTP(httptest.NewRecorder(), req)

	lines := requestLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d request log lines, want exactly 1: %s", len(lines), buf.String())
	}
	rec := lines[0]
	if rec["method"] != "GET" {
		t.Errorf("method = %v, want GET", rec["method"])
	}
	if rec["path"] != "/health" {
		t.Errorf("path = %v, want /health", rec["path"])
	}
	if rec["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", rec["status"])
	}
	if _, ok := rec["duration_ms"]; !ok {
		t.Error("duration_ms missing")
	}
	if _, ok := rec["bytes"]; !ok {
		t.Error("bytes missing")
	}
}

// TestRequestLoggingHasNoSourceIP: sane-runtime is reached only over a
// Unix socket (ADR 0009), where RemoteAddr carries no meaningful peer
// address. Copying scan-bridge's source_ip field would emit a constant
// or an empty string on every line and invite someone to trust it.
func TestRequestLoggingHasNoSourceIP(t *testing.T) {
	t.Parallel()

	srv, buf := capturingServer(t, &fakeScanner{})
	srv.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	lines := requestLines(t, buf)
	if len(lines) == 0 {
		// Without this the test panics on an index instead of saying
		// what went wrong — and a missing line is exactly the case
		// worth reading about.
		t.Fatalf("no request log line: %s", buf.String())
	}
	if _, ok := lines[0]["source_ip"]; ok {
		t.Error("source_ip present; it is meaningless over a Unix socket")
	}
}

// TestRequestLoggingLevelReflectsStatus keeps an error out of the noise
// floor: a 5xx must not be filtered out by a handler configured at
// warn level or above.
func TestRequestLoggingLevelReflectsStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		scanner scanner.Scanner
		want    string
	}{
		{"success is info", &fakeScanner{}, "INFO"},
		{"client error is warn", &fakeScanner{err: scanner.ErrNoDocuments}, "WARN"},
		{"server error is error", &fakeScanner{err: errors.New("boom")}, "ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv, buf := capturingServer(t, tc.scanner)
			doPost(t, srv, "/scan", validScanBody)

			lines := requestLines(t, buf)
			if len(lines) == 0 {
				t.Fatalf("no request log line: %s", buf.String())
			}
			if got := lines[len(lines)-1]["level"]; got != tc.want {
				t.Errorf("level = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRequestLoggingDurationCoversTheResponseBody guards the one thing a
// naive middleware gets wrong here: POST /scan streams a multipart body
// of several megabytes, and a duration measured when the handler
// returns its header would report a few milliseconds for a twenty-second
// scan — precisely useless for the debugging this exists for.
func TestRequestLoggingDurationCoversTheResponseBody(t *testing.T) {
	t.Parallel()

	const delay = 60 * time.Millisecond
	srv, buf := capturingServer(t, &slowScanner{delay: delay})

	doPost(t, srv, "/scan", validScanBody)

	lines := requestLines(t, buf)
	if len(lines) == 0 {
		t.Fatalf("no request log line: %s", buf.String())
	}
	got, ok := lines[0]["duration_ms"].(float64)
	if !ok {
		t.Fatalf("duration_ms is not a number: %v", lines[0]["duration_ms"])
	}
	if want := float64(delay.Milliseconds()); got < want {
		t.Errorf("duration_ms = %v, want >= %v — the timer stopped before the body was written", got, want)
	}
}

// slowScanner delays before returning its pages, standing in for a real
// scanner that takes twenty seconds to pull a duplex sheet.
type slowScanner struct {
	delay time.Duration
}

func (s *slowScanner) ListDevices(context.Context) ([]string, error) {
	return []string{"dev"}, nil
}

func (s *slowScanner) Scan(context.Context, scanner.Params) ([]scanner.Page, error) {
	time.Sleep(s.delay)
	return []scanner.Page{fakePage(0, "page-bytes")}, nil
}

// TestRequestLoggingLogsPanickingRequests: net/http recovers a handler
// panic above this middleware, so a non-deferred log call is skipped for
// exactly the request an operator most needs to see.
func TestRequestLoggingLogsPanickingRequests(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	srv := &Server{
		Scanner: &fakeScanner{},
		Logger:  slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	h := srv.loggingMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	func() {
		defer func() { _ = recover() }()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/scan", nil))
	}()

	if len(requestLines(t, buf)) != 1 {
		t.Fatalf("panicking request produced no log line: %s", buf.String())
	}
}

// TestRequestLoggingDefaultsStatusTo200: a handler that returns without
// writing still makes net/http send 200. Logging 0 would read as a
// broken field and drag the level down with it.
func TestRequestLoggingDefaultsStatusTo200(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	srv := &Server{
		Scanner: &fakeScanner{},
		Logger:  slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	h := srv.loggingMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	lines := requestLines(t, buf)
	if len(lines) == 0 {
		t.Fatalf("no request log line: %s", buf.String())
	}
	if got := lines[0]["status"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", got)
	}
}
