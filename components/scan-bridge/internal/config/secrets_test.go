package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSecretResolverReadsDockerSecretFile verifies the highest-priority
// source: a file named after the secret under the Docker secrets
// directory (/run/secrets/<name>). Trailing whitespace is trimmed
// because Docker secret files commonly end in a newline.
func TestSecretResolverReadsDockerSecretFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "paperless_api_token"), []byte("s3cr3t\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	r := NewSecretResolver(dir, func(string) (string, bool) { return "", false })

	got, err := r.Resolve("paperless_api_token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := "s3cr3t"; got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

// TestSecretResolverFallsBackToEnv verifies the second-priority source:
// when no Docker secret file exists, the resolver reads the environment
// variable whose name is the upper-cased secret name.
func TestSecretResolverFallsBackToEnv(t *testing.T) {
	dir := t.TempDir() // empty: no secret file present
	env := map[string]string{"PAPERLESS_API_TOKEN": "env-secret"}

	r := NewSecretResolver(dir, func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})

	got, err := r.Resolve("paperless_api_token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := "env-secret"; got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

// TestSecretResolverFilePrecedesEnv verifies the Docker secret file wins
// over an environment variable when both are present.
func TestSecretResolverFilePrecedesEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "paperless_api_token"), []byte("file-secret"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	env := map[string]string{"PAPERLESS_API_TOKEN": "env-secret"}

	r := NewSecretResolver(dir, func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})

	got, err := r.Resolve("paperless_api_token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := "file-secret"; got != want {
		t.Fatalf("Resolve = %q, want %q (file must win over env)", got, want)
	}
}

// TestSecretResolverNotFound verifies a clear error when a secret is in
// no source.
func TestSecretResolverNotFound(t *testing.T) {
	r := NewSecretResolver(t.TempDir(), func(string) (string, bool) { return "", false })

	if _, err := r.Resolve("missing"); err == nil {
		t.Fatal("Resolve of a missing secret returned nil error, want not-found")
	}
}

// TestSecretResolverRejectsInvalidNames locks in the fix for a path
// traversal: Resolve previously did filepath.Join(r.dir, name) with no
// validation of name, so a name like ".." or an absolute path could
// escape the Docker secrets directory and read arbitrary files.
// Validation must run before both the file lookup and the env
// fallback, so an invalid name never reaches the filesystem or the
// environment.
func TestSecretResolverRejectsInvalidNames(t *testing.T) {
	t.Parallel()

	sep := string(os.PathSeparator)
	cases := []struct {
		name string
		desc string
	}{
		{"", "empty"},
		{".", "current-dir"},
		{"..", "parent-dir"},
		{"a" + sep + "b", "contains path separator"},
		{sep + "etc" + sep + "passwd", "absolute path"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			// The secrets directory does not exist and lookupEnv fails
			// the test if called: an invalid name must be rejected
			// before either source is consulted.
			r := NewSecretResolver(filepath.Join(t.TempDir(), "does-not-exist"), func(string) (string, bool) {
				t.Fatal("lookupEnv called for an invalid secret name")
				return "", false
			})

			_, err := r.Resolve(tc.name)
			if err == nil {
				t.Fatalf("Resolve(%q) returned nil error, want rejection", tc.name)
			}
		})
	}
}
