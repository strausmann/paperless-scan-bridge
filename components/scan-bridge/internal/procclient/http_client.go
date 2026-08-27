package procclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// processEndpoint and healthEndpoint are fake URLs — the transport
// always dials the configured Unix socket regardless of host, so the
// value only needs to be a syntactically valid URL for net/http to
// parse (mirrors dispatch/http_client.go's scanEndpoint/healthEndpoint).
const (
	processEndpoint = "http://scan-processor.local/process"
	healthEndpoint  = "http://scan-processor.local/health"
)

// processRequestPayload is the JSON control payload — part 0 of the
// outgoing multipart/mixed POST /process request body sent to
// scan-processor. Field names/shape are the frozen contract this
// package defines (design doc sec. 4.2): request_id, ocr,
// deskew/remove_blank/rotate_pages (the profile's existing processing
// flags), page_grouping, output_format, timeout_seconds.
type processRequestPayload struct {
	RequestID      string     `json:"request_id"`
	OCR            ocrPayload `json:"ocr"`
	Deskew         bool       `json:"deskew"`
	RemoveBlank    bool       `json:"remove_blank"`
	RotatePages    bool       `json:"rotate_pages"`
	PageGrouping   string     `json:"page_grouping"`
	OutputFormat   string     `json:"output_format"`
	TimeoutSeconds int        `json:"timeout_seconds"`
}

type ocrPayload struct {
	Enabled       bool     `json:"enabled"`
	Languages     []string `json:"languages"`
	MinConfidence float64  `json:"min_confidence,omitempty"`
}

// processMetadata is part 0 of the multipart/mixed 200 OK response —
// again per the frozen contract with scan-processor.
type processMetadata struct {
	RequestID  string             `json:"request_id"`
	Documents  []documentMetadata `json:"documents"`
	DurationMs int64              `json:"duration_ms"`
}

// documentMetadata describes one assembled document; parts 1..N of the
// response carry the matching bytes in the same order as this slice.
type documentMetadata struct {
	Index         int      `json:"index"`
	PageCount     int      `json:"page_count"`
	Filename      string   `json:"filename"`
	ContentType   string   `json:"content_type"`
	Warnings      []string `json:"warnings"`
	OCRConfidence float64  `json:"ocr_confidence"`
	LowConfidence bool     `json:"low_confidence"`
}

// processErrorEnvelope is scan-processor's non-200 error body:
// {"error": "...", "hint": "..."}. It mirrors dispatch's
// saneRuntimeErrorEnvelope and internal/api's errorResponse but is
// declared independently here — procclient does not (and must not)
// import either.
type processErrorEnvelope struct {
	Error string `json:"error"`
	Hint  string `json:"hint"`
}

// httpUnixClient is the production procclient.Client. It speaks HTTP to
// scan-processor over a Unix-domain socket and writes the documents of
// a completed /process call under outputDir/<requestID>/.
type httpUnixClient struct {
	httpClient *http.Client
	outputDir  string
}

// NewHTTPUnixClient builds a procclient.Client that dials socketPath
// for every request, regardless of the URL host passed to http.Client
// — the DialContext override ignores the network/address net/http
// derives from the URL and always connects to the Unix socket.
// Completed documents are written under outputDir/<requestID>/.
// timeout bounds the whole HTTP round trip (headers + body) as a
// client-side safety net; per-call cancellation is expected to come
// from the context passed to Process (the caller derives it from the
// profile's timeout_seconds, mirroring dispatch.NewHTTPUnixClient's
// same contract).
func NewHTTPUnixClient(socketPath, outputDir string, timeout time.Duration) Client {
	dialer := &net.Dialer{}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &httpUnixClient{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
		outputDir: outputDir,
	}
}

// Process sends one job's pages to scan-processor and, on success,
// writes each returned document to outputDir/<req.RequestID>/<filename>.
func (c *httpUnixClient) Process(ctx context.Context, req ProcessRequest) (ProcessResult, error) {
	body, contentType, err := encodeProcessRequest(req)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("procclient: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, processEndpoint, bytes.NewReader(body))
	if err != nil {
		return ProcessResult{}, fmt.Errorf("procclient: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ProcessResult{}, fmt.Errorf("procclient: %w", ErrTimeout)
		}
		return ProcessResult{}, fmt.Errorf("procclient: scan-processor unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ProcessResult{}, mapScanProcessorError(resp)
	}

	return c.readProcessResponse(req.RequestID, resp)
}

// Ping calls scan-processor's /health and reports whether it answered
// 200 OK.
func (c *httpUnixClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthEndpoint, nil)
	if err != nil {
		return fmt.Errorf("procclient: build ping request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("procclient: ping scan-processor: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("procclient: ping scan-processor returned %d", resp.StatusCode)
	}
	return nil
}

// Close releases the pooled Unix-socket connections. It never returns
// a non-nil error — CloseIdleConnections cannot fail — but keeps the
// error return so callers can treat every Client uniformly (mirrors
// dispatch.httpUnixClient.Close).
func (c *httpUnixClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// encodeProcessRequest builds the multipart/mixed request body: part 0
// the JSON control payload, parts 1..N the TIFF pages read off
// req.PagePaths. Returns the encoded body and the Content-Type header
// value (including the boundary) to send it with.
func encodeProcessRequest(req ProcessRequest) ([]byte, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	payload := processRequestPayload{
		RequestID:      req.RequestID,
		OCR:            ocrPayload{Enabled: req.OCR.Enabled, Languages: req.OCR.Languages, MinConfidence: req.OCR.MinConfidence},
		Deskew:         req.Deskew,
		RemoveBlank:    req.RemoveBlank,
		RotatePages:    req.RotatePages,
		PageGrouping:   string(req.PageGrouping),
		OutputFormat:   string(req.OutputFormat),
		TimeoutSeconds: req.TimeoutSeconds,
	}
	metaBody, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("marshal control payload: %w", err)
	}
	metaPart, err := mw.CreatePart(map[string][]string{"Content-Type": {"application/json"}})
	if err != nil {
		return nil, "", fmt.Errorf("create control-payload part: %w", err)
	}
	if _, err := metaPart.Write(metaBody); err != nil {
		return nil, "", fmt.Errorf("write control-payload part: %w", err)
	}

	for i, path := range req.PagePaths {
		if err := writePagePart(mw, path); err != nil {
			return nil, "", fmt.Errorf("page %d (%q): %w", i, path, err)
		}
	}

	boundary := mw.Boundary()
	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}
	return buf.Bytes(), "multipart/mixed; boundary=" + boundary, nil
}

func writePagePart(mw *multipart.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open page: %w", err)
	}
	// Read-only: nothing is buffered, so Close cannot report lost data.
	// Dropped at the call site rather than by a linter exclusion, which
	// would have covered the write side too.
	defer func() { _ = f.Close() }()

	part, err := mw.CreatePart(map[string][]string{"Content-Type": {"image/tiff"}})
	if err != nil {
		return fmt.Errorf("create page part: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return fmt.Errorf("copy page bytes: %w", err)
	}
	return nil
}

// mapScanProcessorError classifies a non-200 scan-processor response by
// status code into one of the package sentinel errors, wrapping it
// (%w) so errors.Is still matches after the extra context. Statuses
// outside the four scan-processor is documented to use (400/409/422/504)
// — including a genuinely unexpected 500 — fall through to a generic
// wrapped error (mirrors dispatch.mapSaneRuntimeError).
func mapScanProcessorError(resp *http.Response) error {
	var env processErrorEnvelope
	// Best-effort decode: scan-processor is documented to always send
	// the {"error","hint"} envelope on non-200, but a malformed or
	// empty body must not stop us from reporting the status code we do
	// have.
	_ = json.NewDecoder(resp.Body).Decode(&env)

	switch resp.StatusCode {
	case http.StatusBadRequest:
		return wrapScanProcessorError(resp.StatusCode, env, ErrUnsupportedFormat)
	case http.StatusConflict:
		return wrapScanProcessorError(resp.StatusCode, env, ErrBusy)
	case http.StatusUnprocessableEntity:
		return wrapScanProcessorError(resp.StatusCode, env, ErrOCRFailed)
	case http.StatusGatewayTimeout:
		return wrapScanProcessorError(resp.StatusCode, env, ErrTimeout)
	default:
		code := env.Error
		if code == "" {
			code = "unknown_error"
		}
		return fmt.Errorf("procclient: scan-processor returned %d (%s)", resp.StatusCode, code)
	}
}

func wrapScanProcessorError(status int, env processErrorEnvelope, sentinel error) error {
	if env.Hint != "" {
		return fmt.Errorf("procclient: scan-processor %d (%s): %w", status, env.Hint, sentinel)
	}
	return fmt.Errorf("procclient: scan-processor %d: %w", status, sentinel)
}

// readProcessResponse decodes the multipart/mixed 200 OK body: part 0
// is the processMetadata JSON, parts 1..N are the assembled documents,
// written to outputDir/<requestID>/<filename>. requestID is the
// caller-supplied ProcessRequest.RequestID, not a value read back from
// the response (mirrors dispatch.readScanResponse's use of the
// caller-supplied jobID for the same reason: it is authoritative,
// scan-processor's echoed value is not trusted for path construction).
func (c *httpUnixClient) readProcessResponse(requestID string, resp *http.Response) (ProcessResult, error) {
	mediaType, params, err := parseMultipartContentType(resp.Header.Get("Content-Type"))
	if err != nil {
		return ProcessResult{}, fmt.Errorf("procclient: parse content-type: %w", err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return ProcessResult{}, fmt.Errorf("procclient: unexpected content-type %q, want multipart/*", mediaType)
	}
	boundary, ok := params["boundary"]
	if !ok {
		return ProcessResult{}, errors.New("procclient: multipart response missing boundary parameter")
	}

	mr := multipart.NewReader(resp.Body, boundary)

	metaPart, err := mr.NextPart()
	if err != nil {
		return ProcessResult{}, fmt.Errorf("procclient: read metadata part: %w", err)
	}
	var meta processMetadata
	metaErr := json.NewDecoder(metaPart).Decode(&meta)
	_ = metaPart.Close()
	if metaErr != nil {
		return ProcessResult{}, fmt.Errorf("procclient: decode metadata part: %w", metaErr)
	}

	jobDir := filepath.Join(c.outputDir, requestID)
	if err := os.MkdirAll(jobDir, 0o750); err != nil {
		return ProcessResult{}, fmt.Errorf("procclient: create output dir %q: %w", jobDir, err)
	}

	documents := make([]Document, 0, len(meta.Documents))
	for i, docMeta := range meta.Documents {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return ProcessResult{}, fmt.Errorf(
				"procclient: metadata declares %d document(s) but response has only %d part(s)",
				len(meta.Documents), i)
		}
		if err != nil {
			return ProcessResult{}, fmt.Errorf("procclient: read document part %d: %w", i, err)
		}

		docPath, pathErr := safeDocumentPath(jobDir, docMeta.Filename)
		if pathErr != nil {
			_ = part.Close()
			return ProcessResult{}, fmt.Errorf("procclient: document %d: %w", i, pathErr)
		}

		writeErr := writeDocumentPart(docPath, part)
		_ = part.Close()
		if writeErr != nil {
			return ProcessResult{}, writeErr
		}

		documents = append(documents, Document{
			Index:         docMeta.Index,
			Filename:      docMeta.Filename,
			Path:          docPath,
			ContentType:   docMeta.ContentType,
			PageCount:     docMeta.PageCount,
			Warnings:      docMeta.Warnings,
			OCRConfidence: docMeta.OCRConfidence,
			LowConfidence: docMeta.LowConfidence,
		})
	}

	if extra, err := mr.NextPart(); err == nil {
		_ = extra.Close()
		return ProcessResult{}, fmt.Errorf(
			"procclient: response has more parts than the %d document(s) declared in metadata",
			len(meta.Documents))
	}

	return ProcessResult{
		RequestID:      requestID,
		Documents:      documents,
		DurationMillis: meta.DurationMs,
	}, nil
}

// safeDocumentPath joins dir with filename after rejecting anything
// that is not a bare, single-segment name — scan-processor's suggested
// Filename is untrusted input from another container, and writing it
// into dir unchecked would let a compromised or buggy scan-processor
// write outside outputDir (path traversal via "../", an absolute path,
// or an empty name).
func safeDocumentPath(dir, filename string) (string, error) {
	if filename == "" {
		return "", errors.New("empty filename")
	}
	if filename == "." || filename == ".." {
		return "", fmt.Errorf("unsafe filename %q", filename)
	}
	clean := filepath.Base(filename)
	if clean != filename || strings.ContainsAny(filename, `/\`) {
		return "", fmt.Errorf("unsafe filename %q", filename)
	}
	return filepath.Join(dir, clean), nil
}

// parseMultipartContentType parses a Content-Type header value into its
// media type and parameters (thin wrapper over mime.ParseMediaType,
// named here so both production code and tests share one call site for
// the response's multipart boundary parsing).
func parseMultipartContentType(contentType string) (string, map[string]string, error) {
	return mime.ParseMediaType(contentType)
}

func writeDocumentPart(path string, r io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("procclient: create document file %q: %w", path, err)
	}

	if _, err := io.Copy(f, r); err != nil {
		// See writePagePart in internal/dispatch for the same shape.
		_ = f.Close()
		return fmt.Errorf("procclient: write document file %q: %w", path, err)
	}

	// The assembled document is what reaches Paperless-ngx. A dropped
	// Close error here means uploading a truncated PDF and reporting
	// success for it.
	if err := f.Close(); err != nil {
		return fmt.Errorf("procclient: close document file %q: %w", path, err)
	}
	return nil
}
