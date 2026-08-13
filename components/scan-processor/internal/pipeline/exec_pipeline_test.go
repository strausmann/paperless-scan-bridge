//go:build integration

// Package pipeline's integration tests drive the real
// convert(1)/tesseract(1)/qpdf(1) toolchain ExecPipeline shells out
// to. They are gated behind the "integration" build tag (see the
// package doc comment in pipeline.go) so `go test ./...` — what CI
// and every other test in this module runs — never requires those
// binaries to be installed. Run explicitly with:
//
//	go test -tags integration ./internal/pipeline/...
//
// on a host that has ImageMagick, tesseract-ocr (+ tesseract-ocr-deu,
// tesseract-ocr-eng), and qpdf installed — exactly the toolchain the
// Dockerfile's runtime stage installs.
package pipeline

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// requireBinary skips the test when name is not on PATH, rather than
// failing — this file is only ever run deliberately (via -tags
// integration), but a developer's machine may still be missing one
// of the three binaries.
func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("integration test requires %s on PATH: %v", name, err)
	}
}

// generateTIFF shells out to convert(1) itself to build a test-fixture
// TIFF page — using the same binary under test to generate fixtures
// avoids adding a Go image-encoding dependency to this module purely
// for an integration test nobody runs without ImageMagick already on
// PATH anyway (requireBinary above already asserts that).
func generateTIFF(t *testing.T, drawBlackRect bool) []byte {
	t.Helper()
	requireBinary(t, "convert")

	dir := t.TempDir()
	out := filepath.Join(dir, "fixture.tiff")

	args := []string{"-size", "200x200", "xc:white"}
	if drawBlackRect {
		args = append(args, "-fill", "black", "-draw", "rectangle 50,50 150,150")
	}
	args = append(args, out)

	cmd := exec.Command("convert", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("generate fixture TIFF: %v (stderr: %s)", err, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read fixture TIFF: %v", err)
	}
	return data
}

func TestExecPipeline_CombinedPDFRemovesBlankPage(t *testing.T) {
	requireBinary(t, "convert")
	requireBinary(t, "qpdf")

	p := &ExecPipeline{}
	req := Request{
		RequestID:      "it-combined",
		Pages:          []Page{{Data: generateTIFF(t, true)}, {Data: generateTIFF(t, false)}},
		RemoveBlank:    true,
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 60,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := p.Process(ctx, req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(result.Documents) != 1 {
		t.Fatalf("len(Documents) = %d, want 1", len(result.Documents))
	}
	doc := result.Documents[0]
	if doc.PageCount != 1 {
		t.Errorf("PageCount = %d, want 1 (blank page should have been removed)", doc.PageCount)
	}
	if doc.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q, want application/pdf", doc.ContentType)
	}
	if len(doc.Content) == 0 {
		t.Error("Content is empty")
	}
	if !bytes.HasPrefix(doc.Content, []byte("%PDF")) {
		want := doc.Content
		if len(want) > 8 {
			want = want[:8]
		}
		t.Errorf("Content does not start with a PDF header: %q", want)
	}
}

func TestExecPipeline_PerPageJPEGProducesOneDocumentPerPage(t *testing.T) {
	requireBinary(t, "convert")

	p := &ExecPipeline{}
	req := Request{
		RequestID:      "it-perpage",
		Pages:          []Page{{Data: generateTIFF(t, true)}, {Data: generateTIFF(t, true)}},
		PageGrouping:   PageGroupingPerPage,
		OutputFormat:   OutputFormatJPEG,
		TimeoutSeconds: 60,
	}

	result, err := p.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(result.Documents) != 2 {
		t.Fatalf("len(Documents) = %d, want 2", len(result.Documents))
	}
	for i, doc := range result.Documents {
		if doc.ContentType != "image/jpeg" {
			t.Errorf("document %d ContentType = %q, want image/jpeg", i, doc.ContentType)
		}
		if len(doc.Content) == 0 {
			t.Errorf("document %d Content is empty", i)
		}
	}
}
