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
	"strconv"
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

// defaultMaxRequestBytes is the --max-request-bytes flag's default.
// Mirrors internal/procapi's own defaultMaxRequestBytes (kept as a
// separate constant rather than exported from that package: main.go
// only needs the single int64 value to seed the flag's default, and a
// cross-package export for that alone would be more coupling than the
// value is worth). The two are documented to stay in sync by each
// package's own test asserting its constant against the same literal
// (TestDefaultMaxRequestBytes here, TestDefaultMaxRequestBytes in
// internal/procapi) rather than by a single cross-package test --
// see internal/procapi/api.go's defaultMaxRequestBytes doc comment
// for how the 512 MiB figure itself was derived (a real page at the
// repo's own default.yaml scan profile, not a hypothetical one).
const defaultMaxRequestBytes int64 = 512 << 20 // 512 MiB

// defaultReadTimeoutSeconds is the --read-timeout-seconds flag's
// default: how long net/http.Server.ReadTimeout allows for reading an
// entire inbound request (headers AND body), unlike
// ReadHeaderTimeout, which only bounds the header phase and lets a
// slow-client (or slow-loris-style) body hang the connection
// indefinitely (issue #47). 30s is generous for a Unix-domain-socket
// transfer of a handful of TIFF pages from the sibling scan-bridge
// container -- the pipeline's own processing budget
// (req.TimeoutSeconds, decoded from the control payload) is separate
// and typically much larger.
const defaultReadTimeoutSeconds = 30

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
	// Resolved (and, for the two numeric ones, validated) BEFORE the
	// flags are declared, since a flag.FlagSet needs its default
	// value up front. A malformed SCAN_PROCESSOR_MAX_REQUEST_BYTES /
	// SCAN_PROCESSOR_READ_TIMEOUT_SECONDS fails run() loudly here --
	// matching scan-bridge's internal/config.applyEnv's contract for
	// its own SCAN_BRIDGE_MAX_REQUEST_BYTES / SCAN_BRIDGE_READ_TIMEOUT_SECONDS
	// (a typo'd deployment env var is a configuration bug, not
	// something either daemon should silently paper over with a
	// default the operator never asked for) -- rather than the
	// silent-fallback behaviour an earlier version of this function
	// had, which was inconsistent between the two sibling daemons of
	// this same repo.
	maxRequestBytesDefault, err := envInt64OrErr("SCAN_PROCESSOR_MAX_REQUEST_BYTES", defaultMaxRequestBytes)
	if err != nil {
		return err
	}
	readTimeoutSecondsDefault, err := envIntOrErr("SCAN_PROCESSOR_READ_TIMEOUT_SECONDS", defaultReadTimeoutSeconds)
	if err != nil {
		return err
	}

	maxRequestBytes := fs.Int64("max-request-bytes", maxRequestBytesDefault,
		"maximum size in bytes of an inbound POST /process request body (http.MaxBytesReader)")
	readTimeoutSeconds := fs.Int("read-timeout-seconds", readTimeoutSecondsDefault,
		"maximum seconds net/http spends reading an inbound request (headers AND body) before aborting it")
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
		Logger:          logger,
		MaxRequestBytes: *maxRequestBytes,
	}
	httpServer := &http.Server{
		Handler:           apiServer.Router(),
		ReadHeaderTimeout: 5 * time.Second,
		// ReadTimeout bounds the ENTIRE request read (headers AND
		// body), unlike ReadHeaderTimeout above -- the fix for issue
		// #47's "kein Read-Timeout an den multipart-Legs" (a slow or
		// stalled client body can otherwise hang the connection
		// indefinitely; see decodeProcessRequest's io.ReadAll(part)
		// calls and its MaxBytesReader wrap for the size half of the
		// same hardening).
		ReadTimeout: time.Duration(*readTimeoutSeconds) * time.Second,
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

// envInt64OrErr mirrors envOr for an int64-valued env var (e.g.
// SCAN_PROCESSOR_MAX_REQUEST_BYTES): unset or empty returns fallback
// unchanged. Unlike envOr (a plain string pass-through, where
// "malformed" cannot occur), a SET-but-non-numeric value is an error,
// not a silent fallback -- matching
// internal/config.applyEnv's contract for scan-bridge's equivalent
// SCAN_BRIDGE_MAX_REQUEST_BYTES override (that sibling daemon's own
// numeric env vars fail Load() loudly on a typo; this one now does
// too, rather than the two daemons of the same repo disagreeing on
// what a malformed deployment env var means).
func envInt64OrErr(key string, fallback int64) (int64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", key, v, err)
	}
	return n, nil
}

// envIntOrErr is envInt64OrErr's int-valued counterpart (e.g.
// SCAN_PROCESSOR_READ_TIMEOUT_SECONDS).
func envIntOrErr(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", key, v, err)
	}
	return n, nil
}
