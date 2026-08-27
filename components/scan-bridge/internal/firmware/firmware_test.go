package firmware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGitHub serves a releases/latest payload plus the assets it
// names, from an in-memory set of files. The tests mutate it between
// refreshes to model a new release landing upstream.
type fakeGitHub struct {
	t   *testing.T
	srv *httptest.Server

	tag    string
	assets map[string][]byte
	// sums, when non-nil, replaces the generated SHA256SUMS body — the
	// hook the corruption and traversal tests use.
	sums []byte
	// omitFromRelease drops a name from the API's asset list while
	// leaving it in SHA256SUMS.
	omitFromRelease string

	downloads atomic.Int64
	apiCalls  atomic.Int64
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{t: t, tag: "v1.0.0", assets: map[string][]byte{
		ManifestName:                 []byte(`{"name":"CYD Scan Panel","version":"v1.0.0"}`),
		"cyd-scan-panel.ota.bin":     []byte("ota-image-bytes"),
		"cyd-scan-panel.factory.bin": []byte("factory-image-bytes"),
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/{owner}/{repo}/releases/latest", g.handleLatest)
	mux.HandleFunc("GET /download/{name}", g.handleDownload)
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

// checksums renders the SHA256SUMS body the way the release workflow
// does: `sha256sum ./*.bin ./manifest.json`, i.e. names prefixed "./".
func (g *fakeGitHub) checksums() []byte {
	if g.sums != nil {
		return g.sums
	}
	var b strings.Builder
	for name, body := range g.assets {
		sum := sha256.Sum256(body)
		fmt.Fprintf(&b, "%s  ./%s\n", hex.EncodeToString(sum[:]), name)
	}
	return []byte(b.String())
}

func (g *fakeGitHub) handleLatest(w http.ResponseWriter, _ *http.Request) {
	g.apiCalls.Add(1)

	type asset struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}
	payload := struct {
		TagName string  `json:"tag_name"`
		HTMLURL string  `json:"html_url"`
		Assets  []asset `json:"assets"`
	}{
		TagName: g.tag,
		HTMLURL: "https://example.invalid/releases/" + g.tag,
	}
	for name := range g.assets {
		if name == g.omitFromRelease {
			continue
		}
		payload.Assets = append(payload.Assets, asset{
			Name: name, URL: g.srv.URL + "/download/" + name,
		})
	}
	payload.Assets = append(payload.Assets, asset{
		Name: ChecksumsName, URL: g.srv.URL + "/download/" + ChecksumsName,
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		g.t.Errorf("encode release payload: %v", err)
	}
}

func (g *fakeGitHub) handleDownload(w http.ResponseWriter, r *http.Request) {
	g.downloads.Add(1)
	name := r.PathValue("name")
	if name == ChecksumsName {
		_, _ = w.Write(g.checksums())
		return
	}
	body, ok := g.assets[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(body)
}

func newTestMirror(t *testing.T, g *fakeGitHub) *Mirror {
	t.Helper()
	m, err := New(Options{
		CacheDir:   t.TempDir(),
		Repo:       "strausmann/paperless-scan-bridge",
		APIBase:    g.srv.URL,
		HTTPClient: g.srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func TestRefreshMirrorsAndServes(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)

	rel, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if rel.Tag != "v1.0.0" {
		t.Errorf("tag = %q, want v1.0.0", rel.Tag)
	}
	if rel.ReleaseURL == "" {
		t.Error("release URL not carried through from the API payload")
	}

	// Every file SHA256SUMS named must be readable. This is the
	// package's central invariant, from the caller's side: the manifest
	// may only ever name files the mirror can actually serve.
	for _, name := range rel.Files {
		f, _, err := m.Open(name)
		if err != nil {
			t.Fatalf("Open(%q) after refresh: %v", name, err)
		}
		_ = f.Close()
	}
	if !contains(rel.Files, ManifestName) {
		t.Errorf("files = %v, must contain %s", rel.Files, ManifestName)
	}
}

func TestRefreshIsNoOpWhenTagUnchanged(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)

	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	after := g.downloads.Load()

	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if g.downloads.Load() != after {
		t.Errorf("second refresh downloaded %d extra files; an unchanged tag must cost one API call and nothing else",
			g.downloads.Load()-after)
	}
	if g.apiCalls.Load() != 2 {
		t.Errorf("apiCalls = %d, want 2", g.apiCalls.Load())
	}
}

// The regression that motivates the whole staging dance: a corrupted
// download must not become the served release, and must not damage the
// release already being served.
func TestRefreshWithBadChecksumKeepsPreviousRelease(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)

	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	// A new release upstream whose SHA256SUMS does not describe its
	// own binary.
	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("new-ota-image-bytes")
	good := g.checksums()
	g.sums = []byte(strings.Replace(string(good),
		hex.EncodeToString(hashOf("new-ota-image-bytes")),
		strings.Repeat("00", sha256.Size), 1))

	if _, err := m.Refresh(t.Context()); err == nil {
		t.Fatal("Refresh accepted a file whose checksum does not match SHA256SUMS")
	}

	cur, ok := m.Current()
	if !ok {
		t.Fatal("a failed refresh took the served release away")
	}
	if cur.Tag != "v1.0.0" {
		t.Errorf("served tag = %q, want the previous v1.0.0", cur.Tag)
	}

	// And the failed tag must have left nothing behind that a later
	// Load could adopt.
	if _, err := os.Stat(filepath.Join(m.cacheDir, "v2.0.0")); !os.IsNotExist(err) {
		t.Errorf("v2.0.0 directory survived a failed refresh: %v", err)
	}
}

func TestRefreshRejectsChecksumEntryWithoutAsset(t *testing.T) {
	g := newFakeGitHub(t)
	g.omitFromRelease = "cyd-scan-panel.factory.bin"
	m := newTestMirror(t, g)

	_, err := m.Refresh(t.Context())
	if err == nil {
		t.Fatal("Refresh accepted a SHA256SUMS entry with no matching release asset")
	}
	if _, ok := m.Current(); ok {
		t.Error("a release the mirror could not fully download became current")
	}
}

func TestRefreshRejectsReleaseWithoutManifest(t *testing.T) {
	g := newFakeGitHub(t)
	delete(g.assets, ManifestName)
	m := newTestMirror(t, g)

	if _, err := m.Refresh(t.Context()); err == nil {
		t.Fatal("Refresh accepted a release with no manifest.json")
	}
}

// A hostile SHA256SUMS must not be able to choose where a download
// lands.
//
// The release is rigged so that validateAssetName is the ONLY thing
// that can stop it: the traversal name is a real, downloadable asset
// with a correct checksum, and manifest.json is present alongside it so
// the "release lists no manifest" guard does not fire first. Two
// earlier versions of this test passed with validation removed, each
// for a different unrelated reason -- so the assertions below check the
// cache directory is untouched rather than just that some error came
// back.
func TestRefreshRejectsTraversalInChecksums(t *testing.T) {
	for _, name := range []string{"../evil.bin", "/etc/passwd", "sub/dir.bin", ".hidden"} {
		t.Run(name, func(t *testing.T) {
			g := newFakeGitHub(t)
			manifest := []byte(`{"name":"CYD Scan Panel"}`)
			payload := []byte("payload")
			g.assets = map[string][]byte{ManifestName: manifest, name: payload}
			g.sums = fmt.Appendf(nil, "%s  %s\n%s  %s\n",
				hex.EncodeToString(hashOf(string(payload))), name,
				hex.EncodeToString(hashOf(string(manifest))), ManifestName)
			m := newTestMirror(t, g)

			if _, err := m.Refresh(t.Context()); err == nil {
				t.Fatalf("Refresh accepted %q as an asset name", name)
			}
			if _, ok := m.Current(); ok {
				t.Errorf("a release naming %q became current", name)
			}
			// A rejected refresh leaves nothing behind: no published
			// tag directory, no state file, and no file the traversal
			// wrote next to them.
			entries, err := os.ReadDir(m.cacheDir)
			if err != nil {
				t.Fatalf("ReadDir: %v", err)
			}
			for _, e := range entries {
				t.Errorf("cache dir holds %q after a rejected refresh", e.Name())
			}
		})
	}
}

func TestOpenRejectsNamesOutsideTheRelease(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	for _, name := range []string{"../state.json", "state.json", "nope.bin", "", "."} {
		if _, _, err := m.Open(name); err == nil {
			t.Errorf("Open(%q) succeeded; only files of the cached release may be served", name)
		}
	}
}

func TestOpenOnColdCache(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, _, err := m.Open(ManifestName); err == nil {
		t.Error("Open succeeded on a cold cache")
	}
	if _, ok := m.Current(); ok {
		t.Error("Current reported a release on a cold cache")
	}
}

func TestLoadAdoptsAnExistingCache(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// A second Mirror over the same directory models a restart.
	restarted, err := New(Options{CacheDir: m.cacheDir, APIBase: g.srv.URL, HTTPClient: g.srv.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := restarted.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	cur, ok := restarted.Current()
	if !ok || cur.Tag != "v1.0.0" {
		t.Fatalf("Current() = %+v, %v; a restart must keep serving the cached release", cur, ok)
	}
	f, _, err := restarted.Open(ManifestName)
	if err != nil {
		t.Fatalf("Open after Load: %v", err)
	}
	_ = f.Close()
}

func TestLoadRejectsAnIncompleteCache(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// Remove a file the state file still claims.
	if err := os.Remove(filepath.Join(m.cacheDir, "v1.0.0", "cyd-scan-panel.ota.bin")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	restarted, err := New(Options{CacheDir: m.cacheDir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := restarted.Load(); err == nil {
		t.Fatal("Load adopted a cache whose files are missing; the manifest would then name a file that 404s")
	}
	if _, ok := restarted.Current(); ok {
		t.Error("an unusable cache became current")
	}
}

func TestLoadOnEmptyCacheDirIsNotAnError(t *testing.T) {
	m, err := New(Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Errorf("Load on a cold cache = %v, want nil", err)
	}
}

func TestRefreshPrunesTheOldRelease(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("newer-ota-image-bytes")
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	if _, err := os.Stat(filepath.Join(m.cacheDir, "v1.0.0")); !os.IsNotExist(err) {
		t.Errorf("v1.0.0 not pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.cacheDir, "v2.0.0")); err != nil {
		t.Errorf("v2.0.0 missing after refresh: %v", err)
	}
}

// TriggerRefresh must never block its caller: the panel's POST reaches
// this through a synchronous http_request on its main loop, and a wait
// there is a watchdog reboot.
func TestTriggerRefreshCoalescesAndNeverBlocks(t *testing.T) {
	m, err := New(Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan bool, 2)
	go func() {
		done <- m.TriggerRefresh()
		done <- m.TriggerRefresh()
	}()

	var results []bool
	for range 2 {
		select {
		case v := <-done:
			results = append(results, v)
		case <-time.After(2 * time.Second):
			t.Fatal("TriggerRefresh blocked")
		}
	}
	if !results[0] {
		t.Error("first TriggerRefresh did not queue")
	}
	if results[1] {
		t.Error("second TriggerRefresh queued a duplicate instead of coalescing")
	}
}

func TestRunRefreshesOnTriggerAndStopsWithContext(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	m.interval = time.Hour // only the trigger should fire during the test

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() { m.Run(ctx); close(stopped) }()

	// Run refreshes once immediately.
	waitFor(t, func() bool {
		_, ok := m.Current()
		return ok
	}, "initial refresh")

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("triggered-ota-bytes")
	m.TriggerRefresh()
	waitFor(t, func() bool {
		cur, ok := m.Current()
		return ok && cur.Tag == "v2.0.0"
	}, "triggered refresh")

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

func TestRefreshRejectsUnusableTag(t *testing.T) {
	for _, tag := range []string{"", ".", "..", "../escape", "v1/2", ".hidden", stateFile} {
		t.Run(tag, func(t *testing.T) {
			g := newFakeGitHub(t)
			g.tag = tag
			m := newTestMirror(t, g)
			if _, err := m.Refresh(t.Context()); err == nil {
				t.Fatalf("Refresh accepted tag %q as a cache directory name", tag)
			}
		})
	}
}

func TestParseChecksums(t *testing.T) {
	valid := hex.EncodeToString(hashOf("x"))

	tests := []struct {
		name    string
		body    string
		want    []string
		wantErr bool
	}{
		{
			name: "workflow format with ./ prefix",
			body: valid + "  ./cyd-scan-panel.ota.bin\n" + valid + "  ./manifest.json\n",
			want: []string{"cyd-scan-panel.ota.bin", "manifest.json"},
		},
		{
			name: "binary-mode asterisk",
			body: valid + " *manifest.json\n",
			want: []string{"manifest.json"},
		},
		{
			name: "blank lines ignored",
			body: "\n\n" + valid + "  manifest.json\n\n",
			want: []string{"manifest.json"},
		},
		{name: "empty", body: "", wantErr: true},
		{name: "no digest", body: "not-a-digest  manifest.json\n", wantErr: true},
		{name: "short digest", body: "abcd  manifest.json\n", wantErr: true},
		{name: "no name", body: valid + "\n", wantErr: true},
		{name: "duplicate name", body: valid + "  a.bin\n" + valid + "  a.bin\n", wantErr: true},
		{name: "traversal", body: valid + "  ../a.bin\n", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sums, names, err := parseChecksums(strings.NewReader(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseChecksums(%q) = %v, want error", tc.body, names)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseChecksums: %v", err)
			}
			if len(names) != len(tc.want) {
				t.Fatalf("names = %v, want %v", names, tc.want)
			}
			for i, n := range tc.want {
				if names[i] != n {
					t.Errorf("names[%d] = %q, want %q", i, names[i], n)
				}
				if sums[n] != valid {
					t.Errorf("sums[%q] = %q, want %q", n, sums[n], valid)
				}
			}
		})
	}
}

func TestNewRequiresCacheDir(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("New accepted an empty cache dir")
	}
}

func hashOf(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
