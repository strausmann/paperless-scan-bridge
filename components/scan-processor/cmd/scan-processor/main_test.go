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
	if !strings.HasPrefix(stdout.String(), "scan-processor ") {
		t.Errorf("version output = %q, want prefix 'scan-processor '", stdout.String())
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
	path := filepath.Join(dir, "s.sock")

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

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	_ = conn.Close()
}

func TestNewUnixListener_CreatesMissingParentDir(t *testing.T) {
	t.Parallel()

	// "d" (not a descriptive name) keeps the AF_UNIX sun_path (108
	// byte Linux limit) well under budget — same rationale as
	// sane-runtime's identical test.
	parent := filepath.Join(t.TempDir(), "d")
	path := filepath.Join(parent, "s.sock")

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

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	_ = conn.Close()
}

// TestEnvOr deliberately does NOT call t.Parallel() and manipulates
// the environment with plain os.Setenv/os.Unsetenv rather than
// t.Setenv, for the same reason sane-runtime's identical test does
// (t.Setenv panics once any test in the run has called Parallel).
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
			key:      "SCAN_PROCESSOR_TEST_ENV_OR",
			value:    "/custom/scan-processor.sock",
			setEnv:   true,
			fallback: defaultSocketPath,
			want:     "/custom/scan-processor.sock",
		},
		{
			name:     "env unset falls back",
			key:      "SCAN_PROCESSOR_TEST_ENV_OR_UNSET",
			setEnv:   false,
			fallback: defaultSocketPath,
			want:     defaultSocketPath,
		},
		{
			name:     "env set to empty falls back",
			key:      "SCAN_PROCESSOR_TEST_ENV_OR_EMPTY",
			value:    "",
			setEnv:   true,
			fallback: defaultSocketPath,
			want:     defaultSocketPath,
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

// TestRun_ServesHealthAndShutsDownOnSIGTERM is the end-to-end test
// for run(): it starts the real Unix-socket listener, confirms GET
// /health answers over that socket, sends this process a real
// SIGTERM, and asserts run() returns with no error and has removed
// the socket file. Mirrors sane-runtime's identical test.
//
// Not marked t.Parallel(): it manipulates process-wide signal
// delivery and must not race a second instance of this test.
func TestRun_ServesHealthAndShutsDownOnSIGTERM(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "s.sock")

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- run([]string{"--socket", socketPath}, io.Discard, io.Discard)
	}()

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
	resp, err := client.Get("http://scan-processor.invalid/health")
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
