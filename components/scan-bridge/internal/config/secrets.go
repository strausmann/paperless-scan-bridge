package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SecretResolver resolves a named secret from one of several sources,
// in priority order. It lets profiles and config reference secrets by
// name (e.g. "paperless_api_token") without embedding the value.
type SecretResolver struct {
	// dir is the Docker secrets directory (typically /run/secrets).
	dir string
	// lookupEnv reads an environment variable; injected for testing.
	lookupEnv func(string) (string, bool)
}

// NewSecretResolver builds a resolver over the given Docker secrets
// directory and environment lookup.
func NewSecretResolver(dir string, lookupEnv func(string) (string, bool)) SecretResolver {
	return SecretResolver{dir: dir, lookupEnv: lookupEnv}
}

// Resolve returns the value for name, checking the Docker secrets
// directory first. The value is trimmed of surrounding whitespace.
//
// name must be a simple filename: non-empty, not "." or "..", and
// without a path separator or a leading path separator (absolute
// path). This is checked before either source is consulted, so a
// crafted name (e.g. "../../etc/passwd") can never escape the Docker
// secrets directory or reach an unintended source.
func (r SecretResolver) Resolve(name string) (string, error) {
	if err := validateSecretName(name); err != nil {
		return "", err
	}
	if r.dir != "" {
		b, err := os.ReadFile(filepath.Join(r.dir, name))
		if err == nil {
			return strings.TrimSpace(string(b)), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read secret file %q: %w", name, err)
		}
	}
	if r.lookupEnv != nil {
		if v, ok := r.lookupEnv(strings.ToUpper(name)); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("secret %q not found", name)
}

// validateSecretName rejects any name that is not a plain filename,
// preventing path traversal (e.g. "..", "../secret") or an absolute
// path (e.g. "/etc/passwd") from reaching filepath.Join below.
func validateSecretName(name string) error {
	if name == "" {
		return fmt.Errorf("secret name is empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("secret name %q is not a valid filename", name)
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("secret name %q must not be an absolute path", name)
	}
	if name != filepath.Base(name) {
		return fmt.Errorf("secret name %q must not contain a path separator", name)
	}
	return nil
}
