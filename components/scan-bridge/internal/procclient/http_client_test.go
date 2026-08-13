package procclient

import (
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// startFakeScanProcessor brings up handler on a fresh Unix-domain
// socket and returns the socket path. It stands in for the
// scan-processor container (Task 6, not yet built) — Task 5 fixes the
// wire contract client and server must agree on; this fake server plays
// the server side of that contract for tests, exactly as
// dispatch/http_client_test.go's startFakeSaneRuntime does for
// sane-runtime.
func startFakeScanProcessor(t *testing.T, handler http.Handler) string {
	t.Helper()

	// Deliberately not t.TempDir(): its path embeds the (sub)test
	// name, which for table-driven subtests routinely exceeds the
	// ~104-108 byte sun_path limit the kernel enforces for AF_UNIX
	// addresses ("bind: invalid argument"). A short,
	// unrelated-to-the-test-name temp dir keeps the socket path well
	// under the limit.
	dir, err := os.MkdirTemp("", "sp-sock")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix %q: %v", sockPath, err)
	}

	srv := &http.Server{Handler: handler}
	go func() {
		_ = srv.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = srv.Close()
	})

	return sockPath
}

// writeTestPage creates a small TIFF-standin file under dir and returns
// its path, for use as a ProcessRequest.PagePaths entry.
func writeTestPage(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test page %q: %v", path, err)
	}
	return path
}

func testRequest(requestID string, pagePaths ...string) ProcessRequest {
	return ProcessRequest{
		RequestID:      requestID,
		PagePaths:      pagePaths,
		OCR:            OCRConfig{Enabled: true, Languages: []string{"deu", "eng"}},
		Deskew:         true,
		RemoveBlank:    true,
		RotatePages:    false,
		PageGrouping:   PageGroupingCombined,
		OutputFormat:   OutputFormatPDF,
		TimeoutSeconds: 90,
	}
}

func TestProcessHappyPathWritesDocumentsAndReturnsPaths(t *testing.T) {
	t.Parallel()

	pagesDir := t.TempDir()
	page1 := writeTestPage(t, pagesDir, "page-1.tiff", "fake-tiff-bytes-1")
	page2 := writeTestPage(t, pagesDir, "page-2.tiff", "fake-tiff-bytes-2")

	handler := http.NewServeMux()
	handler.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := parseMultipartContentType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("fake scan-processor: parse content-type: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if mediaType != "multipart/mixed" {
			t.Errorf("fake scan-processor: content-type = %q, want multipart/mixed", mediaType)
		}

		mr := multipart.NewReader(r.Body, params["boundary"])

		ctrlPart, err := mr.NextPart()
		if err != nil {
			t.Fatalf("fake scan-processor: read control part: %v", err)
		}
		var got processRequestPayload
		if err := json.NewDecoder(ctrlPart).Decode(&got); err != nil {
			t.Errorf("fake scan-processor: decode control payload: %v", err)
		}
		_ = ctrlPart.Close()

		if got.RequestID != "job-abc" {
			t.Errorf("fake scan-processor: request_id = %q, want job-abc", got.RequestID)
		}
		if !got.OCR.Enabled {
			t.Errorf("fake scan-processor: ocr.enabled = false, want true")
		}
		if got.PageGrouping != "combined" {
			t.Errorf("fake scan-processor: page_grouping = %q, want combined", got.PageGrouping)
		}
		if got.OutputFormat != "pdf" {
			t.Errorf("fake scan-processor: output_format = %q, want pdf", got.OutputFormat)
		}

		var pageBodies []string
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			buf := make([]byte, 64)
			n, _ := part.Read(buf)
			pageBodies = append(pageBodies, string(buf[:n]))
			_ = part.Close()
		}
		if len(pageBodies) != 2 {
			t.Errorf("fake scan-processor: got %d page parts, want 2", len(pageBodies))
		}

		mw := multipart.NewWriter(w)
		w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
		w.WriteHeader(http.StatusOK)

		metaPart, err := mw.CreatePart(map[string][]string{"Content-Type": {"application/json"}})
		if err != nil {
			t.Fatalf("fake scan-processor: create metadata part: %v", err)
		}
		meta := processMetadata{
			RequestID: got.RequestID,
			Documents: []documentMetadata{
				{Index: 0, PageCount: 2, Filename: "2026-08-13_receipt.pdf", ContentType: "application/pdf", Warnings: nil},
			},
			DurationMs: 4321,
		}
		if err := json.NewEncoder(metaPart).Encode(meta); err != nil {
			t.Fatalf("fake scan-processor: encode metadata: %v", err)
		}

		docPart, err := mw.CreatePart(map[string][]string{"Content-Type": {"application/pdf"}})
		if err != nil {
			t.Fatalf("fake scan-processor: create document part: %v", err)
		}
		if _, err := docPart.Write([]byte("fake-pdf-bytes")); err != nil {
			t.Fatalf("fake scan-processor: write document bytes: %v", err)
		}
		if err := mw.Close(); err != nil {
			t.Fatalf("fake scan-processor: close multipart writer: %v", err)
		}
	})

	sockPath := startFakeScanProcessor(t, handler)
	outputDir := t.TempDir()
	client := NewHTTPUnixClient(sockPath, outputDir, 5*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	result, err := client.Process(context.Background(), testRequest("job-abc", page1, page2))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.RequestID != "job-abc" {
		t.Errorf("RequestID = %q, want job-abc", result.RequestID)
	}
	if result.DurationMillis != 4321 {
		t.Errorf("DurationMillis = %d, want 4321", result.DurationMillis)
	}
	if len(result.Documents) != 1 {
		t.Fatalf("len(Documents) = %d, want 1", len(result.Documents))
	}

	doc := result.Documents[0]
	if doc.Filename != "2026-08-13_receipt.pdf" {
		t.Errorf("Filename = %q, want 2026-08-13_receipt.pdf", doc.Filename)
	}
	if doc.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q, want application/pdf", doc.ContentType)
	}
	if doc.PageCount != 2 {
		t.Errorf("PageCount = %d, want 2", doc.PageCount)
	}
	if filepath.Dir(doc.Path) != filepath.Join(outputDir, "job-abc") {
		t.Errorf("Path %q not under %s/job-abc", doc.Path, outputDir)
	}
	data, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatalf("read document file %q: %v", doc.Path, err)
	}
	if string(data) != "fake-pdf-bytes" {
		t.Errorf("document file contents = %q, want fake-pdf-bytes", string(data))
	}
}

func TestProcessPerPageGroupingWritesMultipleDocuments(t *testing.T) {
	t.Parallel()

	pagesDir := t.TempDir()
	page1 := writeTestPage(t, pagesDir, "page-1.tiff", "fake-tiff-bytes-1")

	handler := http.NewServeMux()
	handler.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := parseMultipartContentType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		// Drain the request body (control payload + page parts) so the
		// fake server behaves like a real one that reads before
		// replying.
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			_, _ = part.Read(make([]byte, 64))
			_ = part.Close()
		}

		mw := multipart.NewWriter(w)
		w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
		w.WriteHeader(http.StatusOK)

		metaPart, _ := mw.CreatePart(map[string][]string{"Content-Type": {"application/json"}})
		meta := processMetadata{
			RequestID: "job-perpage",
			Documents: []documentMetadata{
				{Index: 0, PageCount: 1, Filename: "page-1.jpeg", ContentType: "image/jpeg"},
				{Index: 1, PageCount: 1, Filename: "page-2.jpeg", ContentType: "image/jpeg"},
			},
			DurationMs: 111,
		}
		_ = json.NewEncoder(metaPart).Encode(meta)

		for _, name := range []string{"doc0", "doc1"} {
			p, _ := mw.CreatePart(map[string][]string{"Content-Type": {"image/jpeg"}})
			_, _ = p.Write([]byte(name))
		}
		_ = mw.Close()
	})

	sockPath := startFakeScanProcessor(t, handler)
	client := NewHTTPUnixClient(sockPath, t.TempDir(), 5*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	req := testRequest("job-perpage", page1)
	req.PageGrouping = PageGroupingPerPage
	result, err := client.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(result.Documents) != 2 {
		t.Fatalf("len(Documents) = %d, want 2", len(result.Documents))
	}
	if result.Documents[0].Filename != "page-1.jpeg" || result.Documents[1].Filename != "page-2.jpeg" {
		t.Errorf("unexpected filenames: %q, %q", result.Documents[0].Filename, result.Documents[1].Filename)
	}
}

func TestProcessScanProcessorErrorEnvelopeMapsToSentinel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"unsupported format", http.StatusBadRequest, ErrUnsupportedFormat},
		{"busy", http.StatusConflict, ErrBusy},
		{"ocr failed", http.StatusUnprocessableEntity, ErrOCRFailed},
		{"scan-processor timeout", http.StatusGatewayTimeout, ErrTimeout},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := http.NewServeMux()
			handler.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "synthetic",
					"hint":  "injected by test",
				})
			})

			sockPath := startFakeScanProcessor(t, handler)
			pagesDir := t.TempDir()
			page1 := writeTestPage(t, pagesDir, "page-1.tiff", "x")
			client := NewHTTPUnixClient(sockPath, t.TempDir(), 5*time.Second)
			t.Cleanup(func() { _ = client.Close() })

			_, err := client.Process(context.Background(), testRequest("job-err", page1))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want wrapped %v", err, tc.want)
			}
		})
	}
}

func TestProcessSocketMissingReturnsWrappedError(t *testing.T) {
	t.Parallel()

	missingSock := filepath.Join(t.TempDir(), "does-not-exist.sock")
	pagesDir := t.TempDir()
	page1 := writeTestPage(t, pagesDir, "page-1.tiff", "x")
	client := NewHTTPUnixClient(missingSock, t.TempDir(), 2*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	_, err := client.Process(context.Background(), testRequest("job-net", page1))
	if err == nil {
		t.Fatal("expected error processing against a nonexistent socket")
	}
	// The interesting assertion is that we got here at all — a dial
	// failure must not panic anywhere in the request-building or
	// response-reading path.
}

func TestProcessMissingPageFileReturnsError(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	handler.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		t.Error("fake scan-processor: handler must not be reached when a page file is missing")
	})
	sockPath := startFakeScanProcessor(t, handler)
	client := NewHTTPUnixClient(sockPath, t.TempDir(), 2*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	missingPage := filepath.Join(t.TempDir(), "does-not-exist.tiff")
	_, err := client.Process(context.Background(), testRequest("job-missing-page", missingPage))
	if err == nil {
		t.Fatal("expected error when a page file does not exist")
	}
}

func TestProcessUnsafeDocumentFilenameIsRejected(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	handler.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := parseMultipartContentType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			_, _ = part.Read(make([]byte, 64))
			_ = part.Close()
		}

		mw := multipart.NewWriter(w)
		w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
		w.WriteHeader(http.StatusOK)

		metaPart, _ := mw.CreatePart(map[string][]string{"Content-Type": {"application/json"}})
		meta := processMetadata{
			RequestID: "job-traversal",
			Documents: []documentMetadata{
				{Index: 0, PageCount: 1, Filename: "../../etc/passwd", ContentType: "application/pdf"},
			},
			DurationMs: 1,
		}
		_ = json.NewEncoder(metaPart).Encode(meta)
		p, _ := mw.CreatePart(map[string][]string{"Content-Type": {"application/pdf"}})
		_, _ = p.Write([]byte("evil"))
		_ = mw.Close()
	})

	sockPath := startFakeScanProcessor(t, handler)
	pagesDir := t.TempDir()
	page1 := writeTestPage(t, pagesDir, "page-1.tiff", "x")
	client := NewHTTPUnixClient(sockPath, t.TempDir(), 2*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	_, err := client.Process(context.Background(), testRequest("job-traversal", page1))
	if err == nil {
		t.Fatal("expected error for a path-traversal document filename")
	}
}

func TestProcessDocumentCountMismatchReturnsError(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	handler.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := parseMultipartContentType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			_, _ = part.Read(make([]byte, 64))
			_ = part.Close()
		}

		mw := multipart.NewWriter(w)
		w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
		w.WriteHeader(http.StatusOK)

		metaPart, _ := mw.CreatePart(map[string][]string{"Content-Type": {"application/json"}})
		// Metadata declares two documents, but only one part follows.
		meta := processMetadata{
			RequestID: "job-mismatch",
			Documents: []documentMetadata{
				{Index: 0, PageCount: 1, Filename: "a.pdf", ContentType: "application/pdf"},
				{Index: 1, PageCount: 1, Filename: "b.pdf", ContentType: "application/pdf"},
			},
			DurationMs: 1,
		}
		_ = json.NewEncoder(metaPart).Encode(meta)
		p, _ := mw.CreatePart(map[string][]string{"Content-Type": {"application/pdf"}})
		_, _ = p.Write([]byte("only-one"))
		_ = mw.Close()
	})

	sockPath := startFakeScanProcessor(t, handler)
	pagesDir := t.TempDir()
	page1 := writeTestPage(t, pagesDir, "page-1.tiff", "x")
	client := NewHTTPUnixClient(sockPath, t.TempDir(), 2*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	_, err := client.Process(context.Background(), testRequest("job-mismatch", page1))
	if err == nil {
		t.Fatal("expected error when metadata declares more documents than parts received")
	}
}

func TestPingHealthOK(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	handler.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	sockPath := startFakeScanProcessor(t, handler)
	client := NewHTTPUnixClient(sockPath, t.TempDir(), 2*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestPingHealthNon200(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	handler.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	sockPath := startFakeScanProcessor(t, handler)
	client := NewHTTPUnixClient(sockPath, t.TempDir(), 2*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(context.Background()); err == nil {
		t.Error("expected error from non-200 /health response")
	}
}

func TestPingSocketMissingReturnsError(t *testing.T) {
	t.Parallel()

	missingSock := filepath.Join(t.TempDir(), "does-not-exist.sock")
	client := NewHTTPUnixClient(missingSock, t.TempDir(), 2*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(context.Background()); err == nil {
		t.Error("expected error pinging a nonexistent socket")
	}
}
