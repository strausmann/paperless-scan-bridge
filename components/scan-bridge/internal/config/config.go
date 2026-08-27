// Package config loads and validates the scan-bridge daemon
// configuration. The precedence order, lowest to highest, is:
//
//  1. Compiled-in defaults (Default()).
//  2. A TOML file on disk (typically /etc/scan-bridge/config.toml).
//  3. Environment variables prefixed SCAN_BRIDGE_.
//  4. Command-line flags applied by the caller after Load returns.
//
// See CONTAINER_SUITE.md sec. 4.10 for the canonical specification.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/firmware"
)

// AuthMode enumerates the supported authentication strategies.
type AuthMode string

const (
	AuthModeToken       AuthMode = "token"
	AuthModeIPAllowlist AuthMode = "ip_allowlist"
)

// Config is the full configuration tree for scan-bridge.
type Config struct {
	Server   ServerConfig   `toml:"server"`
	Auth     AuthConfig     `toml:"auth"`
	Paths    PathsConfig    `toml:"paths"`
	Logging  LoggingConfig  `toml:"logging"`
	Shutdown ShutdownConfig `toml:"shutdown"`
	Firmware FirmwareConfig `toml:"firmware"`
}

// FirmwareConfig configures the panel-firmware mirror (internal/
// firmware, ADR 0024, issue #111): the bridge fetches the panel's
// firmware from GitHub Releases and serves it over plain HTTP on the
// LAN, because the ESP32 has no heap left for a TLS session and cannot
// reach GitHub itself.
type FirmwareConfig struct {
	// Enabled turns the mirror and its /firmware routes on. Default
	// true: a deployment that runs the panel is the normal case, and a
	// bridge with no panel pays one API call every five hours.
	Enabled bool `toml:"enabled"`
	// CacheDir holds one directory per mirrored release plus the
	// state file. Under StateDir on purpose — unlike scan output
	// (which is tmpfs, see deploy/compose/scan-bridge.yml) this has to
	// survive a restart, or every reboot re-downloads ~1.7 MB and
	// serves 503 until it finishes.
	CacheDir string `toml:"cache_dir"`
	// Repo is the owner/name the releases come from.
	Repo string `toml:"repo"`
	// APIBase is GitHub's API root. Overridable so tests, and an
	// air-gapped mirror, can point somewhere else.
	APIBase string `toml:"api_base"`
	// RefreshIntervalSeconds is how often the mirror asks GitHub.
	// Shorter than the panel's own 6h check on purpose: the bridge
	// should know before the panel asks.
	RefreshIntervalSeconds int `toml:"refresh_interval_seconds"`
}

// ServerConfig controls the public REST API and the metrics endpoint.
type ServerConfig struct {
	Listen        string `toml:"listen"`
	MetricsListen string `toml:"metrics_listen"`
	// MaxRequestBytes bounds the size of an inbound POST /scan request
	// body via http.MaxBytesReader (internal/api's handleScan, issue
	// #47 hardening). scanRequest carries no file bytes -- unlike
	// scan-processor's POST /process, this is JSON only (a profile
	// name plus a caller-supplied tag_ids list) -- so the default is
	// sized generously above any legitimate payload rather than around
	// file uploads.
	MaxRequestBytes int64 `toml:"max_request_bytes"`
	// ReadTimeoutSeconds bounds http.Server.ReadTimeout: the total
	// time net/http spends reading an inbound request, headers AND
	// body. Deliberately distinct from ReadHeaderTimeout (main.go,
	// hardcoded 5s), which only bounds the header phase and lets a
	// slow-body client hang a connection indefinitely (issue #47).
	ReadTimeoutSeconds int `toml:"read_timeout_seconds"`
}

// AuthConfig describes how inbound requests are authenticated.
//
// In token mode the daemon expects a bearer token whose SHA-256 hex
// digest matches TokenHash. In ip_allowlist mode the daemon accepts
// unauthenticated requests whose source IP falls into AllowedCIDRs.
type AuthConfig struct {
	Mode         AuthMode `toml:"mode"`
	TokenHash    string   `toml:"token_hash"`
	AllowedCIDRs []string `toml:"allowed_cidrs"`
	parsedCIDRs  []*net.IPNet
}

// PathsConfig collects the on-disk locations the daemon reads or writes.
type PathsConfig struct {
	Profiles   string `toml:"profiles"`
	StateDir   string `toml:"state_dir"`
	SaneSocket string `toml:"sane_socket"`
	// OutputDir is where the dispatch client (internal/dispatch) writes
	// the page images it reads out of a sane-runtime multipart
	// response, one subdirectory per scan_id. Deliberately distinct
	// from StateDir: StateDir is daemon bookkeeping (Phase 1.4 job
	// store), OutputDir is scan output that scan-processor consumes
	// downstream. It is also where internal/procclient writes the
	// assembled documents scan-processor returns (design doc
	// docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
	// sec. 4.2/9 Task 5/7) -- both dispatch and procclient key their
	// subdirectory off the same scan_id, so a job's raw pages and its
	// processed output live side by side under OutputDir/<scan_id>/.
	OutputDir string `toml:"output_dir"`
	// ScanProcessorSocket is the Unix-domain socket path scan-bridge
	// dials to reach scan-processor (internal/procclient, design doc
	// sec. 4.2/9 Task 5/7) -- mirrors SaneSocket's role for the
	// sane-runtime leg of the pipeline.
	ScanProcessorSocket string `toml:"scan_processor_socket"`
	// KeepScanOutput disables handleScan's post-request cleanup of
	// OutputDir/<scan_id> (issue #49 point 1). false (the default)
	// means the raw scanned pages and assembled documents there --
	// receipts/invoices, i.e. PII -- are removed after every /scan
	// request, successful or not, rather than accumulating on disk
	// unbounded. true opts out, for local debugging.
	KeepScanOutput bool `toml:"keep_scan_output"`
}

// LoggingConfig configures the slog handler.
type LoggingConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

// ShutdownConfig holds the graceful-shutdown timing bounds described
// in CONTAINER_SUITE.md sec. 4.11.
type ShutdownConfig struct {
	SIGTERMTimeoutSeconds int `toml:"sigterm_timeout_seconds"`
	SIGINTTimeoutSeconds  int `toml:"sigint_timeout_seconds"`
	HardTimeoutSeconds    int `toml:"hard_timeout_seconds"`
}

// DefaultMaxRequestBytes is ServerConfig.MaxRequestBytes's compiled-in
// default (issue #47). 1 MiB is generous for scanRequest's small JSON
// shape while still bounding an attacker's ability to make the daemon
// buffer an unbounded body via handleScan's json.Decoder.
const DefaultMaxRequestBytes int64 = 1 << 20 // 1 MiB

// DefaultReadTimeoutSeconds is ServerConfig.ReadTimeoutSeconds's
// compiled-in default (issue #47).
const DefaultReadTimeoutSeconds = 30

// Default returns a Config populated with the compiled-in defaults.
// These are designed to be production-safe out of the box for the
// reference Pi deployment.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Listen:             ":8080",
			MetricsListen:      ":9090",
			MaxRequestBytes:    DefaultMaxRequestBytes,
			ReadTimeoutSeconds: DefaultReadTimeoutSeconds,
		},
		Auth: AuthConfig{
			Mode: AuthModeToken,
		},
		Paths: PathsConfig{
			Profiles:            "/etc/scan-bridge/profiles.yaml",
			StateDir:            "/var/lib/scan-bridge",
			SaneSocket:          "/run/sane-runtime/sane.sock",
			OutputDir:           "/var/lib/scan-bridge/scans",
			ScanProcessorSocket: "/run/scan-processor/scan-processor.sock",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Shutdown: ShutdownConfig{
			SIGTERMTimeoutSeconds: 30,
			SIGINTTimeoutSeconds:  5,
			HardTimeoutSeconds:    60,
		},
		Firmware: FirmwareConfig{
			Enabled:                true,
			CacheDir:               "/var/lib/scan-bridge/firmware",
			Repo:                   firmware.DefaultRepo,
			APIBase:                firmware.DefaultAPIBase,
			RefreshIntervalSeconds: int(firmware.DefaultRefreshInterval / time.Second),
		},
	}
}

// Load applies the loading precedence to produce a validated Config.
//
// path may be empty, in which case only defaults and the environment
// are consulted. When path is non-empty the file must exist —
// silently falling back to defaults makes typos in --config or env
// overrides effectively undebuggable. main.go decides whether the
// default config path is "expected" (and passes "" if not).
//
// The environment is read from osLookupEnv (typically os.LookupEnv);
// the indirection exists for tests.
func Load(path string, osLookupEnv func(string) (string, bool)) (Config, error) {
	if osLookupEnv == nil {
		osLookupEnv = os.LookupEnv
	}

	cfg := Default()

	if path != "" {
		meta, err := toml.DecodeFile(path, &cfg)
		if err != nil {
			return Config{}, fmt.Errorf("decode config %q: %w", path, err)
		}
		if undecoded := meta.Undecoded(); len(undecoded) > 0 {
			keys := make([]string, 0, len(undecoded))
			for _, k := range undecoded {
				keys = append(keys, k.String())
			}
			return Config{}, fmt.Errorf(
				"config %q has unknown keys: %s",
				path, strings.Join(keys, ", "))
		}
	}

	if err := applyEnv(&cfg, osLookupEnv); err != nil {
		return Config{}, fmt.Errorf("apply environment: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// applyEnv overlays SCAN_BRIDGE_*-prefixed environment variables onto
// cfg (see the package doc comment's precedence order). It returns an
// error rather than silently ignoring a malformed numeric override
// (SCAN_BRIDGE_MAX_REQUEST_BYTES / SCAN_BRIDGE_READ_TIMEOUT_SECONDS /
// SCAN_BRIDGE_KEEP_SCAN_OUTPUT below) — a typo'd deployment env var is
// a configuration bug that should fail loudly at startup (Load
// already does this for a bad --config path and a bad TOML file),
// not silently fall back to a default the operator never asked for.
func applyEnv(cfg *Config, look func(string) (string, bool)) error {
	if v, ok := look("SCAN_BRIDGE_LISTEN"); ok {
		cfg.Server.Listen = v
	}
	if v, ok := look("SCAN_BRIDGE_METRICS_LISTEN"); ok {
		cfg.Server.MetricsListen = v
	}
	if v, ok := look("SCAN_BRIDGE_MAX_REQUEST_BYTES"); ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("SCAN_BRIDGE_MAX_REQUEST_BYTES %q: %w", v, err)
		}
		cfg.Server.MaxRequestBytes = n
	}
	if v, ok := look("SCAN_BRIDGE_READ_TIMEOUT_SECONDS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("SCAN_BRIDGE_READ_TIMEOUT_SECONDS %q: %w", v, err)
		}
		cfg.Server.ReadTimeoutSeconds = n
	}
	if v, ok := look("SCAN_BRIDGE_AUTH_MODE"); ok {
		cfg.Auth.Mode = AuthMode(v)
	}
	// SCAN_BRIDGE_API_TOKEN carries the plaintext token per
	// CONTAINER_SUITE.md sec. 4.5; we hash it on load and never
	// retain the plaintext on the Config struct.
	if v, ok := look("SCAN_BRIDGE_API_TOKEN"); ok && v != "" {
		sum := sha256.Sum256([]byte(v))
		cfg.Auth.TokenHash = hex.EncodeToString(sum[:])
	}
	if v, ok := look("SCAN_BRIDGE_PROFILES_PATH"); ok {
		cfg.Paths.Profiles = v
	}
	if v, ok := look("SCAN_BRIDGE_STATE_DIR"); ok {
		cfg.Paths.StateDir = v
	}
	if v, ok := look("SCAN_BRIDGE_SANE_SOCKET"); ok {
		cfg.Paths.SaneSocket = v
	}
	if v, ok := look("SCAN_BRIDGE_OUTPUT_DIR"); ok {
		cfg.Paths.OutputDir = v
	}
	if v, ok := look("SCAN_BRIDGE_SCAN_PROCESSOR_SOCKET"); ok {
		cfg.Paths.ScanProcessorSocket = v
	}
	if v, ok := look("SCAN_BRIDGE_KEEP_SCAN_OUTPUT"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("SCAN_BRIDGE_KEEP_SCAN_OUTPUT %q: %w", v, err)
		}
		cfg.Paths.KeepScanOutput = b
	}
	if v, ok := look("SCAN_BRIDGE_FIRMWARE_ENABLED"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("SCAN_BRIDGE_FIRMWARE_ENABLED %q: %w", v, err)
		}
		cfg.Firmware.Enabled = b
	}
	if v, ok := look("SCAN_BRIDGE_FIRMWARE_CACHE_DIR"); ok {
		cfg.Firmware.CacheDir = v
	}
	if v, ok := look("SCAN_BRIDGE_FIRMWARE_REPO"); ok {
		cfg.Firmware.Repo = v
	}
	if v, ok := look("SCAN_BRIDGE_FIRMWARE_API_BASE"); ok {
		cfg.Firmware.APIBase = v
	}
	if v, ok := look("SCAN_BRIDGE_FIRMWARE_REFRESH_INTERVAL_SECONDS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("SCAN_BRIDGE_FIRMWARE_REFRESH_INTERVAL_SECONDS %q: %w", v, err)
		}
		cfg.Firmware.RefreshIntervalSeconds = n
	}
	if v, ok := look("SCAN_BRIDGE_LOG_LEVEL"); ok {
		cfg.Logging.Level = v
	}
	if v, ok := look("SCAN_BRIDGE_LOG_FORMAT"); ok {
		cfg.Logging.Format = v
	}
	return nil
}

// Validate checks invariants that the loader cannot enforce
// structurally. It is exported so flag-driven overrides applied by the
// caller can be re-validated.
func (c *Config) Validate() error {
	switch c.Auth.Mode {
	case AuthModeToken, AuthModeIPAllowlist:
	case "":
		return errors.New("auth.mode is required")
	default:
		return fmt.Errorf("auth.mode %q: must be %q or %q",
			c.Auth.Mode, AuthModeToken, AuthModeIPAllowlist)
	}

	if c.Auth.Mode == AuthModeIPAllowlist && len(c.Auth.AllowedCIDRs) == 0 {
		return errors.New("auth.allowed_cidrs must be non-empty when auth.mode = ip_allowlist")
	}

	// TODO(phase 1.4): once the auth middleware actually consumes
	// TokenHash, Validate must also reject auth.mode = token with an
	// empty TokenHash. We do not enforce it yet because Phase 1.1
	// ships the daemon without active authentication, and a
	// non-empty hash requirement here would block local development
	// and CI smoke runs that do not need auth.

	parsed := make([]*net.IPNet, 0, len(c.Auth.AllowedCIDRs))
	for _, raw := range c.Auth.AllowedCIDRs {
		_, n, err := net.ParseCIDR(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("auth.allowed_cidrs %q: %w", raw, err)
		}
		parsed = append(parsed, n)
	}
	c.Auth.parsedCIDRs = parsed

	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("logging.level %q: must be debug, info, warn, or error",
			c.Logging.Level)
	}

	switch c.Logging.Format {
	case "json", "text":
	default:
		return fmt.Errorf("logging.format %q: must be json or text",
			c.Logging.Format)
	}

	if c.Shutdown.SIGTERMTimeoutSeconds <= 0 ||
		c.Shutdown.SIGINTTimeoutSeconds <= 0 ||
		c.Shutdown.HardTimeoutSeconds <= 0 {
		return errors.New("shutdown timeouts must be positive")
	}
	if c.Shutdown.HardTimeoutSeconds < c.Shutdown.SIGTERMTimeoutSeconds {
		return fmt.Errorf(
			"shutdown.hard_timeout_seconds (%d) must be >= sigterm_timeout_seconds (%d)",
			c.Shutdown.HardTimeoutSeconds, c.Shutdown.SIGTERMTimeoutSeconds)
	}

	if c.Server.Listen == "" {
		return errors.New("server.listen must be non-empty")
	}
	if c.Server.MetricsListen == "" {
		return errors.New("server.metrics_listen must be non-empty")
	}
	if c.Server.MaxRequestBytes <= 0 {
		return errors.New("server.max_request_bytes must be positive")
	}
	if c.Server.ReadTimeoutSeconds <= 0 {
		return errors.New("server.read_timeout_seconds must be positive")
	}

	if c.Paths.OutputDir == "" {
		return errors.New("paths.output_dir must be non-empty")
	}

	if c.Firmware.Enabled {
		if c.Firmware.CacheDir == "" {
			return errors.New("firmware.cache_dir must be non-empty when firmware.enabled")
		}
		if owner, name, ok := strings.Cut(c.Firmware.Repo, "/"); !ok ||
			owner == "" || name == "" || strings.Contains(name, "/") {
			return fmt.Errorf("firmware.repo %q: must be owner/name", c.Firmware.Repo)
		}
		if c.Firmware.APIBase == "" {
			return errors.New("firmware.api_base must be non-empty when firmware.enabled")
		}
		// The floor is about GitHub's rate limit, not about taste:
		// unauthenticated API calls are capped at 60 per hour per IP,
		// and the mirror deliberately carries no token. A misconfigured
		// interval of a few seconds would exhaust that in a minute and
		// leave the mirror rate-limited for the rest of the hour --
		// i.e. a value meant to make updates arrive faster would stop
		// them arriving at all.
		if c.Firmware.RefreshIntervalSeconds < MinFirmwareRefreshSeconds {
			return fmt.Errorf(
				"firmware.refresh_interval_seconds (%d) must be >= %d",
				c.Firmware.RefreshIntervalSeconds, MinFirmwareRefreshSeconds)
		}
	}

	return nil
}

// MinFirmwareRefreshSeconds is the lowest firmware.refresh_interval_seconds
// Validate accepts. See the rate-limit reasoning there.
const MinFirmwareRefreshSeconds = 300

// FirmwareRefreshInterval returns the mirror's poll interval as a
// duration.
func (c *Config) FirmwareRefreshInterval() time.Duration {
	return time.Duration(c.Firmware.RefreshIntervalSeconds) * time.Second
}

// SIGTERMTimeout returns the configured graceful-shutdown deadline as
// a duration.
func (c *Config) SIGTERMTimeout() time.Duration {
	return time.Duration(c.Shutdown.SIGTERMTimeoutSeconds) * time.Second
}

// SIGINTTimeout returns the configured Ctrl-C deadline as a duration.
func (c *Config) SIGINTTimeout() time.Duration {
	return time.Duration(c.Shutdown.SIGINTTimeoutSeconds) * time.Second
}

// HardTimeout returns the absolute shutdown deadline as a duration.
func (c *Config) HardTimeout() time.Duration {
	return time.Duration(c.Shutdown.HardTimeoutSeconds) * time.Second
}

// IPAllowed reports whether ip falls into any configured allowlist
// CIDR. It returns false if Validate has not been called or the
// allowlist is empty; callers should consult Auth.Mode to decide
// whether to invoke this in the first place.
func (c *Config) IPAllowed(ip net.IP) bool {
	return c.Auth.IPAllowed(ip)
}

// IPAllowed reports whether ip falls into any of this AuthConfig's
// parsed allowlist CIDRs. It lives on AuthConfig (not just Config) so
// a caller that only carries the AuthConfig — internal/api.Server does,
// deliberately, to avoid depending on the whole Config tree — can
// still perform the ip_allowlist check without going through Config.
func (a *AuthConfig) IPAllowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range a.parsedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Description returns a one-line, secret-free summary of the loaded
// configuration suitable for logging at startup.
func (c *Config) Description() string {
	tokenSet := "no"
	if c.Auth.TokenHash != "" {
		tokenSet = "yes"
	}
	return strings.Join([]string{
		"listen=" + c.Server.Listen,
		"metrics=" + c.Server.MetricsListen,
		"auth=" + string(c.Auth.Mode),
		"token_hash_set=" + tokenSet,
		"allowed_cidrs=" + strconv.Itoa(len(c.Auth.AllowedCIDRs)),
		"profiles=" + c.Paths.Profiles,
		"state_dir=" + c.Paths.StateDir,
		"output_dir=" + c.Paths.OutputDir,
		"log_level=" + c.Logging.Level,
	}, " ")
}
