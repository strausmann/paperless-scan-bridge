// Command scan-bridge is the entry point for the scan-bridge daemon.
//
// It wires together the config, profiles, api, and metrics packages,
// brings up two HTTP servers (public REST on Server.Listen and
// Prometheus metrics on Server.MetricsListen), and orchestrates the
// graceful-shutdown sequence specified in CONTAINER_SUITE.md sec. 4.11.
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
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/api"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/metrics"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
)

// Build identity, populated by ldflags. Defaults are useful in
// `go run` and `go test` contexts.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "scan-bridge: "+err.Error())
		os.Exit(1)
	}
}

// run is the testable entry point. It reads the supplied args,
// honours --version, builds the daemon, and blocks until shutdown.
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("scan-bridge", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "/etc/scan-bridge/config.toml",
		"path to the TOML configuration file")
	showVersion := fs.Bool("version", false,
		"print version information and exit")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		fmt.Fprintf(stdout, "scan-bridge %s (commit %s, built %s)\n",
			version, commit, buildDate)
		return nil
	}

	// If --config was not explicitly set and the default file does not
	// exist, hand an empty path to Load so it falls back to defaults +
	// env. An explicit --config that points at a missing file IS an
	// error, so we forward it to Load unchanged in that case and let
	// it fail loudly.
	loadPath := *configPath
	configExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configExplicit = true
		}
	})
	if !configExplicit {
		if _, statErr := os.Stat(loadPath); errors.Is(statErr, os.ErrNotExist) {
			loadPath = ""
		}
	}

	cfg, err := config.Load(loadPath, os.LookupEnv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger, err := newLogger(cfg.Logging, stderr)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	logger = logger.With(
		slog.String("component", "scan-bridge"),
		slog.String("version", version),
	)

	logger.Info("starting scan-bridge",
		slog.String("config_path", *configPath),
		slog.String("config_summary", cfg.Description()))

	profileSet, err := profiles.Load(cfg.Paths.Profiles)
	if err != nil {
		return fmt.Errorf("profiles: %w", err)
	}
	logger.Info("profiles loaded",
		slog.Int("count", profileSet.Len()),
		slog.Any("names", profileSet.Names()))

	metricsRegistry := prometheus.NewRegistry()
	collectors := metrics.New(version, commit, buildDate)
	if err := collectors.Register(metricsRegistry); err != nil {
		return fmt.Errorf("metrics: %w", err)
	}

	apiServer := &api.Server{
		Profiles: profileSet,
		Build: api.BuildInfo{
			Version:   version,
			Commit:    commit,
			BuildDate: buildDate,
		},
		Logger: logger,
	}

	publicSrv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           apiServer.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler(metricsRegistry))
	metricsSrv := &http.Server{
		Addr:              cfg.Server.MetricsListen,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	srvErrs := make(chan error, 2)
	go listenAndServe(srvErrs, publicSrv, "public", logger)
	go listenAndServe(srvErrs, metricsSrv, "metrics", logger)

	select {
	case err := <-srvErrs:
		logger.Error("listener exited unexpectedly", slog.Any("err", err))
		shutdownAll(logger, cfg.SIGTERMTimeout(), cfg.HardTimeout(),
			publicSrv, metricsSrv)
		return err

	case sig := <-sigCh:
		name, timeout := signalDetails(sig, &cfg)
		logger.Info("shutdown signal received",
			slog.String("signal", name),
			slog.Duration("graceful_timeout", timeout))
		shutdownAll(logger, timeout, cfg.HardTimeout(),
			publicSrv, metricsSrv)
		return nil
	}
}

func listenAndServe(out chan<- error, srv *http.Server, label string,
	logger *slog.Logger) {
	logger.Info("listener up",
		slog.String("listener", label),
		slog.String("addr", srv.Addr))
	if err := srv.ListenAndServe(); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		out <- fmt.Errorf("%s listener: %w", label, err)
	}
}

// shutdownAll runs http.Server.Shutdown on every listener with the
// supplied graceful timeout, then enforces the hard deadline by
// calling Close on whatever did not finish in time.
//
// TODO(phase 1.4): once the jobs and dispatch subsystems land, the
// shutdown sequence also needs to mark queued jobs as
// cancelled_at_shutdown, allow currently dispatched jobs to complete,
// flush metrics one last time, and close the BoltDB store. The hooks
// belong here.
func shutdownAll(logger *slog.Logger, graceful, hard time.Duration,
	servers ...*http.Server) {
	gracefulCtx, cancel := context.WithTimeout(context.Background(), graceful)
	defer cancel()

	// Run Shutdown on every server in parallel so each one gets the
	// full graceful budget; a slow public listener must not eat the
	// metrics listener's deadline.
	var wg sync.WaitGroup
	for _, srv := range servers {
		srv := srv
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := srv.Shutdown(gracefulCtx); err != nil {
				logger.Warn("graceful shutdown failed",
					slog.String("addr", srv.Addr),
					slog.Any("err", err))
			}
		}()
	}
	wg.Wait()

	if errors.Is(gracefulCtx.Err(), context.DeadlineExceeded) {
		logger.Error("graceful shutdown deadline exceeded; forcing close",
			slog.Duration("graceful_timeout", graceful),
			slog.Duration("hard_timeout", hard))
		for _, srv := range servers {
			_ = srv.Close()
		}
	}
}

func signalDetails(sig os.Signal, cfg *config.Config) (string, time.Duration) {
	if sig == syscall.SIGINT {
		return "SIGINT", cfg.SIGINTTimeout()
	}
	return "SIGTERM", cfg.SIGTERMTimeout()
}

func newLogger(cfg config.LoggingConfig, w io.Writer) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: level}
	switch cfg.Format {
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	case "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("unknown logging.format %q", cfg.Format)
	}
}

func parseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown logging.level %q", raw)
	}
}
