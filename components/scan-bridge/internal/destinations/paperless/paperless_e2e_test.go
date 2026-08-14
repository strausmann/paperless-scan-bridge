//go:build integration

// This file is a permanent, opt-in integration test against a REAL
// Paperless-ngx instance — it never runs as part of the normal
// `go test ./...` suite (the "integration" build tag excludes it by
// default) and is skipped even when built with that tag unless both
// PAPERLESS_E2E_BASE_URL and PAPERLESS_API_TOKEN are set. Its purpose
// is to catch exactly the class of bug this file's sibling
// (paperless.go's decodeTaskID) fixes: a mismatch between what this
// package assumes Paperless's API returns and what it actually
// returns. httptest-based unit tests can only be as correct as the
// fakes they hand-write; this test asks the real thing.
//
// Run it with:
//
//	PAPERLESS_E2E_BASE_URL=http://100.100.50.42:28080 \
//	PAPERLESS_API_TOKEN=<token> \
//	GOTOOLCHAIN=auto go test -tags integration -run TestDeliverAgainstLivePaperlessInstance \
//	  ./internal/destinations/paperless/ -v
package paperless

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations"
)

// minimalOnePagePDF is a hand-built, valid, single-page PDF — the same
// minimal shape (%PDF-1.4, one Catalog/Pages/Page object chain, 200x200
// MediaBox) already verified to be accepted and consumed by a real
// Paperless-ngx v3.0.5 instance during this bug's investigation. It
// deliberately has no page content stream (an empty /Resources page is
// enough for Paperless's consumer to accept and index the file — OCR
// on a blank page finds no text, which is fine for this test's
// purpose: proving the upload+consume round trip, not OCR accuracy).
const minimalOnePagePDF = `%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> >>
endobj
xref
0 4
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
trailer
<< /Size 4 /Root 1 0 R >>
startxref
203
%%EOF
`

// documentsListResponse is the shape of Paperless-ngx's
// GET /api/documents/ list endpoint — only the fields this test needs
// (verified against .claude/skills/paperless-ngx/references/api-endpoints.md
// in the homelab-management repo, "Response" example under the
// documents-list endpoint).
type documentsListResponse struct {
	Count int `json:"count"`
}

// TestDeliverAgainstLivePaperlessInstance uploads a minimal real PDF to
// a real Paperless-ngx instance via Deliver, then polls the documents
// list until the uploaded document is actually consumed and
// searchable — proving both halves of the round trip: (1) Paperless
// accepted the multipart upload and returned a task_id this package
// could decode (the bug this file's PR fixes), and (2) that task_id
// corresponds to a real, asynchronously-consumed document, not just an
// HTTP 200 that this package happened to parse.
func TestDeliverAgainstLivePaperlessInstance(t *testing.T) {
	baseURL := os.Getenv("PAPERLESS_E2E_BASE_URL")
	token := os.Getenv("PAPERLESS_API_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("PAPERLESS_E2E_BASE_URL and/or PAPERLESS_API_TOKEN not set — skipping live Paperless integration test")
	}

	// Env-based SecretResolver, matching how scan-bridge itself wires
	// SecretResolver in production (Docker secret file first, env var
	// fallback) — here dir="" so only the env fallback is exercised.
	secrets := config.NewSecretResolver("", os.LookupEnv)

	dest, err := destinations.Build("paperless", destinations.ProfileDestinationConfig{
		Target: "paperless",
		Config: map[string]any{"base_url": baseURL},
	}, secrets)
	if err != nil {
		t.Fatalf("destinations.Build(%q) error = %v", "paperless", err)
	}

	title := fmt.Sprintf("scan-bridge-e2e-%d", time.Now().UnixNano())
	doc := destinations.Document{
		ID:          "e2e-" + title,
		Filename:    title + ".pdf",
		Content:     strings.NewReader(minimalOnePagePDF),
		ContentType: "application/pdf",
		PageCount:   1,
	}
	meta := destinations.Metadata{Title: title}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := dest.Deliver(ctx, doc, meta, destinations.ProfileDestinationConfig{
		Target: "paperless",
		Config: map[string]any{"base_url": baseURL},
	})
	if err != nil {
		t.Fatalf("Deliver() against live Paperless instance error = %v", err)
	}
	if result.Status != "submitted" {
		t.Errorf("Deliver() result.Status = %q, want %q", result.Status, "submitted")
	}
	if result.Reference == "" {
		t.Fatal("Deliver() result.Reference is empty, want a non-empty task_id from the real API")
	}
	t.Logf("Deliver() accepted upload, task_id=%s, polling for consumption of title=%q", result.Reference, title)

	if err := pollUntilDocumentIndexed(ctx, baseURL, token, title, 90*time.Second); err != nil {
		t.Fatalf("document %q was never indexed by Paperless: %v", title, err)
	}
}

// pollUntilDocumentIndexed polls Paperless's documents list, filtered
// by title, until at least one match appears or timeout elapses —
// proving the task_id Deliver returned corresponds to a real,
// asynchronously consumed document, not just an accepted-but-abandoned
// upload.
func pollUntilDocumentIndexed(ctx context.Context, baseURL, token, title string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 10 * time.Second}
	endpoint := strings.TrimRight(baseURL, "/") + "/api/documents/?title__icontains=" + url.QueryEscape(title)

	backoff := 500 * time.Millisecond
	const maxBackoff = 5 * time.Second

	for {
		count, err := fetchDocumentCount(ctx, client, endpoint, token)
		if err == nil && count >= 1 {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("last poll error: %w", err)
			}
			return fmt.Errorf("count still 0 after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// fetchDocumentCount performs one GET against endpoint and returns the
// decoded "count" field. The token is injected only into the request
// header, never logged or included in any returned error.
func fetchDocumentCount(ctx context.Context, client *http.Client, endpoint, token string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+token)

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET documents list: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GET documents list: unexpected status %d", resp.StatusCode)
	}

	var parsed documentsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("decode documents list response: %w", err)
	}
	return parsed.Count, nil
}
