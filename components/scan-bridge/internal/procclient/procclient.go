// Package procclient is the client to the scan-processor container.
//
// It speaks HTTP-over-Unix-socket, mirroring internal/dispatch's
// already-proven pattern (ADR 0009's precedent, realized in
// dispatch/http_client.go) so scan-bridge can hand a completed scan's
// raw TIFF pages off for OCR/deskew/blank-removal/rotation/format
// conversion/assembly without a shared writable volume between the two
// containers — see
// docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
// sec. 4.2 (transport Option A) and sec. 9 Task 5.
//
// This package defines the HTTP contract scan-processor (Task 6, not
// yet built) must implement. Client and server are built and reviewed
// independently against this frozen wire shape; http_client_test.go
// verifies it here against a hand-rolled fake Unix-socket HTTP server,
// standing in for the not-yet-built container exactly as
// dispatch/http_client_test.go's startFakeSaneRuntime stands in for
// sane-runtime.
//
// # Wire contract
//
// scan-bridge POSTs multipart/mixed to
// http://scan-processor.local/process (the URL host is fake — the
// transport always dials the configured Unix socket): part 0 is a JSON
// control payload (processRequestPayload) carrying every processing
// parameter scan-processor needs without knowing about profiles as a
// concept (design doc sec. 4.2); parts 1..N are the job's TIFF pages,
// read back off the OutputDir scan-bridge's dispatch.Client already
// wrote them to.
//
// scan-processor replies multipart/mixed: part 0 is a JSON metadata
// block (processMetadata) listing one entry per assembled document
// (one when PageGrouping is "combined", one per source page when
// "per_page"); parts 1..N are the assembled document bytes, in the same
// order as the metadata's Documents slice. httpUnixClient writes each
// part to outputDir/<requestID>/<filename> (mirroring
// dispatch/http_client.go's readScanResponse, which writes pages to
// outputDir/<jobID>/page-N.tiff for the same reason: a multipart.Part
// is only readable while its parent *http.Response.Body is open, so it
// cannot be handed to a caller as a live io.Reader) and returns the
// written paths in ProcessResult.
//
// A non-200 response carries scan-processor's error envelope
// ({"error","hint"}, mirroring sane-runtime's and internal/api's
// shape) and maps to one of the sentinel errors below via errors.Is,
// so a future internal/api integration can reuse the same
// mapDispatchError-shaped pattern it already has for sane-runtime
// errors (design doc sec. 4.2).
package procclient

import (
	"context"
	"errors"
)

// OCRConfig is the profile's OCR toggle and language set, carried
// through the control payload verbatim (design doc sec. 4.2/6).
type OCRConfig struct {
	Enabled   bool
	Languages []string
	// MinConfidence overrides scan-processor's default OCR confidence
	// gate threshold when non-zero — carried through verbatim, same
	// as Languages. See scan-processor's
	// internal/pipeline.OCRConfig.MinConfidence doc comment for the
	// gate itself (PR brief "Konfidenz-/Qualitäts-Gate").
	MinConfidence float64
}

// PageGrouping selects whether scan-processor assembles all of a job's
// pages into one document or emits one document per source page (ADR
// 0017, design doc sec. 4.3 stage 7).
type PageGrouping string

const (
	// PageGroupingCombined merges every page in the job into a single
	// assembled document.
	PageGroupingCombined PageGrouping = "combined"
	// PageGroupingPerPage emits one assembled document per source
	// page.
	PageGroupingPerPage PageGrouping = "per_page"
)

// OutputFormat is the container scan-processor assembles pages into.
// The values mirror profiles.Format ("pdf"/"jpeg"/"tiff"), but this
// type is declared independently: scan-processor (and this client)
// must not depend on the profiles package, per the design's "everything
// scan-processor needs to act without knowing about profiles as a
// concept" (sec. 4.2).
type OutputFormat string

const (
	OutputFormatPDF  OutputFormat = "pdf"
	OutputFormatJPEG OutputFormat = "jpeg"
	OutputFormatTIFF OutputFormat = "tiff"
)

// ProcessRequest is a single /process dispatch sent to scan-processor.
// PagePaths are absolute paths to the TIFF pages scan-bridge's
// dispatch.Client already wrote under OutputDir/<jobID>/; Process reads
// them back off disk and streams them as multipart parts (design doc
// sec. 4.2 — scan-bridge is "the same daemon that just wrote them — no
// new dependency").
type ProcessRequest struct {
	RequestID      string
	PagePaths      []string
	OCR            OCRConfig
	Deskew         bool
	RemoveBlank    bool
	RotatePages    bool
	PageGrouping   PageGrouping
	OutputFormat   OutputFormat
	TimeoutSeconds int
}

// Document is one assembled object scan-processor produced, written to
// disk by httpUnixClient. Path points at outputDir/<RequestID>/<Filename>;
// downstream code (destination delivery, a later task) reads Content
// from Path rather than holding a reference into the now-closed HTTP
// response body.
type Document struct {
	// Index is the document's position in scan-processor's response,
	// preserved for correlation with warnings/logging.
	Index int
	// Filename is the name scan-processor suggested and the client
	// wrote the file under (sanitized — see safeDocumentPath).
	Filename string
	// Path is the absolute path httpUnixClient wrote this document's
	// bytes to.
	Path string
	// ContentType is the MIME type matching the request's
	// OutputFormat.
	ContentType string
	// PageCount is the number of source pages assembled into this
	// document.
	PageCount int
	// Warnings carries any non-fatal issues scan-processor reported
	// for this document (e.g. a page that failed deskew but was still
	// included).
	Warnings []string
	// OCRConfidence is the mean per-word OCR confidence (0..100)
	// scan-processor's confidence gate computed for this document.
	// Zero when OCR did not run.
	OCRConfidence float64
	// LowConfidence is true when scan-processor's confidence gate
	// flagged this document (OCRConfidence below the request's
	// effective OCR.MinConfidence threshold). Advisory only.
	LowConfidence bool
}

// ProcessResult carries the outcome of a completed Process call.
// RequestID echoes the caller-supplied ProcessRequest.RequestID (not a
// server-reported value — mirrors dispatch.Response.JobID's same
// choice, so a caller never has to reconcile two identifiers for one
// request).
type ProcessResult struct {
	RequestID      string
	Documents      []Document
	DurationMillis int64
}

// Client is the contract scan-bridge programs against. Implementations
// are in-package (production HTTP-over-Unix client, http_client.go) or
// in tests (in-memory fakes).
type Client interface {
	// Process sends one job's pages to scan-processor and returns the
	// assembled document(s) it produced.
	Process(ctx context.Context, req ProcessRequest) (ProcessResult, error)
	// Ping calls scan-processor's /health and reports whether it
	// answered 200 OK.
	Ping(ctx context.Context) error
	// Close releases the client's pooled Unix-socket connections.
	// Mirrors dispatch.Client.Close, added here for the same resource-
	// cleanup symmetry even though the task brief that requested this
	// package only specified Process/Ping — see the PR description for
	// this deliberate, non-contract-affecting addition.
	Close() error
}

// Sentinel errors classifying a failed Process call. httpUnixClient
// wraps one of these (via %w) so callers can use errors.Is regardless
// of the underlying scan-processor status code or transport error.
// Status-code mapping (chosen here, since this package fixes the
// contract scan-processor must implement):
//
//   - 400 Bad Request      -> ErrUnsupportedFormat (request rejected:
//     an OutputFormat or PageGrouping value scan-processor does not
//     support)
//   - 409 Conflict          -> ErrBusy (mirrors dispatch's ErrBusy;
//     scan-processor is already handling another request)
//   - 422 Unprocessable Entity -> ErrOCRFailed (something about the
//     supplied pages defeated OCR/processing — mirrors dispatch's use
//     of 422 for "problem with the input", ErrNoDocuments)
//   - 504 Gateway Timeout   -> ErrTimeout (mirrors dispatch's
//     ErrTimeout; also returned client-side when the context deadline
//     expires before scan-processor responds)
var (
	// ErrUnsupportedFormat means scan-processor rejected the request's
	// OutputFormat or PageGrouping value (scan-processor HTTP 400).
	ErrUnsupportedFormat = errors.New("procclient: unsupported output format or page grouping")
	// ErrBusy means scan-processor is already handling another request
	// (scan-processor HTTP 409).
	ErrBusy = errors.New("procclient: scan-processor busy")
	// ErrOCRFailed means OCR or another processing stage could not
	// complete on the supplied pages (scan-processor HTTP 422).
	ErrOCRFailed = errors.New("procclient: OCR or processing failed")
	// ErrTimeout means processing did not finish within the caller's
	// deadline, either because scan-processor itself reported a
	// timeout (HTTP 504) or because the request's context deadline
	// expired client-side.
	ErrTimeout = errors.New("procclient: timed out")
)
