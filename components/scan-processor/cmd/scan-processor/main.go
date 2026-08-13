// Command scan-processor is the entry point for the scan-processor
// daemon.
//
// It serves the procapi HTTP surface (POST /process, GET /health)
// over a Unix-domain socket, mirroring
// components/sane-runtime/cmd/sane-runtime/main.go's shape for the
// same transport decision applied to this leg of the pipeline
// (design doc sec. 4.2, "Option A — HTTP over Unix socket"). The
// pipeline backing every request is pipeline.ExecPipeline, which
// shells out to convert(1)/tesseract(1)/qpdf(1).
//
// version, commit, and buildDate are populated at build time via
// -ldflags "-X main.version=... -X main.commit=... -X main.buildDate=...".
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-processor/internal/pipeline"
	"github.com/strausmann/paperless-scan-bridge/components/scan-processor/internal/procapi"
)

// Build identity, populated by ldflags. Defaults are useful in
// `go run` and `go test` contexts.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// defaultSocketPath is this component's socket path (design doc sec.
// 4.2: "/run/scan-processor/scan-processor.sock"), analogous to
// sane-runtime's ADR-0009-finalized /run/sane-runtime/sane.sock.
const defaultSocketPath = "/run/scan-processor/scan-processor.sock"

// gracefulShutdownTimeout bounds how long an in-flight /process
// request gets to finish before the listener is force-closed. Larger
// than sane-runtime's (30s): a multi-page OCR+assembly run can
// legitimately take longer than a single scanimage(1) invocation.
const gracefulShutdownTimeout = 60 * time.Second

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "scan-processor: "+err.Error())
		os.Exit(1)
	}
}

// run is the testable entry point. It reads the supplied args,
// honours --version, and otherwise blocks serving the Unix socket
// until a shutdown signal arrives or the listener fails.
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("scan-processor", flag.ContinueOnError)
	fs.SetOutput(stderr)

	socketPath := fs.String("socket", envOr("SCAN_PROCESSOR_SOCKET", defaultSocketPath),
		"path to the Unix-domain socket to serve the procapi HTTP surface on")
	convertBin := fs.String("convert-bin", os.Getenv("SCAN_PROCESSOR_CONVERT_BIN"),
		"override the convert(1) (ImageMagick) binary path; empty resolves via PATH")
	tesseractBin := fs.String("tesseract-bin", os.Getenv("SCAN_PROCESSOR_TESSERACT_BIN"),
		"override the tesseract(1) binary path; empty resolves via PATH")
	qpdfBin := fs.String("qpdf-bin", os.Getenv("SCAN_PROCESSOR_QPDF_BIN"),
		"override the qpdf(1) binary path; empty resolves via PATH")
	showVersion := fs.Bool("version", false, "print version information and exit")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		fmt.Fprintf(stdout, "scan-processor %s (commit %s, built %s)\n", version, commit, buildDate)
		return nil
	}

	logger := slog.New(slog.NewJSONHandler(stderr, nil)).With(
		slog.String("component", "scan-processor"),
		slog.String("version", version),
	)

	ln, err := newUnixListener(*socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer func() { _ = os.Remove(*socketPath) }()

	apiServer := &procapi.Server{
		Pipeline: &pipeline.ExecPipeline{
			ConvertBin:   *convertBin,
			TesseractBin: *tesseractBin,
			QpdfBin:      *qpdfBin,
			Logger:       logger,
		},
		Logger: logger,
	}
	httpServer := &http.Server{
		Handler:           apiServer.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	srvErrs := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("socket", *socketPath))
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErrs <- fmt.Errorf("serve: %w", err)
			return
		}
		srvErrs <- nil
	}()

	select {
	case err := <-srvErrs:
		if err != nil {
			logger.Error("listener exited unexpectedly", slog.Any("err", err))
			return err
		}
		return nil

	case sig := <-sigCh:
		logger.Info("shutdown signal received", slog.String("signal", sig.String()))
		return shutdown(logger, httpServer)
	}
}

// shutdown drains in-flight requests within gracefulShutdownTimeout,
// then force-closes the listener if that budget is exceeded.
func shutdown(logger *slog.Logger, srv *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown deadline exceeded; forcing close",
			slog.Any("err", err))
		if closeErr := srv.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			logger.Error("forced close failed", slog.Any("err", closeErr))
		}
		return fmt.Errorf("shutdown exceeded %s: %w", gracefulShutdownTimeout, err)
	}
	return nil
}

// newUnixListener removes any stale socket file left behind by an
// unclean previous exit, listens on path, and relaxes its
// permissions to group-writable so a sibling container in the same
// Compose network namespace (scan-bridge) can connect — mirrors
// sane-runtime's newUnixListener exactly (same rationale, same
// interim 0o660 default pending uid/gid coordination, see that
// function's doc comment).
//
// It also creates path's parent directory (mode 0o750) if it does
// not exist yet, for the same standalone-`docker run`/fresh-bind-mount
// reason sane-runtime's version documents.
func newUnixListener(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create socket parent dir for %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale socket %s: %w", path, err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("chmod %s: %w", path, err)
	}
	return ln, nil
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
