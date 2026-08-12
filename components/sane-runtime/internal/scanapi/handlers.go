package scanapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/sane-runtime/internal/scanner"
)

// writeJSON serialises body as JSON with the given status code. Encode
// failures are logged, not swallowed, mirroring scan-bridge's
// internal/api.writeJSON — by the time json.Encoder fails the status
// line is already written, so there is nothing left to recover, but
// per the project's "no swallowed errors" rule the failure must still
// surface somewhere.
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

// errorResponse is the canonical error envelope for 4xx/5xx responses,
// matching scan-bridge's internal/api.errorResponse shape so clients
// of both daemons share one error contract.
type errorResponse struct {
	Error string `json:"error"`
	Hint  string `json:"hint,omitempty"`
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, hint string) {
	s.writeJSON(w, r, status, errorResponse{Error: code, Hint: hint})
}

// healthResponse is the /health payload. The handler answers 200
// unconditionally — /health is process liveness, not device
// readiness; that distinction is /ready's job.
type healthResponse struct {
	Status string `json:"status"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, healthResponse{Status: "ok"})
}

// readyResponse is the /ready payload.
type readyResponse struct {
	Ready       bool `json:"ready"`
	DeviceCount int  `json:"device_count"`
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	devices, err := s.Scanner.ListDevices(r.Context())
	if err != nil {
		s.logger().Warn("readiness check: ListDevices failed", slog.Any("err", err))
		s.writeJSON(w, r, http.StatusServiceUnavailable, readyResponse{Ready: false})
		return
	}
	if len(devices) == 0 {
		s.writeJSON(w, r, http.StatusServiceUnavailable, readyResponse{Ready: false, DeviceCount: 0})
		return
	}
	s.writeJSON(w, r, http.StatusOK, readyResponse{Ready: true, DeviceCount: len(devices)})
}

// scanRequest is the POST /scan JSON body. Fields mirror
// scanner.Params 1:1 except MaxPages/TimeoutSeconds, which stay
// request-shaped (int) rather than scanner.Params' matching int
// fields — same names, so the mapping in handleScan is a direct copy.
type scanRequest struct {
	RequestID      string `json:"request_id"`
	Device         string `json:"device"`
	Source         string `json:"source"`
	Resolution     int    `json:"resolution"`
	Mode           string `json:"mode"`
	Format         string `json:"format"`
	MaxPages       int    `json:"max_pages"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// scanMetadata is Part 0 of the multipart/mixed response: everything
// about the scan except the page bytes themselves.
type scanMetadata struct {
	RequestID  string `json:"request_id"`
	PageCount  int    `json:"page_count"`
	DurationMs int64  `json:"duration_ms"`
	Device     string `json:"device"`
	Source     string `json:"source"`
	Resolution int    `json:"resolution"`
	Mode       string `json:"mode"`
}

// defaultScanTimeout applies when the caller omits timeout_seconds
// (or sends 0). Chosen generously for a duplex ADF batch at the
// reference hardware's slowest supported resolution.
const defaultScanTimeout = 300 * time.Second

var allowedModes = map[string]bool{
	"Lineart": true,
	"Gray":    true,
	"Color":   true,
}

var allowedSources = map[string]bool{
	"ADF Front":  true,
	"ADF Duplex": true,
	"Flatbed":    true,
}

var allowedFormats = map[string]bool{
	"tiff": true,
	"pnm":  true,
}

// validateScanRequest checks the fields sane-runtime itself can
// reject cheaply, before ever touching the scanner. Anything it lets
// through and the hardware still rejects (e.g. a source string that
// is syntactically fine but not present on this device) surfaces
// later as a scanner.Err* mapped by writeScanError.
func validateScanRequest(req scanRequest) error {
	if req.Source == "" {
		return fmt.Errorf("source is required")
	}
	if !allowedSources[req.Source] {
		return fmt.Errorf("source %q is not supported", req.Source)
	}
	if req.Resolution < 75 || req.Resolution > 600 {
		return fmt.Errorf("resolution %d out of supported range 75..600", req.Resolution)
	}
	if !allowedModes[req.Mode] {
		return fmt.Errorf("mode %q is not supported", req.Mode)
	}
	if req.Format != "" && !allowedFormats[req.Format] {
		return fmt.Errorf("format %q is not supported", req.Format)
	}
	if req.MaxPages < 0 {
		return fmt.Errorf("max_pages must not be negative")
	}
	if req.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must not be negative")
	}
	return nil
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	var req scanRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := validateScanRequest(req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if !s.scanMu.TryLock() {
		s.writeError(w, r, http.StatusConflict, "scanner_busy",
			"a scan is already in progress; sane-runtime drives one scanner at a time")
		return
	}
	defer s.scanMu.Unlock()

	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultScanTimeout
	}
	scanCtx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	format := req.Format
	if format == "" {
		format = "tiff"
	}

	start := time.Now()
	pages, err := s.Scanner.Scan(scanCtx, scanner.Params{
		Device:         req.Device,
		Source:         req.Source,
		Resolution:     req.Resolution,
		Mode:           req.Mode,
		Format:         format,
		MaxPages:       req.MaxPages,
		TimeoutSeconds: req.TimeoutSeconds,
	})
	duration := time.Since(start)
	if err != nil {
		s.writeScanError(w, r, err)
		return
	}
	defer closeAllPages(pages)

	s.writeScanResponse(w, r, req, format, pages, duration)
}

// closeAllPages closes every page's backing reader, logging (not
// swallowing) any close error — the scan already succeeded by the
// time this runs, so a close failure must not turn into a 5xx, but it
// must not vanish silently either.
func closeAllPages(pages []scanner.Page) {
	for _, p := range pages {
		_ = p.Data.Close()
	}
}

// writeScanError maps a Scan failure onto the HTTP status/error-code
// table from the sane-runtime HTTP contract. Order matters: the
// sentinel checks must run before the generic fallback.
func (s *Server) writeScanError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, scanner.ErrNoScannerDetected):
		s.writeError(w, r, http.StatusServiceUnavailable, "no_scanner_detected", err.Error())
	case errors.Is(err, scanner.ErrNoDocuments):
		s.writeError(w, r, http.StatusUnprocessableEntity, "no_documents", err.Error())
	case errors.Is(err, scanner.ErrDeviceError):
		s.writeError(w, r, http.StatusUnprocessableEntity, "device_error", err.Error())
	case errors.Is(err, scanner.ErrTimeout):
		s.writeError(w, r, http.StatusGatewayTimeout, "scan_timeout", err.Error())
	default:
		s.logger().Error("scan failed", slog.Any("err", err))
		s.writeError(w, r, http.StatusInternalServerError, "scan_failed", err.Error())
	}
}

// contentTypeForFormat maps a capture format to its MIME type for the
// page parts' Content-Type header.
func contentTypeForFormat(format string) string {
	if format == "pnm" {
		return "image/x-portable-anymap"
	}
	return "image/tiff"
}

// writeScanResponse streams the multipart/mixed response: part 0 is
// the JSON scanMetadata, parts 1..N are the pages in scan order.
func (s *Server) writeScanResponse(w http.ResponseWriter, r *http.Request,
	req scanRequest, format string, pages []scanner.Page, duration time.Duration) {
	mw := multipart.NewWriter(w)
	w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	w.WriteHeader(http.StatusOK)

	meta := scanMetadata{
		RequestID:  req.RequestID,
		PageCount:  len(pages),
		DurationMs: duration.Milliseconds(),
		Device:     req.Device,
		Source:     req.Source,
		Resolution: req.Resolution,
		Mode:       req.Mode,
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

	for _, p := range pages {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Type", contentTypeForFormat(format))
		hdr.Set("Content-Disposition",
			fmt.Sprintf(`form-data; name="page"; filename="page-%d.%s"`, p.Index, format))
		part, err := mw.CreatePart(hdr)
		if err != nil {
			s.logger().Error("create page part failed", slog.Int("index", p.Index), slog.Any("err", err))
			return
		}
		if _, err := io.Copy(part, p.Data); err != nil {
			s.logger().Error("write page part failed", slog.Int("index", p.Index), slog.Any("err", err))
			return
		}
	}

	if err := mw.Close(); err != nil {
		s.logger().Error("close multipart writer failed", slog.Any("err", err))
	}
}
