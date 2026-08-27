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
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/dispatch"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/metrics"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/procclient"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"

	// Destination modules register themselves via init() (ADR 0016,
	// destinations.Register) — main.go blank-imports only the modules
	// it wants compiled in (design doc
	// docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
	// sec. 5.1). v1 blank-imports paperless only.
	_ "github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations/paperless"
)

// dispatchClientTimeout bounds the whole HTTP round trip to
// sane-runtime as a client-side safety net. It is deliberately larger
// than any sane profile's timeout_seconds (internal/profiles caps
// resolution/etc. but not timeout_seconds) — the per-call deadline
// that actually governs a scan comes from the context handleScan
// derives from the profile (internal/api/scan.go), not from this
// value. Reused as procClientTimeout below for the scan-processor
// leg of the pipeline — same reasoning applies verbatim.
const dispatchClientTimeout = 5 * time.Minute

// secretsDir is the Docker secrets directory config.SecretResolver
// checks first (design doc sec. 5.3, matching the 2026-04-30 spec's
// documented convention). Not exposed as a config.PathsConfig field:
// it is a Docker/Compose deployment convention, not something an
// operator has a reason to override per-instance the way
// SaneSocket/ScanProcessorSocket are.
const secretsDir = "/run/secrets"

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
	// `healthcheck` as a bare subcommand, checked before flag parsing
	// because flag stops at the first non-flag argument anyway.
	//
	// This exists for the container healthcheck. The image is
	// distroless: no shell, no curl, no wget, so `test: ["CMD", "curl",
	// ...]` cannot work. Before this, deploy/compose used
	// `["CMD", "/scan-bridge", "healthcheck"]` and the argument was
	// simply ignored -- every probe started a SECOND daemon inside the
	// container, which then failed to bind the port. Caught by running
	// the image rather than by reading it.
	if len(args) > 0 && args[0] == "healthcheck" {
		return runHealthcheck(args[1:], stdout)
	}

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
		_, _ = fmt.Fprintf(stdout, "scan-bridge %s (commit %s, built %s)\n",
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

	// Both Close calls only release idle keep-alive connections on the
	// way out of a process that is exiting anyway, so their errors are
	// dropped -- but explicitly, so the linter's point stands and a
	// future Close that does matter is not silently swallowed with it.
	dispatchClient := dispatch.NewHTTPUnixClient(cfg.Paths.SaneSocket, cfg.Paths.OutputDir, dispatchClientTimeout)
	defer func() { _ = dispatchClient.Close() }()

	procClient := procclient.NewHTTPUnixClient(cfg.Paths.ScanProcessorSocket, cfg.Paths.OutputDir, dispatchClientTimeout)
	defer func() { _ = procClient.Close() }()

	secrets := config.NewSecretResolver(secretsDir, os.LookupEnv)

	// The bearer token, from the same secret store everything else uses.
	//
	// Without this the published compose stack mounted a bridge_token
	// secret that nothing read, and authentication fell back to
	// whatever token_hash happened to be in config.toml -- which in the
	// shipped example is a throwaway value committed to a public
	// repository. Anyone who can read the repo could authenticate.
	//
	// A value in config.toml still wins: an operator who set one there
	// meant it, and silently overriding it from a file they may not
	// know about would be worse than not looking. Absence of the secret
	// is not an error either -- ip_allowlist mode has no token at all.
	if cfg.Auth.TokenHash == "" {
		if plaintext, err := secrets.Resolve("bridge_token"); err == nil && plaintext != "" {
			sum := sha256.Sum256([]byte(plaintext))
			cfg.Auth.TokenHash = hex.EncodeToString(sum[:])
			logger.Info("bearer token loaded from the bridge_token secret",
				"component", "scan-bridge")
		}
	}

	apiServer := &api.Server{
		Profiles: profileSet,
		Build: api.BuildInfo{
			Version:   version,
			Commit:    commit,
			BuildDate: buildDate,
		},
		Logger:          logger,
		Auth:            cfg.Auth,
		Dispatch:        dispatchClient,
		ProcClient:      procClient,
		Secrets:         secrets,
		OutputDir:       cfg.Paths.OutputDir,
		KeepScanOutput:  cfg.Paths.KeepScanOutput,
		MaxRequestBytes: cfg.Server.MaxRequestBytes,
	}

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler(metricsRegistry))

	publicSrv, metricsSrv := newHTTPServers(cfg, apiServer.Router(), metricsMux)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	srvErrs := make(chan error, 2)
	go listenAndServe(srvErrs, publicSrv, "public", logger)
	go listenAndServe(srvErrs, metricsSrv, "metrics", logger)

	select {
	case err := <-srvErrs:
		logger.Error("listener exited unexpectedly", slog.Any("err", err))
		if shutdownErr := shutdownAll(logger,
			cfg.SIGTERMTimeout(), cfg.HardTimeout(),
			publicSrv, metricsSrv); shutdownErr != nil {
			return errors.Join(err, shutdownErr)
		}
		return err

	case sig := <-sigCh:
		name, timeout := signalDetails(sig, &cfg)
		logger.Info("shutdown signal received",
			slog.String("signal", name),
			slog.Duration("graceful_timeout", timeout))
		// shutdownAll returns non-nil only when the hard deadline is
		// breached; per CONTAINER_SUITE.md sec. 4.11 that is exit 1.
		return shutdownAll(logger, timeout, cfg.HardTimeout(),
			publicSrv, metricsSrv)
	}
}

// newHTTPServers builds the public REST and metrics *http.Server
// values from cfg, without starting either listener. Split out of
// run() so main_test.go can assert the ReadTimeout wiring (issue #47:
// a Read-Timeout that bounds the ENTIRE request — headers AND body —
// unlike ReadHeaderTimeout, which only bounds the header phase and
// lets a slow-body client hang a connection indefinitely) without
// needing a real network listener.
//
// Both servers share cfg.Server.ReadTimeoutSeconds: the metrics
// listener has no user-facing body to speak of, but giving it the
// same bound costs nothing and keeps this function's contract simple
// (one config value, one timeout, both listeners) rather than adding
// a second, metrics-only knob nobody has asked for.
func newHTTPServers(cfg config.Config, publicHandler, metricsHandler http.Handler) (public, metricsSrv *http.Server) {
	readTimeout := time.Duration(cfg.Server.ReadTimeoutSeconds) * time.Second
	public = &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           publicHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       readTimeout,
	}
	metricsSrv = &http.Server{
		Addr:              cfg.Server.MetricsListen,
		Handler:           metricsHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       readTimeout,
	}
	return public, metricsSrv
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
// supplied graceful timeout. If shutdown does not complete within
// `hard`, it forces every listener closed and returns a non-nil error
// so main can propagate exit code 1.
//
// CONTAINER_SUITE.md sec. 4.11 specifies "If shutdown takes longer
// than 60 seconds, log an error and exit 1"; the dual-deadline shape
// implements that contract: graceful first, then a hard window for
// in-flight requests to drain, then forced close.
//
// TODO(phase 1.4): once the jobs and dispatch subsystems land, the
// shutdown sequence also needs to mark queued jobs as
// cancelled_at_shutdown, allow currently dispatched jobs to complete,
// flush metrics one last time, and close the BoltDB store. The hooks
// belong here.
func shutdownAll(logger *slog.Logger, graceful, hard time.Duration,
	servers ...*http.Server) error {
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

	if !errors.Is(gracefulCtx.Err(), context.DeadlineExceeded) {
		return nil
	}

	// Graceful budget exhausted. Open the second deadline window —
	// the hard timeout, less the graceful seconds we already spent —
	// and let any straggler in-flight request drain before we force
	// the connection closed.
	remainder := hard - graceful
	if remainder <= 0 {
		remainder = time.Second
	}
	logger.Error("graceful shutdown deadline exceeded; entering hard window",
		slog.Duration("graceful_timeout", graceful),
		slog.Duration("hard_remaining", remainder))

	hardCtx, hardCancel := context.WithTimeout(context.Background(), remainder)
	defer hardCancel()

	wg = sync.WaitGroup{}
	for _, srv := range servers {
		srv := srv
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := srv.Shutdown(hardCtx); err != nil &&
				!errors.Is(err, context.Canceled) {
				logger.Warn("hard shutdown failed",
					slog.String("addr", srv.Addr),
					slog.Any("err", err))
			}
		}()
	}
	wg.Wait()

	// Force-close anything still alive. Any error here means the
	// kernel kept a socket around; loud-log it so operators see why
	// the process exited 1.
	for _, srv := range servers {
		if err := srv.Close(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			logger.Error("forced close failed",
				slog.String("addr", srv.Addr),
				slog.Any("err", err))
		}
	}

	return fmt.Errorf("shutdown exceeded hard deadline of %s", hard)
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
