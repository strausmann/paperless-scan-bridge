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
	"strings"
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
// 4.3 stage 5: "deu+eng by default"). It also doubles as
// ocrPageAuto's first-pass language set (exec_argv.go).
var defaultOCRLanguages = []string{"deu", "eng"}

// defaultMinOCRConfidence is applied when OCRConfig.MinConfidence is
// zero (a profile that never set ocr.min_confidence). 80 (tesseract's
// own 0..100 mean-word-confidence scale) is a practical threshold
// that tolerates the confidence dips a merely noisy real-world scan
// produces without flagging it, while still catching genuinely bad
// OCR runs (garbled/rotated text, wrong language, mostly-graphic
// pages) — see the PR brief's "Server-Default ~80".
const defaultMinOCRConfidence = 80.0

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
	case OutputFormatPDF, OutputFormatJPEG, OutputFormatTIFF, OutputFormatPNG:
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

	autoDetect := req.OCR.Enabled && isAutoLanguageRequest(req.OCR.Languages)
	languages := req.OCR.Languages
	if req.OCR.Enabled && !autoDetect && len(languages) == 0 {
		languages = defaultOCRLanguages
	}
	minConfidence := req.OCR.MinConfidence
	if minConfidence <= 0 {
		minConfidence = defaultMinOCRConfidence
	}

	ext := fileExtensionForFormat(req.OutputFormat)
	assembledPages := make([]string, 0, len(survivors))
	assembledOriginalIndexes := make([]int, 0, len(survivors))
	var confidences []float64 // parallel to assembledPages, only appended to when req.OCR.Enabled
	for _, st := range survivors {
		if err := ctx.Err(); err != nil {
			return Result{}, classifyContextError(err)
		}

		var pagePath string
		if req.OCR.Enabled && req.OutputFormat == OutputFormatPDF {
			outBase, confidence, err := p.ocrPage(ctx, tesseractBin, st.path, scratch, st.originalIndex, true, autoDetect, languages, &warnings)
			if err != nil {
				return Result{}, err
			}
			pagePath = outBase + ".pdf"
			confidences = append(confidences, confidence)
		} else {
			if req.OCR.Enabled {
				// OCR is meaningful for the searchable-PDF case only
				// (design doc sec. 4.3 stage 6); for jpeg/tiff output
				// we still run OCR so a page tesseract cannot read at
				// all is surfaced as a failure, but discard the text
				// layer, since neither format carries one — the
				// confidence gate still reads this pass's tsv output,
				// though, since ocrPage always requests it.
				_, confidence, err := p.ocrPage(ctx, tesseractBin, st.path, scratch, st.originalIndex, false, autoDetect, languages, &warnings)
				if err != nil {
					return Result{}, err
				}
				confidences = append(confidences, confidence)
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
		ocrEnabled:      req.OCR.Enabled,
		confidences:     confidences,
		minConfidence:   minConfidence,
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

// ocrPage runs OCR for one page and returns the output base (its
// .pdf/.tsv files live under scratch, named outputBase+".pdf"/".tsv")
// and the mean confidence the confidence gate (assemble) reads.
// Dispatches to the two-pass auto-detect flow (ocrPageAuto) when
// autoDetect is true; otherwise runs a single tesseract pass against
// the request's configured languages. wantPDF controls whether a
// searchable PDF is produced alongside the always-requested tsv
// confidence data (buildOCRArgs).
func (p *ExecPipeline) ocrPage(ctx context.Context, tesseractBin, inPath, scratch string, originalIndex int, wantPDF, autoDetect bool, languages []string, warnings *[]string) (outputBase string, confidence float64, err error) {
	if autoDetect {
		return p.ocrPageAuto(ctx, tesseractBin, inPath, scratch, originalIndex, wantPDF, warnings)
	}
	suffix := "ocr"
	if !wantPDF {
		suffix = "ocr-check"
	}
	outBase := filepath.Join(scratch, fmt.Sprintf("page-%d-%s", originalIndex, suffix))
	if _, err := p.runTesseract(ctx, tesseractBin, buildOCRArgs(inPath, outBase, languages, wantPDF)); err != nil {
		return "", 0, fmt.Errorf("pipeline: OCR page %d: %w: %w", originalIndex, err, ErrOCRFailed)
	}
	return outBase, p.readOCRConfidence(outBase, originalIndex, warnings), nil
}

// ocrPageAuto implements the "ocr.languages: [auto]" two-pass flow for
// one page (PR brief: "pragmatische Auto-Language-Detection"):
//
//  1. OCR the page with defaultOCRLanguages (deu+eng) — the project's
//     long-standing HomeLab default, and a safe first guess.
//  2. Run detectLanguage (exec_argv.go) over that pass's recognized
//     text against autoDetectCandidateLanguages to guess the page's
//     actual language.
//  3. If the guess is empty (no recognizable stopwords) or already
//     covered by defaultOCRLanguages, stop here — a second pass would
//     add no information (this is also what keeps "auto" from ever
//     costing more than deu+eng for the common case where that
//     default was already right).
//  4. Otherwise, re-OCR once more with just the detected language and
//     keep whichever of the two passes scored the higher mean
//     confidence (parseOCRTSV) — the confidence gate's own signal
//     doubles as this flow's pass-selection criterion, so a wrong
//     detectLanguage guess costs at most one wasted tesseract call,
//     never a worse result than pass 1 alone would have given.
//
// This deliberately never OCRs with more than two language
// candidates in a single tesseract invocation — the project's own OCR
// evaluation found that passing every installed language to `-l`
// simultaneously measurably reduces recognition quality (shared
// dictionary overlap between similar-script languages "correcting"
// words into the wrong language's spelling — the same effect
// documented in the scan-processor README's "OCR languages" section
// for why a profile should not simply name every installed language).
// A tesseract failure on pass 2 (e.g. a candidate language whose
// tessdata went missing from the image despite being listed in
// autoDetectCandidateLanguages) is not fatal: pass 1's already-usable
// result is kept and a warning recorded, matching the confidence
// gate's own "never fail, only flag/warn" contract.
func (p *ExecPipeline) ocrPageAuto(ctx context.Context, tesseractBin, inPath, scratch string, originalIndex int, wantPDF bool, warnings *[]string) (outputBase string, confidence float64, err error) {
	pass1Base := filepath.Join(scratch, fmt.Sprintf("page-%d-ocr-auto1", originalIndex))
	if _, err := p.runTesseract(ctx, tesseractBin, buildOCRArgs(inPath, pass1Base, defaultOCRLanguages, wantPDF)); err != nil {
		return "", 0, fmt.Errorf("pipeline: OCR page %d (auto-language pass 1): %w: %w", originalIndex, err, ErrOCRFailed)
	}
	tsv1, readErr := os.ReadFile(pass1Base + ".tsv")
	if readErr != nil {
		*warnings = append(*warnings, fmt.Sprintf("page %d: could not read OCR confidence data (auto-language pass 1): %v", originalIndex, readErr))
		return pass1Base, 0, nil
	}
	conf1, wordCount1, words1 := parseOCRTSV(string(tsv1))
	if wordCount1 == 0 {
		warnNoWordsFound(originalIndex, warnings)
	}

	detected := detectLanguage(strings.Join(words1, " "), autoDetectCandidateLanguages)
	if detected == "" || containsLanguage(defaultOCRLanguages, detected) {
		return pass1Base, conf1, nil
	}

	pass2Base := filepath.Join(scratch, fmt.Sprintf("page-%d-ocr-auto2", originalIndex))
	if _, err := p.runTesseract(ctx, tesseractBin, buildOCRArgs(inPath, pass2Base, []string{detected}, wantPDF)); err != nil {
		// A context expiry/cancellation surfacing here (as an ordinary
		// tesseract error, since runCommand's classifyContextError
		// already folded it into err) must NOT be swallowed as a
		// soft pass-2 failure -- unlike a genuine tesseract error
		// (bad tessdata, corrupt image), this is the invariant every
		// other exec call in Process() enforces via its own ctx.Err()
		// check at the top of each per-page loop iteration
		// (design doc / wire contract: ErrTimeout -> HTTP 504). This
		// call is the one exec site with no further loop iteration
		// behind it to catch a mid-flight expiry, so it must check
		// explicitly rather than inheriting the "soft failure, fall
		// back to pass 1" handling below.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", 0, classifyContextError(ctxErr)
		}
		*warnings = append(*warnings, fmt.Sprintf("page %d: auto-language re-OCR with detected language %q failed, keeping deu+eng result: %v", originalIndex, detected, err))
		return pass1Base, conf1, nil
	}
	tsv2, readErr := os.ReadFile(pass2Base + ".tsv")
	if readErr != nil {
		*warnings = append(*warnings, fmt.Sprintf("page %d: could not read OCR confidence data (auto-language pass 2, detected %q): %v", originalIndex, detected, readErr))
		return pass1Base, conf1, nil
	}
	conf2, wordCount2, _ := parseOCRTSV(string(tsv2))
	if wordCount2 == 0 {
		warnNoWordsFound(originalIndex, warnings)
	}

	if conf2 > conf1 {
		return pass2Base, conf2, nil
	}
	return pass1Base, conf1, nil
}

// warnNoWordsFound appends the confidence gate's "no recognizable
// words" warning for originalIndex -- shared by readOCRConfidence
// (the non-auto OCR path) and ocrPageAuto (both of its tsv reads), so
// the exact wording cannot drift between call sites.
func warnNoWordsFound(originalIndex int, warnings *[]string) {
	*warnings = append(*warnings, fmt.Sprintf("page %d: OCR found no recognizable words", originalIndex))
}

// readOCRConfidence reads outputBase+".tsv" (written by buildOCRArgs's
// always-included "tsv" configfile) and returns its mean per-word OCR
// confidence for the confidence gate. A read failure is logged via a
// warning and treated as 0 confidence rather than failing the page —
// the PDF/converted output tesseract already produced is still
// usable; losing only the advisory confidence signal applies the
// gate's own "never fail, only flag" contract to its own plumbing,
// not just its threshold check. A tsv with no recognized words at all
// (wordCount 0 — a blank or entirely unreadable page) also gets its
// own warning, since a mean of exactly 0 in that case reflects "no
// data", not "0% confident text was found".
func (p *ExecPipeline) readOCRConfidence(outputBase string, originalIndex int, warnings *[]string) float64 {
	data, err := os.ReadFile(outputBase + ".tsv")
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("page %d: could not read OCR confidence data: %v", originalIndex, err))
		return 0
	}
	mean, wordCount, _ := parseOCRTSV(string(data))
	if wordCount == 0 {
		warnNoWordsFound(originalIndex, warnings)
	}
	return mean
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

	// ocrEnabled, confidences, and minConfidence feed the confidence
	// gate (applyConfidenceGate below) — PR brief "Konfidenz-/
	// Qualitäts-Gate". confidences[i] is the mean OCR confidence
	// (parseOCRTSV, exec_argv.go) for the page that produced
	// pagePaths[i]; it is only populated (and only meaningful) when
	// ocrEnabled is true, mirroring how Process only appends to its
	// own confidences slice under the same condition.
	ocrEnabled    bool
	confidences   []float64
	minConfidence float64
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
			doc := Document{
				Index:       i,
				Filename:    fmt.Sprintf("%s-page-%d.%s", params.requestID, i+1, ext),
				Content:     content,
				ContentType: contentType,
				PageCount:   1,
				Warnings:    warningsFor(params.originalIndexes[i], params.warnings),
			}
			applyConfidenceGate(&doc, params, i, i+1)
			docs = append(docs, doc)
		}
		return docs, nil
	}

	// PageGroupingCombined.
	if len(params.pagePaths) == 1 {
		content, err := os.ReadFile(params.pagePaths[0])
		if err != nil {
			return nil, fmt.Errorf("pipeline: read assembled page: %w", err)
		}
		doc := Document{
			Index:       0,
			Filename:    fmt.Sprintf("%s.%s", params.requestID, ext),
			Content:     content,
			ContentType: contentType,
			PageCount:   1,
			Warnings:    params.warnings,
		}
		applyConfidenceGate(&doc, params, 0, len(params.confidences))
		return []Document{doc}, nil
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
		doc := Document{
			Index:       0,
			Filename:    fmt.Sprintf("%s.%s", params.requestID, ext),
			Content:     content,
			ContentType: contentType,
			PageCount:   len(params.pagePaths),
			Warnings:    params.warnings,
		}
		applyConfidenceGate(&doc, params, 0, len(params.confidences))
		return []Document{doc}, nil

	case OutputFormatTIFF:
		out := filepath.Join(params.scratch, "combined.tiff")
		if err := p.runConvert(ctx, params.convertBin, buildTIFFMergeArgs(params.pagePaths, out)); err != nil {
			return nil, fmt.Errorf("pipeline: merge %d pages into combined TIFF: %w: %w", len(params.pagePaths), err, ErrOCRFailed)
		}
		content, err := os.ReadFile(out)
		if err != nil {
			return nil, fmt.Errorf("pipeline: read combined TIFF: %w", err)
		}
		doc := Document{
			Index:       0,
			Filename:    fmt.Sprintf("%s.%s", params.requestID, ext),
			Content:     content,
			ContentType: contentType,
			PageCount:   len(params.pagePaths),
			Warnings:    params.warnings,
		}
		applyConfidenceGate(&doc, params, 0, len(params.confidences))
		return []Document{doc}, nil

	default:
		// JPEG and PNG hold exactly one image per file — a multi-page
		// "combined" request in either is a request the pipeline
		// cannot satisfy, not a transient processing failure. The
		// format is named in the message because "combined" is the
		// caller's word and the profile is where they would fix it.
		return nil, fmt.Errorf(
			"pipeline: page_grouping=combined with output_format=%s and %d pages: %s does not support multiple pages per file: %w",
			params.format, len(params.pagePaths), strings.ToUpper(string(params.format)), ErrUnsupportedFormat)
	}
}

// applyConfidenceGate sets doc.OCRConfidence/doc.LowConfidence (and,
// when flagged, appends a matching warning to doc.Warnings) from
// params.confidences[from:to] — a per_page document reads its own
// single entry (from==to-1), a combined document aggregates every
// surviving page's confidence via meanFloat64 (from==0,
// to==len(params.confidences)). A no-op when OCR did not run
// (params.ocrEnabled false) or there is no confidence data for the
// requested range (a defensive bounds check, not expected to trigger
// in practice since Process always appends exactly one confidences
// entry per assembled page when OCR is enabled).
func applyConfidenceGate(doc *Document, params assembleParams, from, to int) {
	if !params.ocrEnabled || from < 0 || to > len(params.confidences) || from >= to {
		return
	}
	doc.OCRConfidence = meanFloat64(params.confidences[from:to])
	doc.LowConfidence = isLowConfidence(doc.OCRConfidence, params.minConfidence)
	if doc.LowConfidence {
		doc.Warnings = append(doc.Warnings, fmt.Sprintf(
			"low OCR confidence (%.1f, threshold %.1f)", doc.OCRConfidence, params.minConfidence))
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
