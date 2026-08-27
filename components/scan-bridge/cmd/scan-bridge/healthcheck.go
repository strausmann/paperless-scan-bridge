package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"
)

// healthcheckTimeout bounds the whole probe. Docker's own `timeout:`
// kills the process anyway, but a probe that relies on being killed
// leaves no useful exit status behind -- and the compose healthcheck
// is the one place where "unhealthy" and "timed out" should not look
// the same in `docker inspect`.
const healthcheckTimeout = 4 * time.Second

// runHealthcheck probes the daemon's own /ready endpoint and maps the
// answer to a process exit status: 0 for ready, non-zero otherwise.
//
// It exists because the production image is distroless. There is no
// shell in it, no curl and no wget, so the usual
// `test: ["CMD", "curl", "-f", "..."]` has nothing to run. The binary
// that is already in the image is the only thing that can probe it.
//
// /ready, not /health: health answers "the process is up", which a
// container that cannot reach the scanner would also answer. /ready
// additionally requires that profiles loaded and sane-runtime replied,
// which is the difference between "running" and "a scan would work".
func runHealthcheck(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("scan-bridge healthcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	url := fs.String("url", "http://127.0.0.1:8080/ready",
		"readiness endpoint to probe")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *url, nil)
	if err != nil {
		return fmt.Errorf("healthcheck: build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck: %s: %w", *url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The body is small and its content is the useful part of a
	// failure -- {"error":"sane_runtime_unreachable"} says which half
	// is down. Bounded anyway: a wrong --url could point at something
	// that streams.
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
	if readErr != nil {
		// Not fatal: the status code already decides ready vs not, and
		// a probe that failed only because the body could not be read
		// should still report the status it did get. But saying so
		// beats a silently empty reason, which reads as "the bridge
		// answered 503 with nothing to say".
		body = []byte("<body unreadable: " + readErr.Error() + ">")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: %s returned %d: %s",
			*url, resp.StatusCode, string(body))
	}

	_, _ = fmt.Fprintf(stdout, "ready: %s\n", string(body))
	return nil
}
