package config

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
