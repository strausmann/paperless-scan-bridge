// Package scanner defines the abstraction the sane-runtime HTTP service
// scans through: a Params/Page vocabulary and a Scanner interface that
// the real SANE-backed implementation (ExecScanner) and test fakes both
// satisfy. Keeping the interface here, decoupled from exec.Command,
// lets internal/scanapi be tested without any scanner hardware.
package scanner

import (
	"context"
	"errors"
	"io"
)

// Params describes one scan request, translated 1:1 from the scanapi
// JSON contract into scanimage CLI arguments by buildArgv.
type Params struct {
	Device         string
	Source         string
	Resolution     int
	Mode           string
	Format         string
	MaxPages       int
	TimeoutSeconds int
}

// Page is one scanned page. Data streams the page bytes; callers must
// Close it once done reading so ExecScanner can release its scratch
// directory.
type Page struct {
	Index int
	Data  io.ReadCloser
}

// Sentinel errors classify scan failures so scanapi can map them to
// the HTTP status/error-code table in the sane-runtime contract.
// Callers compare with errors.Is; ExecScanner wraps the underlying
// exec/stderr detail with fmt.Errorf("%w: ...") so context survives.
var (
	// ErrNoScannerDetected means ListDevices (or scanimage -L during
	// auto-select) found zero devices.
	ErrNoScannerDetected = errors.New("scanner: no device detected")
	// ErrNoDocuments means the ADF reported an empty feeder
	// (SANE_STATUS_NO_DOCS).
	ErrNoDocuments = errors.New("scanner: ADF reports no documents")
	// ErrDeviceError means the device reported a hardware condition
	// (cover open, paper jam, I/O error) distinct from "no documents".
	ErrDeviceError = errors.New("scanner: device reported an error")
	// ErrTimeout means the scan did not complete within the caller's
	// context deadline.
	ErrTimeout = errors.New("scanner: scan timed out")
)

// Scanner performs scans and enumerates attached devices. The
// production implementation (ExecScanner) shells out to scanimage;
// tests use in-memory fakes so the HTTP layer never needs hardware.
type Scanner interface {
	Scan(ctx context.Context, params Params) ([]Page, error)
	ListDevices(ctx context.Context) ([]string, error)
}
