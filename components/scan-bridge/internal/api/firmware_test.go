package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/firmware"
)

// fakeMirror implements FirmwareMirror over a temp directory.
type fakeMirror struct {
	dir      string
	rel      firmware.Release
	cached   bool
	queued   bool
	triggers int
	// manifestErr models a cached release whose manifest cannot be
	// read or decoded — a 500, not a 404.
	manifestErr error
}

func (f *fakeMirror) Current() (firmware.Release, bool) {
	if !f.cached {
		return firmware.Release{}, false
	}
	return f.rel, true
}

func (f *fakeMirror) Open(name string) (io.ReadSeekCloser, time.Time, error) {
	if !f.cached {
		return nil, time.Time{}, firmware.ErrNotCached
	}
	for _, n := range f.rel.Files {
		if n == name {
			fh, err := os.Open(filepath.Join(f.dir, name))
			if err != nil {
				return nil, time.Time{}, err
			}
			return fh, time.Unix(0, 0), nil
		}
	}
	return nil, time.Time{}, firmware.ErrNotCached
}

func (f *fakeMirror) OpenAt(tag, name string) (io.ReadSeekCloser, time.Time, error) {
	if !f.cached || tag != f.rel.Tag {
		return nil, time.Time{}, firmware.ErrNotCached
	}
	return f.Open(name)
}

func (f *fakeMirror) Manifest() ([]byte, firmware.Release, error) {
	if !f.cached {
		return nil, firmware.Release{}, firmware.ErrNotCached
	}
	if f.manifestErr != nil {
		return nil, firmware.Release{}, f.manifestErr
	}
	body := fmt.Appendf(nil,
		`{"name":"CYD Scan Panel","version":%q,"builds":[{"ota":{"md5":"deadbeef","path":"/firmware/%s/cyd-scan-panel.ota.bin"}}]}`,
		f.rel.Tag, f.rel.Tag)
	return body, f.rel, nil
}

func (f *fakeMirror) TriggerRefresh() bool {
	f.triggers++
	return f.queued
}

func newFirmwareServer(t *testing.T, mirror FirmwareMirror) http.Handler {
	t.Helper()
	s := &Server{
		Logger:   slog.New(slog.DiscardHandler),
		Profiles: nil,
	}
	if mirror != nil {
		s.Firmware = mirror
	}
	return s.Router()
}

func newCachedMirror(t *testing.T) *fakeMirror {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"name":"CYD Scan Panel","version":"v1.2.3"}`
	if err := os.WriteFile(filepath.Join(dir, firmware.ManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cyd-scan-panel.ota.bin"), []byte("ota"), 0o644); err != nil {
		t.Fatalf("write ota: %v", err)
	}
	return &fakeMirror{
		dir:    dir,
		cached: true,
		queued: true,
		rel: firmware.Release{
			Tag:   "v1.2.3",
			Files: []string{"cyd-scan-panel.ota.bin", firmware.ManifestName},
		},
	}
}

func TestFirmwareManifestServed(t *testing.T) {
	h := newFirmwareServer(t, newCachedMirror(t))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/firmware/manifest.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if v := rec.Header().Get("X-Firmware-Version"); v != "v1.2.3" {
		t.Errorf("X-Firmware-Version = %q, want v1.2.3", v)
	}
	if !strings.Contains(rec.Body.String(), "CYD Scan Panel") {
		t.Errorf("body = %q, want the mirrored manifest", rec.Body.String())
	}
}

func TestFirmwareBinaryServed(t *testing.T) {
	h := newFirmwareServer(t, newCachedMirror(t))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/firmware/cyd-scan-panel.ota.bin", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
	if rec.Body.String() != "ota" {
		t.Errorf("body = %q, want the binary bytes", rec.Body.String())
	}
}

// The panel must not be locked out of updating by a token it does not
// have yet -- and there is nothing to protect: the bytes are a public
// release asset.
func TestFirmwareRoutesNeedNoBearerToken(t *testing.T) {
	h := newFirmwareServer(t, newCachedMirror(t))

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/firmware/manifest.json", http.StatusOK},
		{http.MethodPost, "/firmware/refresh", http.StatusAccepted},
	} {
		rec := httptest.NewRecorder()
		// No Authorization header at all.
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}

func TestFirmwareColdCacheIs503(t *testing.T) {
	h := newFirmwareServer(t, &fakeMirror{dir: t.TempDir()})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/firmware/manifest.json", nil))

	// 503, not 404: the panel should come back, not conclude the
	// endpoint does not exist.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "firmware_not_cached" {
		t.Errorf("error = %q, want firmware_not_cached", body.Error)
	}
}

func TestFirmwareUnknownFileIs404(t *testing.T) {
	h := newFirmwareServer(t, newCachedMirror(t))

	for _, name := range []string{"nope.bin", "state.json", "..%2Fstate.json"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/firmware/"+name, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /firmware/%s = %d, want 404", name, rec.Code)
		}
	}
}

func TestFirmwareRefreshReturnsImmediately(t *testing.T) {
	m := newCachedMirror(t)
	h := newFirmwareServer(t, m)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/firmware/refresh", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if m.triggers != 1 {
		t.Errorf("TriggerRefresh called %d times, want 1", m.triggers)
	}
	var body firmwareRefreshResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Queued {
		t.Error("queued = false, want true")
	}
	if body.CachedVersion != "v1.2.3" {
		t.Errorf("cached_version = %q, want v1.2.3", body.CachedVersion)
	}
}

// A second press while one refresh is already pending is still a 202 —
// the caller's request is satisfied either way.
func TestFirmwareRefreshAlreadyQueuedIsStill202(t *testing.T) {
	m := newCachedMirror(t)
	m.queued = false
	h := newFirmwareServer(t, m)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/firmware/refresh", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	var body firmwareRefreshResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Queued {
		t.Error("queued = true, want false when a refresh is already pending")
	}
}

// With the mirror disabled the routes must answer the project's uniform
// 501 envelope -- and, above all, must not panic. A nil *firmware.Mirror
// assigned into the interface field would make routes.go's nil check
// pass and crash here on the first request; main.go guards that, and
// this is the test that would notice if the guard went away.
func TestFirmwareRoutesDisabled(t *testing.T) {
	h := newFirmwareServer(t, nil)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/firmware/manifest.json"},
		{http.MethodPost, "/firmware/refresh"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s %s = %d, want 501", tc.method, tc.path, rec.Code)
		}
	}
}

// The manifest must send the panel to a version-qualified path, so the
// binary it downloads is the one the manifest it read describes even if
// a newer release lands in between.
func TestFirmwareManifestPointsAtTheVersionedRoute(t *testing.T) {
	h := newFirmwareServer(t, newCachedMirror(t))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/firmware/manifest.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if want := "/firmware/v1.2.3/cyd-scan-panel.ota.bin"; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("manifest body %q does not point at %q", rec.Body.String(), want)
	}
}

func TestFirmwareVersionedFileServed(t *testing.T) {
	h := newFirmwareServer(t, newCachedMirror(t))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/firmware/v1.2.3/cyd-scan-panel.ota.bin", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "ota" {
		t.Errorf("body = %q", rec.Body.String())
	}
	if v := rec.Header().Get("X-Firmware-Version"); v != "v1.2.3" {
		t.Errorf("X-Firmware-Version = %q, want v1.2.3", v)
	}
}

func TestFirmwareVersionedFileUnknownIs404(t *testing.T) {
	h := newFirmwareServer(t, newCachedMirror(t))

	for _, p := range []string{
		"/firmware/v9.9.9/cyd-scan-panel.ota.bin",
		"/firmware/v1.2.3/nope.bin",
		"/firmware/v1.2.3/..%2Fstate.json",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
}

func TestFirmwareManifestUnreadableIs500(t *testing.T) {
	m := newCachedMirror(t)
	m.manifestErr = errors.New("disk is on fire")
	h := newFirmwareServer(t, m)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/firmware/manifest.json", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
