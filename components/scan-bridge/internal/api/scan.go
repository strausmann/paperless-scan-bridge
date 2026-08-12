package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/dispatch"
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

// scanResult is the 200 OK body for POST /scan.
//
// Milestone deviation from the documented async flow
// (CONTAINER_SUITE.md sec. 5.4 / the 2026-04-30 Phase 1.2 spec, both
// pre-dating the 2026-08-06 ADR reconciliation): this milestone dispatches
// synchronously and returns the finished result inline as 200 OK.
// /jobs* remain 501 — there is no job store yet for a caller to poll
// even if it wanted to.
type scanResult struct {
	ScanID          string   `json:"scan_id"`
	Profile         string   `json:"profile"`
	PageCount       int      `json:"page_count"`
	Pages           []string `json:"pages"`
	DurationMillis  int64    `json:"duration_ms"`
	EffectiveTagIDs []int    `json:"effective_tag_ids"`
}

// handleScan implements POST /scan behind requireBearer (routes.go).
// It decodes the strict body, resolves the profile, computes the
// effective tag-ID list, dispatches to sane-runtime with a context
// bounded by the profile's own timeout, and maps any dispatch failure
// to the appropriate HTTP status via mapDispatchError.
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body scanRequest
	if err := dec.Decode(&body); err != nil {
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

	// Task 10 will populate per-profile default tag IDs and a
	// profile-level merge strategy; until then the profile side of
	// the merge is empty and every effective tag ID comes from the
	// caller. tag.Merge already implements the full add/override/
	// remove algebra (internal/tag), so this call is forward-
	// compatible with Task 10 without a handleScan change.
	effectiveTagIDs := tag.Merge(nil, "", body.TagIDs, tag.Strategy(body.TagStrategy))

	scanID, err := newRequestID()
	if err != nil {
		s.writeJSON(w, r, http.StatusInternalServerError, errorResponse{
			Error: "internal",
			Hint:  "failed to generate a scan id",
		})
		return
	}

	timeout := time.Duration(profile.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	resp, err := s.Dispatch.Dispatch(ctx, dispatch.Request{
		JobID:   scanID,
		Profile: profile,
	})
	if err != nil {
		status, errBody := mapDispatchError(err)
		s.writeJSON(w, r, status, errBody)
		return
	}

	s.writeJSON(w, r, http.StatusOK, scanResult{
		ScanID:          scanID,
		Profile:         profile.Name,
		PageCount:       len(resp.Pages),
		Pages:           resp.Pages,
		DurationMillis:  resp.DurationMillis,
		EffectiveTagIDs: effectiveTagIDs,
	})
}

// newRequestID returns a random 128-bit hex string. A dedicated ULID
// library is deliberately not introduced for this — scanID only needs
// to be unique per dispatch call (it becomes the dispatch subdirectory
// name under config.PathsConfig.OutputDir), not sortable.
func newRequestID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate request id: %w", err)
	}
	return hex.EncodeToString(buf), nil
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
