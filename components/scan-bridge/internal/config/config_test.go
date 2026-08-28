package config

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envFunc(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestDefaultIsValid(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default() failed validation: %v", err)
	}
	if cfg.Server.Listen != ":8080" {
		t.Errorf("Default Server.Listen = %q, want :8080", cfg.Server.Listen)
	}
	if cfg.Auth.Mode != AuthModeToken {
		t.Errorf("Default Auth.Mode = %q, want %q", cfg.Auth.Mode, AuthModeToken)
	}
	if cfg.Shutdown.SIGTERMTimeoutSeconds != 30 {
		t.Errorf("Default sigterm timeout = %d, want 30", cfg.Shutdown.SIGTERMTimeoutSeconds)
	}
}

func TestLoadEmptyPathReturnsDefaultsPlusEnv(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"SCAN_BRIDGE_LOG_LEVEL": "debug",
	}

	cfg, err := Load("", envFunc(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want debug", cfg.Logging.Level)
	}
	if cfg.Server.Listen != ":8080" {
		t.Errorf("Server.Listen = %q, want default :8080", cfg.Server.Listen)
	}
}

func TestLoadTOMLOverridesDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	body := `
[server]
listen = ":7000"

[auth]
mode = "ip_allowlist"
allowed_cidrs = ["192.168.1.0/24", "10.0.0.0/8"]

[logging]
level = "warn"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path, envFunc(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Listen != ":7000" {
		t.Errorf("Server.Listen = %q, want :7000", cfg.Server.Listen)
	}
	if cfg.Auth.Mode != AuthModeIPAllowlist {
		t.Errorf("Auth.Mode = %q, want %q", cfg.Auth.Mode, AuthModeIPAllowlist)
	}
	if len(cfg.Auth.AllowedCIDRs) != 2 {
		t.Errorf("Auth.AllowedCIDRs len = %d, want 2", len(cfg.Auth.AllowedCIDRs))
	}
	if cfg.Logging.Level != "warn" {
		t.Errorf("Logging.Level = %q, want warn", cfg.Logging.Level)
	}
}

func TestLoadEnvOverridesTOML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`
[logging]
level = "warn"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path, envFunc(map[string]string{
		"SCAN_BRIDGE_LOG_LEVEL": "error",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Logging.Level != "error" {
		t.Errorf("env override failed: Logging.Level = %q, want error", cfg.Logging.Level)
	}
}

// TestLoadEmptyPathSkipsFile pins the contract that callers can opt
// out of the file load by passing path == "". main.go relies on this
// when the default config file does not exist on disk and --config
// was not explicitly set.
func TestLoadEmptyPathSkipsFile(t *testing.T) {
	t.Parallel()

	cfg, err := Load("", envFunc(nil))
	if err != nil {
		t.Fatalf("Load with empty path: %v", err)
	}
	if cfg.Server.Listen != ":8080" {
		t.Errorf("Server.Listen = %q, want default :8080", cfg.Server.Listen)
	}
}

// TestLoadMissingExplicitPathErrors locks in the fix for the silent-
// fallback foot-gun: an explicitly supplied path that does not exist
// is a configuration error, not a fall-through to defaults.
func TestLoadMissingExplicitPathErrors(t *testing.T) {
	t.Parallel()

	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"), envFunc(nil))
	if err == nil {
		t.Fatal("expected error on missing explicit config path")
	}
}

// TestLoadRejectsUnknownTOMLKeys catches typos in the config file
// the same way the profiles loader catches typos in the YAML schema.
func TestLoadRejectsUnknownTOMLKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[server]
listen = ":8080"

[serverz]
listen = ":7000"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path, envFunc(nil))
	if err == nil {
		t.Fatal("expected error on unknown TOML key")
	}
	if !strings.Contains(err.Error(), "unknown keys") {
		t.Errorf("error %q did not mention unknown keys", err.Error())
	}
}

func TestValidateRejectsUnknownAuthMode(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Auth.Mode = "magic"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "auth.mode") {
		t.Fatalf("Validate did not reject unknown auth mode: %v", err)
	}
}

func TestValidateRequiresCIDRsInAllowlistMode(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Auth.Mode = AuthModeIPAllowlist
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to fail with no allowed_cidrs")
	}
}

func TestValidateRejectsBadCIDR(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Auth.Mode = AuthModeIPAllowlist
	cfg.Auth.AllowedCIDRs = []string{"not-a-cidr"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to fail on malformed CIDR")
	}
}

func TestValidateRejectsBadLogLevel(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Logging.Level = "trace"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to fail on unknown log level")
	}
}

func TestValidateRejectsHardTimeoutBelowSigterm(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Shutdown.HardTimeoutSeconds = 5
	cfg.Shutdown.SIGTERMTimeoutSeconds = 30
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to fail when hard < sigterm")
	}
}

func TestIPAllowed(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Auth.Mode = AuthModeIPAllowlist
	cfg.Auth.AllowedCIDRs = []string{"192.168.1.0/24"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	cases := []struct {
		ip   string
		want bool
	}{
		{"192.168.1.5", true},
		{"192.168.2.5", false},
		{"10.0.0.1", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.ip, func(t *testing.T) {
			t.Parallel()
			got := cfg.IPAllowed(net.ParseIP(tc.ip))
			if got != tc.want {
				t.Errorf("IPAllowed(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// TestAPITokenEnvIsHashed pins the contract that
// SCAN_BRIDGE_API_TOKEN carries plaintext on the wire (per
// CONTAINER_SUITE.md sec. 4.5) and is SHA-256-hashed before it
// reaches Config.Auth.TokenHash. Plaintext must never linger.
func TestAPITokenEnvIsHashed(t *testing.T) {
	t.Parallel()

	cfg, err := Load("", envFunc(map[string]string{
		"SCAN_BRIDGE_API_TOKEN": "plaintext-secret-value",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.TokenHash == "" {
		t.Fatal("TokenHash empty after env-supplied plaintext token")
	}
	if cfg.Auth.TokenHash == "plaintext-secret-value" {
		t.Fatal("TokenHash equals plaintext — must be hashed")
	}
	if len(cfg.Auth.TokenHash) != 64 {
		t.Errorf("TokenHash len = %d, want 64 (SHA-256 hex)",
			len(cfg.Auth.TokenHash))
	}
}

// TestDefaultIncludesOutputDir pins the compiled-in default for the
// dispatch client's page-output directory (D2 in the Phase 1.2
// implementation brief) — deliberately distinct from Paths.StateDir.
func TestDefaultIncludesOutputDir(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if cfg.Paths.OutputDir != "/var/lib/scan-bridge/scans" {
		t.Errorf("Default Paths.OutputDir = %q, want /var/lib/scan-bridge/scans", cfg.Paths.OutputDir)
	}
	if cfg.Paths.OutputDir == cfg.Paths.StateDir {
		t.Error("Paths.OutputDir must not alias Paths.StateDir")
	}
}

func TestLoadOutputDirEnvOverride(t *testing.T) {
	t.Parallel()

	cfg, err := Load("", envFunc(map[string]string{
		"SCAN_BRIDGE_OUTPUT_DIR": "/custom/scans",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Paths.OutputDir != "/custom/scans" {
		t.Errorf("Paths.OutputDir = %q, want /custom/scans", cfg.Paths.OutputDir)
	}
}

func TestValidateRejectsEmptyOutputDir(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Paths.OutputDir = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to fail on empty paths.output_dir")
	}
}

// TestDefaultIncludesScanProcessorSocket pins the compiled-in default
// for the scan-processor client's Unix-socket path (design doc
// docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
// sec. 9 Task 7) -- mirrors TestDefaultIncludesOutputDir's pattern for
// SaneSocket's newer sibling.
func TestDefaultIncludesScanProcessorSocket(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if cfg.Paths.ScanProcessorSocket != "/run/scan-processor/scan-processor.sock" {
		t.Errorf("Default Paths.ScanProcessorSocket = %q, want /run/scan-processor/scan-processor.sock",
			cfg.Paths.ScanProcessorSocket)
	}
}

func TestLoadScanProcessorSocketEnvOverride(t *testing.T) {
	t.Parallel()

	cfg, err := Load("", envFunc(map[string]string{
		"SCAN_BRIDGE_SCAN_PROCESSOR_SOCKET": "/custom/scan-processor.sock",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Paths.ScanProcessorSocket != "/custom/scan-processor.sock" {
		t.Errorf("Paths.ScanProcessorSocket = %q, want /custom/scan-processor.sock", cfg.Paths.ScanProcessorSocket)
	}
}

func TestDescriptionDoesNotLeakSecrets(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Auth.TokenHash = "abc123-secret-hash"
	out := cfg.Description()

	if strings.Contains(out, "abc123") {
		t.Errorf("Description leaked token hash: %q", out)
	}
	if !strings.Contains(out, "token_hash_set=yes") {
		t.Errorf("Description did not include token_hash_set marker: %q", out)
	}
}

// TestDefaultIncludesRequestHardeningFields pins the compiled-in
// defaults for issue #47's body-size and read-timeout hardening.
func TestDefaultIncludesRequestHardeningFields(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if cfg.Server.MaxRequestBytes != DefaultMaxRequestBytes {
		t.Errorf("Default Server.MaxRequestBytes = %d, want %d", cfg.Server.MaxRequestBytes, DefaultMaxRequestBytes)
	}
	if cfg.Server.ReadTimeoutSeconds != DefaultReadTimeoutSeconds {
		t.Errorf("Default Server.ReadTimeoutSeconds = %d, want %d", cfg.Server.ReadTimeoutSeconds, DefaultReadTimeoutSeconds)
	}
	if cfg.Paths.KeepScanOutput {
		t.Error("Default Paths.KeepScanOutput = true, want false (clean up by default, issue #49 point 1)")
	}
}

func TestLoadRequestHardeningEnvOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := Load("", envFunc(map[string]string{
		"SCAN_BRIDGE_MAX_REQUEST_BYTES":    "2048",
		"SCAN_BRIDGE_READ_TIMEOUT_SECONDS": "15",
		"SCAN_BRIDGE_KEEP_SCAN_OUTPUT":     "true",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.MaxRequestBytes != 2048 {
		t.Errorf("Server.MaxRequestBytes = %d, want 2048", cfg.Server.MaxRequestBytes)
	}
	if cfg.Server.ReadTimeoutSeconds != 15 {
		t.Errorf("Server.ReadTimeoutSeconds = %d, want 15", cfg.Server.ReadTimeoutSeconds)
	}
	if !cfg.Paths.KeepScanOutput {
		t.Error("Paths.KeepScanOutput = false, want true")
	}
}

// TestLoadRejectsMalformedNumericEnvOverrides covers applyEnv's error
// path for each of the three new env vars: a typo'd value must fail
// Load loudly rather than silently falling back to the default.
func TestLoadRejectsMalformedNumericEnvOverrides(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  map[string]string
	}{
		{"max request bytes not a number", map[string]string{"SCAN_BRIDGE_MAX_REQUEST_BYTES": "not-a-number"}},
		{"read timeout seconds not a number", map[string]string{"SCAN_BRIDGE_READ_TIMEOUT_SECONDS": "soon"}},
		{"keep scan output not a bool", map[string]string{"SCAN_BRIDGE_KEEP_SCAN_OUTPUT": "maybe"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Load("", envFunc(tc.env)); err == nil {
				t.Fatal("expected Load to fail on a malformed env override")
			}
		})
	}
}

func TestValidateRejectsNonPositiveMaxRequestBytes(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Server.MaxRequestBytes = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to fail on server.max_request_bytes = 0")
	}
}

func TestValidateRejectsNonPositiveReadTimeoutSeconds(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Server.ReadTimeoutSeconds = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to fail on server.read_timeout_seconds <= 0")
	}
}

func TestDefaultEnablesTheFirmwareMirror(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if !cfg.Firmware.Enabled {
		t.Error("Default Firmware.Enabled = false; a deployment with a panel is the normal case")
	}
	if want := "/var/lib/scan-bridge/firmware"; cfg.FirmwareCacheDir() != want {
		t.Errorf("Default FirmwareCacheDir() = %q, want %q", cfg.FirmwareCacheDir(), want)
	}
	// The interval bounds how far an unattended deployment may lag a
	// release. Not paired with the panel's poll -- that reads this
	// bridge's cache, not GitHub, and is independent. So the assertion
	// is the property that actually matters: comfortably above the
	// API-call floor, and inside a working day.
	if got := cfg.FirmwareRefreshInterval(); got <= MinFirmwareRefreshSeconds*time.Second || got > 12*time.Hour {
		t.Errorf("Default firmware refresh interval %v is outside (floor, 12h]", got)
	}
}

func TestFirmwareEnvOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := Load("", envFunc(map[string]string{
		"SCAN_BRIDGE_FIRMWARE_CACHE_DIR":                "/tmp/fw",
		"SCAN_BRIDGE_FIRMWARE_REPO":                     "someone/else",
		"SCAN_BRIDGE_FIRMWARE_API_BASE":                 "http://localhost:9999",
		"SCAN_BRIDGE_FIRMWARE_REFRESH_INTERVAL_SECONDS": "900",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Firmware.CacheDir != "/tmp/fw" ||
		cfg.Firmware.Repo != "someone/else" ||
		cfg.Firmware.APIBase != "http://localhost:9999" ||
		cfg.Firmware.RefreshIntervalSeconds != 900 {
		t.Errorf("firmware env overrides not applied: %+v", cfg.Firmware)
	}
}

func TestFirmwareDisabledSkipsItsValidation(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Firmware.Enabled = false
	cfg.Firmware.CacheDir = ""
	cfg.Firmware.Repo = "nonsense"
	cfg.Firmware.RefreshIntervalSeconds = 0
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate on a disabled mirror = %v, want nil", err)
	}
}

func TestValidateRejectsBadFirmwareConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		// An empty cache_dir is no longer an error on its own: it means
		// "derive from state_dir". Only having neither is.
		{"neither cache dir nor state dir", func(c *Config) {
			c.Firmware.CacheDir = ""
			c.Paths.StateDir = ""
		}, "cache_dir"},
		{"repo without slash", func(c *Config) { c.Firmware.Repo = "nonsense" }, "owner/name"},
		{"repo with empty owner", func(c *Config) { c.Firmware.Repo = "/name" }, "owner/name"},
		{"repo with empty name", func(c *Config) { c.Firmware.Repo = "owner/" }, "owner/name"},
		{"empty api base", func(c *Config) { c.Firmware.APIBase = "" }, "api_base"},
		// Below GitHub's 60-per-hour unauthenticated limit the mirror
		// would rate-limit itself out of updating at all.
		{"interval too small", func(c *Config) { c.Firmware.RefreshIntervalSeconds = 5 }, "refresh_interval_seconds"},
		{"zero interval", func(c *Config) { c.Firmware.RefreshIntervalSeconds = 0 }, "refresh_interval_seconds"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := Default()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The firmware cache follows state_dir. Both settings describe one
// thing -- where durable daemon state lives -- and an operator who
// moves the first must not be left with a firmware cache pointing at
// the old location, which fails at startup with a message that names
// neither setting.
func TestFirmwareCacheDirFollowsStateDir(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Paths.StateDir = "/srv/psb"
	if got, want := cfg.FirmwareCacheDir(), "/srv/psb/firmware"; got != want {
		t.Errorf("FirmwareCacheDir() = %q, want %q", got, want)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}

	// An explicit value still wins.
	cfg.Firmware.CacheDir = "/mnt/elsewhere"
	if got := cfg.FirmwareCacheDir(); got != "/mnt/elsewhere" {
		t.Errorf("explicit cache_dir ignored: %q", got)
	}
}

func TestValidateRejectsAFirmwareCacheWithNowhereToLive(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Paths.StateDir = ""
	cfg.Firmware.CacheDir = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a firmware cache with neither state_dir nor cache_dir")
	}
	if !strings.Contains(err.Error(), "cache_dir") {
		t.Errorf("error %q does not mention cache_dir", err)
	}
}
