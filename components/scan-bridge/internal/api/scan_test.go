package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/dispatch"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/procclient"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
)

// fakeDispatchClient is an in-memory dispatch.Client double for
// handler tests — the real transport is exercised in
// internal/dispatch/http_client_test.go.
type fakeDispatchClient struct {
	dispatchFn func(ctx context.Context, req dispatch.Request) (dispatch.Response, error)
	// pingFn backs Ping for ready_test.go. Unset (nil) keeps the
	// long-standing default of every other test here: Ping always
	// succeeds.
	pingFn func(ctx context.Context) error
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

func (f *fakeDispatchClient) Ping(ctx context.Context) error {
	if f.pingFn == nil {
		return nil
	}
	return f.pingFn(ctx)
}

func (f *fakeDispatchClient) Close() error { return nil }

// fakeProcClient is an in-memory procclient.Client double for handler
// tests — the real transport is exercised in
// internal/procclient/http_client_test.go.
type fakeProcClient struct {
	processFn func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error)
	pingFn    func(ctx context.Context) error
}

func (f *fakeProcClient) Process(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
	if f.processFn == nil {
		return procclient.ProcessResult{}, errors.New("fakeProcClient: processFn not set")
	}
	return f.processFn(ctx, req)
}

func (f *fakeProcClient) Ping(ctx context.Context) error {
	if f.pingFn == nil {
		return nil
	}
	return f.pingFn(ctx)
}

func (f *fakeProcClient) Close() error { return nil }

// fakeDestinationDouble is this file's own destinations.Destination
// conformance fake, mirroring internal/destinations/destination_test.go's
// fakeDestination — kept local (rather than exported from the
// destinations package for reuse) because that package's fake is
// deliberately unexported test-only scaffolding, and scan_test.go
// needs its own instance registered under a test-unique name anyway
// (the destinations package's registry is process-global and panics on
// a duplicate Register call).
type fakeDestinationDouble struct {
	name      string
	deliverFn func(ctx context.Context, doc destinations.Document, meta destinations.Metadata, cfg destinations.ProfileDestinationConfig) (destinations.DeliveryResult, error)
	calls     []fakeDeliverCall
}

type fakeDeliverCall struct {
	doc  destinations.Document
	meta destinations.Metadata
	cfg  destinations.ProfileDestinationConfig
}

// fakeDestinationDoubleDefaultResult is what Deliver returns on
// success when a test does not set deliverFn — "submitted", matching
// this suite's long-standing expectation for the plain happy path,
// with no destination-specific reference (tests that care about
// TaskID/Reference propagation set deliverFn explicitly).
var fakeDestinationDoubleDefaultResult = destinations.DeliveryResult{Status: "submitted"}

func (f *fakeDestinationDouble) Name() string { return f.name }

func (f *fakeDestinationDouble) Deliver(ctx context.Context, doc destinations.Document, meta destinations.Metadata, cfg destinations.ProfileDestinationConfig) (destinations.DeliveryResult, error) {
	f.calls = append(f.calls, fakeDeliverCall{doc: doc, meta: meta, cfg: cfg})
	if f.deliverFn == nil {
		return fakeDestinationDoubleDefaultResult, nil
	}
	return f.deliverFn(ctx, doc, meta, cfg)
}

// registerTestDestination registers dest under a name derived from
// t.Name() (unique per (sub)test, avoiding collisions against the
// destinations package's process-global, panic-on-duplicate registry —
// mirrors internal/destinations/destination_test.go's uniqueName
// helper) and returns that name for use as a profile's
// destinations[].target.
func registerTestDestination(t *testing.T, dest *fakeDestinationDouble) string {
	t.Helper()
	name := "scantest-" + strings.ReplaceAll(t.Name(), "/", "-")
	dest.name = name
	destinations.Register(name, func(destinations.ProfileDestinationConfig, config.SecretResolver) (destinations.Destination, error) {
		return dest, nil
	})
	return name
}

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

// scanTestProfileWithDestination returns a single-profile YAML
// document identical to scanTestProfilesYAML but routed to one
// destination target, optionally with an OCR block and a
// document_type — the shared fixture for every test that needs
// destination delivery, OCR wiring, or doc-type-map resolution
// exercised through the full HTTP handler.
func scanTestProfileWithDestination(target, extra string) string {
	return `
profiles:
  - name: receipts
    description: "Receipts, grayscale"
    source: "ADF"
    resolution: 200
    mode: "Gray"
    format: "pdf"
    page_size: "auto"
    timeout_seconds: 60
` + extra + `
    destinations:
      - target: "` + target + `"
        config:
          tag_ids: [3]
`
}

// newScanTestServer builds a Server wired for POST /scan tests: real
// profiles (parsed from profilesYAML), the supplied auth config, and
// the supplied dispatch/proc clients (fakes in every test here).
func newScanTestServer(t *testing.T, profilesYAML string, auth config.AuthConfig, dispatchClient dispatch.Client, procClient procclient.Client) *Server {
	t.Helper()

	set, err := profiles.Parse(strings.NewReader(profilesYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return &Server{
		Profiles:   set,
		Build:      BuildInfo{Version: "0.1.0-test"},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:       auth,
		Dispatch:   dispatchClient,
		ProcClient: procClient,
		Secrets:    config.NewSecretResolver("", func(string) (string, bool) { return "", false }),
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

// writeProcessedDoc creates a small file under t.TempDir() standing in
// for one document procclient.Process wrote to OutputDir/<requestID>/,
// and returns a procclient.Document pointing at it — the shape
// deliverToDestination (scan_destinations.go) reads back off disk.
func writeProcessedDoc(t *testing.T, index int, filename, content string) procclient.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write processed doc: %v", err)
	}
	return procclient.Document{
		Index:       index,
		Filename:    filename,
		Path:        path,
		ContentType: "application/pdf",
		PageCount:   1,
	}
}

func TestScanNoAuthHeaderReturns401(t *testing.T) {
	t.Parallel()

	srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), &fakeDispatchClient{}, &fakeProcClient{})
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

	srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), &fakeDispatchClient{}, &fakeProcClient{})
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

	srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), &fakeDispatchClient{}, &fakeProcClient{})
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

	srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), &fakeDispatchClient{}, &fakeProcClient{})
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

// TestScanHappyPathNoDestinationsReturnsProcessedDocuments covers a
// profile that has not adopted the destinations schema (design doc
// sec. 6's "a profile that has not adopted the new schema yet ... has
// no Destinations entries" case): scan-processor still runs and the
// response still reports the assembled document(s), just with an
// empty (never nil, so the JSON body has "destinations":[] rather than
// null) destinations list per document.
func TestScanHappyPathNoDestinationsReturnsProcessedDocuments(t *testing.T) {
	t.Parallel()

	var gotDispatchReq dispatch.Request
	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			gotDispatchReq = req
			return dispatch.Response{
				JobID:          req.JobID,
				Pages:          []string{"/scans/" + req.JobID + "/page-1.tiff", "/scans/" + req.JobID + "/page-2.tiff"},
				DurationMillis: 4200,
			}, nil
		},
	}

	processedDoc := writeProcessedDoc(t, 0, "receipt.pdf", "pdf-bytes")
	var gotProcReq procclient.ProcessRequest
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			gotProcReq = req
			return procclient.ProcessResult{
				RequestID:      req.RequestID,
				Documents:      []procclient.Document{processedDoc},
				DurationMillis: 900,
			}, nil
		},
	}

	srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), dispatchClient, procClient)
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
	if body.ScanID == "" {
		t.Error("scan_id must not be empty")
	}
	if len(body.Documents) != 1 {
		t.Fatalf("len(documents) = %d, want 1", len(body.Documents))
	}
	doc := body.Documents[0]
	if doc.Filename != "receipt.pdf" || doc.PageCount != 1 {
		t.Errorf("documents[0] = %+v, want filename=receipt.pdf page_count=1", doc)
	}
	if doc.Destinations == nil || len(doc.Destinations) != 0 {
		t.Errorf("documents[0].Destinations = %v, want a non-nil empty slice", doc.Destinations)
	}

	// Dispatch/process wiring: gotDispatchReq.JobID feeds gotProcReq.RequestID
	// (same scan_id), and the dispatched pages become the process
	// request's PagePaths.
	if gotDispatchReq.JobID != body.ScanID {
		t.Errorf("dispatch JobID %q != response scan_id %q", gotDispatchReq.JobID, body.ScanID)
	}
	if gotProcReq.RequestID != body.ScanID {
		t.Errorf("process RequestID %q != response scan_id %q", gotProcReq.RequestID, body.ScanID)
	}
	if len(gotProcReq.PagePaths) != 2 {
		t.Errorf("process PagePaths = %v, want 2 entries (the dispatched pages)", gotProcReq.PagePaths)
	}
	if gotProcReq.OutputFormat != procclient.OutputFormatPDF {
		t.Errorf("process OutputFormat = %q, want pdf (profile.format)", gotProcReq.OutputFormat)
	}
	if gotProcReq.PageGrouping != procclient.PageGroupingCombined {
		t.Errorf("process PageGrouping = %q, want combined (profile default)", gotProcReq.PageGrouping)
	}
	if gotProcReq.OCR.Enabled {
		t.Errorf("process OCR.Enabled = true, want false (profile did not set ocr.enabled)")
	}
}

// TestScanOCRConfigPassedThroughToProcessor pins that a profile's
// ocr.enabled/languages block reaches scan-processor's control payload
// unchanged (design doc sec. 4.2/6).
func TestScanOCRConfigPassedThroughToProcessor(t *testing.T) {
	t.Parallel()

	profilesYAML := `
profiles:
  - name: receipts
    source: "ADF"
    resolution: 200
    mode: "Gray"
    format: "pdf"
    page_size: "auto"
    timeout_seconds: 60
    ocr:
      enabled: true
      languages: [deu, eng]
`
	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
		},
	}
	processedDoc := writeProcessedDoc(t, 0, "receipt.pdf", "pdf-bytes")
	var gotProcReq procclient.ProcessRequest
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			gotProcReq = req
			return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{processedDoc}}, nil
		},
	}

	srv := newScanTestServer(t, profilesYAML, tokenAuth(t, "correct-token"), dispatchClient, procClient)
	rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !gotProcReq.OCR.Enabled {
		t.Error("process OCR.Enabled = false, want true (profile set ocr.enabled)")
	}
	if got := gotProcReq.OCR.Languages; len(got) != 2 || got[0] != "deu" || got[1] != "eng" {
		t.Errorf("process OCR.Languages = %v, want [deu eng]", got)
	}
}

// TestScanAssemblyAndFormatPassedThroughToProcessor covers the
// procPageGrouping/procOutputFormat conversions' non-default branches
// (per_page / jpeg) — TestScanHappyPathNoDestinationsReturnsProcessedDocuments
// already covers their default branches (combined / pdf).
func TestScanAssemblyAndFormatPassedThroughToProcessor(t *testing.T) {
	t.Parallel()

	profilesYAML := `
profiles:
  - name: receipts
    source: "ADF"
    resolution: 200
    mode: "Gray"
    format: "jpeg"
    page_size: "auto"
    timeout_seconds: 60
    assembly:
      page_grouping: per_page
`
	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
		},
	}
	processedDoc := writeProcessedDoc(t, 0, "receipt.jpg", "jpeg-bytes")
	var gotProcReq procclient.ProcessRequest
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			gotProcReq = req
			return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{processedDoc}}, nil
		},
	}

	srv := newScanTestServer(t, profilesYAML, tokenAuth(t, "correct-token"), dispatchClient, procClient)
	rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if gotProcReq.PageGrouping != procclient.PageGroupingPerPage {
		t.Errorf("process PageGrouping = %q, want per_page", gotProcReq.PageGrouping)
	}
	if gotProcReq.OutputFormat != procclient.OutputFormatJPEG {
		t.Errorf("process OutputFormat = %q, want jpeg", gotProcReq.OutputFormat)
	}
}

// TestScanDestinationDocumentUnreadableReturnsFailedResult covers
// deliverToDestination's own error path: scan-processor's response
// claimed a document at a path that, by the time delivery runs, is not
// readable (e.g. removed, permissions changed) — the destination
// result reports "failed" with the open error, and (mirroring the
// Deliver-failure test) this must not fail the whole scan.
func TestScanDestinationDocumentUnreadableReturnsFailedResult(t *testing.T) {
	t.Parallel()

	dest := &fakeDestinationDouble{}
	target := registerTestDestination(t, dest)

	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
		},
	}
	missingDoc := procclient.Document{
		Index:       0,
		Filename:    "receipt.pdf",
		Path:        filepath.Join(t.TempDir(), "does-not-exist.pdf"),
		ContentType: "application/pdf",
		PageCount:   1,
	}
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{missingDoc}}, nil
		},
	}

	srv := newScanTestServer(t, scanTestProfileWithDestination(target, ""), tokenAuth(t, "correct-token"), dispatchClient, procClient)
	rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (an unreadable document must not fail the whole scan), body=%s", rec.Code, rec.Body.String())
	}
	var body scanResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := body.Documents[0].Destinations[0]
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.Error, "open assembled document") {
		t.Errorf("error = %q, want it to mention 'open assembled document'", got.Error)
	}
	if len(dest.calls) != 0 {
		t.Errorf("Deliver called %d times, want 0 (open failed before Deliver could run)", len(dest.calls))
	}
}

// TestScanDeliversToDestinationReturnsSubmitted covers the destination-
// success path (design doc sec. 8): a fake destination's Deliver call
// returns nil, and the response reports status "submitted" for it,
// with the resolved Metadata (destination default tag 3 + caller tag 7,
// add-merged) reaching Deliver.
func TestScanDeliversToDestinationReturnsSubmitted(t *testing.T) {
	t.Parallel()

	dest := &fakeDestinationDouble{}
	target := registerTestDestination(t, dest)

	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
		},
	}
	processedDoc := writeProcessedDoc(t, 0, "receipt.pdf", "pdf-bytes")
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{processedDoc}}, nil
		},
	}

	srv := newScanTestServer(t, scanTestProfileWithDestination(target, ""), tokenAuth(t, "correct-token"), dispatchClient, procClient)
	rec := postScan(t, srv, "correct-token", map[string]any{
		"profile": "receipts",
		"tag_ids": []int{7},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var body scanResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Documents) != 1 || len(body.Documents[0].Destinations) != 1 {
		t.Fatalf("documents = %+v, want exactly one document with one destination result", body.Documents)
	}
	got := body.Documents[0].Destinations[0]
	if got.Name != target || got.Status != "submitted" || got.Error != "" {
		t.Errorf("destination result = %+v, want name=%q status=submitted error=empty", got, target)
	}

	if len(dest.calls) != 1 {
		t.Fatalf("Deliver called %d times, want 1", len(dest.calls))
	}
	call := dest.calls[0]
	if call.doc.Filename != "receipt.pdf" {
		t.Errorf("Deliver doc.Filename = %q, want receipt.pdf", call.doc.Filename)
	}
	if call.doc.ID != body.ScanID {
		t.Errorf("Deliver doc.ID = %q, want scan_id %q", call.doc.ID, body.ScanID)
	}
	if len(call.meta.TagIDs) != 2 || call.meta.TagIDs[0] != 3 || call.meta.TagIDs[1] != 7 {
		t.Errorf("Deliver meta.TagIDs = %v, want [3 7] (destination default 3 + caller 7)", call.meta.TagIDs)
	}

	// The document's underlying file must still be openable/readable
	// after Deliver ran — deliverToDestination must not leave it in a
	// broken state for a hypothetical second destination.
	content, err := os.ReadFile(processedDoc.Path)
	if err != nil || string(content) != "pdf-bytes" {
		t.Errorf("processed doc file unreadable or altered after Deliver: content=%q err=%v", content, err)
	}
}

// TestScanDeliversToDestinationReturnsTaskID covers the gap
// destinations.Destination.Deliver's (DeliveryResult, error) signature
// closes (see internal/destinations/destination.go and
// internal/destinations/paperless/paperless.go): a destination's
// DeliveryResult.Reference (Paperless's task_id in production) must
// reach the response's destinationResult.TaskID field on a successful
// delivery — the design doc sec. 8 shape
// "destinations: [{name, status, task_id}]" is not fully honoured
// until this is wired through deliverToDestination
// (internal/api/scan_destinations.go).
func TestScanDeliversToDestinationReturnsTaskID(t *testing.T) {
	t.Parallel()

	const wantTaskID = "a1b2c3d4-e5f6-7890-1234-567890abcdef"
	dest := &fakeDestinationDouble{
		deliverFn: func(ctx context.Context, doc destinations.Document, meta destinations.Metadata, cfg destinations.ProfileDestinationConfig) (destinations.DeliveryResult, error) {
			return destinations.DeliveryResult{Status: "submitted", Reference: wantTaskID}, nil
		},
	}
	target := registerTestDestination(t, dest)

	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
		},
	}
	processedDoc := writeProcessedDoc(t, 0, "receipt.pdf", "pdf-bytes")
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{processedDoc}}, nil
		},
	}

	srv := newScanTestServer(t, scanTestProfileWithDestination(target, ""), tokenAuth(t, "correct-token"), dispatchClient, procClient)
	rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var body scanResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Documents) != 1 || len(body.Documents[0].Destinations) != 1 {
		t.Fatalf("documents = %+v, want exactly one document with one destination result", body.Documents)
	}
	got := body.Documents[0].Destinations[0]
	if got.Status != "submitted" {
		t.Errorf("status = %q, want submitted", got.Status)
	}
	if got.TaskID != wantTaskID {
		t.Errorf("task_id = %q, want %q (the destination's DeliveryResult.Reference)", got.TaskID, wantTaskID)
	}
}

// TestScanDestinationFailureDoesNotFailWholeScan covers the
// destination-error path (design doc sec. 8: "eine
// Destination-Failure ≠ Scan-Failure"): a fake destination's Deliver
// call returns an error, the overall request still answers 200, and
// the response reports status "failed" with that destination's error
// message for the affected document.
func TestScanDestinationFailureDoesNotFailWholeScan(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("paperless: upload rejected (401): invalid token")
	dest := &fakeDestinationDouble{
		deliverFn: func(ctx context.Context, doc destinations.Document, meta destinations.Metadata, cfg destinations.ProfileDestinationConfig) (destinations.DeliveryResult, error) {
			return destinations.DeliveryResult{}, wantErr
		},
	}
	target := registerTestDestination(t, dest)

	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
		},
	}
	processedDoc := writeProcessedDoc(t, 0, "receipt.pdf", "pdf-bytes")
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{processedDoc}}, nil
		},
	}

	srv := newScanTestServer(t, scanTestProfileWithDestination(target, ""), tokenAuth(t, "correct-token"), dispatchClient, procClient)
	rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a destination failure must not fail the whole scan), body=%s", rec.Code, rec.Body.String())
	}
	var body scanResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := body.Documents[0].Destinations[0]
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.Error != wantErr.Error() {
		t.Errorf("error = %q, want %q", got.Error, wantErr.Error())
	}
	if got.TaskID != "" {
		t.Errorf("task_id = %q, want empty on failure", got.TaskID)
	}
}

// TestScanUnknownDestinationTargetReturnsFailedResult covers a
// destinations[].target that has no registered constructor (a profile
// typo, or a destination module that is not blank-imported) — Build
// fails, and that failure surfaces as a per-document "failed" result
// rather than aborting the whole scan (mirrors the Deliver-failure
// case above; a build-time failure is just an earlier point in the
// same "one destination's problem doesn't sink the others" contract).
func TestScanUnknownDestinationTargetReturnsFailedResult(t *testing.T) {
	t.Parallel()

	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
		},
	}
	processedDoc := writeProcessedDoc(t, 0, "receipt.pdf", "pdf-bytes")
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{processedDoc}}, nil
		},
	}

	srv := newScanTestServer(t, scanTestProfileWithDestination("does-not-exist", ""), tokenAuth(t, "correct-token"), dispatchClient, procClient)
	rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var body scanResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := body.Documents[0].Destinations[0]
	if got.Name != "does-not-exist" || got.Status != "failed" {
		t.Errorf("destination result = %+v, want name=does-not-exist status=failed", got)
	}
	if !strings.Contains(got.Error, "unknown destination") {
		t.Errorf("error = %q, want it to mention unknown destination", got.Error)
	}
}

// TestScanDocumentTypeMapAffectsDelivery is the HTTP-level companion to
// scan_metadata_test.go's resolveMetadata unit tests: it proves the
// document_type_map resolution actually reaches Deliver's Metadata
// argument through the full handler, not just in isolation.
func TestScanDocumentTypeMapAffectsDelivery(t *testing.T) {
	t.Parallel()

	dest := &fakeDestinationDouble{}
	target := registerTestDestination(t, dest)

	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
		},
	}
	processedDoc := writeProcessedDoc(t, 0, "receipt.pdf", "pdf-bytes")
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{processedDoc}}, nil
		},
	}

	profilesYAML := `
profiles:
  - name: receipts
    source: "ADF"
    resolution: 200
    mode: "Gray"
    format: "pdf"
    page_size: "auto"
    timeout_seconds: 60
    document_type: eingangsrechnung
    destinations:
      - target: "` + target + `"
        config:
          tag_ids: [3]
          document_type_map:
            eingangsrechnung:
              document_type_id: 5
              tag_ids: [7]
`
	srv := newScanTestServer(t, profilesYAML, tokenAuth(t, "correct-token"), dispatchClient, procClient)
	rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(dest.calls) != 1 {
		t.Fatalf("Deliver called %d times, want 1", len(dest.calls))
	}
	meta := dest.calls[0].meta
	if meta.DocumentType == nil || *meta.DocumentType != 5 {
		t.Errorf("meta.DocumentType = %v, want *5 (from the document_type_map entry)", meta.DocumentType)
	}
	if len(meta.TagIDs) != 2 || meta.TagIDs[0] != 3 || meta.TagIDs[1] != 7 {
		t.Errorf("meta.TagIDs = %v, want [3 7]", meta.TagIDs)
	}
	if dest.calls[0].doc.DocType != "eingangsrechnung" {
		t.Errorf("doc.DocType = %q, want eingangsrechnung", dest.calls[0].doc.DocType)
	}
}

func TestScanTagMergeReflected(t *testing.T) {
	t.Parallel()

	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
		},
	}
	processedDoc := writeProcessedDoc(t, 0, "receipt.pdf", "pdf-bytes")
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{processedDoc}}, nil
		},
	}
	srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), dispatchClient, procClient)
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

			dispatchClient := &fakeDispatchClient{dispatchFn: tc.dispatchFn}
			srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), dispatchClient, &fakeProcClient{})
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

// TestScanProcessErrorsMapToStatus is TestScanDispatchErrorsMapToStatus's
// counterpart for the scan-processor leg of the pipeline
// (mapProcessError, scan.go).
func TestScanProcessErrorsMapToStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		processFn  func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error)
		wantStatus int
		wantError  string
	}{
		{
			// scan-processor's 400 means scan-bridge built it an
			// invalid request (a profile misconfiguration), not that
			// scan-processor itself misbehaved — mapProcessError maps
			// this to 500, not the generic-fallback 502 (issue #49
			// point 2).
			name: "unsupported format",
			processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
				return procclient.ProcessResult{}, procclient.ErrUnsupportedFormat
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "unsupported_output",
		},
		{
			name: "busy",
			processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
				return procclient.ProcessResult{}, procclient.ErrBusy
			},
			wantStatus: http.StatusConflict,
			wantError:  "processor_busy",
		},
		{
			name: "ocr failed",
			processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
				return procclient.ProcessResult{}, procclient.ErrOCRFailed
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantError:  "processing_failed",
		},
		{
			name: "timeout",
			processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
				return procclient.ProcessResult{}, procclient.ErrTimeout
			},
			wantStatus: http.StatusGatewayTimeout,
			wantError:  "timeout",
		},
		{
			name: "unclassified error maps to bad gateway",
			processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
				return procclient.ProcessResult{}, errors.New("boom")
			},
			wantStatus: http.StatusBadGateway,
			wantError:  "processing_dispatch_failed",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dispatchClient := &fakeDispatchClient{
				dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
					return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
				},
			}
			procClient := &fakeProcClient{processFn: tc.processFn}
			srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), dispatchClient, procClient)
			rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tc.wantStatus, rec.Body.String())
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

// TestScanRequestBodyTooLargeReturns413 covers handleScan's
// http.MaxBytesReader wrap (issue #47): a body bigger than
// Server.MaxRequestBytes must be rejected with 413, before it is ever
// handed to Dispatch.
func TestScanRequestBodyTooLargeReturns413(t *testing.T) {
	t.Parallel()

	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			t.Fatal("Dispatch must not be called for an oversized request body")
			return dispatch.Response{}, nil
		},
	}
	srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), dispatchClient, &fakeProcClient{})
	srv.MaxRequestBytes = 16 // deliberately tiny

	// Syntactically valid (if it were ever fully decoded) and
	// well-formed as far as the decoder can tell from the bytes it
	// DOES see -- unlike outright garbage, this cannot be rejected as
	// a JSON syntax error before MaxBytesReader's limit kicks in, so
	// the test actually exercises the size limit rather than
	// incidentally hitting the unrelated "invalid_body" branch first.
	oversized := []byte(`{"profile":"` + strings.Repeat("a", 100) + `"}`)
	rec := postScan(t, srv, "correct-token", oversized)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413, body=%s", rec.Code, rec.Body.String())
	}
	var body errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "request_too_large" {
		t.Errorf("error = %q, want request_too_large", body.Error)
	}
}

// TestScanRequestBodyWithinLimitStillWorks is
// TestScanRequestBodyTooLargeReturns413's happy-path counterpart: a
// small Server.MaxRequestBytes must not reject a legitimate,
// well-under-the-limit request.
func TestScanRequestBodyWithinLimitStillWorks(t *testing.T) {
	t.Parallel()

	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
		},
	}
	processedDoc := writeProcessedDoc(t, 0, "receipt.pdf", "pdf-bytes")
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{processedDoc}}, nil
		},
	}
	srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), dispatchClient, procClient)
	srv.MaxRequestBytes = 4096

	rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

// TestScanCleansUpOutputDirAfterHandling covers handleScan's
// OutputDir/<scan_id> cleanup (issue #49 point 1: the raw scanned
// pages and assembled documents there are PII). writeAssembledDoc
// below writes the fake processed document under
// outputDir/<scanID>/... exactly like the real procclient.httpUnixClient
// would (internal/procclient/http_client.go's readProcessResponse), so
// this exercises the real directory shape handleScan cleans up.
func TestScanCleansUpOutputDirAfterHandling(t *testing.T) {
	t.Parallel()

	writeAssembledDoc := func(t *testing.T, outputDir, scanID string) procclient.Document {
		t.Helper()
		dir := filepath.Join(outputDir, scanID)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir job dir: %v", err)
		}
		path := filepath.Join(dir, "receipt.pdf")
		if err := os.WriteFile(path, []byte("pdf-bytes"), 0o600); err != nil {
			t.Fatalf("write assembled doc: %v", err)
		}
		return procclient.Document{Index: 0, Filename: "receipt.pdf", Path: path, ContentType: "application/pdf", PageCount: 1}
	}

	t.Run("removed by default after a successful scan", func(t *testing.T) {
		t.Parallel()
		outputDir := t.TempDir()

		dispatchClient := &fakeDispatchClient{
			dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
				return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
			},
		}
		procClient := &fakeProcClient{
			processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
				doc := writeAssembledDoc(t, outputDir, req.RequestID)
				return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{doc}}, nil
			},
		}
		srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), dispatchClient, procClient)
		srv.OutputDir = outputDir

		rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
		var body scanResult
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}

		dir := filepath.Join(outputDir, body.ScanID)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("OutputDir/%s still exists after handleScan returned (err=%v), want removed", body.ScanID, err)
		}
	})

	t.Run("preserved when KeepScanOutput is set", func(t *testing.T) {
		t.Parallel()
		outputDir := t.TempDir()

		dispatchClient := &fakeDispatchClient{
			dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
				return dispatch.Response{JobID: req.JobID, Pages: []string{"/scans/p1.tiff"}}, nil
			},
		}
		procClient := &fakeProcClient{
			processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
				doc := writeAssembledDoc(t, outputDir, req.RequestID)
				return procclient.ProcessResult{RequestID: req.RequestID, Documents: []procclient.Document{doc}}, nil
			},
		}
		srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), dispatchClient, procClient)
		srv.OutputDir = outputDir
		srv.KeepScanOutput = true

		rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
		var body scanResult
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}

		dir := filepath.Join(outputDir, body.ScanID)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("OutputDir/%s was removed despite KeepScanOutput=true: %v", body.ScanID, err)
		}
	})

	// Cleanup must also run on the error paths -- the point of issue
	// #49's PII concern is exactly the case where the pipeline did NOT
	// finish successfully but Dispatch already wrote raw pages to
	// OutputDir/<scan_id>/ before scan-processor failed.
	t.Run("removed even when scan-processor fails", func(t *testing.T) {
		t.Parallel()
		outputDir := t.TempDir()

		var dispatchedJobID string
		dispatchClient := &fakeDispatchClient{
			dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
				dispatchedJobID = req.JobID
				// Mirror dispatch.httpUnixClient.Dispatch: it writes
				// raw pages under outputDir/<jobID>/ before handleScan
				// ever calls ProcClient.Process.
				dir := filepath.Join(outputDir, req.JobID)
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatalf("mkdir job dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "page-1.tiff"), []byte("tiff-bytes"), 0o600); err != nil {
					t.Fatalf("write raw page: %v", err)
				}
				return dispatch.Response{JobID: req.JobID, Pages: []string{filepath.Join(dir, "page-1.tiff")}}, nil
			},
		}
		procClient := &fakeProcClient{
			processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
				return procclient.ProcessResult{}, procclient.ErrOCRFailed
			},
		}
		srv := newScanTestServer(t, scanTestProfilesYAML, tokenAuth(t, "correct-token"), dispatchClient, procClient)
		srv.OutputDir = outputDir

		rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422, body=%s", rec.Code, rec.Body.String())
		}
		if dispatchedJobID == "" {
			t.Fatal("dispatchFn was never called")
		}

		dir := filepath.Join(outputDir, dispatchedJobID)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("OutputDir/%s still exists after a failed scan (err=%v), want removed", dispatchedJobID, err)
		}
	})
}

// TestScanPipelineTimeoutHeadroom covers pipelineTimeout: a profile
// with assembly.page_grouping=per_page and more than one destination
// gets a scaled-up context deadline (issue #49 point 3); every other
// shape keeps the unscaled profile.TimeoutSeconds budget.
func TestScanPipelineTimeoutHeadroom(t *testing.T) {
	t.Parallel()

	const timeoutSeconds = 60

	newDeadlineCapturingServer := func(t *testing.T, profilesYAML string) (*Server, *time.Time) {
		t.Helper()
		var gotDeadline time.Time
		dispatchClient := &fakeDispatchClient{
			dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
				d, ok := ctx.Deadline()
				if !ok {
					t.Fatal("dispatch context has no deadline")
				}
				gotDeadline = d
				return dispatch.Response{}, dispatch.ErrNoScannerDetected
			},
		}
		srv := newScanTestServer(t, profilesYAML, tokenAuth(t, "correct-token"), dispatchClient, &fakeProcClient{})
		return srv, &gotDeadline
	}

	t.Run("per_page with 2 destinations gets headroom", func(t *testing.T) {
		t.Parallel()

		// The destination targets below are never actually built --
		// dispatchFn fails before handleScan reaches
		// buildDestinations -- so they need no registered constructor;
		// pipelineTimeout only inspects
		// profile.DestinationConfigs()'s length, read straight off the
		// parsed YAML.
		profilesYAML := `
profiles:
  - name: receipts
    source: "ADF"
    resolution: 200
    mode: "Gray"
    format: "pdf"
    page_size: "auto"
    timeout_seconds: ` + fmt.Sprint(timeoutSeconds) + `
    assembly:
      page_grouping: per_page
    destinations:
      - target: "dest-a"
      - target: "dest-b"
`
		srv, gotDeadline := newDeadlineCapturingServer(t, profilesYAML)
		before := time.Now()
		rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503, body=%s", rec.Code, rec.Body.String())
		}

		got := gotDeadline.Sub(before)
		wantMin := (timeoutSeconds*perPageMultiDestinationHeadroomFactor - 5) * float64(time.Second)
		wantMax := (timeoutSeconds*perPageMultiDestinationHeadroomFactor + 5) * float64(time.Second)
		if float64(got) < wantMin || float64(got) > wantMax {
			t.Errorf("dispatch context budget = %s, want ~%gs (headroom factor %g applied to %ds)",
				got, timeoutSeconds*perPageMultiDestinationHeadroomFactor, perPageMultiDestinationHeadroomFactor, timeoutSeconds)
		}
	})

	t.Run("combined page_grouping keeps the unscaled budget even with 2 destinations", func(t *testing.T) {
		t.Parallel()

		profilesYAML := `
profiles:
  - name: receipts
    source: "ADF"
    resolution: 200
    mode: "Gray"
    format: "pdf"
    page_size: "auto"
    timeout_seconds: ` + fmt.Sprint(timeoutSeconds) + `
    destinations:
      - target: "dest-a"
      - target: "dest-b"
`
		srv, gotDeadline := newDeadlineCapturingServer(t, profilesYAML)
		before := time.Now()
		rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503, body=%s", rec.Code, rec.Body.String())
		}

		got := gotDeadline.Sub(before)
		wantMin := (timeoutSeconds - 5) * time.Second
		wantMax := (timeoutSeconds + 5) * time.Second
		if got < wantMin || got > wantMax {
			t.Errorf("dispatch context budget = %s, want ~%ds (no headroom -- combined page_grouping)", got, timeoutSeconds)
		}
	})

	t.Run("per_page with a single destination keeps the unscaled budget", func(t *testing.T) {
		t.Parallel()

		profilesYAML := `
profiles:
  - name: receipts
    source: "ADF"
    resolution: 200
    mode: "Gray"
    format: "pdf"
    page_size: "auto"
    timeout_seconds: ` + fmt.Sprint(timeoutSeconds) + `
    assembly:
      page_grouping: per_page
`
		srv, gotDeadline := newDeadlineCapturingServer(t, profilesYAML)
		before := time.Now()
		rec := postScan(t, srv, "correct-token", map[string]string{"profile": "receipts"})
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503, body=%s", rec.Code, rec.Body.String())
		}

		got := gotDeadline.Sub(before)
		wantMin := (timeoutSeconds - 5) * time.Second
		wantMax := (timeoutSeconds + 5) * time.Second
		if got < wantMin || got > wantMax {
			t.Errorf("dispatch context budget = %s, want ~%ds (no headroom -- 0 destinations)", got, timeoutSeconds)
		}
	})
}

func TestScanAuthMiddlewareIPAllowlistMode(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Auth.Mode = config.AuthModeIPAllowlist
	cfg.Auth.AllowedCIDRs = []string{"10.0.0.0/24"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	dispatchClient := &fakeDispatchClient{
		dispatchFn: func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			return dispatch.Response{JobID: req.JobID}, nil
		},
	}
	procClient := &fakeProcClient{
		processFn: func(ctx context.Context, req procclient.ProcessRequest) (procclient.ProcessResult, error) {
			return procclient.ProcessResult{RequestID: req.RequestID}, nil
		},
	}
	srv := newScanTestServer(t, scanTestProfilesYAML, cfg.Auth, dispatchClient, procClient)

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
