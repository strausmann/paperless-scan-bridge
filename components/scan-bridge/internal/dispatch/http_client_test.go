package dispatch

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

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
)

// startFakeSaneRuntime brings up handler on a fresh Unix-domain socket
// and returns the socket path. It stands in for the sane-runtime
// container (Task 7) — the frozen contract (docs/superpowers implementation
// brief) means the two sides only need to agree on wire shape, not on
// sharing a Go module.
func startFakeSaneRuntime(t *testing.T, handler http.Handler) string {
	t.Helper()

	// Deliberately not t.TempDir(): its path embeds the (sub)test
	// name, which for table-driven subtests routinely exceeds the
	// ~104-108 byte sun_path limit the kernel enforces for
	// AF_UNIX addresses ("bind: invalid argument"). A short,
	// unrelated-to-the-test-name temp dir keeps the socket path
	// well under the limit.
	dir, err := os.MkdirTemp("", "sb-sock")
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

func testRequest(jobID string) Request {
	return Request{
		JobID: jobID,
		Profile: profiles.Profile{
			Name:           "receipts",
			Source:         "ADF",
			Resolution:     200,
			Mode:           profiles.ColorModeGray,
			Format:         profiles.FormatPDF,
			TimeoutSeconds: 30,
		},
	}
}

func TestDispatchHappyPathWritesFilesAndReturnsPaths(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	handler.HandleFunc("/scan", func(w http.ResponseWriter, r *http.Request) {
		var got scanRequestPayload
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("fake sane-runtime: decode request: %v", err)
		}
		if got.Format != "tiff" {
			t.Errorf("fake sane-runtime: format = %q, want tiff (always tiff per contract)", got.Format)
		}
		if got.Device != "" {
			t.Errorf("fake sane-runtime: device = %q, want empty", got.Device)
		}
		if got.Source != "ADF" {
			t.Errorf("fake sane-runtime: source = %q, want ADF", got.Source)
		}

		mw := multipart.NewWriter(w)
		w.Header().Set("Content-Type", mw.FormDataContentType())
		w.WriteHeader(http.StatusOK)

		metaPart, err := mw.CreatePart(map[string][]string{
			"Content-Type": {"application/json"},
		})
		if err != nil {
			t.Fatalf("fake sane-runtime: create metadata part: %v", err)
		}
		meta := scanMetadata{
			RequestID:  got.RequestID,
			PageCount:  2,
			DurationMs: 1234,
			Device:     "avision:libusb:001:002",
			Source:     "ADF",
			Resolution: 200,
			Mode:       "Gray",
		}
		if err := json.NewEncoder(metaPart).Encode(meta); err != nil {
			t.Fatalf("fake sane-runtime: encode metadata: %v", err)
		}

		for i := 0; i < 2; i++ {
			part, err := mw.CreatePart(map[string][]string{
				"Content-Type": {"image/tiff"},
			})
			if err != nil {
				t.Fatalf("fake sane-runtime: create page part: %v", err)
			}
			if _, err := part.Write([]byte("fake-tiff-bytes")); err != nil {
				t.Fatalf("fake sane-runtime: write page bytes: %v", err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatalf("fake sane-runtime: close multipart writer: %v", err)
		}
	})

	sockPath := startFakeSaneRuntime(t, handler)
	outputDir := t.TempDir()
	client := NewHTTPUnixClient(sockPath, outputDir, 5*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	resp, err := client.Dispatch(context.Background(), testRequest("job-123"))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if resp.JobID != "job-123" {
		t.Errorf("JobID = %q, want job-123", resp.JobID)
	}
	if resp.DurationMillis != 1234 {
		t.Errorf("DurationMillis = %d, want 1234", resp.DurationMillis)
	}
	if len(resp.Pages) != 2 {
		t.Fatalf("len(Pages) = %d, want 2", len(resp.Pages))
	}
	for _, p := range resp.Pages {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read page file %q: %v", p, err)
		}
		if string(data) != "fake-tiff-bytes" {
			t.Errorf("page file %q contents = %q, want fake-tiff-bytes", p, string(data))
		}
		if filepath.Dir(p) != filepath.Join(outputDir, "job-123") {
			t.Errorf("page file %q not under %s/job-123", p, outputDir)
		}
	}
}

func TestDispatchSaneRuntimeErrorEnvelopeMapsToSentinel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"no scanner detected", http.StatusServiceUnavailable, ErrNoScannerDetected},
		{"scanner busy", http.StatusConflict, ErrBusy},
		{"no documents", http.StatusUnprocessableEntity, ErrNoDocuments},
		{"sane-runtime timeout", http.StatusGatewayTimeout, ErrTimeout},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := http.NewServeMux()
			handler.HandleFunc("/scan", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "synthetic",
					"hint":  "injected by test",
				})
			})

			sockPath := startFakeSaneRuntime(t, handler)
			client := NewHTTPUnixClient(sockPath, t.TempDir(), 5*time.Second)
			t.Cleanup(func() { _ = client.Close() })

			_, err := client.Dispatch(context.Background(), testRequest("job-err"))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want wrapped %v", err, tc.want)
			}
		})
	}
}

func TestDispatchNetworkErrorSocketMissing(t *testing.T) {
	t.Parallel()

	missingSock := filepath.Join(t.TempDir(), "does-not-exist.sock")
	client := NewHTTPUnixClient(missingSock, t.TempDir(), 2*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	_, err := client.Dispatch(context.Background(), testRequest("job-net"))
	if err == nil {
		t.Fatal("expected error dispatching to a nonexistent socket")
	}
	// The interesting assertion is that we got here at all — a
	// dial failure must not panic anywhere in the response-reading
	// path.
}

func TestPingHealthOK(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	handler.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	sockPath := startFakeSaneRuntime(t, handler)
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
	sockPath := startFakeSaneRuntime(t, handler)
	client := NewHTTPUnixClient(sockPath, t.TempDir(), 2*time.Second)
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(context.Background()); err == nil {
		t.Error("expected error from non-200 /health response")
	}
}

// TestDispatchSendsProfileMaxPages is the regression guard for roadmap
// Epic A5. Every piece of the feeder cap already existed -- sane-runtime
// accepts max_pages and turns it into `scanimage --batch-count` -- and
// the only thing missing was this client passing the profile's value
// instead of a hardcoded 0. A refactor that reinstates that literal
// would not fail any other test: the scan still succeeds, it just
// silently drains the whole ADF for a profile that asked for one sheet.
func TestDispatchSendsProfileMaxPages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		maxPages int
	}{
		{"unset drains the feeder", 0},
		{"single sheet", 1},
		{"a bounded batch", 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := make(chan int, 1)
			handler := http.NewServeMux()
			handler.HandleFunc("/scan", func(w http.ResponseWriter, r *http.Request) {
				var payload scanRequestPayload
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("fake sane-runtime: decode request: %v", err)
				}
				got <- payload.MaxPages
				// The dispatch fails after this point (no multipart
				// body), which is fine: the assertion is about what
				// was sent, and an error return does not unsend it.
				w.WriteHeader(http.StatusInternalServerError)
			})

			sockPath := startFakeSaneRuntime(t, handler)
			client := NewHTTPUnixClient(sockPath, t.TempDir(), 5*time.Second)
			t.Cleanup(func() { _ = client.Close() })

			req := testRequest("job-max-pages")
			req.Profile.MaxPages = tc.maxPages
			_, _ = client.Dispatch(context.Background(), req)

			select {
			case sent := <-got:
				if sent != tc.maxPages {
					t.Fatalf("max_pages sent = %d, want %d", sent, tc.maxPages)
				}
			default:
				t.Fatal("fake sane-runtime never received a request")
			}
		})
	}
}
