package procapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"

	"github.com/strausmann/paperless-scan-bridge/components/scan-processor/internal/pipeline"
)

// fakePipeline is an in-memory pipeline.Pipeline test double. It
// never shells out, so handlers_test.go covers the HTTP contract
// (request decoding, response encoding, error-status mapping,
// page_grouping orchestration) with no convert/tesseract/qpdf
// dependency — only exec_pipeline_test.go (build tag "integration")
// needs those binaries.
type fakePipeline struct {
	mu sync.Mutex

	result pipeline.Result
	err    error
	calls  int

	// started/proceed gate Process for the concurrency test: Process
	// closes started as soon as it is entered (so the test knows the
	// server mutex is held) and blocks on proceed until the test
	// releases it.
	started chan struct{}
	proceed chan struct{}

	// lastReq captures the most recent request Process was called
	// with, for assertions that the handler decoded the control
	// payload correctly.
	lastReq pipeline.Request
}

func (f *fakePipeline) Process(ctx context.Context, req pipeline.Request) (pipeline.Result, error) {
	f.mu.Lock()
	f.calls++
	f.lastReq = req
	f.mu.Unlock()

	if f.started != nil {
		close(f.started)
	}
	if f.proceed != nil {
		<-f.proceed
	}
	if f.err != nil {
		return pipeline.Result{}, f.err
	}
	return f.result, nil
}

func (f *fakePipeline) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newTestServer(t *testing.T, p pipeline.Pipeline) *Server {
	t.Helper()
	return &Server{
		Pipeline: p,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// buildProcessBody encodes payload plus pages into the multipart/mixed
// request body the wire contract requires, mirroring
// components/scan-bridge/internal/procclient/http_client.go's
// encodeProcessRequest (which this test cannot import — procapi must
// not depend on scan-bridge — so the encoding is reproduced here).
func buildProcessBody(t *testing.T, payload processRequestPayload, pages [][]byte) (body []byte, contentType string) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	metaBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal control payload: %v", err)
	}
	metaPart, err := mw.CreatePart(map[string][]string{"Content-Type": {"application/json"}})
	if err != nil {
		t.Fatalf("create control part: %v", err)
	}
	if _, err := metaPart.Write(metaBody); err != nil {
		t.Fatalf("write control part: %v", err)
	}

	for i, page := range pages {
		part, err := mw.CreatePart(map[string][]string{"Content-Type": {"image/tiff"}})
		if err != nil {
			t.Fatalf("create page part %d: %v", i, err)
		}
		if _, err := part.Write(page); err != nil {
			t.Fatalf("write page part %d: %v", i, err)
		}
	}

	boundary := mw.Boundary()
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return buf.Bytes(), "multipart/mixed; boundary=" + boundary
}

func doProcessPost(t *testing.T, srv *Server, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/process", bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
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

func validPayload() processRequestPayload {
	return processRequestPayload{
		RequestID:      "req-1",
		OCR:            ocrPayload{Enabled: true, Languages: []string{"deu", "eng"}},
		Deskew:         true,
		RemoveBlank:    true,
		RotatePages:    false,
		PageGrouping:   string(pipeline.PageGroupingCombined),
		OutputFormat:   string(pipeline.OutputFormatPDF),
		TimeoutSeconds: 90,
	}
}

// parsedMultipart is the decoded shape of a successful /process
// response.
type parsedMultipart struct {
	meta  processMetadata
	docs  [][]byte
	types []string
	names []string
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
	var meta processMetadata
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
			t.Fatalf("read document part: %v", err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read document bytes: %v", err)
		}
		out.docs = append(out.docs, data)
		out.types = append(out.types, part.Header.Get("Content-Type"))
		out.names = append(out.names, parseFilenameFromDisposition(part.Header.Get("Content-Disposition")))
	}
	return out
}

func parseFilenameFromDisposition(disposition string) string {
	_, params, err := mime.ParseMediaType(disposition)
	if err != nil {
		return ""
	}
	return params["filename"]
}

func TestHandleProcess_HappyPathCombined(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{
		result: pipeline.Result{
			Documents: []pipeline.Document{
				{
					Index:       0,
					Filename:    "req-1.pdf",
					Content:     []byte("assembled-pdf-bytes"),
					ContentType: "application/pdf",
					PageCount:   2,
					Warnings:    []string{"page 1: deskew skipped"},
				},
			},
			DurationMillis: 4321,
		},
	}
	srv := newTestServer(t, fp)

	payload := validPayload()
	body, contentType := buildProcessBody(t, payload, [][]byte{[]byte("tiff-page-1"), []byte("tiff-page-2")})

	rec := doProcessPost(t, srv, body, contentType)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	if fp.callCount() != 1 {
		t.Fatalf("Process called %d times, want 1", fp.callCount())
	}
	if fp.lastReq.RequestID != "req-1" {
		t.Errorf("decoded RequestID = %q, want req-1", fp.lastReq.RequestID)
	}
	if !fp.lastReq.OCR.Enabled || len(fp.lastReq.OCR.Languages) != 2 {
		t.Errorf("decoded OCR = %+v, want enabled with 2 languages", fp.lastReq.OCR)
	}
	if len(fp.lastReq.Pages) != 2 {
		t.Fatalf("decoded Pages count = %d, want 2", len(fp.lastReq.Pages))
	}
	if string(fp.lastReq.Pages[0].Data) != "tiff-page-1" || string(fp.lastReq.Pages[1].Data) != "tiff-page-2" {
		t.Errorf("decoded page bytes did not round-trip: %q, %q",
			fp.lastReq.Pages[0].Data, fp.lastReq.Pages[1].Data)
	}
	if fp.lastReq.PageGrouping != pipeline.PageGroupingCombined {
		t.Errorf("decoded PageGrouping = %q, want combined", fp.lastReq.PageGrouping)
	}
	if fp.lastReq.OutputFormat != pipeline.OutputFormatPDF {
		t.Errorf("decoded OutputFormat = %q, want pdf", fp.lastReq.OutputFormat)
	}

	parsed := parseMultipartResponse(t, rec)
	if parsed.meta.RequestID != "req-1" {
		t.Errorf("response RequestID = %q, want req-1", parsed.meta.RequestID)
	}
	if parsed.meta.DurationMs != 4321 {
		t.Errorf("response DurationMs = %d, want 4321", parsed.meta.DurationMs)
	}
	if len(parsed.meta.Documents) != 1 {
		t.Fatalf("response Documents count = %d, want 1", len(parsed.meta.Documents))
	}
	docMeta := parsed.meta.Documents[0]
	if docMeta.Filename != "req-1.pdf" || docMeta.PageCount != 2 || docMeta.ContentType != "application/pdf" {
		t.Errorf("unexpected document metadata: %+v", docMeta)
	}
	if len(docMeta.Warnings) != 1 || docMeta.Warnings[0] != "page 1: deskew skipped" {
		t.Errorf("unexpected document warnings: %v", docMeta.Warnings)
	}

	if len(parsed.docs) != 1 {
		t.Fatalf("response document parts = %d, want 1", len(parsed.docs))
	}
	if string(parsed.docs[0]) != "assembled-pdf-bytes" {
		t.Errorf("document bytes = %q, want assembled-pdf-bytes", parsed.docs[0])
	}
	if parsed.types[0] != "application/pdf" {
		t.Errorf("document Content-Type = %q, want application/pdf", parsed.types[0])
	}
	if parsed.names[0] != "req-1.pdf" {
		t.Errorf("document filename = %q, want req-1.pdf", parsed.names[0])
	}
}

func TestHandleProcess_PerPageMultipleDocuments(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{
		result: pipeline.Result{
			Documents: []pipeline.Document{
				{Index: 0, Filename: "req-2-page-1.jpeg", Content: []byte("doc0"), ContentType: "image/jpeg", PageCount: 1},
				{Index: 1, Filename: "req-2-page-2.jpeg", Content: []byte("doc1"), ContentType: "image/jpeg", PageCount: 1},
			},
			DurationMillis: 111,
		},
	}
	srv := newTestServer(t, fp)

	payload := validPayload()
	payload.RequestID = "req-2"
	payload.PageGrouping = string(pipeline.PageGroupingPerPage)
	payload.OutputFormat = string(pipeline.OutputFormatJPEG)
	body, contentType := buildProcessBody(t, payload, [][]byte{[]byte("tiff-page-1")})

	rec := doProcessPost(t, srv, body, contentType)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if fp.lastReq.PageGrouping != pipeline.PageGroupingPerPage {
		t.Errorf("decoded PageGrouping = %q, want per_page", fp.lastReq.PageGrouping)
	}

	parsed := parseMultipartResponse(t, rec)
	if len(parsed.docs) != 2 {
		t.Fatalf("response document parts = %d, want 2", len(parsed.docs))
	}
	if parsed.names[0] != "req-2-page-1.jpeg" || parsed.names[1] != "req-2-page-2.jpeg" {
		t.Errorf("unexpected filenames: %q, %q", parsed.names[0], parsed.names[1])
	}
}

func TestHandleProcess_PipelineErrorMapsToStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		err       error
		wantCode  int
		wantError string
	}{
		{"unsupported format", pipeline.ErrUnsupportedFormat, http.StatusBadRequest, "unsupported_format"},
		{"busy", pipeline.ErrBusy, http.StatusConflict, "processor_busy"},
		{"ocr failed", pipeline.ErrOCRFailed, http.StatusUnprocessableEntity, "processing_failed"},
		{"timeout", pipeline.ErrTimeout, http.StatusGatewayTimeout, "processing_timeout"},
		{"generic failure", errors.New("boom: exit status 2"), http.StatusInternalServerError, "process_failed"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fp := &fakePipeline{err: tc.err}
			srv := newTestServer(t, fp)

			body, contentType := buildProcessBody(t, validPayload(), [][]byte{[]byte("page")})
			rec := doProcessPost(t, srv, body, contentType)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			var respBody errorResponse
			if err := json.NewDecoder(rec.Body).Decode(&respBody); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if respBody.Error != tc.wantError {
				t.Errorf("error = %q, want %q", respBody.Error, tc.wantError)
			}
		})
	}
}

func TestHandleProcess_InvalidPageGroupingRejectedBeforePipelineCall(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{}
	srv := newTestServer(t, fp)

	payload := validPayload()
	payload.PageGrouping = "every_other_page"
	body, contentType := buildProcessBody(t, payload, [][]byte{[]byte("page")})

	rec := doProcessPost(t, srv, body, contentType)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	var respBody errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&respBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if respBody.Error != "invalid_request" {
		t.Errorf("error = %q, want invalid_request", respBody.Error)
	}
	if fp.callCount() != 0 {
		t.Errorf("Process called %d times, want 0 (rejected before pipeline)", fp.callCount())
	}
}

func TestHandleProcess_InvalidOutputFormatRejectedBeforePipelineCall(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{}
	srv := newTestServer(t, fp)

	payload := validPayload()
	payload.OutputFormat = "docx"
	body, contentType := buildProcessBody(t, payload, [][]byte{[]byte("page")})

	rec := doProcessPost(t, srv, body, contentType)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if fp.callCount() != 0 {
		t.Errorf("Process called %d times, want 0 (rejected before pipeline)", fp.callCount())
	}
}

func TestHandleProcess_NoPagesReturns400(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{}
	srv := newTestServer(t, fp)

	body, contentType := buildProcessBody(t, validPayload(), nil)
	rec := doProcessPost(t, srv, body, contentType)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if fp.callCount() != 0 {
		t.Errorf("Process called %d times, want 0", fp.callCount())
	}
}

func TestHandleProcess_MalformedContentTypeReturns400(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{}
	srv := newTestServer(t, fp)

	rec := doProcessPost(t, srv, []byte("not a multipart body"), "application/json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if fp.callCount() != 0 {
		t.Errorf("Process called %d times, want 0", fp.callCount())
	}
}

func TestHandleProcess_MissingBoundaryReturns400(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{}
	srv := newTestServer(t, fp)

	rec := doProcessPost(t, srv, []byte("body"), "multipart/mixed")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleProcess_MalformedControlJSONReturns400(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{}
	srv := newTestServer(t, fp)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	ctrlPart, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json"}})
	if err != nil {
		t.Fatalf("create control part: %v", err)
	}
	if _, err := ctrlPart.Write([]byte("{not valid json")); err != nil {
		t.Fatalf("write control part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	rec := doProcessPost(t, srv, buf.Bytes(), "multipart/mixed; boundary="+mw.Boundary())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if fp.callCount() != 0 {
		t.Errorf("Process called %d times, want 0", fp.callCount())
	}
}

func TestHandleProcess_ConcurrentRequestsSecondGets409(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{
		result:  pipeline.Result{Documents: []pipeline.Document{{Index: 0, Filename: "a.pdf", Content: []byte("x"), ContentType: "application/pdf", PageCount: 1}}},
		started: make(chan struct{}),
		proceed: make(chan struct{}),
	}
	srv := newTestServer(t, fp)

	body, contentType := buildProcessBody(t, validPayload(), [][]byte{[]byte("page")})

	var rec1 *httptest.ResponseRecorder
	done := make(chan struct{})
	go func() {
		defer close(done)
		rec1 = doProcessPost(t, srv, body, contentType)
	}()

	<-fp.started // first request is now inside Process, holding the server's processing slot

	body2, contentType2 := buildProcessBody(t, validPayload(), [][]byte{[]byte("page")})
	rec2 := doProcessPost(t, srv, body2, contentType2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second request status = %d, want 409; body = %s", rec2.Code, rec2.Body.String())
	}
	var respBody errorResponse
	if err := json.NewDecoder(rec2.Body).Decode(&respBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if respBody.Error != "processor_busy" {
		t.Errorf("error = %q, want processor_busy", respBody.Error)
	}

	close(fp.proceed)
	<-done
	if rec1.Code != http.StatusOK {
		t.Errorf("first request status = %d, want 200; body = %s", rec1.Code, rec1.Body.String())
	}
}

func TestHandleProcess_ZeroTimeoutFallsBackToDefault(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{
		result: pipeline.Result{Documents: []pipeline.Document{{Index: 0, Filename: "a.pdf", Content: []byte("x"), ContentType: "application/pdf", PageCount: 1}}},
	}
	srv := newTestServer(t, fp)

	payload := validPayload()
	payload.TimeoutSeconds = 0
	body, contentType := buildProcessBody(t, payload, [][]byte{[]byte("page")})

	rec := doProcessPost(t, srv, body, contentType)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	// The interesting assertion is that a zero timeout_seconds did not
	// make the handler reject the request or fail to reach the
	// pipeline — defaultProcessTimeout must have been substituted.
	if fp.callCount() != 1 {
		t.Errorf("Process called %d times, want 1", fp.callCount())
	}
}

func TestHandleHealth_Returns200(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, &fakePipeline{})
	rec := doGet(t, srv, "/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode health body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
}

// TestDefaultMaxRequestBytes pins defaultMaxRequestBytes's value.
// cmd/scan-processor/main.go's own copy of this constant carries the
// derivation rationale (a real page at the repo's own
// deploy/profiles/default.yaml scan profile: 300 DPI, Color, A4 ≈
// 25 MiB/page uncompressed TIFF, sent as one /process POST per whole
// scan) and asserts the identical literal via its own
// TestDefaultMaxRequestBytes -- this test and that one are how the
// two packages' independently-declared constants are kept in sync
// without a cross-package export (see this package's api.go and
// main.go's doc comments).
func TestDefaultMaxRequestBytes(t *testing.T) {
	t.Parallel()

	const want = 512 << 20 // 512 MiB
	if defaultMaxRequestBytes != want {
		t.Errorf("defaultMaxRequestBytes = %d, want %d (512 MiB)", defaultMaxRequestBytes, want)
	}
}

// TestHandleProcess_RequestBodyTooLargeReturns413 covers
// decodeProcessRequest's http.MaxBytesReader wrap (issue #47): a body
// bigger than Server.MaxRequestBytes must be rejected with 413
// before the pipeline is ever invoked.
func TestHandleProcess_RequestBodyTooLargeReturns413(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	srv.MaxRequestBytes = 32 // deliberately tiny -- smaller than a real control payload + one page

	body, contentType := buildProcessBody(t, validPayload(), [][]byte{[]byte("a fairly long page of tiff-looking bytes that will not fit")})
	rec := doProcessPost(t, srv, body, contentType)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body = %s", rec.Code, rec.Body.String())
	}
	var respBody errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&respBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if respBody.Error != "request_too_large" {
		t.Errorf("error = %q, want request_too_large", respBody.Error)
	}
	if fp.callCount() != 0 {
		t.Errorf("Process called %d times, want 0 (rejected before pipeline)", fp.callCount())
	}
}

// TestHandleProcess_RequestBodyWithinLimitStillWorks is
// TestHandleProcess_RequestBodyTooLargeReturns413's happy-path
// counterpart: a small Server.MaxRequestBytes must not reject a
// legitimate, well-under-the-limit request.
func TestHandleProcess_RequestBodyWithinLimitStillWorks(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{
		result: pipeline.Result{Documents: []pipeline.Document{{Index: 0, Filename: "a.pdf", Content: []byte("x"), ContentType: "application/pdf", PageCount: 1}}},
	}
	srv := newTestServer(t, fp)
	srv.MaxRequestBytes = 8192

	body, contentType := buildProcessBody(t, validPayload(), [][]byte{[]byte("page")})
	rec := doProcessPost(t, srv, body, contentType)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
}

// TestHandleProcess_OCRLanguageAllowlist covers validateProcessRequest's
// ocr.languages check (issue #47): a language not among the runtime
// image's installed tessdata packs is rejected with 400 before the
// pipeline ever runs (turning a wasted OCR round-trip + 422 into a
// cheap, immediate rejection); an allowed language passes through
// unchanged; and a request with OCR disabled is never checked at all
// (an unsupported entry there is inert -- exec_pipeline.go never
// reads Languages unless OCR.Enabled).
func TestHandleProcess_OCRLanguageAllowlist(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		ocr          ocrPayload
		wantCode     int
		wantPipeline bool
	}{
		{
			name:         "allowed languages pass through",
			ocr:          ocrPayload{Enabled: true, Languages: []string{"deu", "eng"}},
			wantCode:     http.StatusOK,
			wantPipeline: true,
		},
		{
			name:         "unsupported language rejected before pipeline",
			ocr:          ocrPayload{Enabled: true, Languages: []string{"fra"}},
			wantCode:     http.StatusBadRequest,
			wantPipeline: false,
		},
		{
			name:         "argv-injection-shaped language rejected the same as any other unknown one",
			ocr:          ocrPayload{Enabled: true, Languages: []string{"-x"}},
			wantCode:     http.StatusBadRequest,
			wantPipeline: false,
		},
		{
			name:         "ocr disabled: an unsupported language is inert and not checked",
			ocr:          ocrPayload{Enabled: false, Languages: []string{"fra"}},
			wantCode:     http.StatusOK,
			wantPipeline: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fp := &fakePipeline{
				result: pipeline.Result{Documents: []pipeline.Document{{Index: 0, Filename: "a.pdf", Content: []byte("x"), ContentType: "application/pdf", PageCount: 1}}},
			}
			srv := newTestServer(t, fp)

			payload := validPayload()
			payload.OCR = tc.ocr
			body, contentType := buildProcessBody(t, payload, [][]byte{[]byte("page")})
			rec := doProcessPost(t, srv, body, contentType)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			gotCalled := fp.callCount() == 1
			if gotCalled != tc.wantPipeline {
				t.Errorf("Process called %d times, want called=%v", fp.callCount(), tc.wantPipeline)
			}
			if tc.wantCode == http.StatusBadRequest {
				var respBody errorResponse
				if err := json.NewDecoder(rec.Body).Decode(&respBody); err != nil {
					t.Fatalf("decode error body: %v", err)
				}
				if respBody.Error != "invalid_request" {
					t.Errorf("error = %q, want invalid_request", respBody.Error)
				}
				if !strings.Contains(respBody.Hint, "tessdata") {
					t.Errorf("hint = %q, want it to mention tessdata", respBody.Hint)
				}
			}
		})
	}
}

// TestHandleProcess_OCRLanguageAllowlistOverride covers
// Server.AllowedOCRLanguages: a deployment that overrides the default
// {deu, eng} set must have its own allowlist enforced instead.
func TestHandleProcess_OCRLanguageAllowlistOverride(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{
		result: pipeline.Result{Documents: []pipeline.Document{{Index: 0, Filename: "a.pdf", Content: []byte("x"), ContentType: "application/pdf", PageCount: 1}}},
	}
	srv := newTestServer(t, fp)
	srv.AllowedOCRLanguages = map[string]bool{"fra": true}

	payload := validPayload()
	payload.OCR = ocrPayload{Enabled: true, Languages: []string{"deu"}} // no longer allowed under the override
	body, contentType := buildProcessBody(t, payload, [][]byte{[]byte("page")})
	rec := doProcessPost(t, srv, body, contentType)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (deu excluded from the overridden allowlist); body = %s", rec.Code, rec.Body.String())
	}

	payload.OCR.Languages = []string{"fra"}
	body, contentType = buildProcessBody(t, payload, [][]byte{[]byte("page")})
	rec = doProcessPost(t, srv, body, contentType)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fra allowed under the override); body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleProcess_NegativeTimeoutReturns400(t *testing.T) {
	t.Parallel()

	fp := &fakePipeline{}
	srv := newTestServer(t, fp)

	payload := validPayload()
	payload.TimeoutSeconds = -5
	body, contentType := buildProcessBody(t, payload, [][]byte{[]byte("page")})

	rec := doProcessPost(t, srv, body, contentType)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if fp.callCount() != 0 {
		t.Errorf("Process called %d times, want 0", fp.callCount())
	}
}
