package pipeline

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// This file holds pure, exec-free helpers for ExecPipeline: building
// the argument lists passed to convert(1)/tesseract(1)/qpdf(1), and
// parsing their textual output. Splitting these out of exec_pipeline.go
// (which does the actual os/exec.CommandContext calls) mirrors
// components/sane-runtime/internal/scanner/argv.go's separation —
// buildArgv there is unit-tested without ever invoking scanimage(1),
// and exec_argv_test.go does the same here for a toolchain (three
// binaries) that is not installed on the CI runners building this
// module.

// deskewThreshold is the ImageMagick -deskew percentage. 40% is a
// commonly used default for scanned-document deskewing (enough to
// correct typical ADF feed skew without over-rotating clean scans).
const deskewThreshold = "40%"

// buildDeskewArgs returns the convert(1) argument list that
// deskews inPath and writes the result to outPath. +repage resets
// the virtual canvas convert's -deskew leaves behind, which
// otherwise carries through as unwanted offset/padding into every
// later stage.
func buildDeskewArgs(inPath, outPath string) []string {
	return []string{inPath, "-deskew", deskewThreshold, "+repage", outPath}
}

// buildMeanBrightnessArgs returns the identify(1) argument list that
// prints a page's mean pixel brightness (0..1, 1 = pure white) to
// stdout, used by isBlankPage to detect blank pages.
func buildMeanBrightnessArgs(inPath string) []string {
	return []string{"-format", "%[fx:mean]", inPath}
}

// meanBrightnessPattern matches identify(1)'s "%[fx:mean]" output: a
// bare decimal, optionally with leading/trailing whitespace.
var meanBrightnessPattern = regexp.MustCompile(`^\s*([0-9]*\.?[0-9]+)\s*$`)

// parseMeanBrightness parses identify(1)'s stdout for
// buildMeanBrightnessArgs into a 0..1 float.
func parseMeanBrightness(stdout string) (float64, error) {
	sub := meanBrightnessPattern.FindStringSubmatch(stdout)
	if sub == nil {
		return 0, fmt.Errorf("pipeline: unexpected identify(1) output %q", strings.TrimSpace(stdout))
	}
	v, err := strconv.ParseFloat(sub[1], 64)
	if err != nil {
		return 0, fmt.Errorf("pipeline: parse mean brightness %q: %w", sub[1], err)
	}
	return v, nil
}

// buildOSDArgs returns the tesseract(1) argument list that runs
// orientation-and-script detection on inPath and writes the result
// to stdout (outputbase "stdout" is a tesseract convention, not a
// literal file path). --psm 0 selects OSD-only mode: no OCR text
// output, just the orientation report parseOSDRotation reads.
func buildOSDArgs(inPath string) []string {
	return []string{inPath, "stdout", "--psm", "0"}
}

// osdRotatePattern matches tesseract --psm 0's "Rotate: N" line —
// the clockwise degrees to rotate the image to correct its detected
// orientation.
var osdRotatePattern = regexp.MustCompile(`(?m)^Rotate:\s*(-?\d+)\s*$`)

// parseOSDRotation extracts the rotation angle (degrees, clockwise)
// from tesseract --psm 0's stdout. It returns 0 with no error when no
// "Rotate:" line is present — a page tesseract could not confidently
// classify is left as-is rather than treated as a failure (matches
// the design doc's "independently skippable" stage framing; rotation
// is a best-effort correction, not a hard requirement).
func parseOSDRotation(stdout string) (int, error) {
	sub := osdRotatePattern.FindStringSubmatch(stdout)
	if sub == nil {
		return 0, nil
	}
	angle, err := strconv.Atoi(sub[1])
	if err != nil {
		return 0, fmt.Errorf("pipeline: parse OSD rotate angle %q: %w", sub[1], err)
	}
	return angle, nil
}

// buildRotateArgs returns the convert(1) argument list that rotates
// inPath by angle degrees (clockwise) and writes the result to
// outPath.
func buildRotateArgs(inPath, outPath string, angle int) []string {
	return []string{inPath, "-rotate", strconv.Itoa(angle), outPath}
}

// buildOCRArgs returns the tesseract(1) argument list that OCRs
// inPath and writes its output to outputBase-based files: a `tsv`
// configfile output (outputBase+".tsv") is always requested — its
// per-word confidence column feeds the confidence gate
// (parseOCRTSV, ExecPipeline.readOCRConfidence) — and, when wantPDF
// is true, additionally a searchable PDF (outputBase+".pdf",
// tesseract's own convention: the "pdf" configfile argument appends
// the extension to outputBase itself). tesseract accepts multiple
// configfile arguments in a single invocation and produces one output
// file per configfile from the same OCR pass, so requesting both
// "pdf" and "tsv" together costs exactly one tesseract run, never two
// (the jpeg/tiff "OCR check" caller, which does not need a PDF, calls
// this with wantPDF=false and still gets the tsv confidence data from
// that same single pass). languages are joined with "+" (tesseract's
// multi-language syntax, e.g. "deu+eng" per the design doc's
// default).
func buildOCRArgs(inPath, outputBase string, languages []string, wantPDF bool) []string {
	args := []string{inPath, outputBase}
	if len(languages) > 0 {
		args = append(args, "-l", strings.Join(languages, "+"))
	}
	if wantPDF {
		args = append(args, "pdf")
	}
	return append(args, "tsv")
}

// tesseract's `tsv` configfile output is tab-separated with a fixed
// 12-column header: level, page_num, block_num, par_num, line_num,
// word_num, left, top, width, height, conf, text. Only level-5 (word)
// rows carry a meaningful conf (0..100); every other level reports
// tesseract's documented -1 "not applicable" sentinel, since
// confidence is only ever computed per recognized word.
const (
	tsvLevelWord  = "5"
	tsvColConf    = 10
	tsvColText    = 11
	tsvMinColumns = tsvColText + 1
)

// parseOCRTSV parses tesseract's `tsv` configfile output and returns
// the mean per-word confidence (0..100, tesseract's own scale) and
// recognized word text across every level-5 (word) row with conf >=
// 0 — matching the PR brief's "gültige Wörter, conf >= 0 filtern".
// wordCount is the number of rows the mean was computed from; words
// is that same row set's text column, in document order, feeding
// detectLanguage's crude language-identification heuristic. A
// malformed conf field on an individual row is skipped rather than
// failing the whole parse (one bad row must not lose every other
// page's confidence signal); wordCount is 0 (mean 0, words nil) when
// tsv carries no valid word rows at all, e.g. a blank or entirely
// unreadable page.
func parseOCRTSV(tsv string) (mean float64, wordCount int, words []string) {
	var sum float64
	for i, line := range strings.Split(tsv, "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // header row / trailing blank line
		}
		cols := strings.Split(line, "\t")
		if len(cols) < tsvMinColumns {
			continue
		}
		if strings.TrimSpace(cols[0]) != tsvLevelWord {
			continue
		}
		conf, err := strconv.ParseFloat(strings.TrimSpace(cols[tsvColConf]), 64)
		if err != nil || conf < 0 {
			continue
		}
		sum += conf
		wordCount++
		if text := strings.TrimSpace(cols[tsvColText]); text != "" {
			words = append(words, text)
		}
	}
	if wordCount == 0 {
		return 0, 0, nil
	}
	return sum / float64(wordCount), wordCount, words
}

// isLowConfidence reports whether mean falls below threshold — the
// confidence gate's entire decision rule, pulled into its own named
// function (rather than inlined as "<") so the comparison direction
// and its single call site (ExecPipeline.applyConfidenceGate) both
// read unambiguously, and so a future refinement (e.g. hysteresis
// between consecutive requests) has one place to change.
func isLowConfidence(mean, threshold float64) bool {
	return mean < threshold
}

// meanFloat64 returns the arithmetic mean of vals, or 0 for an empty
// slice — used to aggregate a "combined" document's confidence across
// its surviving pages' individual OCR passes (assemble.applyConfidenceGate
// in exec_pipeline.go).
func meanFloat64(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// autoLanguageToken is the special OCRConfig.Languages value that
// requests the two-pass auto-language flow (ExecPipeline.ocrPageAuto)
// instead of a fixed language set — design: PR brief "pragmatische
// Auto-Language-Detection". It must be the request's only languages
// entry (validated by internal/procapi's own copy of this constant,
// api.go/handlers.go — see that package's doc comment on the
// duplication convention this mirrors for OCRConfig/PageGrouping/
// OutputFormat).
const autoLanguageToken = "auto"

// isAutoLanguageRequest reports whether languages requests the
// two-pass auto-detect flow: exactly one entry, equal to
// autoLanguageToken. A caller that also validates languages (e.g.
// procapi.validateProcessRequest) is expected to reject "auto" mixed
// with any other entry before this ever runs — this function only
// recognizes the one valid shape, it does not itself validate.
func isAutoLanguageRequest(languages []string) bool {
	return len(languages) == 1 && languages[0] == autoLanguageToken
}

// containsLanguage reports whether lang is present in list — used to
// decide whether ocrPageAuto's detected language is already covered
// by its first pass (defaultOCRLanguages), in which case a second
// tesseract pass would add no information.
func containsLanguage(list []string, lang string) bool {
	for _, l := range list {
		if l == lang {
			return true
		}
	}
	return false
}

// autoDetectCandidateLanguages lists the languages ocrPageAuto's
// detectLanguage call scores a page's pass-1 text against. It MUST
// stay in lockstep with the runtime image's installed tessdata packs
// — the same set internal/procapi/api.go's allowedOCRLanguageCodes
// enforces and ../../Dockerfile installs — for the same reason that
// comment gives: naming a language here whose tessdata is not
// installed means ocrPageAuto's second pass (buildOCRArgs with that
// language) fails at tesseract, not at a cheap validation step
// (mitigated here by ocrPageAuto treating a failed second pass as
// non-fatal and falling back to the first pass's result — see that
// function's doc comment — but the detection itself would still be
// wasted work for a language nothing can ever OCR against).
var autoDetectCandidateLanguages = []string{"deu", "eng", "fra", "ita", "spa", "nld", "por"}

// languageStopwords is a minimal, hand-picked set of very common short
// function words per autoDetectCandidateLanguages entry, used by
// detectLanguage's frequency-scoring heuristic. This is deliberately
// NOT a real language-identification model — no such library is a
// dependency of this module (go.mod carries none), and the PR brief's
// own framing accepts a "pragmatic" approach whose limits are
// documented rather than a from-scratch NLP component. See
// detectLanguage's doc comment for the accuracy limits this implies.
var languageStopwords = map[string][]string{
	"deu": {"der", "die", "das", "und", "ist", "nicht", "mit", "den", "von", "ein", "eine", "für", "im", "auf", "sich", "wir", "sie", "werden"},
	"eng": {"the", "and", "is", "of", "to", "in", "for", "with", "that", "this", "are", "was", "on", "as", "be", "you", "have"},
	"fra": {"le", "la", "les", "de", "et", "est", "un", "une", "des", "que", "pour", "dans", "avec", "vous", "ce", "nous"},
	"ita": {"il", "di", "che", "un", "una", "per", "con", "sono", "non", "gli", "questo", "del", "alla", "nel"},
	"spa": {"el", "de", "que", "y", "es", "un", "una", "para", "con", "los", "las", "por", "no", "se", "su"},
	"nld": {"het", "en", "van", "een", "is", "dat", "op", "voor", "met", "niet", "zijn", "aan", "te", "die", "wordt"},
	"por": {"o", "a", "de", "que", "e", "do", "da", "em", "para", "com", "os", "as", "não", "uma", "por"},
}

// detectLanguage scores rawText's words against each of candidates'
// stopword lists (case-insensitive, punctuation-trimmed whole-word
// match) and returns the highest-scoring candidate. Ties resolve
// toward whichever candidate appears earlier in candidates — callers
// pass autoDetectCandidateLanguages, a fixed order, so the same input
// always resolves the same way. Returns "" when no candidate scores
// above 0 (too little recognizable text to guess from), signalling
// ocrPageAuto to keep its first pass's result rather than force a
// second pass on a guess with no support.
//
// This is a crude, dependency-free heuristic, not a real
// language-identification model — see the README's "Auto language
// detection" section for the documented accuracy limits: short texts,
// closely related languages sharing vocabulary (es/it/pt, de/nl), and
// heavily garbled OCR output are all realistic misdetection cases.
// The confidence gate (parseOCRTSV/isLowConfidence) is the safety net
// for when this heuristic guesses wrong — ocrPageAuto keeps whichever
// pass actually scored higher, so a wrong guess here costs at most an
// unnecessary second pass, never a worse result than the first pass
// would have given alone.
func detectLanguage(rawText string, candidates []string) string {
	scores := make(map[string]int, len(candidates))
	for _, raw := range strings.Fields(strings.ToLower(rawText)) {
		word := strings.Trim(raw, ".,;:!?()[]{}\"'«»„“”—-")
		if word == "" {
			continue
		}
		for _, lang := range candidates {
			for _, stop := range languageStopwords[lang] {
				if word == stop {
					scores[lang]++
				}
			}
		}
	}
	best, bestScore := "", 0
	for _, lang := range candidates {
		if scores[lang] > bestScore {
			best, bestScore = lang, scores[lang]
		}
	}
	return best
}

// buildConvertFormatArgs returns the convert(1) argument list that
// converts inPath into outPath, with ImageMagick inferring the
// target format from outPath's extension.
func buildConvertFormatArgs(inPath, outPath string) []string {
	return []string{inPath, outPath}
}

// buildQpdfMergeArgs returns the qpdf(1) argument list that merges
// pagePDFs (each a single-page PDF, in order) into one combined PDF
// at outPath.
func buildQpdfMergeArgs(pagePDFs []string, outPath string) []string {
	args := []string{"--empty", "--pages"}
	args = append(args, pagePDFs...)
	args = append(args, "--", outPath)
	return args
}

// buildTIFFMergeArgs returns the convert(1) argument list that
// merges tiffPages (in order) into one multi-page TIFF at outPath —
// TIFF, unlike JPEG, natively supports multiple pages per file.
func buildTIFFMergeArgs(tiffPages []string, outPath string) []string {
	args := make([]string, 0, len(tiffPages)+1)
	args = append(args, tiffPages...)
	args = append(args, outPath)
	return args
}

// contentTypeForFormat maps an OutputFormat to the MIME type its
// assembled Document.ContentType carries.
func contentTypeForFormat(format OutputFormat) string {
	switch format {
	case OutputFormatJPEG:
		return "image/jpeg"
	case OutputFormatTIFF:
		return "image/tiff"
	default:
		return "application/pdf"
	}
}

// fileExtensionForFormat maps an OutputFormat to the extension
// convert(1) needs on an output path to pick the right encoder.
func fileExtensionForFormat(format OutputFormat) string {
	switch format {
	case OutputFormatJPEG:
		return "jpg"
	case OutputFormatTIFF:
		return "tiff"
	default:
		return "pdf"
	}
}
