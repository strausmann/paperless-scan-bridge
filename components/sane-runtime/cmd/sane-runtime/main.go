// Command sane-runtime is the entry point for the sane-runtime daemon.
//
// It serves the scanapi HTTP surface (POST /scan, GET /health,
// GET /ready) over a Unix-domain socket, per ADR 0009
// (scan-bridge <-> sane-runtime communicate over HTTP on a Unix
// socket, not TCP). The scanner backing every request is
// scanner.ExecScanner, which shells out to scanimage(1).
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
	"syscall"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/sane-runtime/internal/scanapi"
	"github.com/strausmann/paperless-scan-bridge/components/sane-runtime/internal/scanner"
)

// Build identity, populated by ldflags. Defaults are useful in
// `go run` and `go test` contexts.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// defaultSocketPath matches config.Paths.SaneSocket on the scan-bridge
// side and ADR 0009's finalized path (Task 7 brief D3).
const defaultSocketPath = "/run/sane-runtime/sane.sock"

// gracefulShutdownTimeout bounds how long in-flight requests (in
// practice: at most one, since /scan is single-flight) get to finish
// before the listener is force-closed.
const gracefulShutdownTimeout = 30 * time.Second

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "sane-runtime: "+err.Error())
		os.Exit(1)
	}
}

// run is the testable entry point. It reads the supplied args,
// honours --version, and otherwise blocks serving the Unix socket
// until a shutdown signal arrives or the listener fails.
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sane-runtime", flag.ContinueOnError)
	fs.SetOutput(stderr)

	socketPath := fs.String("socket", envOr("SANE_RUNTIME_SOCKET", defaultSocketPath),
		"path to the Unix-domain socket to serve the scanapi HTTP surface on")
	showVersion := fs.Bool("version", false, "print version information and exit")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		fmt.Fprintf(stdout, "sane-runtime %s (commit %s, built %s)\n", version, commit, buildDate)
		return nil
	}

	logger := slog.New(slog.NewJSONHandler(stderr, nil)).With(
		slog.String("component", "sane-runtime"),
		slog.String("version", version),
	)

	ln, err := newUnixListener(*socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer func() { _ = os.Remove(*socketPath) }()

	apiServer := &scanapi.Server{
		Scanner: &scanner.ExecScanner{Logger: logger},
		Logger:  logger,
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
// unclean previous exit, listens on path, and relaxes its permissions
// to group-writable so a sibling container in the same Compose
// network namespace (scan-bridge) can connect. uid/gid coordination
// between the two containers is finalized in Task 13/15 (brief D3);
// 0o660 is the interim, least-surprising default.
func newUnixListener(path string) (net.Listener, error) {
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
