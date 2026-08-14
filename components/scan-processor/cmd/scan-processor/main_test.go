package main

import (
	"bytes"
	"context"
	"fmt"
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

// TestRun_RejectsMalformedNumericEnvOverrides covers run() itself,
// not just envInt64OrErr/envIntOrErr in isolation: a malformed
// SCAN_PROCESSOR_MAX_REQUEST_BYTES or SCAN_PROCESSOR_READ_TIMEOUT_SECONDS
// must make the daemon fail to start, not silently launch with a
// default the operator never asked for. Mirrors
// internal/config.TestLoadRejectsMalformedNumericEnvOverrides on the
// scan-bridge sibling daemon. Not marked t.Parallel() -- manipulates
// process-wide environment variables with plain os.Setenv/os.Unsetenv
// (same t.Setenv-panics-after-Parallel reason as TestEnvOr above).
func TestRun_RejectsMalformedNumericEnvOverrides(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  string
	}{
		{"max request bytes not a number", "SCAN_PROCESSOR_MAX_REQUEST_BYTES", "not-a-number"},
		{"read timeout seconds not a number", "SCAN_PROCESSOR_READ_TIMEOUT_SECONDS", "soon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.Setenv(tc.key, tc.val); err != nil {
				t.Fatalf("Setenv: %v", err)
			}
			defer func() { _ = os.Unsetenv(tc.key) }()

			var stdout, stderr bytes.Buffer
			socketPath := filepath.Join(t.TempDir(), "s.sock")
			err := run([]string{"--socket", socketPath}, &stdout, &stderr)
			if err == nil {
				t.Fatalf("run() with %s=%q returned nil error, want a config error", tc.key, tc.val)
			}
			if _, statErr := os.Stat(socketPath); statErr == nil {
				t.Error("run() created the socket despite the malformed env var -- it must fail before ever listening")
			}
		})
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

// TestEnvInt64Or mirrors TestEnvOr for the int64-valued env var
// helper (SCAN_PROCESSOR_MAX_REQUEST_BYTES). Not marked t.Parallel()
// for the same os.Setenv/os.Unsetenv-vs-t.Setenv reason as TestEnvOr.
// TestDefaultMaxRequestBytes pins this package's own copy of
// defaultMaxRequestBytes -- see its doc comment for the derivation
// (a real page at the repo's own deploy/profiles/default.yaml scan
// profile) and internal/procapi's identically-named test, which pins
// the same literal on that package's independently-declared copy.
func TestDefaultMaxRequestBytes(t *testing.T) {
	t.Parallel()

	const want = 512 << 20 // 512 MiB
	if defaultMaxRequestBytes != want {
		t.Errorf("defaultMaxRequestBytes = %d, want %d (512 MiB)", defaultMaxRequestBytes, want)
	}
}

// TestEnvInt64OrErr covers envInt64OrErr's contract: unset/empty
// falls back (no error); a valid value overrides the fallback (no
// error); a malformed value is a hard error (0, err != nil) --
// unlike envOr's plain-string "empty falls back" case, a SET
// numeric override that fails to parse must not be silently
// swallowed (issue #47 review: this now matches
// internal/config.applyEnv's contract on the scan-bridge sibling
// daemon, which was previously inconsistent with this function's
// old silent-fallback behaviour).
func TestEnvInt64OrErr(t *testing.T) {
	cases := []struct {
		name      string
		key       string
		value     string
		setEnv    bool
		fallback  int64
		want      int64
		wantError bool
	}{
		{
			name:     "env set to a valid int64",
			key:      "SCAN_PROCESSOR_TEST_ENV_INT64_OR_ERR",
			value:    "12345",
			setEnv:   true,
			fallback: defaultMaxRequestBytes,
			want:     12345,
		},
		{
			name:     "env unset falls back",
			key:      "SCAN_PROCESSOR_TEST_ENV_INT64_OR_ERR_UNSET",
			setEnv:   false,
			fallback: defaultMaxRequestBytes,
			want:     defaultMaxRequestBytes,
		},
		{
			name:     "env set to empty falls back",
			key:      "SCAN_PROCESSOR_TEST_ENV_INT64_OR_ERR_EMPTY",
			value:    "",
			setEnv:   true,
			fallback: defaultMaxRequestBytes,
			want:     defaultMaxRequestBytes,
		},
		{
			name:      "env set to a non-numeric value is a hard error",
			key:       "SCAN_PROCESSOR_TEST_ENV_INT64_OR_ERR_INVALID",
			value:     "not-a-number",
			setEnv:    true,
			fallback:  defaultMaxRequestBytes,
			wantError: true,
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
			got, err := envInt64OrErr(tc.key, tc.fallback)
			if tc.wantError {
				if err == nil {
					t.Fatalf("envInt64OrErr(%q, %d) = %d, <nil>, want a non-nil error", tc.key, tc.fallback, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("envInt64OrErr(%q, %d) unexpected error: %v", tc.key, tc.fallback, err)
			}
			if got != tc.want {
				t.Errorf("envInt64OrErr(%q, %d) = %d, want %d", tc.key, tc.fallback, got, tc.want)
			}
		})
	}
}

// TestEnvIntOrErr mirrors TestEnvInt64OrErr for the int-valued env
// var helper (SCAN_PROCESSOR_READ_TIMEOUT_SECONDS).
func TestEnvIntOrErr(t *testing.T) {
	cases := []struct {
		name      string
		key       string
		value     string
		setEnv    bool
		fallback  int
		want      int
		wantError bool
	}{
		{
			name:     "env set to a valid int",
			key:      "SCAN_PROCESSOR_TEST_ENV_INT_OR_ERR",
			value:    "5",
			setEnv:   true,
			fallback: defaultReadTimeoutSeconds,
			want:     5,
		},
		{
			name:     "env unset falls back",
			key:      "SCAN_PROCESSOR_TEST_ENV_INT_OR_ERR_UNSET",
			setEnv:   false,
			fallback: defaultReadTimeoutSeconds,
			want:     defaultReadTimeoutSeconds,
		},
		{
			name:     "env set to empty falls back",
			key:      "SCAN_PROCESSOR_TEST_ENV_INT_OR_ERR_EMPTY",
			value:    "",
			setEnv:   true,
			fallback: defaultReadTimeoutSeconds,
			want:     defaultReadTimeoutSeconds,
		},
		{
			name:      "env set to a non-numeric value is a hard error",
			key:       "SCAN_PROCESSOR_TEST_ENV_INT_OR_ERR_INVALID",
			value:     "soon",
			setEnv:    true,
			fallback:  defaultReadTimeoutSeconds,
			wantError: true,
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
			got, err := envIntOrErr(tc.key, tc.fallback)
			if tc.wantError {
				if err == nil {
					t.Fatalf("envIntOrErr(%q, %d) = %d, <nil>, want a non-nil error", tc.key, tc.fallback, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("envIntOrErr(%q, %d) unexpected error: %v", tc.key, tc.fallback, err)
			}
			if got != tc.want {
				t.Errorf("envIntOrErr(%q, %d) = %d, want %d", tc.key, tc.fallback, got, tc.want)
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

// TestRun_SlowRequestBodyTimesOutInsteadOfHanging covers issue #47's
// Read-Timeout hardening end to end: it starts the real daemon with a
// deliberately tiny --read-timeout-seconds, opens a raw connection to
// its Unix socket, sends a POST /process request line and headers
// declaring a large Content-Length, and then — mirroring a stalled or
// slow-loris-style client — never sends the body. The assertion is
// that reading the connection produces SOMETHING (EOF, connection
// reset, or an error response) well within a bound generously larger
// than --read-timeout-seconds, proving the server's ReadTimeout
// closed the stalled connection rather than the handler (and this
// test) hanging on it forever.
//
// Not marked t.Parallel(): same process-wide-signal reason as
// TestRun_ServesHealthAndShutsDownOnSIGTERM, which this test also
// uses to shut the daemon down afterwards.
func TestRun_SlowRequestBodyTimesOutInsteadOfHanging(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "s.sock")

	const readTimeoutSeconds = 1
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- run([]string{
			"--socket", socketPath,
			"--read-timeout-seconds", fmt.Sprint(readTimeoutSeconds),
		}, io.Discard, io.Discard)
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

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Full request line + headers, declaring a body far larger than
	// what we are about to actually send -- then nothing. A real
	// client would keep writing; this one stops here.
	reqHead := "POST /process HTTP/1.1\r\n" +
		"Host: scan-processor.invalid\r\n" +
		"Content-Type: multipart/mixed; boundary=xyz\r\n" +
		"Content-Length: 1000000\r\n" +
		"\r\n"
	if _, err := conn.Write([]byte(reqHead)); err != nil {
		t.Fatalf("write request head: %v", err)
	}

	// Our OWN deadline is deliberately generous relative to the
	// server's 1s --read-timeout-seconds -- if IT fires first, that
	// means the server's ReadTimeout did not (the connection hung),
	// which is the one and only failure this test is trying to catch.
	ownDeadline := time.Duration(readTimeoutSeconds) * time.Second * 5
	if err := conn.SetReadDeadline(time.Now().Add(ownDeadline)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 4096)
	n, readErr := conn.Read(buf)
	if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
		t.Fatalf("read timed out after our OWN %s deadline — the server's --read-timeout-seconds=%d did not fire (connection hung)",
			ownDeadline, readTimeoutSeconds)
	}
	// Anything else counts as proof the server acted well within our
	// much larger deadline: net/http.Server's ReadTimeout can either
	// close the connection outright (readErr == io.EOF / connection
	// reset, n == 0) or write an explicit response first (e.g. "408
	// Request Timeout") before closing (readErr == nil, n > 0) -- both
	// are "did not hang"; only our own deadline firing above would
	// mean the mechanism failed.
	t.Logf("connection outcome within %s: n=%d readErr=%v", ownDeadline, n, readErr)

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
}
