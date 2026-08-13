package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestVersionFlag(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.HasPrefix(stdout.String(), "sane-runtime ") {
		t.Errorf("version output = %q, want prefix 'sane-runtime '", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty, got %q", stderr.String())
	}
}

func TestUnknownFlagReturnsError(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"--bogus"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error from unknown flag")
	}
}

func TestNewUnixListener_CreatesSocketAndRemovesStaleFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "sane.sock")

	// Simulate a stale socket file left behind by an unclean previous
	// exit; newUnixListener must remove it rather than fail with
	// "address already in use".
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	ln, err := newUnixListener(path)
	if err != nil {
		t.Fatalf("newUnixListener: %v", err)
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(path)
	}()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if info.Mode().Perm() != 0o660 {
		t.Errorf("socket perm = %o, want 660", info.Mode().Perm())
	}

	// Confirm it is actually accepting unix connections, not just a
	// file on disk with the right name.
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	_ = conn.Close()
}

// TestNewUnixListener_CreatesMissingParentDir covers the compose /
// bare `docker run` path where the socket's parent directory has
// never been created on the host side: newUnixListener must create it
// (mode 0o750) rather than fail with ENOENT. A shared named volume
// mounted by both containers creates the mountpoint directory itself,
// so this mainly matters for a standalone `docker run` or a fresh
// bind mount that Docker has not pre-created yet.
//
// This replaces the previous TestNewUnixListener_MissingParentDirFails,
// which asserted the opposite (pre-fix) behaviour: newUnixListener
// used to require the parent directory to already exist.
func TestNewUnixListener_CreatesMissingParentDir(t *testing.T) {
	t.Parallel()

	// "d" (not a descriptive name like "does-not-exist") is deliberate:
	// AF_UNIX socket paths are capped at 108 bytes (Linux sun_path), and
	// t.TempDir() already spends most of that budget on this sandbox's
	// long $TMPDIR prefix plus the test name. A longer subdirectory name
	// here reproducibly overflows the limit with "bind: invalid
	// argument" — a test-environment artifact unrelated to the
	// behaviour under test.
	parent := filepath.Join(t.TempDir(), "d")
	path := filepath.Join(parent, "sane.sock")

	ln, err := newUnixListener(path)
	if err != nil {
		t.Fatalf("newUnixListener: %v", err)
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(path)
	}()

	info, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("stat parent dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("parent %q was created but is not a directory", parent)
	}
	if perm := info.Mode().Perm(); perm != 0o750 {
		t.Errorf("parent dir perm = %o, want 750", perm)
	}

	// Confirm the socket itself is actually accepting connections, not
	// just a file on disk with the right name.
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	_ = conn.Close()
}

// TestEnvOr deliberately does NOT call t.Parallel() and manipulates
// the environment with plain os.Setenv/os.Unsetenv rather than
// t.Setenv: this package also has parallel tests (TestVersionFlag
// etc.), and t.Setenv panics whenever any test in the same run has
// called Parallel, even on an unrelated test — see
// https://github.com/golang/go/issues/... "Setenv or Chdir and
// Parallel". Manual env handling with explicit cleanup sidesteps that
// restriction; safety here relies on this test running to completion,
// env restored, before Go's test runner releases the paused parallel
// batch (which only happens once every sequential top-level test has
// returned).
func TestEnvOr(t *testing.T) {
	cases := []struct {
		name     string
		key      string
		value    string
		setEnv   bool
		fallback string
		want     string
	}{
		{
			name:     "env set",
			key:      "SANE_RUNTIME_TEST_ENV_OR",
			value:    "/custom/sane.sock",
			setEnv:   true,
			fallback: "/run/sane-runtime/sane.sock",
			want:     "/custom/sane.sock",
		},
		{
			name:     "env unset falls back",
			key:      "SANE_RUNTIME_TEST_ENV_OR_UNSET",
			setEnv:   false,
			fallback: "/run/sane-runtime/sane.sock",
			want:     "/run/sane-runtime/sane.sock",
		},
		{
			name:     "env set to empty falls back",
			key:      "SANE_RUNTIME_TEST_ENV_OR_EMPTY",
			value:    "",
			setEnv:   true,
			fallback: "/run/sane-runtime/sane.sock",
			want:     "/run/sane-runtime/sane.sock",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				if err := os.Setenv(tc.key, tc.value); err != nil {
					t.Fatalf("Setenv: %v", err)
				}
				defer func() { _ = os.Unsetenv(tc.key) }()
			}
			if got := envOr(tc.key, tc.fallback); got != tc.want {
				t.Errorf("envOr(%q, %q) = %q, want %q", tc.key, tc.fallback, got, tc.want)
			}
		})
	}
}

// TestRun_ServesHealthAndShutsDownOnSIGTERM is the end-to-end test for
// run(): it starts the real Unix-socket listener, confirms GET
// /health answers over that socket, sends this process a real SIGTERM
// (exactly what a container runtime does), and asserts run() returns
// with no error and has removed the socket file — the same graceful
// path shutdown() implements.
//
// Not marked t.Parallel(): it manipulates process-wide signal
// delivery and must not race a second instance of this test.
func TestRun_ServesHealthAndShutsDownOnSIGTERM(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "sane.sock")

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- run([]string{"--socket", socketPath}, io.Discard, io.Discard)
	}()

	// Poll for the socket file: run() creates the listener
	// asynchronously relative to this goroutine.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket file never appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get("http://sane-runtime.invalid/health")
	if err != nil {
		t.Fatalf("GET /health over unix socket: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health status = %d, want 200", resp.StatusCode)
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM to self: %v", err)
	}

	select {
	case err := <-runErrCh:
		if err != nil {
			t.Fatalf("run() returned error after SIGTERM: %v", err)
		}
	case <-time.After(gracefulShutdownTimeout + 5*time.Second):
		t.Fatal("run() did not return after SIGTERM within the graceful shutdown budget")
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket file still present after shutdown: err = %v", err)
	}
}
