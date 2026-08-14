// Package paperless implements the "paperless" destinations.Destination
// module (design doc:
// docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
// sec. 5.3): it multipart-POSTs an assembled scan document to a
// Paperless-ngx instance's POST /api/documents/post_document/ endpoint.
//
// Paperless's own ingestion is asynchronous: a successful POST returns
// a bare JSON string task_id, e.g. "5af5cbd5-a8a8-49d9-af42-0f815d0caa0c"
// (verified live against Paperless-ngx v3.0.5 and against upstream
// source, see decodeTaskID's doc comment — NOT the {"task_id": "<uuid>"}
// object form this file originally, wrongly, assumed), and queues a
// Celery consumption task — it does not return a document ID and does
// not mean the document has finished indexing. Deliver's nil error
// means exactly "Paperless accepted the upload for consumption",
// matching the Destination interface's own documented contract for
// asynchronous destinations. Nothing in this module polls
// GET /api/tasks/ for completion (design sec. 7, Option A for v1) —
// that is an explicit, deferred follow-up (design sec. 7 Option C),
// not an oversight.
package paperless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations"
)

// destinationName is both the name this module registers under and the
// value Name() returns — it is also the profile schema's
// destinations[].target value (design sec. 6).
const destinationName = "paperless"

// defaultTokenSecretName is the config.SecretResolver name this module
// resolves the Paperless API token under when a profile's destination
// config does not override token_secret (design sec. 5.3, matching the
// 2026-04-30 spec's already-documented convention: Docker secret file
// paperless_api_token, env var PAPERLESS_API_TOKEN).
const defaultTokenSecretName = "paperless_api_token"

// postDocumentPath is Paperless-ngx's upload endpoint, verified against
// upstream docs (github.com/paperless-ngx/paperless-ngx docs/api.md,
// dev branch) — see design sec. 2.
const postDocumentPath = "/api/documents/post_document/"

// defaultHTTPTimeout bounds the whole HTTP round trip (headers + body)
// as a client-side safety net, mirroring internal/dispatch's
// httpUnixClient. Per-call cancellation is expected to come from the
// context passed to Deliver.
const defaultHTTPTimeout = 60 * time.Second

// maxErrorBodyBytes caps how much of a non-2xx response body this
// module reads into an error message, so a misbehaving Paperless
// instance cannot make a failed upload OOM the caller.
const maxErrorBodyBytes = 8 << 10 // 8 KiB

// Sentinel errors classifying a failed Deliver call. Each is wrapped
// (via %w) so callers can use errors.Is regardless of the exact
// message, matching internal/dispatch's sentinel-error convention.
var (
	// ErrConfig means a profile's destinations[].config block for
	// this module is missing base_url, has a non-absolute base_url,
	// or overrides token_secret with something other than a
	// non-empty string. Returned by both NewDestination (fail fast
	// at profile-load time) and Deliver (the interface's own
	// contract: cfg is "for its own per-profile configuration",
	// decoded fresh on every call).
	ErrConfig = errors.New("paperless: invalid destination config")
	// ErrUnreachable means the HTTP request to Paperless could not
	// be completed at the transport level (DNS, connection refused,
	// TLS, connection reset, ...) — distinct from ErrTimeout, which
	// is specifically a context deadline.
	ErrUnreachable = errors.New("paperless: unreachable")
	// ErrTimeout means the caller's context deadline was exceeded
	// before the upload completed.
	ErrTimeout = errors.New("paperless: timed out")
	// ErrRejected means Paperless returned a 4xx response — the
	// upload itself was malformed, unauthorized, or otherwise
	// rejected before being queued for consumption.
	ErrRejected = errors.New("paperless: upload rejected")
	// ErrServerError means Paperless returned a 5xx response.
	ErrServerError = errors.New("paperless: server error")
	// ErrInvalidResponse means Paperless answered 200/201 but the
	// body did not decode into {"task_id": "<uuid>"} with a non-empty
	// task_id — per design sec. 2, a synchronous {"id": N}-style body
	// must never be assumed.
	ErrInvalidResponse = errors.New("paperless: invalid response")
)

func init() {
	destinations.Register(destinationName, NewDestination)
}

// Config is this module's decoded view of a profile's
// destinations[].config block (design sec. 5.3, narrowed to what
// Deliver itself needs — profile-level tag/correspondent/document-type
// defaults and doc-type-map merging are resolved upstream of Deliver,
// into the Metadata this module receives, not decoded here).
type Config struct {
	// BaseURL is the Paperless-ngx instance's base URL, trailing
	// slash trimmed. Required.
	BaseURL string
	// TokenSecretName is the config.SecretResolver name the API
	// token is resolved under. Defaults to defaultTokenSecretName
	// when the profile config omits token_secret.
	TokenSecretName string
}

// decodeConfig decodes a profile's destinations[].config block
// (map[string]any, ProfileDestinationConfig.Config) into Config,
// validating base_url and any token_secret override. Used both by
// NewDestination (fail fast at profile-load/Build time) and Deliver
// (per the Destination interface's contract that cfg is decoded for
// "its own per-profile configuration" on every call).
func decodeConfig(raw map[string]any) (Config, error) {
	cfg := Config{TokenSecretName: defaultTokenSecretName}

	baseURLValue, ok := raw["base_url"]
	if !ok {
		return Config{}, fmt.Errorf("%w: base_url is required", ErrConfig)
	}
	baseURL, ok := baseURLValue.(string)
	if !ok || baseURL == "" {
		return Config{}, fmt.Errorf("%w: base_url must be a non-empty string", ErrConfig)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || !parsed.IsAbs() {
		return Config{}, fmt.Errorf("%w: base_url %q is not an absolute URL", ErrConfig, baseURL)
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if tokenSecretValue, ok := raw["token_secret"]; ok {
		tokenSecret, ok := tokenSecretValue.(string)
		if !ok || tokenSecret == "" {
			return Config{}, fmt.Errorf("%w: token_secret must be a non-empty string", ErrConfig)
		}
		cfg.TokenSecretName = tokenSecret
	}

	return cfg, nil
}

// destination is this module's destinations.Destination implementation.
type destination struct {
	secrets    config.SecretResolver
	httpClient *http.Client
}

// NewDestination is the destinations.Constructor registered for
// destinationName. It validates cfg.Config once at Build time so a
// profile-config mistake (missing base_url, ...) surfaces at load time
// rather than on the first scan.
func NewDestination(cfg destinations.ProfileDestinationConfig, secrets config.SecretResolver) (destinations.Destination, error) {
	if _, err := decodeConfig(cfg.Config); err != nil {
		return nil, err
	}
	return &destination{
		secrets:    secrets,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}, nil
}

// Name implements destinations.Destination.
func (d *destination) Name() string { return destinationName }

// Deliver implements destinations.Destination: it multipart-POSTs doc
// to Paperless's post_document/ endpoint, labelled by the fields meta
// carries, using cfg for this call's base_url/token_secret. A nil
// error means "Paperless accepted the upload for consumption" (see the
// package doc comment) — it does not mean consumption finished. The
// returned DeliveryResult carries Status "submitted" and Reference set
// to Paperless's task_id, so a caller (internal/api's
// deliverToDestination) can surface it in the scan response (design
// doc sec. 8). On a non-nil error the returned DeliveryResult is the
// zero value, per the Destination interface's contract.
func (d *destination) Deliver(ctx context.Context, doc destinations.Document, meta destinations.Metadata, cfg destinations.ProfileDestinationConfig) (destinations.DeliveryResult, error) {
	parsed, err := decodeConfig(cfg.Config)
	if err != nil {
		return destinations.DeliveryResult{}, err
	}

	token, err := d.secrets.Resolve(parsed.TokenSecretName)
	if err != nil {
		return destinations.DeliveryResult{}, fmt.Errorf("paperless: resolve token secret %q: %w", parsed.TokenSecretName, err)
	}

	body, contentType, err := buildUploadBody(doc, meta)
	if err != nil {
		return destinations.DeliveryResult{}, fmt.Errorf("paperless: build upload body: %w", err)
	}

	endpoint := parsed.BaseURL + postDocumentPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return destinations.DeliveryResult{}, fmt.Errorf("paperless: build request for %s: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Token "+token) // token itself never logged/wrapped into an error message

	resp, err := d.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return destinations.DeliveryResult{}, fmt.Errorf("paperless: upload to %s: %w", endpoint, ErrTimeout)
		}
		return destinations.DeliveryResult{}, fmt.Errorf("paperless: upload to %s: %w: %w", endpoint, ErrUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	taskID, err := handleUploadResponse(resp)
	if err != nil {
		return destinations.DeliveryResult{}, err
	}
	return destinations.DeliveryResult{Status: "submitted", Reference: taskID}, nil
}

// buildUploadBody assembles the multipart/form-data body Paperless's
// post_document/ endpoint expects (design sec. 2/5.3, verified against
// upstream docs/api.md): a "document" file part, then one optional
// field per Metadata value that is actually present, plus "tags" as a
// repeated integer field — one form part per tag ID, never
// comma-joined and never tag names (the mistake this design
// explicitly corrects, sec. 2).
func buildUploadBody(doc destinations.Document, meta destinations.Metadata) (io.Reader, string, error) {
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)

	if err := writeDocumentPart(mw, doc); err != nil {
		return nil, "", err
	}

	if meta.Title != "" {
		if err := mw.WriteField("title", meta.Title); err != nil {
			return nil, "", fmt.Errorf("write title field: %w", err)
		}
	}
	if meta.Created != nil {
		if err := mw.WriteField("created", meta.Created.Format(time.RFC3339)); err != nil {
			return nil, "", fmt.Errorf("write created field: %w", err)
		}
	}
	if meta.Correspondent != nil {
		if err := mw.WriteField("correspondent", strconv.Itoa(*meta.Correspondent)); err != nil {
			return nil, "", fmt.Errorf("write correspondent field: %w", err)
		}
	}
	if meta.DocumentType != nil {
		if err := mw.WriteField("document_type", strconv.Itoa(*meta.DocumentType)); err != nil {
			return nil, "", fmt.Errorf("write document_type field: %w", err)
		}
	}
	if meta.ASN != nil {
		if err := mw.WriteField("archive_serial_number", strconv.Itoa(*meta.ASN)); err != nil {
			return nil, "", fmt.Errorf("write archive_serial_number field: %w", err)
		}
	}
	for _, tagID := range meta.TagIDs {
		if err := mw.WriteField("tags", strconv.Itoa(tagID)); err != nil {
			return nil, "", fmt.Errorf("write tags field: %w", err)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}
	return buf, mw.FormDataContentType(), nil
}

// writeDocumentPart writes doc's bytes as the "document" form part.
// multipart.Writer.CreateFormFile always sets Content-Type:
// application/octet-stream and cannot be overridden, so this uses
// CreatePart directly to preserve doc.ContentType (falling back to
// application/octet-stream when doc did not set one, matching
// CreateFormFile's own default).
func writeDocumentPart(mw *multipart.Writer, doc destinations.Document) error {
	contentType := doc.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="document"; filename=%q`, doc.Filename))
	header.Set("Content-Type", contentType)

	part, err := mw.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create document part: %w", err)
	}
	if doc.Content == nil {
		return nil
	}
	if _, err := io.Copy(part, doc.Content); err != nil {
		return fmt.Errorf("copy document content: %w", err)
	}
	return nil
}

// postDocumentResponse is the OBJECT-form shape some Paperless-ngx
// version, fork, or reverse-proxy configuration might send in response
// to post_document/ — {"task_id": "<uuid>"}. decodeTaskID accepts this
// as a fallback, but it is NOT what the real, current upstream API
// sends (see decodeTaskID's doc comment) — never assume this is the
// primary shape again.
type postDocumentResponse struct {
	TaskID string `json:"task_id"`
}

// decodeTaskID decodes Paperless's post_document/ 200/201 response body
// into a task_id.
//
// The REAL, verified response shape is a BARE JSON string, e.g.
// "5af5cbd5-a8a8-49d9-af42-0f815d0caa0c" — NOT {"task_id": "<uuid>"} as
// this package's doc comments and the design spec originally (wrongly)
// assumed. Verified two ways against Paperless-ngx v3.0.5:
//  1. A live upload against a real instance returned the bare string.
//  2. Upstream source (github.com/paperless-ngx/paperless-ngx
//     src/documents/views.py, PostDocumentView.post) ends with
//     `return Response(async_task.id)` — DRF serializes a bare UUID
//     as a bare JSON string, never wrapped in an object.
//
// decodeTaskID therefore tries the bare-string form first. If that
// fails to decode, it falls back to the {"task_id": "<uuid>"} object
// form (postDocumentResponse) for robustness against an older/forked
// Paperless-ngx version or a proxy that re-wraps the body — but the
// bare string is the form every real deployment this module has been
// tested against actually sends.
func decodeTaskID(body []byte) (string, error) {
	var bare string
	if err := json.Unmarshal(body, &bare); err == nil {
		return bare, nil
	}
	var wrapped postDocumentResponse
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return "", err
	}
	return wrapped.TaskID, nil
}

// handleUploadResponse classifies resp into the accepted task_id
// (accepted for consumption) or one of the package's sentinel errors,
// per design sec. 5.3's "Response handling" and the sync/async framing
// in sec. 7. On error the returned string is always empty.
func handleUploadResponse(resp *http.Response) (string, error) {
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))

	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		if readErr != nil {
			return "", fmt.Errorf("paperless: read response body: %w", readErr)
		}
		taskID, err := decodeTaskID(body)
		if err != nil {
			return "", fmt.Errorf("paperless: decode response body %q: %w: %w", trimBody(body), ErrInvalidResponse, err)
		}
		if taskID == "" {
			return "", fmt.Errorf("paperless: response missing task_id: %w", ErrInvalidResponse)
		}
		return taskID, nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return "", fmt.Errorf("paperless: upload rejected (%d): %s: %w", resp.StatusCode, trimBody(body), ErrRejected)
	case resp.StatusCode >= 500:
		return "", fmt.Errorf("paperless: server error (%d): %s: %w", resp.StatusCode, trimBody(body), ErrServerError)
	default:
		return "", fmt.Errorf("paperless: unexpected status %d: %s", resp.StatusCode, trimBody(body))
	}
}

// trimBody renders a response body for inclusion in an error message:
// empty becomes "<empty body>", anything else is trimmed of
// surrounding whitespace so multi-line HTML/JSON error pages don't
// blow up a one-line error message.
func trimBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	if s == "" {
		return "<empty body>"
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
