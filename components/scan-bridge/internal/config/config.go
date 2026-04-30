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
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
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
}

// ServerConfig controls the public REST API and the metrics endpoint.
type ServerConfig struct {
	Listen        string `toml:"listen"`
	MetricsListen string `toml:"metrics_listen"`
}

// AuthConfig describes how inbound requests are authenticated.
//
// In token mode the daemon expects a bearer token whose SHA-256 hex
// digest matches TokenHash. In ip_allowlist mode the daemon accepts
// unauthenticated requests whose source IP falls into AllowedCIDRs.
type AuthConfig struct {
	Mode          AuthMode `toml:"mode"`
	TokenHash     string   `toml:"token_hash"`
	AllowedCIDRs  []string `toml:"allowed_cidrs"`
	parsedCIDRs   []*net.IPNet
}

// PathsConfig collects the on-disk locations the daemon reads or writes.
type PathsConfig struct {
	Profiles   string `toml:"profiles"`
	StateDir   string `toml:"state_dir"`
	SaneSocket string `toml:"sane_socket"`
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

// Default returns a Config populated with the compiled-in defaults.
// These are designed to be production-safe out of the box for the
// reference Pi deployment.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Listen:        ":8080",
			MetricsListen: ":9090",
		},
		Auth: AuthConfig{
			Mode: AuthModeToken,
		},
		Paths: PathsConfig{
			Profiles:   "/etc/scan-bridge/profiles.yaml",
			StateDir:   "/var/lib/scan-bridge",
			SaneSocket: "/run/sane-runtime/sane.sock",
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
	}
}

// Load applies the loading precedence to produce a validated Config.
//
// path may be empty, in which case only defaults and the environment
// are consulted. The environment is read from osLookupEnv (typically
// os.LookupEnv); the indirection exists for tests.
func Load(path string, osLookupEnv func(string) (string, bool)) (Config, error) {
	if osLookupEnv == nil {
		osLookupEnv = os.LookupEnv
	}

	cfg := Default()

	if path != "" {
		if _, err := os.Stat(path); err == nil {
			if _, err := toml.DecodeFile(path, &cfg); err != nil {
				return Config{}, fmt.Errorf("decode config %q: %w", path, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("stat config %q: %w", path, err)
		}
	}

	applyEnv(&cfg, osLookupEnv)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func applyEnv(cfg *Config, look func(string) (string, bool)) {
	if v, ok := look("SCAN_BRIDGE_LISTEN"); ok {
		cfg.Server.Listen = v
	}
	if v, ok := look("SCAN_BRIDGE_METRICS_LISTEN"); ok {
		cfg.Server.MetricsListen = v
	}
	if v, ok := look("SCAN_BRIDGE_AUTH_MODE"); ok {
		cfg.Auth.Mode = AuthMode(v)
	}
	if v, ok := look("SCAN_BRIDGE_API_TOKEN_HASH"); ok {
		cfg.Auth.TokenHash = v
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
	if v, ok := look("SCAN_BRIDGE_LOG_LEVEL"); ok {
		cfg.Logging.Level = v
	}
	if v, ok := look("SCAN_BRIDGE_LOG_FORMAT"); ok {
		cfg.Logging.Format = v
	}
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

	return nil
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
	if ip == nil {
		return false
	}
	for _, n := range c.Auth.parsedCIDRs {
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
		"log_level=" + c.Logging.Level,
	}, " ")
}
