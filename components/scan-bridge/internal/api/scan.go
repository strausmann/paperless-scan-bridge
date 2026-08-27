package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/dispatch"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/procclient"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/tag"
)

// scanRequest is the strict JSON body for POST /scan (ADR 0005: this
// is the single canonical trigger for every client). Unknown fields
// are rejected by handleScan via json.Decoder.DisallowUnknownFields —
// fields the milestone does not yet support (e.g. a per-call
// correspondent override) must fail loudly rather than be silently
// ignored, which would make an unsupported request look successful.
type scanRequest struct {
	Profile     string `json:"profile"`
	TagIDs      []int  `json:"tag_ids,omitempty"`
	TagStrategy string `json:"tag_strategy,omitempty"`
}

// destinationResult is one destination's outcome for one assembled
// document (design doc
// docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
// sec. 8).
type destinationResult struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "submitted" or "failed"
	// TaskID is a destination's own delivery reference for this
	// document when Status is "submitted" — for the paperless
	// destination, Paperless-ngx's post_document/ task_id
	// (destinations.paperless.Deliver's DeliveryResult.Reference,
	// design doc sec. 8). Sourced from
	// destinations.Destination.Deliver's DeliveryResult return value
	// (internal/destinations/destination.go); empty when the
	// destination has no such reference to offer, or when Status is
	// "failed".
	TaskID string `json:"task_id,omitempty"`
	// Error carries the destination's own error message when Status is
	// "failed". A per-destination failure never aborts delivery to
	// other destinations or other documents (design doc sec. 8: "eine
	// Destination-Failure ≠ Scan-Failure").
	Error string `json:"error,omitempty"`
}

// documentResult is one assembled document scan-processor produced,
// together with the outcome of delivering it to every one of the
// profile's configured destinations (design doc sec. 8).
type documentResult struct {
	Index        int                 `json:"index"`
	PageCount    int                 `json:"page_count"`
	Filename     string              `json:"filename"`
	ContentType  string              `json:"content_type,omitempty"`
	Warnings     []string            `json:"warnings,omitempty"`
	Destinations []destinationResult `json:"destinations"`
	// OCRConfidence and LowConfidence surface scan-processor's OCR
	// confidence gate (PR brief "Konfidenz-/Qualitäts-Gate") to the
	// caller of POST /scan — the minimal "the flag reaches the
	// caller" contract the brief asks for. Both are omitted from the
	// response entirely when OCR did not run (the common case for a
	// profile that never enabled it), rather than emitting
	// ocr_confidence:0/low_confidence:false noise on every scan.
	OCRConfidence float64 `json:"ocr_confidence,omitempty"`
	LowConfidence bool    `json:"low_confidence,omitempty"`
}

// scanResult is the 200 OK body for POST /scan.
//
// Milestone deviation from the documented async flow
// (CONTAINER_SUITE.md sec. 5.4 / the 2026-04-30 Phase 1.2 spec, both
// pre-dating the 2026-08-06 ADR reconciliation): this milestone
// dispatches synchronously through scan → scan-processor → every
// configured destination's Deliver call, and returns the finished
// result inline as 200 OK (design doc sec. 7, Option A — extends this
// existing synchronous contract rather than introducing the
// job-queue/202-polling flow Option B would need). /jobs* remain 501
// — there is no job store yet for a caller to poll even if it wanted
// to.
//
// This is a breaking change from the milestone's earlier shape: the
// raw scanned-page list (Pages []string, top-level PageCount) is
// replaced by Documents — the assembled, processed output the
// destinations actually received, per design doc sec. 8's "documents:
// [{filename, page_count, destinations: [...]}]" — since a caller of
// this endpoint cares about what was delivered, not the intermediate
// TIFF pages scan-processor consumed.
type scanResult struct {
	ScanID          string           `json:"scan_id"`
	Profile         string           `json:"profile"`
	DurationMillis  int64            `json:"duration_ms"`
	EffectiveTagIDs []int            `json:"effective_tag_ids"`
	Documents       []documentResult `json:"documents"`
}

// handleScan implements POST /scan behind requireBearer (routes.go).
//
// It decodes the strict body, resolves the profile, computes the
// effective tag-ID list, dispatches to sane-runtime, hands the
// resulting pages to scan-processor for OCR/deskew/assembly, and
// delivers each assembled document to every one of the profile's
// configured destinations — all bounded by a single context derived
// from the profile's timeout_seconds (design doc sec. 7 Option A: the
// existing timeout_seconds field's semantics are extended to cover the
// whole pipeline — scan + processing + destination-upload
// *submission* — rather than introducing a second, dedicated pipeline
// timeout; this was the simpler of the two options the design doc
// sec. 9 Task 7 offered, and matches Option A's own framing of the
// bound as "scan + OCR + one upload POST, roughly comparable to
// today's scan-only timeout with headroom").
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// http.MaxBytesReader bounds the whole body scanRequest's
	// json.Decoder below reads (issue #47 hardening) — POST /scan
	// carries no file bytes, so any body anywhere near the limit is
	// already a malformed or hostile request.
	r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes())

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body scanRequest
	if err := dec.Decode(&body); err != nil {
		if isMaxBytesError(err) {
			s.writeJSON(w, r, http.StatusRequestEntityTooLarge, errorResponse{
				Error: "request_too_large",
				Hint:  fmt.Sprintf("request body exceeds the %d byte limit", s.maxRequestBytes()),
			})
			return
		}
		s.writeJSON(w, r, http.StatusBadRequest, errorResponse{
			Error: "invalid_body",
			Hint:  err.Error(),
		})
		return
	}

	profile, ok := s.Profiles.Get(body.Profile)
	if !ok {
		s.writeJSON(w, r, http.StatusNotFound, errorResponse{
			Error: "profile_not_found",
			Hint:  "GET /profiles to list configured profile names.",
		})
		return
	}

	callerStrategy := tag.Strategy(body.TagStrategy)

	// effectiveTagIDs is the top-level, informational field the
	// milestone has reported since before destinations existed: the
	// caller's own tags, add/override/remove-merged against an empty
	// profile-level default (there is no single profile-wide tag
	// default — defaults are per-destination, see resolveMetadata).
	// Per-destination tag resolution (which DOES have a default, from
	// that destination's own config) happens separately below.
	effectiveTagIDs := tag.Merge(nil, "", body.TagIDs, callerStrategy)

	scanID, err := newRequestID()
	if err != nil {
		s.writeJSON(w, r, http.StatusInternalServerError, errorResponse{
			Error: "internal",
			Hint:  "failed to generate a scan id",
		})
		return
	}

	// Runs unconditionally once scanID exists, regardless of which
	// stage below succeeds or fails — os.RemoveAll on a directory that
	// was never created (e.g. Dispatch failed before writing anything)
	// is a no-op, so deferring this immediately is safe and simpler
	// than threading a "did we get far enough to write files" flag
	// through every early return (issue #49 point 1: PII cleanup).
	if !s.KeepScanOutput {
		defer s.cleanupScanOutput(scanID)
	}

	timeout := pipelineTimeout(profile)
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	dispatchResp, err := s.Dispatch.Dispatch(ctx, dispatch.Request{
		JobID:   scanID,
		Profile: profile,
	})
	if err != nil {
		status, errBody := mapDispatchError(err)
		s.writeJSON(w, r, status, errBody)
		return
	}

	procResult, err := s.ProcClient.Process(ctx, procclient.ProcessRequest{
		RequestID: scanID,
		PagePaths: dispatchResp.Pages,
		OCR: procclient.OCRConfig{
			Enabled:       profile.OCR.Enabled,
			Languages:     profile.OCR.Languages,
			MinConfidence: profile.OCR.MinConfidence,
		},
		Deskew:         profile.Deskew,
		RemoveBlank:    profile.RemoveBlank,
		RotatePages:    profile.RotatePages,
		PageGrouping:   procPageGrouping(profile.Assembly.PageGrouping),
		OutputFormat:   procOutputFormat(profile.Format),
		TimeoutSeconds: profile.TimeoutSeconds,
	})
	if err != nil {
		status, errBody := mapProcessError(err)
		s.writeJSON(w, r, status, errBody)
		return
	}

	builds := buildDestinations(profile.DestinationConfigs(), s.Secrets)

	documents := make([]documentResult, 0, len(procResult.Documents))
	for _, doc := range procResult.Documents {
		documents = append(documents, documentResult{
			Index:         doc.Index,
			PageCount:     doc.PageCount,
			Filename:      doc.Filename,
			ContentType:   doc.ContentType,
			Warnings:      doc.Warnings,
			Destinations:  deliverDocument(ctx, s.Logger, scanID, profile, doc, builds, body.TagIDs, callerStrategy),
			OCRConfidence: doc.OCRConfidence,
			LowConfidence: doc.LowConfidence,
		})
	}

	s.writeJSON(w, r, http.StatusOK, scanResult{
		ScanID:          scanID,
		Profile:         profile.Name,
		DurationMillis:  time.Since(start).Milliseconds(),
		EffectiveTagIDs: effectiveTagIDs,
		Documents:       documents,
	})
}

// newRequestID returns a random 128-bit hex string. A dedicated ULID
// library is deliberately not introduced for this — scanID only needs
// to be unique per dispatch call (it becomes the dispatch subdirectory
// name under config.PathsConfig.OutputDir, and the same requestID
// procclient.Process writes its output documents under), not sortable.
func newRequestID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate request id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// procPageGrouping converts a profile's Assembly.PageGrouping into the
// procclient wire value. profiles.PageGroupingCombined and the zero
// value both map to procclient.PageGroupingCombined —
// profiles.applyProfileDefaults already fills "" to
// PageGroupingCombined at load time, but this function tolerates the
// zero value directly too so it stays correct even if called against a
// profiles.Profile built by hand (e.g. in a test) that skipped that
// defaulting step.
func procPageGrouping(pg profiles.PageGrouping) procclient.PageGrouping {
	if pg == profiles.PageGroupingPerPage {
		return procclient.PageGroupingPerPage
	}
	return procclient.PageGroupingCombined
}

// procOutputFormat converts a profile's Format into the procclient
// wire value. profiles.Format is validated to be exactly
// pdf/jpeg/tiff at profile-load time (profiles.validateProfile), so
// the default case below is unreachable in practice for a
// Set-sourced profile; it is kept as a safe fallback (pdf) rather than
// a panic for the same hand-built-Profile reason as procPageGrouping.
func procOutputFormat(f profiles.Format) procclient.OutputFormat {
	switch f {
	case profiles.FormatJPEG:
		return procclient.OutputFormatJPEG
	case profiles.FormatTIFF:
		return procclient.OutputFormatTIFF
	case profiles.FormatPNG:
		return procclient.OutputFormatPNG
	default:
		return procclient.OutputFormatPDF
	}
}

// mapDispatchError classifies a Dispatch error into the HTTP status
// and error envelope handleScan returns. errors.Is against the
// dispatch package's sentinel errors (dispatch.go) keeps this
// decoupled from sane-runtime's wire-level status codes — that
// mapping lives entirely in internal/dispatch/http_client.go.
func mapDispatchError(err error) (int, errorResponse) {
	switch {
	case errors.Is(err, dispatch.ErrNoScannerDetected):
		return http.StatusServiceUnavailable, errorResponse{
			Error: "no_scanner_detected",
			Hint:  "sane-runtime has not detected an attached scanner yet.",
		}
	case errors.Is(err, dispatch.ErrNoDocuments):
		return http.StatusUnprocessableEntity, errorResponse{
			Error: "no_documents",
			Hint:  "no documents were found in the feeder.",
		}
	case errors.Is(err, dispatch.ErrBusy):
		return http.StatusConflict, errorResponse{
			Error: "scanner_busy",
			Hint:  "the scanner is already processing another job.",
		}
	case errors.Is(err, dispatch.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, errorResponse{
			Error: "timeout",
			Hint:  "the scan did not complete within the profile's timeout_seconds.",
		}
	default:
		return http.StatusBadGateway, errorResponse{
			Error: "dispatch_failed",
			Hint:  "sane-runtime dispatch failed; see daemon logs.",
		}
	}
}

// mapProcessError classifies a ProcClient.Process error into the HTTP
// status and error envelope handleScan returns, mirroring
// mapDispatchError's shape for the scan-processor leg of the pipeline.
// errors.Is against internal/procclient's sentinel errors keeps this
// decoupled from scan-processor's wire-level status codes.
func mapProcessError(err error) (int, errorResponse) {
	switch {
	case errors.Is(err, procclient.ErrUnsupportedFormat):
		// scan-processor's 400 means WE built it an invalid request —
		// an unsupported output_format/page_grouping is a scan-bridge
		// profile-config problem (profiles.validateProfile SHOULD have
		// rejected it at load time), not a scan-processor malfunction
		// or transient upstream failure. 502 (Bad Gateway, this
		// switch's other branches' generic fallback) implies the
		// latter and misdirects an operator's troubleshooting (issue
		// #49 point 2).
		return http.StatusInternalServerError, errorResponse{
			Error: "unsupported_output",
			Hint:  "scan-processor rejected the profile's output_format or assembly.page_grouping — this profile is misconfigured.",
		}
	case errors.Is(err, procclient.ErrBusy):
		return http.StatusConflict, errorResponse{
			Error: "processor_busy",
			Hint:  "scan-processor is already handling another request.",
		}
	case errors.Is(err, procclient.ErrOCRFailed):
		return http.StatusUnprocessableEntity, errorResponse{
			Error: "processing_failed",
			Hint:  "OCR or another processing stage failed on the scanned pages.",
		}
	case errors.Is(err, procclient.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, errorResponse{
			Error: "timeout",
			Hint:  "processing did not complete within the profile's timeout_seconds.",
		}
	default:
		return http.StatusBadGateway, errorResponse{
			Error: "processing_dispatch_failed",
			Hint:  "scan-processor dispatch failed; see daemon logs.",
		}
	}
}

// isMaxBytesError reports whether err originates from an
// http.MaxBytesReader hitting its limit (issue #47). Checks the typed
// *http.MaxBytesError (Go 1.19+) first; falls back to matching
// MaxBytesError.Error()'s exact, stable text ("http: request body too
// large") in case a decoder in the call chain re-wraps the error in
// its own message rather than propagating it unwrapped — encoding/json
// does not currently do this for a plain io error, but relying on
// that as an implementation detail would make this check brittle
// across a stdlib version bump.
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

// perPageMultiDestinationHeadroomFactor scales profile.TimeoutSeconds
// for the one profile shape issue #49 point 3 identified as most
// likely to exceed handleScan's single shared context.WithTimeout
// budget: assembly.page_grouping=per_page (N assembled documents,
// N proportional to surviving page count) combined with more than one
// configured destination, each document's Deliver call to each
// destination running serially inside that one context (design doc
// sec. 7 Option A: "scan + OCR + one upload POST, roughly comparable
// to today's scan-only timeout with headroom" — an assumption of
// roughly one delivery that a per_page × multi-destination profile
// can violate several times over).
//
// This is deliberately the less invasive of the two remedies the
// issue offered: rather than a new, independently-sized
// PipelineTimeoutSeconds profile field (a config/schema change plus a
// migration story for every existing profiles.yaml), it keeps
// today's single timeout_seconds contract and only widens the derived
// deadline for the shape that actually needs it. A dedicated field
// stays tracked in issue #49 point 3 for if/when a real profile still
// exhausts this headroom in practice — combined (single-document)
// profiles and per_page profiles with at most one destination are
// unaffected and keep their existing, unscaled budget.
const perPageMultiDestinationHeadroomFactor = 2.0

// pipelineTimeout returns the deadline handleScan's single
// context.WithTimeout call uses to bound dispatch + processing +
// every destination's Deliver call (see perPageMultiDestinationHeadroomFactor's
// doc comment for the rationale).
func pipelineTimeout(profile profiles.Profile) time.Duration {
	base := time.Duration(profile.TimeoutSeconds) * time.Second
	if profile.Assembly.PageGrouping == profiles.PageGroupingPerPage &&
		len(profile.DestinationConfigs()) > 1 {
		return time.Duration(float64(base) * perPageMultiDestinationHeadroomFactor)
	}
	return base
}

// cleanupScanOutput removes OutputDir/<scanID> — see
// Server.KeepScanOutput's doc comment (api.go) for why handleScan
// does this after every request. Safe to call unconditionally right
// after scanID is minted, before any file under that path necessarily
// exists yet: os.RemoveAll on a missing path returns nil, so an early
// handleScan failure (e.g. Dispatch never even ran) cleans up nothing
// and reports no error.
func (s *Server) cleanupScanOutput(scanID string) {
	if s.OutputDir == "" || scanID == "" {
		return
	}
	dir := filepath.Join(s.OutputDir, scanID)
	if err := os.RemoveAll(dir); err != nil {
		s.Logger.Warn("cleanup scan output failed",
			slog.String("scan_id", scanID),
			slog.String("dir", dir),
			slog.Any("err", err))
	}
}
