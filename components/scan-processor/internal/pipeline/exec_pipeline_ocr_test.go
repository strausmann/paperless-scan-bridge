package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAssembleAppliesConfidenceGate exercises applyConfidenceGate
// through assemble() for the two code paths that need no external
// binary at all — PageGroupingPerPage (each document is read straight
// off disk) and PageGroupingCombined with exactly one surviving page
// (the "no merge needed" shortcut) — so this test runs as part of the
// default `go test ./...` (no "integration" build tag, no
// convert/tesseract/qpdf on PATH required). The >1-page combined
// merge paths (qpdf/convert) are covered by exec_pipeline_test.go's
// integration tests instead; the confidence-aggregation logic under
// test here is identical for those paths (meanFloat64 over the full
// params.confidences slice), so this is not a coverage gap.
func TestAssembleAppliesConfidenceGate(t *testing.T) {
	t.Parallel()

	writeFixturePage := func(t *testing.T, dir, name string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("fixture-page-bytes"), 0o600); err != nil {
			t.Fatalf("write fixture page %s: %v", path, err)
		}
		return path
	}

	t.Run("per_page: each document gets its own page's confidence", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := &ExecPipeline{}

		docs, err := p.assemble(context.Background(), assembleParams{
			requestID:       "req-1",
			pageGrouping:    PageGroupingPerPage,
			format:          OutputFormatJPEG,
			pagePaths:       []string{writeFixturePage(t, dir, "p0.jpg"), writeFixturePage(t, dir, "p1.jpg")},
			originalIndexes: []int{0, 1},
			ocrEnabled:      true,
			confidences:     []float64{95, 40},
			minConfidence:   80,
		})
		if err != nil {
			t.Fatalf("assemble: %v", err)
		}
		if len(docs) != 2 {
			t.Fatalf("len(docs) = %d, want 2", len(docs))
		}

		if docs[0].OCRConfidence != 95 || docs[0].LowConfidence {
			t.Errorf("docs[0] = confidence %v low %v, want 95/false", docs[0].OCRConfidence, docs[0].LowConfidence)
		}
		if len(docs[0].Warnings) != 0 {
			t.Errorf("docs[0].Warnings = %v, want empty (high confidence)", docs[0].Warnings)
		}

		if docs[1].OCRConfidence != 40 || !docs[1].LowConfidence {
			t.Errorf("docs[1] = confidence %v low %v, want 40/true", docs[1].OCRConfidence, docs[1].LowConfidence)
		}
		if len(docs[1].Warnings) != 1 || !strings.Contains(docs[1].Warnings[0], "low OCR confidence") {
			t.Errorf("docs[1].Warnings = %v, want one entry mentioning low OCR confidence", docs[1].Warnings)
		}
	})

	t.Run("combined single page: confidence carried straight through", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := &ExecPipeline{}

		docs, err := p.assemble(context.Background(), assembleParams{
			requestID:       "req-2",
			pageGrouping:    PageGroupingCombined,
			format:          OutputFormatPDF,
			pagePaths:       []string{writeFixturePage(t, dir, "p0.pdf")},
			originalIndexes: []int{0},
			ocrEnabled:      true,
			confidences:     []float64{60},
			minConfidence:   80,
		})
		if err != nil {
			t.Fatalf("assemble: %v", err)
		}
		if len(docs) != 1 {
			t.Fatalf("len(docs) = %d, want 1", len(docs))
		}
		if docs[0].OCRConfidence != 60 || !docs[0].LowConfidence {
			t.Errorf("docs[0] = confidence %v low %v, want 60/true", docs[0].OCRConfidence, docs[0].LowConfidence)
		}
		if len(docs[0].Warnings) != 1 {
			t.Errorf("docs[0].Warnings = %v, want one low-confidence entry", docs[0].Warnings)
		}
	})

	t.Run("ocr disabled: no confidence fields set, no warning added", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := &ExecPipeline{}

		docs, err := p.assemble(context.Background(), assembleParams{
			requestID:       "req-3",
			pageGrouping:    PageGroupingCombined,
			format:          OutputFormatPDF,
			pagePaths:       []string{writeFixturePage(t, dir, "p0.pdf")},
			originalIndexes: []int{0},
			ocrEnabled:      false,
			minConfidence:   80,
		})
		if err != nil {
			t.Fatalf("assemble: %v", err)
		}
		if docs[0].OCRConfidence != 0 || docs[0].LowConfidence {
			t.Errorf("docs[0] = confidence %v low %v, want 0/false (OCR disabled)", docs[0].OCRConfidence, docs[0].LowConfidence)
		}
		if len(docs[0].Warnings) != 0 {
			t.Errorf("docs[0].Warnings = %v, want empty (OCR disabled)", docs[0].Warnings)
		}
	})
}

// tesseractFixture writes a POSIX-sh fixture standing in for
// tesseract(1) (mirrors ExecPipeline's own doc comment: "Tests point
// these at fixture scripts"), so the tests below exercise the real
// ocrPage/ocrPageAuto/readOCRConfidence call chain — including
// Process()'s own orchestration of it — without needing the real
// tesseract binary on PATH (unlike exec_pipeline_test.go, this file
// carries no "integration" build tag).
//
// The script fakes tesseract's `pdf`/`tsv` configfile outputs based
// on the `-l` languages it was invoked with, so a single fixture can
// drive every scenario below: a fixed low/high confidence pair for
// the plain (non-auto) confidence-gate tests, and a "pass 1 guesses
// wrong, pass 2 (detected language) scores higher" pair for the
// auto-detect tests.
func tesseractFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tesseract")

	script := `#!/bin/sh
set -eu
in="$1"; shift
outbase="$1"; shift

langs=""
want_pdf=0
want_tsv=0
while [ $# -gt 0 ]; do
  case "$1" in
    -l) shift; langs="$1" ;;
    pdf) want_pdf=1 ;;
    tsv) want_tsv=1 ;;
  esac
  shift
done

text="hello world this is a test"
conf="88"
case "$langs" in
  deu+eng)
    # Pass 1 of the auto-detect flow: recognizable French text at
    # deliberately mediocre confidence, so detectLanguage guesses
    # "fra" and ocrPageAuto's pass-2 comparison has something to beat.
    text="bonjour le monde et la vie pour vous"
    conf="55"
    ;;
  fra)
    text="bonjour le monde et la vie pour vous"
    conf="92"
    ;;
  eng)
    text="hello world this is a test"
    conf="30"
    ;;
  low)
    text="hello world this is a test"
    conf="45"
    ;;
  noword)
    # No recognizable words at all -- a blank or entirely unreadable
    # page. The word loop below iterates zero times over an empty
    # text, so the tsv carries only its header row.
    text=""
    conf="0"
    ;;
esac

if [ "$want_pdf" -eq 1 ]; then
  printf '%%PDF-fake' > "${outbase}.pdf"
fi

if [ "$want_tsv" -eq 1 ]; then
  {
    printf 'level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n'
    for w in $text; do
      printf '5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t%s\t%s\n' "$conf" "$w"
    done
  } > "${outbase}.tsv"
fi

test -f "$in"
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // fixture script needs to be executable
		t.Fatalf("write tesseract fixture: %v", err)
	}
	return path
}

func TestExecPipeline_ProcessConfidenceGateFlagsLowConfidenceDocument(t *testing.T) {
	t.Parallel()

	p := &ExecPipeline{
		ConvertBin:   "/fixture-convert-never-invoked-for-this-request-shape",
		QpdfBin:      "/fixture-qpdf-never-invoked-single-page",
		TesseractBin: tesseractFixture(t),
	}

	req := Request{
		RequestID: "low-conf",
		Pages:     []Page{{Data: []byte("tiff-bytes")}},
		OCR: OCRConfig{
			Enabled:   true,
			Languages: []string{"eng"}, // fixture: "eng" -> conf 30
		},
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 30,
	}

	result, err := p.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(result.Documents) != 1 {
		t.Fatalf("len(Documents) = %d, want 1", len(result.Documents))
	}
	doc := result.Documents[0]
	if doc.OCRConfidence != 30 {
		t.Errorf("OCRConfidence = %v, want 30", doc.OCRConfidence)
	}
	if !doc.LowConfidence {
		t.Error("LowConfidence = false, want true (30 < default threshold 80)")
	}
	found := false
	for _, w := range doc.Warnings {
		if strings.Contains(w, "low OCR confidence") {
			found = true
		}
	}
	if !found {
		t.Errorf("Warnings = %v, want an entry mentioning low OCR confidence", doc.Warnings)
	}
}

func TestExecPipeline_ProcessConfidenceGateRespectsMinConfidenceOverride(t *testing.T) {
	t.Parallel()

	p := &ExecPipeline{
		ConvertBin:   "/fixture-convert-never-invoked-for-this-request-shape",
		QpdfBin:      "/fixture-qpdf-never-invoked-single-page",
		TesseractBin: tesseractFixture(t),
	}

	req := Request{
		RequestID: "override-threshold",
		Pages:     []Page{{Data: []byte("tiff-bytes")}},
		OCR: OCRConfig{
			Enabled:       true,
			Languages:     []string{"eng"}, // fixture: conf 30
			MinConfidence: 20,              // below 30 -> should NOT flag, unlike the 80 default
		},
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 30,
	}

	result, err := p.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	doc := result.Documents[0]
	if doc.LowConfidence {
		t.Errorf("LowConfidence = true, want false (30 >= overridden threshold 20)")
	}
}

func TestExecPipeline_ProcessAutoLanguageTwoPassPicksHigherConfidencePass(t *testing.T) {
	t.Parallel()

	p := &ExecPipeline{
		ConvertBin:   "/fixture-convert-never-invoked-for-this-request-shape",
		QpdfBin:      "/fixture-qpdf-never-invoked-single-page",
		TesseractBin: tesseractFixture(t),
	}

	req := Request{
		RequestID: "auto-detect",
		Pages:     []Page{{Data: []byte("tiff-bytes")}},
		OCR: OCRConfig{
			Enabled:   true,
			Languages: []string{"auto"},
		},
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 30,
	}

	result, err := p.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	doc := result.Documents[0]
	// Fixture: pass 1 (deu+eng) scores French text at conf 55;
	// detectLanguage should identify "fra" from that text; pass 2
	// (fra) scores the same text at conf 92 -- higher, so the pipeline
	// must have kept pass 2's result.
	if doc.OCRConfidence != 92 {
		t.Errorf("OCRConfidence = %v, want 92 (pass 2's higher-confidence result)", doc.OCRConfidence)
	}
	if doc.LowConfidence {
		t.Error("LowConfidence = true, want false (92 >= default threshold 80)")
	}
	if !strings.HasPrefix(string(doc.Content), "%PDF") {
		t.Errorf("Content = %q, want it to start with the fixture's %%PDF marker", doc.Content)
	}
}

func TestExecPipeline_ProcessAutoLanguageSkipsSecondPassWhenAlreadyCovered(t *testing.T) {
	t.Parallel()

	// A dedicated fixture whose deu+eng pass already recognizes
	// clearly-English text: detectLanguage should land on "eng",
	// which containsLanguage(defaultOCRLanguages, "eng") already
	// covers, so ocrPageAuto must stop after one pass. Reusing the
	// shared fixture's "deu+eng" case (French) would trigger a second
	// pass instead, so this test needs its own, English-only fixture.
	dir := t.TempDir()
	path := filepath.Join(dir, "tesseract")
	script := `#!/bin/sh
set -eu
in="$1"; shift
outbase="$1"; shift
want_pdf=0
want_tsv=0
while [ $# -gt 0 ]; do
  case "$1" in
    -l) shift ;;
    pdf) want_pdf=1 ;;
    tsv) want_tsv=1 ;;
  esac
  shift
done
if [ "$want_pdf" -eq 1 ]; then
  printf '%%PDF-fake' > "${outbase}.pdf"
fi
if [ "$want_tsv" -eq 1 ]; then
  {
    printf 'level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n'
    for w in the and is this that are; do
      printf '5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t97\t%s\n' "$w"
    done
  } > "${outbase}.tsv"
fi
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec
		t.Fatalf("write tesseract fixture: %v", err)
	}

	p := &ExecPipeline{
		ConvertBin:   "/fixture-convert-never-invoked-for-this-request-shape",
		QpdfBin:      "/fixture-qpdf-never-invoked-single-page",
		TesseractBin: path,
	}
	req := Request{
		RequestID:      "auto-detect-no-second-pass",
		Pages:          []Page{{Data: []byte("tiff-bytes")}},
		OCR:            OCRConfig{Enabled: true, Languages: []string{"auto"}},
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 30,
	}

	result, err := p.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.Documents[0].OCRConfidence != 97 {
		t.Errorf("OCRConfidence = %v, want 97 (single pass 1 result, no second pass triggered)", result.Documents[0].OCRConfidence)
	}
}

func TestExecPipeline_ProcessNoRecognizableWordsFlagsLowConfidenceAndWarns(t *testing.T) {
	t.Parallel()

	p := &ExecPipeline{
		ConvertBin:   "/fixture-convert-never-invoked-for-this-request-shape",
		QpdfBin:      "/fixture-qpdf-never-invoked-single-page",
		TesseractBin: tesseractFixture(t),
	}
	req := Request{
		RequestID:      "no-words",
		Pages:          []Page{{Data: []byte("tiff-bytes")}},
		OCR:            OCRConfig{Enabled: true, Languages: []string{"noword"}},
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 30,
	}

	result, err := p.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	doc := result.Documents[0]
	if doc.OCRConfidence != 0 {
		t.Errorf("OCRConfidence = %v, want 0 (no words found)", doc.OCRConfidence)
	}
	if !doc.LowConfidence {
		t.Error("LowConfidence = false, want true (0 < default threshold 80)")
	}
	foundWarning := false
	for _, w := range doc.Warnings {
		if strings.Contains(w, "no recognizable words") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("Warnings = %v, want an entry about no recognizable words", doc.Warnings)
	}
}

// TestExecPipeline_ProcessAutoLanguageNoRecognizableWordsOnPass1Warns covers
// ocrPageAuto's own "no recognizable words" warning (threaded through
// from parseOCRTSV's wordCount, mirroring readOCRConfidence's
// identical check for the non-auto path) — a blank/unreadable page
// via ocr.languages: [auto] must get the same specific warning text
// as the non-auto path, not just the generic low-confidence one.
// detectLanguage("") returns "" for zero recognized words, so pass 1
// with no words also means no second pass is attempted.
func TestExecPipeline_ProcessAutoLanguageNoRecognizableWordsOnPass1Warns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "tesseract")
	script := `#!/bin/sh
set -eu
in="$1"; shift
outbase="$1"; shift
want_pdf=0
want_tsv=0
while [ $# -gt 0 ]; do
  case "$1" in
    -l) shift ;;
    pdf) want_pdf=1 ;;
    tsv) want_tsv=1 ;;
  esac
  shift
done
if [ "$want_pdf" -eq 1 ]; then
  printf '%%PDF-fake' > "${outbase}.pdf"
fi
if [ "$want_tsv" -eq 1 ]; then
  printf 'level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n' > "${outbase}.tsv"
fi
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec
		t.Fatalf("write tesseract fixture: %v", err)
	}

	p := &ExecPipeline{
		ConvertBin:   "/fixture-convert-never-invoked-for-this-request-shape",
		QpdfBin:      "/fixture-qpdf-never-invoked-single-page",
		TesseractBin: path,
	}
	req := Request{
		RequestID:      "auto-no-words",
		Pages:          []Page{{Data: []byte("tiff-bytes")}},
		OCR:            OCRConfig{Enabled: true, Languages: []string{"auto"}},
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 30,
	}

	result, err := p.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	doc := result.Documents[0]
	if doc.OCRConfidence != 0 {
		t.Errorf("OCRConfidence = %v, want 0 (no words found)", doc.OCRConfidence)
	}
	if !doc.LowConfidence {
		t.Error("LowConfidence = false, want true (0 < default threshold 80)")
	}
	foundWarning := false
	for _, w := range doc.Warnings {
		if strings.Contains(w, "OCR found no recognizable words") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("Warnings = %v, want the same specific 'OCR found no recognizable words' entry the non-auto path uses", doc.Warnings)
	}
}

// failingTesseractFixture writes a fixture that always exits non-zero,
// exercising ocrPage's ErrOCRFailed wrapping without needing a real
// tesseract failure (e.g. a genuinely corrupt image).
func failingTesseractFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tesseract")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil { //nolint:gosec
		t.Fatalf("write failing tesseract fixture: %v", err)
	}
	return path
}

func TestExecPipeline_ProcessOCRFailureWrapsErrOCRFailed(t *testing.T) {
	t.Parallel()

	p := &ExecPipeline{
		ConvertBin:   "/fixture-convert-never-invoked-for-this-request-shape",
		QpdfBin:      "/fixture-qpdf-never-invoked-single-page",
		TesseractBin: failingTesseractFixture(t),
	}
	req := Request{
		RequestID:      "ocr-fails",
		Pages:          []Page{{Data: []byte("tiff-bytes")}},
		OCR:            OCRConfig{Enabled: true, Languages: []string{"eng"}},
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 30,
	}

	_, err := p.Process(context.Background(), req)
	if !errors.Is(err, ErrOCRFailed) {
		t.Fatalf("Process error = %v, want it to wrap ErrOCRFailed", err)
	}
}

func TestExecPipeline_ProcessAutoLanguageOCRFailureOnPass1WrapsErrOCRFailed(t *testing.T) {
	t.Parallel()

	p := &ExecPipeline{
		ConvertBin:   "/fixture-convert-never-invoked-for-this-request-shape",
		QpdfBin:      "/fixture-qpdf-never-invoked-single-page",
		TesseractBin: failingTesseractFixture(t),
	}
	req := Request{
		RequestID:      "auto-ocr-fails",
		Pages:          []Page{{Data: []byte("tiff-bytes")}},
		OCR:            OCRConfig{Enabled: true, Languages: []string{"auto"}},
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 30,
	}

	_, err := p.Process(context.Background(), req)
	if !errors.Is(err, ErrOCRFailed) {
		t.Fatalf("Process error = %v, want it to wrap ErrOCRFailed", err)
	}
}

// TestExecPipeline_ProcessAutoLanguagePass2FailureKeepsPass1Result covers
// ocrPageAuto's non-fatal fallback: pass 2 (the detected language)
// failing must not fail the whole request — pass 1's already-usable
// result is kept, with a warning recorded.
func TestExecPipeline_ProcessAutoLanguagePass2FailureKeepsPass1Result(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "tesseract")
	// Pass 1 (deu+eng) succeeds with recognizable French text (so
	// detectLanguage guesses "fra" and a pass 2 is attempted); pass 2
	// (-l fra) fails outright, simulating a candidate language whose
	// tessdata went missing from the runtime image.
	script := `#!/bin/sh
set -eu
in="$1"; shift
outbase="$1"; shift
langs=""
want_pdf=0
want_tsv=0
while [ $# -gt 0 ]; do
  case "$1" in
    -l) shift; langs="$1" ;;
    pdf) want_pdf=1 ;;
    tsv) want_tsv=1 ;;
  esac
  shift
done
if [ "$langs" = "fra" ]; then
  exit 1
fi
if [ "$want_pdf" -eq 1 ]; then
  printf '%%PDF-fake' > "${outbase}.pdf"
fi
if [ "$want_tsv" -eq 1 ]; then
  {
    printf 'level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n'
    for w in bonjour le monde et la vie; do
      printf '5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t60\t%s\n' "$w"
    done
  } > "${outbase}.tsv"
fi
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec
		t.Fatalf("write tesseract fixture: %v", err)
	}

	p := &ExecPipeline{
		ConvertBin:   "/fixture-convert-never-invoked-for-this-request-shape",
		QpdfBin:      "/fixture-qpdf-never-invoked-single-page",
		TesseractBin: path,
	}
	req := Request{
		RequestID:      "auto-pass2-fails",
		Pages:          []Page{{Data: []byte("tiff-bytes")}},
		OCR:            OCRConfig{Enabled: true, Languages: []string{"auto"}},
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 30,
	}

	result, err := p.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process: %v (pass 2 failure must not fail the whole request)", err)
	}
	if result.Documents[0].OCRConfidence != 60 {
		t.Errorf("OCRConfidence = %v, want 60 (pass 1's result, kept after pass 2 failed)", result.Documents[0].OCRConfidence)
	}
	foundWarning := false
	for _, w := range result.Documents[0].Warnings {
		if strings.Contains(w, "auto-language re-OCR") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("Warnings = %v, want an entry about the failed auto-language re-OCR", result.Documents[0].Warnings)
	}
}

// TestExecPipeline_ProcessAutoLanguageContextExpiryDuringPass2ReturnsErrTimeout
// covers the invariant every other exec call in this file already
// enforces via a ctx.Err() check at the top of its loop iteration
// (Process's per-page loops): a request whose context expires while
// scan-processor is mid-flight MUST surface as ErrTimeout (-> HTTP
// 504), never as a silent success. ocrPageAuto's pass-2 fallback is
// the one exec call in the whole pipeline that sits behind no further
// ctx-checking loop iteration (there is nothing left to run after it
// for the last/only page) -- if it treats a context-cancellation
// failure the same as an ordinary tesseract failure ("soft failure,
// fall back to pass 1"), the request wrongly returns 200 OK with
// degraded pass-1 OCR instead of timing out.
//
// The fixture's pass-2 branch (-l fra) sleeps far longer than the
// context timeout below; exec.CommandContext kills it once the
// deadline passes, so this test's own wall-clock cost is bounded by
// the timeout, not the sleep duration.
func TestExecPipeline_ProcessAutoLanguageContextExpiryDuringPass2ReturnsErrTimeout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "tesseract")
	script := `#!/bin/sh
set -eu
in="$1"; shift
outbase="$1"; shift
langs=""
want_pdf=0
want_tsv=0
while [ $# -gt 0 ]; do
  case "$1" in
    -l) shift; langs="$1" ;;
    pdf) want_pdf=1 ;;
    tsv) want_tsv=1 ;;
  esac
  shift
done
if [ "$langs" = "fra" ]; then
  # Pass 2 (the detected language): simulate it taking far longer
  # than the request's context deadline. "exec" replaces this
  # script's own process image with sleep(1) instead of forking a
  # child -- with a plain "sleep 5 &" or a forked "sleep 5" followed
  # by more script code, the sleep would become an orphaned
  # grandchild that keeps the Cmd's stdout/stderr pipe open after
  # exec.CommandContext kills the (already-exited) shell, so
  # cmd.Wait() would not observe EOF until the orphan itself exits --
  # a well-known os/exec gotcha, not something to reproduce here.
  # "exec" keeps this the SAME process Go is tracking, so killing it
  # closes the pipe immediately and the test's cost stays bounded by
  # the context timeout below, not the sleep duration.
  exec sleep 5
fi
if [ "$want_pdf" -eq 1 ]; then
  printf '%%PDF-fake' > "${outbase}.pdf"
fi
if [ "$want_tsv" -eq 1 ]; then
  {
    printf 'level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n'
    for w in bonjour le monde et la vie pour vous; do
      printf '5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t55\t%s\n' "$w"
    done
  } > "${outbase}.tsv"
fi
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec
		t.Fatalf("write tesseract fixture: %v", err)
	}

	p := &ExecPipeline{
		ConvertBin:   "/fixture-convert-never-invoked-for-this-request-shape",
		QpdfBin:      "/fixture-qpdf-never-invoked-single-page",
		TesseractBin: path,
	}
	req := Request{
		RequestID:      "auto-ctx-expiry-pass2",
		Pages:          []Page{{Data: []byte("tiff-bytes")}},
		OCR:            OCRConfig{Enabled: true, Languages: []string{"auto"}},
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 30,
	}

	// This is the request's own context, distinct from
	// TimeoutSeconds -- Process() only bounds itself by the ctx a
	// caller passes in (procapi's handleProcess derives that ctx from
	// TimeoutSeconds in production; here the test constructs it
	// directly, exactly like every other test in this file passes
	// context.Background()).
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err := p.Process(ctx, req)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Process error = %v, want it to wrap ErrTimeout (context expired during pass 2, must not silently fall back to pass 1)", err)
	}
}
