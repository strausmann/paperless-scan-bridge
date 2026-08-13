// Package pipeline implements scan-processor's internal processing
// stages — deskew, blank-page removal, rotation correction, OCR,
// format conversion, and multi-page assembly — behind the Pipeline
// interface, per
// docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
// sec. 4.3 (task list Task 6).
//
// scan-processor does not know about profiles, destinations, or
// scan-bridge's OutputDir (design doc sec. 4.2/4.3: "scan-processor
// does not know about destinations, Paperless, or profile-destination
// configuration"). Request carries only the processing parameters
// internal/procapi's HTTP handler decoded from the wire contract's
// control payload, and Page carries raw TIFF bytes already read out
// of the incoming multipart request body — this package must not
// import internal/procclient (the scan-bridge-side client package)
// or anything under components/scan-bridge, to keep the dependency
// direction one-way (procclient/scan-bridge -> wire contract <-
// scan-processor, never scan-processor -> scan-bridge).
//
// The production Pipeline (ExecPipeline, exec_pipeline.go) shells out
// to tesseract(1)/convert(1) (ImageMagick)/qpdf(1), none of which are
// installed on this repo's CI runners. internal/procapi's handler
// tests use a hand-rolled fake Pipeline instead, so the HTTP
// contract — request decoding, response encoding, error-status
// mapping, page_grouping orchestration — is fully covered without
// those binaries (see the task brief's "KRITISCH" section). This
// package's own tests split the same way: exec_argv_test.go
// unit-tests the pure argument-building helpers (no exec.Command
// call, no binary needed), while exec_pipeline_test.go (build tag
// "integration") drives the real binaries and is skipped by the
// default `go test ./...` used in CI.
package pipeline

import (
	"context"
	"errors"
)

// OCRConfig is the profile's OCR toggle and language set, carried
// through from the wire contract's control payload verbatim (design
// doc sec. 4.2/6). Declared independently from
// internal/procclient.OCRConfig (same shape, different package —
// see the package doc comment on the dependency-direction rule).
type OCRConfig struct {
	Enabled   bool
	Languages []string
}

// PageGrouping selects whether the pipeline assembles all of a
// request's pages into one document or emits one document per source
// page (ADR 0017, design doc sec. 4.3 stage 7).
type PageGrouping string

const (
	// PageGroupingCombined merges every page in the request into a
	// single assembled document.
	PageGroupingCombined PageGrouping = "combined"
	// PageGroupingPerPage emits one assembled document per source
	// page.
	PageGroupingPerPage PageGrouping = "per_page"
)

// OutputFormat is the container the pipeline assembles pages into.
// The values mirror profiles.Format ("pdf"/"jpeg"/"tiff") and
// procclient.OutputFormat, declared independently here for the same
// dependency-direction reason as OCRConfig above.
type OutputFormat string

const (
	OutputFormatPDF  OutputFormat = "pdf"
	OutputFormatJPEG OutputFormat = "jpeg"
	OutputFormatTIFF OutputFormat = "tiff"
)

// Page is one input page's raw bytes, already read out of the
// incoming multipart request by internal/procapi (design doc sec.
// 4.2: pages "travel as multipart/mixed request body"). The wire
// contract always sends image/tiff (dispatch/http_client.go's
// extForContentType, mirrored on the scan-bridge side), so Data is
// always TIFF-encoded.
type Page struct {
	Data []byte
}

// Request is one call to Process. It carries every processing
// parameter the pipeline needs to act, decoded from the control
// payload by internal/procapi — the pipeline itself never parses
// JSON or HTTP.
type Request struct {
	RequestID      string
	Pages          []Page
	OCR            OCRConfig
	Deskew         bool
	RemoveBlank    bool
	RotatePages    bool
	PageGrouping   PageGrouping
	OutputFormat   OutputFormat
	TimeoutSeconds int
}

// Document is one assembled output document: one per Request when
// PageGrouping is "combined", one per surviving source page when
// "per_page" (design doc sec. 4.3 stage 7). Content holds the fully
// assembled bytes in memory — internal/procapi streams them into the
// multipart/mixed response as soon as Process returns; there is no
// shared volume with scan-bridge for this leg (design doc sec. 4.2
// Option A).
type Document struct {
	// Index is the document's position in the response, preserved
	// for correlation with warnings/logging.
	Index int
	// Filename is the name procapi echoes to scan-bridge (documented
	// contract field documentMetadata.Filename in
	// internal/procclient/http_client.go — scan-bridge sanitizes it
	// again on its own side via safeDocumentPath, so this package
	// only needs to produce a reasonable, extension-correct name, not
	// defend against path traversal itself).
	Filename string
	// Content is the assembled document's bytes, encoded per
	// OutputFormat (and, when OCR is enabled and OutputFormat is pdf,
	// a searchable PDF — design doc sec. 4.3 stage 6).
	Content []byte
	// ContentType is the MIME type matching OutputFormat.
	ContentType string
	// PageCount is the number of source pages assembled into this
	// document (after blank-page removal, so it may be smaller than
	// len(Request.Pages)).
	PageCount int
	// Warnings carries any non-fatal issues encountered while
	// producing this document (e.g. a page that failed deskew but
	// was still included).
	Warnings []string
}

// Result carries the outcome of a completed Process call.
type Result struct {
	Documents      []Document
	DurationMillis int64
}

// Sentinel errors a Pipeline implementation wraps (via %w) so
// internal/procapi's handler can classify a failure with errors.Is
// regardless of which stage produced it, and map it onto the wire
// contract's documented status codes (design doc sec. 4.2, frozen by
// internal/procclient's doc comment):
//
//   - ErrUnsupportedFormat -> HTTP 400
//   - ErrBusy              -> HTTP 409
//   - ErrOCRFailed         -> HTTP 422
//   - ErrTimeout           -> HTTP 504
var (
	// ErrUnsupportedFormat means the request's OutputFormat or
	// PageGrouping value (or an input page's encoding) is not
	// something this pipeline can process.
	ErrUnsupportedFormat = errors.New("pipeline: unsupported output format or page grouping")
	// ErrBusy means the pipeline is already handling another request
	// (single-flight — internal/procapi enforces this before ever
	// calling Process, but the sentinel is declared here too so a
	// future concurrent-capable Pipeline implementation can report
	// its own internal concurrency limit the same way).
	ErrBusy = errors.New("pipeline: scan-processor busy")
	// ErrOCRFailed means OCR or another processing stage
	// (deskew/blank-removal/rotation/assembly) could not complete on
	// the supplied pages.
	ErrOCRFailed = errors.New("pipeline: OCR or processing failed")
	// ErrTimeout means processing did not finish within the
	// request's timeout.
	ErrTimeout = errors.New("pipeline: timed out")
)

// Pipeline is implemented by the production ExecPipeline
// (exec_pipeline.go, shells out to tesseract/convert/qpdf) and by
// hand-rolled fakes in internal/procapi's tests.
type Pipeline interface {
	// Process runs every profile-gated stage (deskew, blank removal,
	// rotation, OCR, format conversion, assembly) over req.Pages in
	// order and returns the assembled Document(s). ctx bounds the
	// whole call; a Pipeline implementation must return a
	// context.Canceled/context.DeadlineExceeded-wrapping error (or
	// ErrTimeout directly) when ctx is done before finishing.
	Process(ctx context.Context, req Request) (Result, error)
}
