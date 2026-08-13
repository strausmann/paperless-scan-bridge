package paperless

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations"
)

// stubSecrets builds a config.SecretResolver that resolves exactly the
// values in kv (upper-cased env-style lookup, matching how
// config.SecretResolver's env fallback behaves) and nothing else.
func stubSecrets(kv map[string]string) config.SecretResolver {
	return config.NewSecretResolver("", func(key string) (string, bool) {
		v, ok := kv[key]
		return v, ok
	})
}

// noSecrets is a resolver that never finds anything, standing in for a
// deployment where the paperless_api_token secret was never provisioned.
func noSecrets() config.SecretResolver {
	return config.NewSecretResolver("", func(string) (string, bool) { return "", false })
}

func profileCfg(base map[string]any) destinations.ProfileDestinationConfig {
	return destinations.ProfileDestinationConfig{Target: "paperless", Config: base}
}

func TestNewDestinationValidatesConfig(t *testing.T) {
	t.Parallel()

	t.Run("missing_base_url_is_rejected", func(t *testing.T) {
		t.Parallel()

		_, err := NewDestination(profileCfg(map[string]any{}), noSecrets())
		if err == nil {
			t.Fatal("NewDestination() with no base_url = nil error, want error")
		}
		if !errors.Is(err, ErrConfig) {
			t.Fatalf("NewDestination() error = %v, want errors.Is(err, ErrConfig)", err)
		}
	})

	t.Run("non_absolute_base_url_is_rejected", func(t *testing.T) {
		t.Parallel()

		_, err := NewDestination(profileCfg(map[string]any{"base_url": "not-a-url"}), noSecrets())
		if !errors.Is(err, ErrConfig) {
			t.Fatalf("NewDestination() error = %v, want errors.Is(err, ErrConfig)", err)
		}
	})

	t.Run("empty_token_secret_override_is_rejected", func(t *testing.T) {
		t.Parallel()

		_, err := NewDestination(profileCfg(map[string]any{
			"base_url":     "https://paperless.example.com",
			"token_secret": "",
		}), noSecrets())
		if !errors.Is(err, ErrConfig) {
			t.Fatalf("NewDestination() error = %v, want errors.Is(err, ErrConfig)", err)
		}
	})

	t.Run("valid_config_builds_a_named_destination", func(t *testing.T) {
		t.Parallel()

		dest, err := NewDestination(profileCfg(map[string]any{
			"base_url": "https://paperless.example.com",
		}), noSecrets())
		if err != nil {
			t.Fatalf("NewDestination() error = %v, want nil", err)
		}
		if got, want := dest.Name(), "paperless"; got != want {
			t.Fatalf("Name() = %q, want %q", got, want)
		}
	})
}

func TestPaperlessRegistersWithDestinationsRegistry(t *testing.T) {
	t.Parallel()

	names := destinations.Names()
	found := false
	for _, n := range names {
		if n == "paperless" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("destinations.Names() = %v, want it to contain %q (package init() must call Register)", names, "paperless")
	}

	dest, err := destinations.Build("paperless", profileCfg(map[string]any{
		"base_url": "https://paperless.example.com",
	}), noSecrets())
	if err != nil {
		t.Fatalf("destinations.Build(%q) error = %v, want nil", "paperless", err)
	}
	if got, want := dest.Name(), "paperless"; got != want {
		t.Fatalf("Build(%q).Name() = %q, want %q", "paperless", got, want)
	}
}

// parsedUpload is what the fake Paperless server decodes an incoming
// multipart post_document/ request into, for assertions.
type parsedUpload struct {
	authHeader   string
	documentName string
	documentBody []byte
	contentType  string
	fields       map[string][]string // multipart form fields, tags may repeat
}

func decodeUpload(t *testing.T, r *http.Request) parsedUpload {
	t.Helper()

	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("fake paperless: parse content-type: %v", err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		t.Fatalf("fake paperless: content-type = %q, want multipart/*", mediaType)
	}

	got := parsedUpload{
		authHeader: r.Header.Get("Authorization"),
		fields:     map[string][]string{},
	}

	mr := multipart.NewReader(r.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("fake paperless: read part: %v", err)
		}
		name := part.FormName()
		if name == "document" {
			got.documentName = part.FileName()
			got.contentType = part.Header.Get("Content-Type")
			b, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("fake paperless: read document part: %v", err)
			}
			got.documentBody = b
		} else {
			b, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("fake paperless: read field %q: %v", name, err)
			}
			got.fields[name] = append(got.fields[name], string(b))
		}
		_ = part.Close()
	}
	return got
}

func TestDeliverHappyPathSubmitsMultipartUpload(t *testing.T) {
	t.Parallel()

	const wantToken = "s3cr3t-token"
	var got parsedUpload
	var gotMethod, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		got = decodeUpload(t, r)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "a1b2c3d4-e5f6-7890-1234-567890abcdef"})
	}))
	t.Cleanup(srv.Close)

	dest, err := NewDestination(profileCfg(map[string]any{"base_url": srv.URL}), stubSecrets(map[string]string{
		"PAPERLESS_API_TOKEN": wantToken,
	}))
	if err != nil {
		t.Fatalf("NewDestination() error = %v", err)
	}

	created := time.Date(2026, 8, 13, 14, 32, 1, 0, time.UTC)
	correspondent := 12
	docType := 3
	asn := 42
	doc := destinations.Document{
		ID:          "scan-1",
		Filename:    "2026-08-13T14-32-01_receipt.pdf",
		Content:     strings.NewReader("%PDF-fake-bytes"),
		ContentType: "application/pdf",
		PageCount:   2,
		DocType:     "eingangsrechnung",
	}
	meta := destinations.Metadata{
		Title:         "Rechnung",
		Created:       &created,
		TagIDs:        []int{3, 7},
		Correspondent: &correspondent,
		DocumentType:  &docType,
		ASN:           &asn,
	}

	const wantTaskID = "a1b2c3d4-e5f6-7890-1234-567890abcdef"
	result, err := dest.Deliver(context.Background(), doc, meta, profileCfg(map[string]any{"base_url": srv.URL}))
	if err != nil {
		t.Fatalf("Deliver() error = %v, want nil", err)
	}
	if result.Status != "submitted" {
		t.Errorf("Deliver() result.Status = %q, want %q", result.Status, "submitted")
	}
	if result.Reference != wantTaskID {
		t.Errorf("Deliver() result.Reference = %q, want %q (Paperless's task_id)", result.Reference, wantTaskID)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/documents/post_document/" {
		t.Errorf("path = %q, want /api/documents/post_document/", gotPath)
	}
	if want := "Token " + wantToken; got.authHeader != want {
		t.Errorf("Authorization header = %q, want %q", got.authHeader, want)
	}
	if got.documentName != doc.Filename {
		t.Errorf("document filename = %q, want %q", got.documentName, doc.Filename)
	}
	if string(got.documentBody) != "%PDF-fake-bytes" {
		t.Errorf("document body = %q, want %q", got.documentBody, "%PDF-fake-bytes")
	}
	if got.contentType != "application/pdf" {
		t.Errorf("document content-type = %q, want application/pdf", got.contentType)
	}
	if want := []string{"Rechnung"}; !equalStrings(got.fields["title"], want) {
		t.Errorf("title field = %v, want %v", got.fields["title"], want)
	}
	if want := []string{"12"}; !equalStrings(got.fields["correspondent"], want) {
		t.Errorf("correspondent field = %v, want %v", got.fields["correspondent"], want)
	}
	if want := []string{"3"}; !equalStrings(got.fields["document_type"], want) {
		t.Errorf("document_type field = %v, want %v", got.fields["document_type"], want)
	}
	if want := []string{"42"}; !equalStrings(got.fields["archive_serial_number"], want) {
		t.Errorf("archive_serial_number field = %v, want %v", got.fields["archive_serial_number"], want)
	}
	if want := created.Format(time.RFC3339); !equalStrings(got.fields["created"], []string{want}) {
		t.Errorf("created field = %v, want %v", got.fields["created"], []string{want})
	}
	if want := []string{"3", "7"}; !equalStrings(got.fields["tags"], want) {
		t.Errorf("tags field = %v, want %v (repeated form values in order)", got.fields["tags"], want)
	}
}

func TestDeliverOmitsAbsentOptionalFields(t *testing.T) {
	t.Parallel()

	var got parsedUpload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = decodeUpload(t, r)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "task-1"})
	}))
	t.Cleanup(srv.Close)

	dest, err := NewDestination(profileCfg(map[string]any{"base_url": srv.URL}), stubSecrets(map[string]string{
		"PAPERLESS_API_TOKEN": "tok",
	}))
	if err != nil {
		t.Fatalf("NewDestination() error = %v", err)
	}

	doc := destinations.Document{Filename: "bare.pdf", Content: strings.NewReader("x"), ContentType: "application/pdf"}
	meta := destinations.Metadata{} // nothing set beyond zero values

	result, err := dest.Deliver(context.Background(), doc, meta, profileCfg(map[string]any{"base_url": srv.URL}))
	if err != nil {
		t.Fatalf("Deliver() error = %v, want nil", err)
	}
	if result.Reference != "task-1" {
		t.Errorf("Deliver() result.Reference = %q, want %q", result.Reference, "task-1")
	}

	for _, field := range []string{"title", "created", "correspondent", "document_type", "archive_serial_number", "tags"} {
		if _, present := got.fields[field]; present {
			t.Errorf("field %q present = true with zero-value Metadata, want absent", field)
		}
	}
}

func TestDeliverMissingTokenSecretFailsWithoutHTTPCall(t *testing.T) {
	t.Parallel()

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	dest, err := NewDestination(profileCfg(map[string]any{"base_url": srv.URL}), noSecrets())
	if err != nil {
		t.Fatalf("NewDestination() error = %v", err)
	}

	doc := destinations.Document{Filename: "x.pdf", Content: strings.NewReader("x"), ContentType: "application/pdf"}
	result, err := dest.Deliver(context.Background(), doc, destinations.Metadata{}, profileCfg(map[string]any{"base_url": srv.URL}))
	if err == nil {
		t.Fatal("Deliver() with unresolved token = nil error, want error")
	}
	if result != (destinations.DeliveryResult{}) {
		t.Errorf("Deliver() result = %+v, want zero value on error", result)
	}
	if called {
		t.Error("Deliver() made an HTTP call despite failing to resolve the token secret first")
	}
}

func TestDeliverErrorPathsFromPaperless(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    error
		wantSubstr string
	}{
		{
			name:       "400_bad_request_is_rejected_upload",
			status:     http.StatusBadRequest,
			body:       `{"document":["Unsupported file type."]}`,
			wantErr:    ErrRejected,
			wantSubstr: "400",
		},
		{
			name:       "401_unauthorized_is_rejected_upload",
			status:     http.StatusUnauthorized,
			body:       `{"detail":"Invalid token."}`,
			wantErr:    ErrRejected,
			wantSubstr: "401",
		},
		{
			name:       "500_server_error",
			status:     http.StatusInternalServerError,
			body:       `Internal Server Error`,
			wantErr:    ErrServerError,
			wantSubstr: "500",
		},
		{
			name:       "503_service_unavailable_is_server_error",
			status:     http.StatusServiceUnavailable,
			body:       ``,
			wantErr:    ErrServerError,
			wantSubstr: "503",
		},
		{
			name:       "200_with_empty_task_id_is_invalid_response",
			status:     http.StatusOK,
			body:       `{"task_id":""}`,
			wantErr:    ErrInvalidResponse,
			wantSubstr: "task_id",
		},
		{
			name:       "200_with_malformed_json_is_invalid_response",
			status:     http.StatusOK,
			body:       `not-json`,
			wantErr:    ErrInvalidResponse,
			wantSubstr: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = decodeUpload(t, r) // drain the body so the client doesn't see a broken pipe
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			dest, err := NewDestination(profileCfg(map[string]any{"base_url": srv.URL}), stubSecrets(map[string]string{
				"PAPERLESS_API_TOKEN": "tok",
			}))
			if err != nil {
				t.Fatalf("NewDestination() error = %v", err)
			}

			doc := destinations.Document{Filename: "x.pdf", Content: strings.NewReader("x"), ContentType: "application/pdf"}
			_, err = dest.Deliver(context.Background(), doc, destinations.Metadata{}, profileCfg(map[string]any{"base_url": srv.URL}))
			if err == nil {
				t.Fatalf("Deliver() = nil error, want error wrapping %v", tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Deliver() error = %v, want errors.Is(err, %v)", err, tc.wantErr)
			}
			if tc.wantSubstr != "" && !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("Deliver() error = %q, want it to contain %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestDeliverNetworkErrorUnreachable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("fake paperless: handler invoked, want the connection to fail before reaching it")
	}))
	baseURL := srv.URL
	srv.Close() // closed before any request — deterministic connection-refused, no server ever answers

	dest, err := NewDestination(profileCfg(map[string]any{"base_url": baseURL}), stubSecrets(map[string]string{
		"PAPERLESS_API_TOKEN": "tok",
	}))
	if err != nil {
		t.Fatalf("NewDestination() error = %v", err)
	}

	doc := destinations.Document{Filename: "x.pdf", Content: strings.NewReader("x"), ContentType: "application/pdf"}
	_, err = dest.Deliver(context.Background(), doc, destinations.Metadata{}, profileCfg(map[string]any{"base_url": baseURL}))
	if err == nil {
		t.Fatal("Deliver() against a closed server = nil error, want error")
	}
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Deliver() error = %v, want errors.Is(err, ErrUnreachable)", err)
	}
}

func TestDeliverContextDeadlineExceededIsTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "irrelevant"})
	}))
	t.Cleanup(srv.Close)

	dest, err := NewDestination(profileCfg(map[string]any{"base_url": srv.URL}), stubSecrets(map[string]string{
		"PAPERLESS_API_TOKEN": "tok",
	}))
	if err != nil {
		t.Fatalf("NewDestination() error = %v", err)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second)) // already expired
	defer cancel()

	doc := destinations.Document{Filename: "x.pdf", Content: strings.NewReader("x"), ContentType: "application/pdf"}
	_, err = dest.Deliver(ctx, doc, destinations.Metadata{}, profileCfg(map[string]any{"base_url": srv.URL}))
	if err == nil {
		t.Fatal("Deliver() with an expired context = nil error, want error")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Deliver() error = %v, want errors.Is(err, ErrTimeout)", err)
	}
}

func TestDecodeConfigDefaultsTokenSecretName(t *testing.T) {
	t.Parallel()

	cfg, err := decodeConfig(map[string]any{"base_url": "https://paperless.example.com/"})
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}
	if got, want := cfg.TokenSecretName, "paperless_api_token"; got != want {
		t.Fatalf("TokenSecretName = %q, want %q (default)", got, want)
	}
	if got, want := cfg.BaseURL, "https://paperless.example.com"; got != want {
		t.Fatalf("BaseURL = %q, want %q (trailing slash trimmed)", got, want)
	}
}

func TestDecodeConfigHonorsTokenSecretOverride(t *testing.T) {
	t.Parallel()

	cfg, err := decodeConfig(map[string]any{
		"base_url":     "https://paperless.example.com",
		"token_secret": "custom_secret_name",
	})
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}
	if got, want := cfg.TokenSecretName, "custom_secret_name"; got != want {
		t.Fatalf("TokenSecretName = %q, want %q", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
