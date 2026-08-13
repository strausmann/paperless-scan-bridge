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

// buildOCRPDFArgs returns the tesseract(1) argument list that OCRs
// inPath and writes a searchable single-page PDF to
// outputBase+".pdf" (tesseract's own convention: the "pdf"
// configfile argument appends the extension to outputBase itself).
// languages are joined with "+" (tesseract's multi-language syntax,
// e.g. "deu+eng" per the design doc's default).
func buildOCRPDFArgs(inPath, outputBase string, languages []string) []string {
	args := []string{inPath, outputBase}
	if len(languages) > 0 {
		args = append(args, "-l", strings.Join(languages, "+"))
	}
	return append(args, "pdf")
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
