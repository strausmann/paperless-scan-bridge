package dispatch

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

// scanEndpoint and healthEndpoint are fake URLs — the transport always
// dials the configured Unix socket regardless of host, so the value
// only needs to be a syntactically valid URL for net/http to parse.
const (
	scanEndpoint   = "http://sane-runtime.local/scan"
	healthEndpoint = "http://sane-runtime.local/health"
)

// scanRequestPayload is the wire shape of the POST /scan request body
// sent to sane-runtime. Field order/names/tags are the frozen contract
// shared with the sane-runtime implementation (Task 7): request_id,
// device (always empty — scan-bridge does not pick a device, profile
// Source does that via SANE's own selection), source, resolution,
// mode, format (always "tiff", never Profile.Format — the raw capture
// format is independent of the profile's final container format,
// which scan-processor produces downstream), max_pages (the profile's
// own cap; 0 drains the ADF, which is what every profile did before
// the field existed), timeout_seconds.
type scanRequestPayload struct {
	RequestID      string `json:"request_id"`
	Device         string `json:"device"`
	Source         string `json:"source"`
	Resolution     int    `json:"resolution"`
	Mode           string `json:"mode"`
	Format         string `json:"format"`
	MaxPages       int    `json:"max_pages"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// scanMetadata is part 0 of the multipart/mixed 200 OK response —
// again per the frozen contract with sane-runtime.
type scanMetadata struct {
	RequestID  string `json:"request_id"`
	PageCount  int    `json:"page_count"`
	DurationMs int64  `json:"duration_ms"`
	Device     string `json:"device"`
	Source     string `json:"source"`
	Resolution int    `json:"resolution"`
	Mode       string `json:"mode"`
}

// saneRuntimeErrorEnvelope is sane-runtime's non-200 error body:
// {"error": "...", "hint": "..."}. It mirrors internal/api's
// errorResponse but is declared independently here — dispatch does
// not (and must not) import internal/api.
type saneRuntimeErrorEnvelope struct {
	Error string `json:"error"`
	Hint  string `json:"hint"`
}

// httpUnixClient is the production dispatch.Client. It speaks HTTP to
// sane-runtime over a Unix-domain socket (ADR 0009) and writes the
// pages of a completed scan under outputDir/<jobID>/.
type httpUnixClient struct {
	httpClient *http.Client
	outputDir  string
}

// NewHTTPUnixClient builds a dispatch.Client that dials socketPath for
// every request, regardless of the URL host passed to http.Client —
// the DialContext override ignores the network/address net/http
// derives from the URL and always connects to the Unix socket.
// Completed scan pages are written under outputDir/<jobID>/.
// timeout bounds the whole HTTP round trip (headers + body) as a
// client-side safety net; per-call cancellation is expected to come
// from the context passed to Dispatch (the caller derives it from the
// profile's timeout_seconds).
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

// Dispatch sends one scan request to sane-runtime and, on success,
// writes each returned page to outputDir/<req.JobID>/page-N.<ext>.
func (c *httpUnixClient) Dispatch(ctx context.Context, req Request) (Response, error) {
	payload := scanRequestPayload{
		RequestID:      req.JobID,
		Device:         "",
		Source:         req.Profile.Source,
		Resolution:     req.Profile.Resolution,
		Mode:           string(req.Profile.Mode),
		Format:         "tiff",
		MaxPages:       req.Profile.MaxPages,
		TimeoutSeconds: req.Profile.TimeoutSeconds,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("dispatch: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, scanEndpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("dispatch: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Response{}, fmt.Errorf("dispatch: %w", ErrTimeout)
		}
		return Response{}, fmt.Errorf("dispatch: sane-runtime unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Response{}, mapSaneRuntimeError(resp)
	}

	return c.readScanResponse(req.JobID, resp)
}

// Cancel is not yet exposed by sane-runtime (CONTAINER_SUITE.md
// sec. 5.4 lists /scan/{id}/cancel as future work).
func (c *httpUnixClient) Cancel(ctx context.Context, jobID string) error {
	return ErrNotImplemented
}

// Ping calls sane-runtime's /health and reports whether it answered
// 200 OK.
func (c *httpUnixClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthEndpoint, nil)
	if err != nil {
		return fmt.Errorf("dispatch: build ping request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("dispatch: ping sane-runtime: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dispatch: ping sane-runtime returned %d", resp.StatusCode)
	}
	return nil
}

// Close releases the pooled Unix-socket connections. It never returns
// a non-nil error — CloseIdleConnections cannot fail — but keeps the
// error return so callers can treat every Client uniformly.
func (c *httpUnixClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// mapSaneRuntimeError classifies a non-200 sane-runtime response by
// status code into one of the package sentinel errors, wrapping it
// (%w) so errors.Is still matches after the extra context. Statuses
// outside the four sane-runtime is documented to use (503/409/422/504)
// — including a genuinely unexpected 400 or 500 — fall through to a
// generic wrapped error; internal/api's mapDispatchError treats
// anything that is not one of the four sentinels as a 502.
func mapSaneRuntimeError(resp *http.Response) error {
	var env saneRuntimeErrorEnvelope
	// Best-effort decode: sane-runtime is documented to always send
	// the {"error","hint"} envelope on non-200, but a malformed or
	// empty body must not stop us from reporting the status code we
	// do have.
	_ = json.NewDecoder(resp.Body).Decode(&env)

	switch resp.StatusCode {
	case http.StatusServiceUnavailable:
		return wrapSaneRuntimeError(resp.StatusCode, env, ErrNoScannerDetected)
	case http.StatusConflict:
		return wrapSaneRuntimeError(resp.StatusCode, env, ErrBusy)
	case http.StatusUnprocessableEntity:
		return wrapSaneRuntimeError(resp.StatusCode, env, ErrNoDocuments)
	case http.StatusGatewayTimeout:
		return wrapSaneRuntimeError(resp.StatusCode, env, ErrTimeout)
	default:
		code := env.Error
		if code == "" {
			code = "unknown_error"
		}
		return fmt.Errorf("dispatch: sane-runtime returned %d (%s)", resp.StatusCode, code)
	}
}

func wrapSaneRuntimeError(status int, env saneRuntimeErrorEnvelope, sentinel error) error {
	if env.Hint != "" {
		return fmt.Errorf("dispatch: sane-runtime %d (%s): %w", status, env.Hint, sentinel)
	}
	return fmt.Errorf("dispatch: sane-runtime %d: %w", status, sentinel)
}

// readScanResponse decodes the multipart/mixed 200 OK body: part 0 is
// the scanMetadata JSON, parts 1..N are the scanned pages, written to
// outputDir/<jobID>/page-<n>.<ext>.
func (c *httpUnixClient) readScanResponse(jobID string, resp *http.Response) (Response, error) {
	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return Response{}, fmt.Errorf("dispatch: parse content-type: %w", err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return Response{}, fmt.Errorf("dispatch: unexpected content-type %q, want multipart/*", mediaType)
	}
	boundary, ok := params["boundary"]
	if !ok {
		return Response{}, errors.New("dispatch: multipart response missing boundary parameter")
	}

	mr := multipart.NewReader(resp.Body, boundary)

	metaPart, err := mr.NextPart()
	if err != nil {
		return Response{}, fmt.Errorf("dispatch: read metadata part: %w", err)
	}
	var meta scanMetadata
	metaErr := json.NewDecoder(metaPart).Decode(&meta)
	_ = metaPart.Close()
	if metaErr != nil {
		return Response{}, fmt.Errorf("dispatch: decode metadata part: %w", metaErr)
	}

	jobDir := filepath.Join(c.outputDir, jobID)
	if err := os.MkdirAll(jobDir, 0o750); err != nil {
		return Response{}, fmt.Errorf("dispatch: create output dir %q: %w", jobDir, err)
	}

	pages := make([]string, 0, meta.PageCount)
	for i := 1; ; i++ {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Response{}, fmt.Errorf("dispatch: read page part %d: %w", i, err)
		}

		pagePath := filepath.Join(jobDir, fmt.Sprintf("page-%d%s", i, extForContentType(part.Header.Get("Content-Type"))))
		writeErr := writePagePart(pagePath, part)
		_ = part.Close()
		if writeErr != nil {
			return Response{}, writeErr
		}
		pages = append(pages, pagePath)
	}

	return Response{
		JobID:          jobID,
		Pages:          pages,
		DurationMillis: meta.DurationMs,
	}, nil
}

// extForContentType maps a page part's Content-Type to a file
// extension. The scan contract always sends image/tiff (D3 in the
// implementation brief pins format to "tiff" regardless of profile);
// the fallback exists so an unexpected content type still produces a
// readable file instead of silently dropping data.
func extForContentType(contentType string) string {
	if strings.HasPrefix(contentType, "image/tiff") {
		return ".tiff"
	}
	return ".bin"
}

func writePagePart(path string, r io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("dispatch: create page file %q: %w", path, err)
	}

	if _, err := io.Copy(f, r); err != nil {
		// Close before returning; the copy error is the interesting
		// one, so a Close failure on this path is deliberately dropped.
		_ = f.Close()
		return fmt.Errorf("dispatch: write page file %q: %w", path, err)
	}

	// Not deferred, and not ignored. A write-side Close is where the
	// final flush happens: dropping its error turns a truncated scan
	// page into a silent success, and the caller assembles a PDF from
	// whatever made it to disk.
	if err := f.Close(); err != nil {
		return fmt.Errorf("dispatch: close page file %q: %w", path, err)
	}
	return nil
}
