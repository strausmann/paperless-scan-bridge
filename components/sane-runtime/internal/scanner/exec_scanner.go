package scanner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
)

// defaultFormat is used when Params.Format is empty. tiff is the
// project's standard capture format (see docs/decisions — capture is
// always lossless; the profile's requested output Format is a later
// scan-processor concern, not sane-runtime's).
const defaultFormat = "tiff"

// ExecScanner is the production Scanner implementation: it shells out
// to scanimage(1) per SANE conventions.
type ExecScanner struct {
	// BinPath overrides the scanimage binary to exec. Empty means
	// resolve "scanimage" via exec.LookPath at call time (the
	// production default); tests point this at a fixture script so
	// the whole suite runs without scanner hardware.
	BinPath string
	// Logger receives diagnostics such as "multiple devices detected,
	// using the first". A nil Logger is safe — it falls back to
	// slog.Default().
	Logger *slog.Logger
}

func (e *ExecScanner) logger() *slog.Logger {
	if e.Logger != nil {
		return e.Logger
	}
	return slog.Default()
}

func (e *ExecScanner) resolveBin() (string, error) {
	if e.BinPath != "" {
		return e.BinPath, nil
	}
	bin, err := exec.LookPath("scanimage")
	if err != nil {
		return "", fmt.Errorf("scanner: scanimage not found on PATH: %w", err)
	}
	return bin, nil
}

// Scan resolves the target device (auto-selecting when Params.Device
// is empty), runs scanimage into a fresh scratch directory, and
// returns the resulting pages in scan order. The caller must Close
// every returned Page.Data; the backing scratch directory is removed
// once all pages have been closed.
func (e *ExecScanner) Scan(ctx context.Context, params Params) ([]Page, error) {
	bin, err := e.resolveBin()
	if err != nil {
		return nil, err
	}

	device := params.Device
	if device == "" {
		devices, err := e.listDevicesWithBin(ctx, bin)
		if err != nil {
			return nil, err
		}
		switch len(devices) {
		case 0:
			return nil, ErrNoScannerDetected
		case 1:
			device = devices[0]
		default:
			device = devices[0]
			e.logger().Warn("multiple scanners detected; using the first",
				slog.Int("count", len(devices)),
				slog.String("selected", device))
		}
	}

	format := params.Format
	if format == "" {
		format = defaultFormat
	}

	scratch, err := os.MkdirTemp("", "sane-runtime-scan-*")
	if err != nil {
		return nil, fmt.Errorf("scanner: create scratch dir: %w", err)
	}

	resolved := params
	resolved.Device = device
	resolved.Format = format
	outputTemplate := filepath.Join(scratch, "page-%d."+format)
	argv := buildArgv(resolved, outputTemplate)

	cmd := exec.CommandContext(ctx, bin, argv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		_ = os.RemoveAll(scratch)
		return nil, classifyExecError(ctx, runErr, stderr.String())
	}

	pages, err := collectPages(scratch, format)
	if err != nil {
		_ = os.RemoveAll(scratch)
		return nil, fmt.Errorf("scanner: collect pages: %w", err)
	}
	if len(pages) == 0 {
		_ = os.RemoveAll(scratch)
		return nil, ErrNoDocuments
	}
	return pages, nil
}

// ListDevices runs `scanimage -L` and parses the device identifiers
// out of its stdout.
func (e *ExecScanner) ListDevices(ctx context.Context) ([]string, error) {
	bin, err := e.resolveBin()
	if err != nil {
		return nil, err
	}
	return e.listDevicesWithBin(ctx, bin)
}

func (e *ExecScanner) listDevicesWithBin(ctx context.Context, bin string) ([]string, error) {
	cmd := exec.CommandContext(ctx, bin, "-L")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("scanner: scanimage -L failed: %w (stderr: %s)",
			err, strings.TrimSpace(stderr.String()))
	}
	return parseScanimageList(stdout.String()), nil
}

// classifyExecError maps a failed scanimage invocation onto the
// sentinel errors scanapi's HTTP layer understands. ctx is checked
// first and independently of the process's own error text: when the
// context deadline caused the kill, that is authoritative regardless
// of what os/exec happens to report for the killed process.
func classifyExecError(ctx context.Context, runErr error, stderr string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	trimmed := strings.TrimSpace(stderr)
	switch {
	case strings.Contains(stderr, "Error during device I/O"):
		return fmt.Errorf("%w: %s", ErrDeviceError, trimmed)
	case strings.Contains(stderr, "cover") && strings.Contains(stderr, "open"):
		return fmt.Errorf("%w: %s", ErrDeviceError, trimmed)
	case strings.Contains(stderr, "jam"):
		return fmt.Errorf("%w: %s", ErrDeviceError, trimmed)
	case strings.Contains(stderr, "Document feeder out of documents"),
		strings.Contains(stderr, "no documents"),
		strings.Contains(stderr, "No documents"):
		return fmt.Errorf("%w: %s", ErrNoDocuments, trimmed)
	default:
		return fmt.Errorf("scanner: scanimage failed: %w (stderr: %s)", runErr, trimmed)
	}
}

// pageFilePattern matches the scratch filenames buildArgv's --batch
// template produces: page-<index>.<format>.
var pageFilePattern = regexp.MustCompile(`^page-(\d+)\.`)

// collectPages globs the scratch directory for pages written by
// scanimage, sorts them numerically by index (not lexically — page-10
// must sort after page-9), and opens each as a cleanup-aware
// io.ReadCloser.
func collectPages(scratch, format string) ([]Page, error) {
	matches, err := filepath.Glob(filepath.Join(scratch, "page-*."+format))
	if err != nil {
		return nil, err
	}

	type indexedPath struct {
		index int
		path  string
	}
	entries := make([]indexedPath, 0, len(matches))
	for _, m := range matches {
		sub := pageFilePattern.FindStringSubmatch(filepath.Base(m))
		if sub == nil {
			continue
		}
		idx, err := strconv.Atoi(sub[1])
		if err != nil {
			continue
		}
		entries = append(entries, indexedPath{index: idx, path: m})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].index < entries[j].index })

	cleanup := &scratchCleanup{dir: scratch}
	cleanup.remaining.Store(int32(len(entries)))

	pages := make([]Page, 0, len(entries))
	for _, entry := range entries {
		f, err := os.Open(entry.path)
		if err != nil {
			for _, p := range pages {
				_ = p.Data.Close()
			}
			return nil, fmt.Errorf("open %s: %w", entry.path, err)
		}
		pages = append(pages, Page{
			Index: entry.index,
			Data:  &pageReadCloser{file: f, cleanup: cleanup},
		})
	}
	return pages, nil
}

// scratchCleanup coordinates removal of a scan's scratch directory
// once every Page opened from it has been closed. remaining starts at
// the page count and is decremented by each pageReadCloser.Close.
type scratchCleanup struct {
	dir       string
	remaining atomic.Int32
}

func (c *scratchCleanup) release() {
	if c.remaining.Add(-1) == 0 {
		_ = os.RemoveAll(c.dir)
	}
}

// pageReadCloser wraps the scratch file for one page. Close removes
// the individual file immediately and, once every page has been
// closed, the now-empty scratch directory.
type pageReadCloser struct {
	file    *os.File
	cleanup *scratchCleanup
}

func (p *pageReadCloser) Read(b []byte) (int, error) {
	return p.file.Read(b)
}

func (p *pageReadCloser) Close() error {
	err := p.file.Close()
	_ = os.Remove(p.file.Name())
	p.cleanup.release()
	return err
}
