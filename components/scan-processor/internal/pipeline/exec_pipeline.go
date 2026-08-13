package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// blankMeanThresholdDefault is the mean-brightness (0..1, 1 = pure
// white) above which a page is classified blank. 0.98 tolerates the
// faint noise/artifacts a real scan carries even on an otherwise
// empty page, while still catching genuinely blank ADF-duplex
// back-sides (the common case profile.RemoveBlank exists for).
const blankMeanThresholdDefault = 0.98

// defaultOCRLanguages is used when OCRConfig.Enabled is true but
// Languages is empty, matching the design doc's stated default (sec.
// 4.3 stage 5: "deu+eng by default").
var defaultOCRLanguages = []string{"deu", "eng"}

// ExecPipeline is the production Pipeline: it shells out to
// convert(1) (ImageMagick), tesseract(1), and qpdf(1) to implement
// every stage in design doc sec. 4.3 (deskew, blank-page removal,
// rotation, OCR, format conversion, multi-page assembly).
//
// Every *Bin field overrides the corresponding binary path; empty
// means resolve via exec.LookPath at call time (the production
// default). Tests point these at fixture scripts or leave them empty
// and skip via exec.LookPath when the real toolchain is not
// installed — see exec_pipeline_test.go (build tag "integration").
type ExecPipeline struct {
	ConvertBin   string
	TesseractBin string
	QpdfBin      string

	// BlankMeanThreshold overrides blankMeanThresholdDefault when
	// non-zero.
	BlankMeanThreshold float64

	Logger *slog.Logger
}

func (p *ExecPipeline) logger() *slog.Logger {
	if p.Logger != nil {
		return p.Logger
	}
	return slog.Default()
}

func (p *ExecPipeline) blankThreshold() float64 {
	if p.BlankMeanThreshold != 0 {
		return p.BlankMeanThreshold
	}
	return blankMeanThresholdDefault
}

func resolveBin(override, name string) (string, error) {
	if override != "" {
		return override, nil
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("pipeline: %s not found on PATH: %w", name, err)
	}
	return bin, nil
}

// pageState tracks one surviving input page through the per-page
// stages (deskew/blank-check/rotate), before OCR/format-conversion
// and assembly.
type pageState struct {
	// originalIndex is the page's position in req.Pages, preserved
	// for warning messages even after blank pages are dropped.
	originalIndex int
	// path is the current on-disk TIFF file for this page —
	// reassigned after each stage that produces a new file.
	path string
}

// Process implements Pipeline. See the package doc comment and design
// doc sec. 4.3 for the stage list and ordering.
func (p *ExecPipeline) Process(ctx context.Context, req Request) (Result, error) {
	start := time.Now()

	if len(req.Pages) == 0 {
		return Result{}, fmt.Errorf("pipeline: no pages supplied: %w", ErrUnsupportedFormat)
	}
	if req.PageGrouping != PageGroupingCombined && req.PageGrouping != PageGroupingPerPage {
		return Result{}, fmt.Errorf("pipeline: unsupported page_grouping %q: %w", req.PageGrouping, ErrUnsupportedFormat)
	}
	switch req.OutputFormat {
	case OutputFormatPDF, OutputFormatJPEG, OutputFormatTIFF:
	default:
		return Result{}, fmt.Errorf("pipeline: unsupported output_format %q: %w", req.OutputFormat, ErrUnsupportedFormat)
	}

	convertBin, err := resolveBin(p.ConvertBin, "convert")
	if err != nil {
		return Result{}, err
	}
	var tesseractBin string
	if req.OCR.Enabled || req.RotatePages {
		tesseractBin, err = resolveBin(p.TesseractBin, "tesseract")
		if err != nil {
			return Result{}, err
		}
	}
	var qpdfBin string
	if req.PageGrouping == PageGroupingCombined && req.OutputFormat == OutputFormatPDF {
		qpdfBin, err = resolveBin(p.QpdfBin, "qpdf")
		if err != nil {
			return Result{}, err
		}
	}

	scratch, err := os.MkdirTemp("", "scan-processor-*")
	if err != nil {
		return Result{}, fmt.Errorf("pipeline: create scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	states := make([]pageState, 0, len(req.Pages))
	for i, page := range req.Pages {
		path := filepath.Join(scratch, fmt.Sprintf("page-%d-src.tiff", i))
		if err := os.WriteFile(path, page.Data, 0o600); err != nil {
			return Result{}, fmt.Errorf("pipeline: write input page %d: %w", i, err)
		}
		states = append(states, pageState{originalIndex: i, path: path})
	}

	var warnings []string
	survivors := make([]pageState, 0, len(states))
	for _, st := range states {
		if err := ctx.Err(); err != nil {
			return Result{}, classifyContextError(err)
		}

		if req.Deskew {
			out := filepath.Join(scratch, fmt.Sprintf("page-%d-deskew.tiff", st.originalIndex))
			if err := p.runConvert(ctx, convertBin, buildDeskewArgs(st.path, out)); err != nil {
				warnings = append(warnings, fmt.Sprintf("page %d: deskew failed, using original: %v", st.originalIndex, err))
			} else {
				st.path = out
			}
		}

		if req.RemoveBlank {
			blank, err := p.isBlankPage(ctx, convertBin, st.path)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("page %d: blank-page check failed, keeping page: %v", st.originalIndex, err))
			} else if blank {
				warnings = append(warnings, fmt.Sprintf("page %d: removed as blank", st.originalIndex))
				continue
			}
		}

		if req.RotatePages {
			angle, err := p.detectOrientation(ctx, tesseractBin, st.path)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("page %d: orientation detection failed, using original orientation: %v", st.originalIndex, err))
			} else if angle != 0 {
				out := filepath.Join(scratch, fmt.Sprintf("page-%d-rotated.tiff", st.originalIndex))
				if err := p.runConvert(ctx, convertBin, buildRotateArgs(st.path, out, angle)); err != nil {
					warnings = append(warnings, fmt.Sprintf("page %d: rotation failed, using unrotated page: %v", st.originalIndex, err))
				} else {
					st.path = out
				}
			}
		}

		survivors = append(survivors, st)
	}

	if len(survivors) == 0 {
		return Result{}, fmt.Errorf("pipeline: every page was removed as blank, nothing to assemble: %w", ErrOCRFailed)
	}

	languages := req.OCR.Languages
	if req.OCR.Enabled && len(languages) == 0 {
		languages = defaultOCRLanguages
	}

	ext := fileExtensionForFormat(req.OutputFormat)
	assembledPages := make([]string, 0, len(survivors))
	assembledOriginalIndexes := make([]int, 0, len(survivors))
	for _, st := range survivors {
		if err := ctx.Err(); err != nil {
			return Result{}, classifyContextError(err)
		}

		var pagePath string
		if req.OCR.Enabled && req.OutputFormat == OutputFormatPDF {
			outBase := filepath.Join(scratch, fmt.Sprintf("page-%d-ocr", st.originalIndex))
			if _, err := p.runTesseract(ctx, tesseractBin, buildOCRPDFArgs(st.path, outBase, languages)); err != nil {
				return Result{}, fmt.Errorf("pipeline: OCR page %d: %w: %w", st.originalIndex, err, ErrOCRFailed)
			}
			pagePath = outBase + ".pdf"
		} else {
			if req.OCR.Enabled {
				// OCR is meaningful for the searchable-PDF case only
				// (design doc sec. 4.3 stage 6); for jpeg/tiff output
				// we still run OCR so a page tesseract cannot read at
				// all is surfaced as a failure, but discard the text
				// layer, since neither format carries one.
				if _, err := p.runTesseract(ctx, tesseractBin, buildOCRPDFArgs(st.path, filepath.Join(scratch, fmt.Sprintf("page-%d-ocr-check", st.originalIndex)), languages)); err != nil {
					return Result{}, fmt.Errorf("pipeline: OCR page %d: %w: %w", st.originalIndex, err, ErrOCRFailed)
				}
			}
			out := filepath.Join(scratch, fmt.Sprintf("page-%d-out.%s", st.originalIndex, ext))
			if err := p.runConvert(ctx, convertBin, buildConvertFormatArgs(st.path, out)); err != nil {
				return Result{}, fmt.Errorf("pipeline: convert page %d to %s: %w: %w", st.originalIndex, req.OutputFormat, err, ErrOCRFailed)
			}
			pagePath = out
		}
		assembledPages = append(assembledPages, pagePath)
		assembledOriginalIndexes = append(assembledOriginalIndexes, st.originalIndex)
	}

	documents, err := p.assemble(ctx, assembleParams{
		requestID:       req.RequestID,
		pageGrouping:    req.PageGrouping,
		format:          req.OutputFormat,
		qpdfBin:         qpdfBin,
		convertBin:      convertBin,
		scratch:         scratch,
		pagePaths:       assembledPages,
		originalIndexes: assembledOriginalIndexes,
		warnings:        warnings,
	})
	if err != nil {
		return Result{}, err
	}

	return Result{
		Documents:      documents,
		DurationMillis: time.Since(start).Milliseconds(),
	}, nil
}

// isBlankPage runs identify(1) and classifies path as blank when its
// mean brightness meets or exceeds the configured threshold.
func (p *ExecPipeline) isBlankPage(ctx context.Context, convertBin, path string) (bool, error) {
	// ImageMagick ships identify(1) alongside convert(1) in the same
	// package/prefix; deriving it from convertBin's directory avoids
	// a second *Bin override field for what is always a sibling
	// binary.
	identifyBin := filepath.Join(filepath.Dir(convertBin), "identify")
	if _, err := os.Stat(identifyBin); err != nil {
		// Not every ImageMagick install exposes a standalone
		// identify(1) next to convert(1) (some distros symlink
		// differently) — fall back to resolving it on PATH directly.
		var lookErr error
		identifyBin, lookErr = exec.LookPath("identify")
		if lookErr != nil {
			return false, fmt.Errorf("identify not found: %w", lookErr)
		}
	}

	stdout, err := p.runCommand(ctx, identifyBin, buildMeanBrightnessArgs(path))
	if err != nil {
		return false, err
	}
	mean, err := parseMeanBrightness(stdout)
	if err != nil {
		return false, err
	}
	return mean >= p.blankThreshold(), nil
}

// detectOrientation runs tesseract --psm 0 and returns the clockwise
// rotation angle needed to correct path's orientation.
func (p *ExecPipeline) detectOrientation(ctx context.Context, tesseractBin, path string) (int, error) {
	stdout, err := p.runTesseract(ctx, tesseractBin, buildOSDArgs(path))
	if err != nil {
		return 0, err
	}
	return parseOSDRotation(stdout)
}

func (p *ExecPipeline) runConvert(ctx context.Context, convertBin string, args []string) error {
	_, err := p.runCommand(ctx, convertBin, args)
	return err
}

func (p *ExecPipeline) runTesseract(ctx context.Context, tesseractBin string, args []string) (string, error) {
	return p.runCommand(ctx, tesseractBin, args)
}

// runCommand executes bin with args, returning trimmed stdout.
// stderr is captured for the error message only, never swallowed.
func (p *ExecPipeline) runCommand(ctx context.Context, bin string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", classifyContextError(ctxErr)
		}
		p.logger().Warn("command failed",
			slog.String("bin", bin), slog.Any("args", args), slog.String("stderr", stderr.String()))
		return "", fmt.Errorf("%s: %w (stderr: %s)", filepath.Base(bin), err, trimmed(stderr.String()))
	}
	return stdout.String(), nil
}

func trimmed(s string) string {
	const maxLen = 500
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// classifyContextError wraps a context.Canceled/DeadlineExceeded into
// ErrTimeout so internal/procapi's handler can map it to HTTP 504
// like any other pipeline timeout.
func classifyContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("pipeline: %w: %w", err, ErrTimeout)
	}
	return err
}

type assembleParams struct {
	requestID    string
	pageGrouping PageGrouping
	format       OutputFormat
	qpdfBin      string
	convertBin   string
	scratch      string
	pagePaths    []string
	// originalIndexes[i] is the source page index (into the original
	// Request.Pages) that produced pagePaths[i] — used to attribute
	// per-page warnings to the right output document after blank
	// pages have shifted per_page's output numbering.
	originalIndexes []int
	warnings        []string
}

// assemble implements design doc sec. 4.3 stage 7: merge params.pagePaths
// into one Document per params.pageGrouping.
func (p *ExecPipeline) assemble(ctx context.Context, params assembleParams) ([]Document, error) {
	ext := fileExtensionForFormat(params.format)
	contentType := contentTypeForFormat(params.format)

	if params.pageGrouping == PageGroupingPerPage {
		docs := make([]Document, 0, len(params.pagePaths))
		for i, path := range params.pagePaths {
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("pipeline: read assembled page %d: %w", i, err)
			}
			docs = append(docs, Document{
				Index:       i,
				Filename:    fmt.Sprintf("%s-page-%d.%s", params.requestID, i+1, ext),
				Content:     content,
				ContentType: contentType,
				PageCount:   1,
				Warnings:    warningsFor(params.originalIndexes[i], params.warnings),
			})
		}
		return docs, nil
	}

	// PageGroupingCombined.
	if len(params.pagePaths) == 1 {
		content, err := os.ReadFile(params.pagePaths[0])
		if err != nil {
			return nil, fmt.Errorf("pipeline: read assembled page: %w", err)
		}
		return []Document{{
			Index:       0,
			Filename:    fmt.Sprintf("%s.%s", params.requestID, ext),
			Content:     content,
			ContentType: contentType,
			PageCount:   1,
			Warnings:    params.warnings,
		}}, nil
	}

	switch params.format {
	case OutputFormatPDF:
		out := filepath.Join(params.scratch, "combined.pdf")
		if err := p.runQpdfMerge(ctx, params.qpdfBin, params.pagePaths, out); err != nil {
			return nil, fmt.Errorf("pipeline: merge %d pages into combined PDF: %w: %w", len(params.pagePaths), err, ErrOCRFailed)
		}
		content, err := os.ReadFile(out)
		if err != nil {
			return nil, fmt.Errorf("pipeline: read combined PDF: %w", err)
		}
		return []Document{{
			Index:       0,
			Filename:    fmt.Sprintf("%s.%s", params.requestID, ext),
			Content:     content,
			ContentType: contentType,
			PageCount:   len(params.pagePaths),
			Warnings:    params.warnings,
		}}, nil

	case OutputFormatTIFF:
		out := filepath.Join(params.scratch, "combined.tiff")
		if err := p.runConvert(ctx, params.convertBin, buildTIFFMergeArgs(params.pagePaths, out)); err != nil {
			return nil, fmt.Errorf("pipeline: merge %d pages into combined TIFF: %w: %w", len(params.pagePaths), err, ErrOCRFailed)
		}
		content, err := os.ReadFile(out)
		if err != nil {
			return nil, fmt.Errorf("pipeline: read combined TIFF: %w", err)
		}
		return []Document{{
			Index:       0,
			Filename:    fmt.Sprintf("%s.%s", params.requestID, ext),
			Content:     content,
			ContentType: contentType,
			PageCount:   len(params.pagePaths),
			Warnings:    params.warnings,
		}}, nil

	default:
		// JPEG cannot hold more than one page per file — a
		// multi-page "combined" JPEG request is a request the
		// pipeline cannot satisfy, not a transient processing
		// failure.
		return nil, fmt.Errorf(
			"pipeline: page_grouping=combined with output_format=jpeg and %d pages: JPEG does not support multiple pages per file: %w",
			len(params.pagePaths), ErrUnsupportedFormat)
	}
}

func (p *ExecPipeline) runQpdfMerge(ctx context.Context, qpdfBin string, pagePDFs []string, out string) error {
	_, err := p.runCommand(ctx, qpdfBin, buildQpdfMergeArgs(pagePDFs, out))
	return err
}

// warningsFor filters warnings for messages mentioning "page %d",
// matching per_page Document.Warnings to the page they describe.
// combined documents keep every warning (they apply to the whole
// assembled result); per_page documents only carry the ones relevant
// to their own source page plus any without a page-specific prefix.
func warningsFor(pageIndex int, all []string) []string {
	prefix := fmt.Sprintf("page %d:", pageIndex)
	var out []string
	for _, w := range all {
		if len(w) >= len(prefix) && w[:len(prefix)] == prefix {
			out = append(out, w)
		}
	}
	return out
}
