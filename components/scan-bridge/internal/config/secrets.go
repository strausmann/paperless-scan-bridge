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
func (r SecretResolver) Resolve(name string) (string, error) {
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
