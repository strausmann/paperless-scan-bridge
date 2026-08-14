package procapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"sort"
	"strings"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-processor/internal/pipeline"
)

// writeJSON serialises body as JSON with the given status code. Encode
// failures are logged, not swallowed, mirroring
// components/sane-runtime/internal/scanapi.Server.writeJSON — by the
// time json.Encoder fails the status line is already written, so
// there is nothing left to recover, but per AGENTS.md's "do not
// silently swallow errors" the failure must still surface somewhere.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.logger().Warn("json encode failed",
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Any("err", err))
	}
}

// errorResponse is scan-processor's error envelope for non-200
// responses: {"error","hint"}, matching procclient's
// processErrorEnvelope field-for-field (design doc sec. 4.2: "Error
// envelope ... mirror dispatch's existing {error, hint}").
type errorResponse struct {
	Error string `json:"error"`
	Hint  string `json:"hint,omitempty"`
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, hint string) {
	s.writeJSON(w, r, status, errorResponse{Error: code, Hint: hint})
}

// isMaxBytesError reports whether err originates from an
// http.MaxBytesReader hitting its limit (issue #47). Both a direct
// *http.MaxBytesError (Go 1.19+, checked first) and a decoder that
// re-wraps the reader's error in its own text (mime/multipart's
// NextPart/Read paths do not consistently propagate the typed error
// unwrapped) are treated as the same condition — the message
// "http: request body too large" is MaxBytesError.Error()'s exact,
// stable text.
func isMaxBytesError(err error) bool {
	if err == nil {
		return false
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return true
	}
	return strings.Contains(err.Error(), "http: request body too large")
}

// healthResponse is the /health payload.
type healthResponse struct {
	Status string `json:"status"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, healthResponse{Status: "ok"})
}

// processRequestPayload is part 0 of the incoming multipart/mixed
// POST /process request body. Field names/shape/JSON tags are the
// frozen wire contract (design doc sec. 4.2), duplicated here from
// components/scan-bridge/internal/procclient/http_client.go's
// processRequestPayload rather than imported — see the package doc
// comment.
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
	Enabled   bool     `json:"enabled"`
	Languages []string `json:"languages"`
}

// processMetadata is part 0 of the multipart/mixed 200 OK response —
// again per the frozen contract.
type processMetadata struct {
	RequestID  string             `json:"request_id"`
	Documents  []documentMetadata `json:"documents"`
	DurationMs int64              `json:"duration_ms"`
}

type documentMetadata struct {
	Index       int      `json:"index"`
	PageCount   int      `json:"page_count"`
	Filename    string   `json:"filename"`
	ContentType string   `json:"content_type"`
	Warnings    []string `json:"warnings"`
}

// defaultProcessTimeout applies when the caller omits
// timeout_seconds (or sends 0) — matches the design doc's example
// profile.TimeoutSeconds (sec. 6 sample YAML: 120), and mirrors
// sane-runtime's defaultScanTimeout pattern of never blocking
// unbounded on a client omission.
const defaultProcessTimeout = 120 * time.Second

var allowedPageGroupings = map[string]bool{
	string(pipeline.PageGroupingCombined): true,
	string(pipeline.PageGroupingPerPage):  true,
}

var allowedOutputFormats = map[string]bool{
	string(pipeline.OutputFormatPDF):  true,
	string(pipeline.OutputFormatJPEG): true,
	string(pipeline.OutputFormatTIFF): true,
}

// validateProcessRequest checks the fields scan-processor can reject
// cheaply, before ever invoking the pipeline — mirrors
// scanapi.validateScanRequest's role and its "cheap rejection before
// touching the backend" rationale. It is a method (not a free
// function) so the ocr.languages check below can consult the
// Server's configured allowlist (issue #47).
func (s *Server) validateProcessRequest(req processRequestPayload) error {
	if req.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if !allowedPageGroupings[req.PageGrouping] {
		return fmt.Errorf("page_grouping %q is not supported (want %q or %q)",
			req.PageGrouping, pipeline.PageGroupingCombined, pipeline.PageGroupingPerPage)
	}
	if !allowedOutputFormats[req.OutputFormat] {
		return fmt.Errorf("output_format %q is not supported (want %q, %q, or %q)",
			req.OutputFormat, pipeline.OutputFormatPDF, pipeline.OutputFormatJPEG, pipeline.OutputFormatTIFF)
	}
	if req.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must not be negative")
	}
	// Only meaningful when OCR actually runs: an ocr.languages entry
	// on a request with ocr.enabled=false is inert (exec_pipeline.go
	// never reads Languages unless OCR.Enabled), so rejecting it here
	// would reject requests that carry harmless leftover config
	// rather than anything that could reach tesseract(1).
	if req.OCR.Enabled {
		allowed := s.allowedOCRLanguages()
		for _, lang := range req.OCR.Languages {
			if !allowed[lang] {
				return fmt.Errorf("ocr.languages: %q is not an installed tessdata language pack (want one of: %s)",
					lang, strings.Join(sortedKeys(allowed), ", "))
			}
		}
	}
	return nil
}

// sortedKeys returns m's keys in sorted order, for a deterministic
// error message in validateProcessRequest.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// handleProcess implements POST /process: decode the multipart/mixed
// request (control payload + TIFF pages), run it through the
// single-flight pipeline slot, and encode the result (or error) per
// the frozen wire contract.
func (s *Server) handleProcess(w http.ResponseWriter, r *http.Request) {
	req, pages, err := s.decodeProcessRequest(w, r)
	if err != nil {
		if isMaxBytesError(err) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large",
				fmt.Sprintf("request body exceeds the %d byte limit", s.maxRequestBytes()))
			return
		}
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.validateProcessRequest(req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(pages) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "no page parts in request body")
		return
	}

	if !s.processMu.TryLock() {
		s.writeError(w, r, http.StatusConflict, "processor_busy",
			"a process request is already in progress; scan-processor handles one request at a time")
		return
	}
	defer s.processMu.Unlock()

	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultProcessTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	pipelineReq := pipeline.Request{
		RequestID:      req.RequestID,
		Pages:          pages,
		OCR:            pipeline.OCRConfig{Enabled: req.OCR.Enabled, Languages: req.OCR.Languages},
		Deskew:         req.Deskew,
		RemoveBlank:    req.RemoveBlank,
		RotatePages:    req.RotatePages,
		PageGrouping:   pipeline.PageGrouping(req.PageGrouping),
		OutputFormat:   pipeline.OutputFormat(req.OutputFormat),
		TimeoutSeconds: req.TimeoutSeconds,
	}

	start := time.Now()
	result, err := s.Pipeline.Process(ctx, pipelineReq)
	if err != nil {
		s.writeProcessError(w, r, err)
		return
	}
	if result.DurationMillis == 0 {
		result.DurationMillis = time.Since(start).Milliseconds()
	}

	s.writeProcessResponse(w, r, req.RequestID, result)
}

// writeProcessError maps a Pipeline failure onto the HTTP
// status/error-code table from the wire contract (design doc sec.
// 4.2, frozen by procclient's doc comment). Order matters: sentinel
// checks must run before the generic fallback.
func (s *Server) writeProcessError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, pipeline.ErrUnsupportedFormat):
		s.writeError(w, r, http.StatusBadRequest, "unsupported_format", err.Error())
	case errors.Is(err, pipeline.ErrBusy):
		s.writeError(w, r, http.StatusConflict, "processor_busy", err.Error())
	case errors.Is(err, pipeline.ErrOCRFailed):
		s.writeError(w, r, http.StatusUnprocessableEntity, "processing_failed", err.Error())
	case errors.Is(err, pipeline.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		s.writeError(w, r, http.StatusGatewayTimeout, "processing_timeout", err.Error())
	default:
		s.logger().Error("process failed", slog.Any("err", err))
		s.writeError(w, r, http.StatusInternalServerError, "process_failed", err.Error())
	}
}

// decodeProcessRequest parses the incoming multipart/mixed body: part
// 0 is the JSON control payload, parts 1..N are the TIFF pages, read
// fully into memory (scan-processor has no shared volume with
// scan-bridge for this leg — design doc sec. 4.2 Option A — so pages
// only ever exist as request-body bytes, never as files scan-bridge
// already wrote).
//
// r.Body is wrapped in http.MaxBytesReader before any part is read
// (issue #47) so the io.ReadAll(part) calls below cannot be made to
// buffer an unbounded amount of memory — once the cumulative read
// across the control payload and every page part exceeds
// s.maxRequestBytes(), the next Read returns a *http.MaxBytesError,
// which handleProcess maps to 413 via isMaxBytesError.
func (s *Server) decodeProcessRequest(w http.ResponseWriter, r *http.Request) (processRequestPayload, []pipeline.Page, error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return processRequestPayload{}, nil, fmt.Errorf("parse content-type: %w", err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return processRequestPayload{}, nil, fmt.Errorf("unexpected content-type %q, want multipart/*", mediaType)
	}
	boundary, ok := params["boundary"]
	if !ok {
		return processRequestPayload{}, nil, errors.New("multipart request missing boundary parameter")
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes())
	mr := multipart.NewReader(r.Body, boundary)

	ctrlPart, err := mr.NextPart()
	if err != nil {
		return processRequestPayload{}, nil, fmt.Errorf("read control part: %w", err)
	}
	var req processRequestPayload
	dec := json.NewDecoder(ctrlPart)
	dec.DisallowUnknownFields()
	decErr := dec.Decode(&req)
	_ = ctrlPart.Close()
	if decErr != nil {
		return processRequestPayload{}, nil, fmt.Errorf("decode control payload: %w", decErr)
	}

	var pages []pipeline.Page
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return processRequestPayload{}, nil, fmt.Errorf("read page part %d: %w", len(pages), err)
		}
		data, readErr := io.ReadAll(part)
		_ = part.Close()
		if readErr != nil {
			return processRequestPayload{}, nil, fmt.Errorf("read page part %d body: %w", len(pages), readErr)
		}
		pages = append(pages, pipeline.Page{Data: data})
	}

	return req, pages, nil
}

// writeProcessResponse streams the multipart/mixed 200 OK response:
// part 0 is the JSON processMetadata, parts 1..N are the assembled
// documents' bytes, in the same order as the metadata's Documents
// slice — the frozen contract procclient.readProcessResponse decodes.
func (s *Server) writeProcessResponse(w http.ResponseWriter, r *http.Request, requestID string, result pipeline.Result) {
	mw := multipart.NewWriter(w)
	w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	w.WriteHeader(http.StatusOK)

	meta := processMetadata{
		RequestID:  requestID,
		DurationMs: result.DurationMillis,
	}
	for _, doc := range result.Documents {
		meta.Documents = append(meta.Documents, documentMetadata{
			Index:       doc.Index,
			PageCount:   doc.PageCount,
			Filename:    doc.Filename,
			ContentType: doc.ContentType,
			Warnings:    doc.Warnings,
		})
	}

	metaHeader := textproto.MIMEHeader{}
	metaHeader.Set("Content-Type", "application/json")
	metaPart, err := mw.CreatePart(metaHeader)
	if err != nil {
		s.logger().Error("create metadata part failed", slog.Any("err", err))
		return
	}
	if err := json.NewEncoder(metaPart).Encode(meta); err != nil {
		s.logger().Error("write metadata part failed", slog.Any("err", err))
		return
	}

	for i, doc := range result.Documents {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Type", doc.ContentType)
		hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="document"; filename=%q`, doc.Filename))
		part, err := mw.CreatePart(hdr)
		if err != nil {
			s.logger().Error("create document part failed", slog.Int("index", i), slog.Any("err", err))
			return
		}
		if _, err := part.Write(doc.Content); err != nil {
			s.logger().Error("write document part failed", slog.Int("index", i), slog.Any("err", err))
			return
		}
	}

	if err := mw.Close(); err != nil {
		s.logger().Error("close multipart writer failed", slog.Any("err", err))
	}
}
