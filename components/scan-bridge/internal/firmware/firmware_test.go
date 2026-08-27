package firmware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	// mu guards tag/assets/sums. Only the concurrency test below needs
	// it; every other test mutates them between requests.
	mu sync.Mutex
}

// setRelease swaps the served release atomically, giving the binary a
// content that is derivable from the tag so a reader can check that the
// bytes and the reported generation agree.
func (g *fakeGitHub) setRelease(tag string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tag = tag
	g.assets["cyd-scan-panel.ota.bin"] = otaBytesFor(tag)
}

func otaBytesFor(tag string) []byte { return []byte("ota-image-bytes-of-" + tag) }

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	// The manifest matches the shape .github/workflows/esphome-firmware.yml
	// actually writes, so a test that changes it is changing something
	// real rather than a placeholder.
	g := &fakeGitHub{t: t, tag: "v1.0.0", assets: map[string][]byte{
		ManifestName: []byte(`{"name":"CYD Scan Panel","version":"v1.0.0","new_install_prompt_erase":true,` +
			`"builds":[{"chipFamily":"ESP32",` +
			`"parts":[{"path":"cyd-scan-panel.factory.bin","offset":0}],` +
			`"ota":{"md5":"0123456789abcdef0123456789abcdef","path":"cyd-scan-panel.ota.bin",` +
			`"summary":"Release v1.0.0","release_url":"https://example.invalid/r/v1.0.0"}}]}`),
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
// checksums must be called with g.mu held.
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
	g.mu.Lock()
	defer g.mu.Unlock()

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
	g.mu.Lock()
	defer g.mu.Unlock()
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
		CacheDir: t.TempDir(),
		Repo:     "strausmann/paperless-scan-bridge",
		APIBase:  g.srv.URL,
		// Negative disables the API-call floor. Every test that wants
		// two refreshes in a row would otherwise get the second one
		// throttled away; TestRefreshThrottlesRepeatedCalls covers the
		// floor itself.
		MinRefreshInterval: -1,
		HTTPClient:         g.srv.Client(),
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
		f, from, _, err := m.Open(name)
		if err != nil {
			t.Fatalf("Open(%q) after refresh: %v", name, err)
		}
		if from.Tag != rel.Tag {
			t.Errorf("Open(%q) reported generation %q, want %q", name, from.Tag, rel.Tag)
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
		if _, _, _, err := m.Open(name); err == nil {
			t.Errorf("Open(%q) succeeded; only files of the cached release may be served", name)
		}
	}
}

func TestOpenOnColdCache(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, _, _, err := m.Open(ManifestName); err == nil {
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
	restarted, err := New(Options{
		CacheDir: m.cacheDir, APIBase: g.srv.URL,
		MinRefreshInterval: -1, HTTPClient: g.srv.Client(),
	})
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
	f, from, _, err := restarted.Open(ManifestName)
	if err != nil {
		t.Fatalf("Open after Load: %v", err)
	}
	if from.Tag != "v1.0.0" {
		t.Errorf("Open reported generation %q after Load", from.Tag)
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

// The previous generation is deliberately RETAINED, not pruned. An
// earlier version of this test asserted the opposite, before it was
// clear that a panel installs when a person clicks -- possibly hours
// after it read the manifest -- and carries the MD5 from that read. See
// generationsKept. Eviction is covered by
// TestPruneKeepsExactlyTwoGenerations.
func TestRefreshKeepsThePreviousRelease(t *testing.T) {
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

	for _, tag := range []string{"v1.0.0", "v2.0.0"} {
		if _, err := os.Stat(filepath.Join(m.cacheDir, tag)); err != nil {
			t.Errorf("%s missing after the second refresh: %v", tag, err)
		}
	}
	if cur, _ := m.Current(); cur.Tag != "v2.0.0" {
		t.Errorf("served tag = %q, want v2.0.0", cur.Tag)
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

// The unauthenticated refresh route means anyone on the LAN can ask for
// a check as often as they like. Coalescing does not bound that -- once
// Run takes the queued token the next call queues behind it -- so the
// floor lives here, at the only place that makes an outbound call.
// Without it a persistent caller exhausts GitHub's anonymous
// 60-per-hour quota and stops real updates arriving.
func TestRefreshThrottlesRepeatedCalls(t *testing.T) {
	g := newFakeGitHub(t)
	m, err := New(Options{
		CacheDir:           t.TempDir(),
		APIBase:            g.srv.URL,
		MinRefreshInterval: time.Hour,
		HTTPClient:         g.srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	// A new release upstream: without the floor this would be fetched.
	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("newer-ota-image-bytes")

	var throttled *ThrottledError
	for range 20 {
		_, err := m.Refresh(t.Context())
		if !errors.As(err, &throttled) {
			t.Fatalf("throttled Refresh = %v, want a *ThrottledError", err)
		}
		if throttled.RetryAfter <= 0 || throttled.RetryAfter > time.Hour {
			t.Errorf("RetryAfter = %v, want it inside the floor", throttled.RetryAfter)
		}
	}
	if got := g.apiCalls.Load(); got != 1 {
		t.Errorf("apiCalls = %d after 21 refreshes inside the floor, want 1", got)
	}
	cur, _ := m.Current()
	if cur.Tag != "v1.0.0" {
		t.Errorf("served tag = %q; a throttled refresh must leave the cache alone", cur.Tag)
	}
}

// A failed attempt still counts against the floor. Otherwise a caller
// who can make the mirror fail -- by any means, including simply being
// faster than the network -- could make it retry without limit.
func TestRefreshThrottleCountsFailedAttempts(t *testing.T) {
	g := newFakeGitHub(t)
	g.sums = []byte("not a checksum file\n")
	m, err := New(Options{
		CacheDir:           t.TempDir(),
		APIBase:            g.srv.URL,
		MinRefreshInterval: time.Hour,
		HTTPClient:         g.srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := m.Refresh(t.Context()); err == nil {
		t.Fatal("Refresh accepted a malformed SHA256SUMS")
	}
	for range 5 {
		_, _ = m.Refresh(t.Context())
	}
	if got := g.apiCalls.Load(); got != 1 {
		t.Errorf("apiCalls = %d; a failed attempt must still count against the floor", got)
	}
}

// The manifest the bridge publishes must point at the generation it
// describes, not at "whatever is newest when the panel gets round to
// installing".
func TestManifestRewritesOtaPathToTheVersionedRoute(t *testing.T) {
	g := newFakeGitHub(t)
	g.assets[ManifestName] = []byte(`{"name":"CYD Scan Panel","version":"v1.0.0","builds":[{"chipFamily":"ESP32","parts":[{"path":"cyd-scan-panel.factory.bin","offset":0}],"ota":{"md5":"deadbeef","path":"cyd-scan-panel.ota.bin","summary":"Release v1.0.0"}}]}`)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	body, rel, err := m.Manifest()
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if rel.Tag != "v1.0.0" {
		t.Errorf("release tag = %q", rel.Tag)
	}

	var doc struct {
		Builds []struct {
			Parts []struct {
				Path string `json:"path"`
			} `json:"parts"`
			OTA struct {
				MD5  string `json:"md5"`
				Path string `json:"path"`
			} `json:"ota"`
		} `json:"builds"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode published manifest: %v", err)
	}
	if len(doc.Builds) != 1 {
		t.Fatalf("builds = %d, want 1", len(doc.Builds))
	}
	if want := "/firmware/v1.0.0/cyd-scan-panel.ota.bin"; doc.Builds[0].OTA.Path != want {
		t.Errorf("ota.path = %q, want %q", doc.Builds[0].OTA.Path, want)
	}
	// The digest is CI's, computed from the binary it shipped. Rewriting
	// it is what ADR 0024 forbids.
	if doc.Builds[0].OTA.MD5 != "deadbeef" {
		t.Errorf("ota.md5 = %q, want it untouched", doc.Builds[0].OTA.MD5)
	}
	// parts is read by ESP Web Tools against the docs site, never here.
	if doc.Builds[0].Parts[0].Path != "cyd-scan-panel.factory.bin" {
		t.Errorf("parts[0].path = %q, want it left relative", doc.Builds[0].Parts[0].Path)
	}
}

func TestManifestOnColdCache(t *testing.T) {
	m, err := New(Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := m.Manifest(); !errors.Is(err, ErrNotCached) {
		t.Errorf("Manifest on a cold cache = %v, want ErrNotCached", err)
	}
}

// The regression the versioned route and the retained generation exist
// for: a panel reads the manifest, a newer release lands, and only then
// does someone press install. The URL it captured must still return the
// bytes that manifest's MD5 describes.
func TestPreviousGenerationStaysServableAfterANewRelease(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	oldBytes := string(g.assets["cyd-scan-panel.ota.bin"])

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("second-generation-ota-bytes")
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	f, _, err := m.OpenAt("v1.0.0", "cyd-scan-panel.ota.bin")
	if err != nil {
		t.Fatalf("OpenAt on the previous generation: %v", err)
	}
	got, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != oldBytes {
		t.Errorf("previous generation returned %q, want %q", got, oldBytes)
	}

	// The current one is obviously still there, and is a different file.
	f, _, err = m.OpenAt("v2.0.0", "cyd-scan-panel.ota.bin")
	if err != nil {
		t.Fatalf("OpenAt on the current generation: %v", err)
	}
	_ = f.Close()
}

// Two generations, not more: the third release evicts the first.
func TestPruneKeepsExactlyTwoGenerations(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)

	for i, tag := range []string{"v1.0.0", "v2.0.0", "v3.0.0"} {
		g.tag = tag
		g.assets["cyd-scan-panel.ota.bin"] = fmt.Appendf(nil, "ota-generation-%d", i)
		if _, err := m.Refresh(t.Context()); err != nil {
			t.Fatalf("Refresh %s: %v", tag, err)
		}
		// os.ReadDir orders by name and prune orders by mtime; a
		// same-second mtime would make the choice arbitrary.
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := os.Stat(filepath.Join(m.cacheDir, "v1.0.0")); !os.IsNotExist(err) {
		t.Errorf("v1.0.0 survived two later releases: %v", err)
	}
	for _, tag := range []string{"v2.0.0", "v3.0.0"} {
		if _, err := os.Stat(filepath.Join(m.cacheDir, tag)); err != nil {
			t.Errorf("%s missing: %v", tag, err)
		}
	}
}

func TestOpenAtRejectsTraversal(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	cases := []struct{ tag, name string }{
		{"..", "state.json"},
		{"../..", "passwd"},
		{"v1.0.0", "../state.json"},
		{"v1.0.0", "../../etc/passwd"},
		{".", "state.json"},
		{"v1.0.0", ""},
		{"", "manifest.json"},
		{"v1.0.0/..", "manifest.json"},
	}
	for _, tc := range cases {
		if _, _, err := m.OpenAt(tc.tag, tc.name); err == nil {
			t.Errorf("OpenAt(%q, %q) succeeded", tc.tag, tc.name)
		}
	}
}

func TestOpenAtUnknownGeneration(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if _, _, err := m.OpenAt("v9.9.9", ManifestName); !errors.Is(err, ErrNotCached) {
		t.Errorf("OpenAt on an unknown tag = %v, want ErrNotCached", err)
	}
}

// A "Check for Update" pressed inside the API-call floor must happen
// late, not never. Before this, Run consumed the trigger and returned
// early, so the press did nothing at all until the next five-hourly
// tick — while POST /firmware/refresh had already answered 202.
func TestRunReArmsATriggerThatHitsTheFloor(t *testing.T) {
	g := newFakeGitHub(t)
	m, err := New(Options{
		CacheDir:           t.TempDir(),
		APIBase:            g.srv.URL,
		Interval:           time.Hour, // only the trigger path may fire
		MinRefreshInterval: 300 * time.Millisecond,
		HTTPClient:         g.srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := make(chan struct{})
	go func() { m.Run(ctx); close(stopped) }()

	// Run's initial refresh sets the floor.
	waitFor(t, func() bool { _, ok := m.Current(); return ok }, "initial refresh")

	// New release upstream, then press the button immediately — well
	// inside the 300ms floor.
	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("deferred-ota-bytes")
	m.TriggerRefresh()

	waitFor(t, func() bool {
		cur, ok := m.Current()
		return ok && cur.Tag == "v2.0.0"
	}, "the deferred refresh to run once the floor expired")

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

// Continuous pressing across the whole floor must still end in exactly
// one refresh, at the moment the floor expires — the trigger path must
// not starve itself. (It cannot: throttleRemaining counts down from a
// lastAttempt that does not move. This test pins that property rather
// than the arming guard, which is only a redundancy saver.)
func TestRunRefreshesOnceDespiteContinuousTriggering(t *testing.T) {
	g := newFakeGitHub(t)
	m, err := New(Options{
		CacheDir:           t.TempDir(),
		APIBase:            g.srv.URL,
		Interval:           time.Hour,
		MinRefreshInterval: 400 * time.Millisecond,
		HTTPClient:         g.srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go m.Run(ctx)
	waitFor(t, func() bool { _, ok := m.Current(); return ok }, "initial refresh")

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("hammered-ota-bytes")

	// Hammer the trigger across the whole floor.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 40 {
			m.TriggerRefresh()
			time.Sleep(20 * time.Millisecond)
		}
	}()

	waitFor(t, func() bool {
		cur, ok := m.Current()
		return ok && cur.Tag == "v2.0.0"
	}, "the deferred refresh despite continuous triggering")
	<-done
}

// Only "it is not there" is a 404. A cache directory that is not a
// directory produces ENOTDIR rather than ENOENT and is still absence;
// anything else is a broken bridge and must surface.
func TestOpenAtReportsIOErrorsRatherThanNotFound(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Make the file unreadable by turning it into a directory: opening
	// dir/name then yields a directory handle, which OpenAt rejects as
	// absence, so instead break the generation itself.
	brokenTag := "v0.9.0"
	if err := os.WriteFile(filepath.Join(m.cacheDir, brokenTag), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err := m.OpenAt(brokenTag, "cyd-scan-panel.ota.bin")
	if !errors.Is(err, ErrNotCached) {
		t.Errorf("OpenAt through a non-directory = %v, want ErrNotCached (ENOTDIR is still absence)", err)
	}

	// A genuinely unreadable file must NOT be reported as absent.
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod cannot make a file unreadable")
	}
	victim := filepath.Join(m.cacheDir, "v1.0.0", "cyd-scan-panel.ota.bin")
	if err := os.Chmod(victim, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(victim, 0o644) })

	_, _, err = m.OpenAt("v1.0.0", "cyd-scan-panel.ota.bin")
	if err == nil {
		t.Fatal("OpenAt on an unreadable file succeeded")
	}
	if errors.Is(err, ErrNotCached) {
		t.Errorf("OpenAt on an unreadable file = %v; a permission error is a broken cache, not a missing file", err)
	}
}

// A best-effort rewrite is worse than none: it would publish a manifest
// whose paths are still relative, silently breaking the one invariant
// the version-qualified route exists for. Every shape below is a hard
// failure instead, and it fails at refresh time so the release never
// becomes current.
func TestRefreshRejectsAnUnrenderableManifest(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{"no builds", `{"name":"CYD Scan Panel"}`},
		{"empty builds", `{"builds":[]}`},
		{"builds not an array", `{"builds":{"ota":{"path":"cyd-scan-panel.ota.bin"}}}`},
		{"build not an object", `{"builds":["nope"]}`},
		{"no ota block", `{"builds":[{"parts":[{"path":"x.bin"}]}]}`},
		{"ota without path", `{"builds":[{"ota":{"md5":"deadbeef"}}]}`},
		{"empty path", `{"builds":[{"ota":{"path":""}}]}`},
		{"path is an absolute URL", `{"builds":[{"ota":{"path":"https://example.invalid/x.bin"}}]}`},
		{"path names a file the release does not carry", `{"builds":[{"ota":{"path":"never-shipped.bin"}}]}`},
		{
			name:     "one good build, one broken",
			manifest: `{"builds":[{"ota":{"path":"cyd-scan-panel.ota.bin"}},{"ota":{"md5":"x"}}]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newFakeGitHub(t)
			g.assets[ManifestName] = []byte(tc.manifest)
			m := newTestMirror(t, g)

			if _, err := m.Refresh(t.Context()); err == nil {
				t.Fatal("Refresh published a release whose manifest cannot be rendered")
			}
			if _, ok := m.Current(); ok {
				t.Error("the release became current anyway")
			}
			// And nothing was left in the cache for a later Load to
			// adopt.
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

// Open must report the generation the bytes came from, so a handler
// cannot label one generation's file with another's tag.
func TestOpenReportsTheGenerationItServed(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("second-generation-bytes")
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	f, from, _, err := m.Open("cyd-scan-panel.ota.bin")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	if from.Tag != "v2.0.0" {
		t.Errorf("Open reported %q, want v2.0.0", from.Tag)
	}
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "second-generation-bytes" {
		t.Errorf("bytes = %q, do not match the reported generation", got)
	}
}

// The generation Open reports and the bytes it returns must come from
// one read of the published pointer. Reading them separately -- Current()
// for the tag, Open() for the file -- lets a refresh land in between and
// put one generation's tag on another generation's bytes.
//
// Sequential tests cannot observe that, so this one publishes releases
// underneath a reader in a loop and checks the pair agrees every time.
// Run with -race in CI.
func TestOpenIsAtomicAcrossARefresh(t *testing.T) {
	g := newFakeGitHub(t)
	g.setRelease("v1.0.0")
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("initial Refresh: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	publishing := make(chan struct{})
	go func() {
		defer close(publishing)
		for i := 2; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
			}
			g.setRelease(fmt.Sprintf("v%d.0.0", i))
			if _, err := m.Refresh(ctx); err != nil && ctx.Err() == nil {
				t.Errorf("Refresh while publishing: %v", err)
				return
			}
		}
	}()

	for range 300 {
		f, from, _, err := m.Open("cyd-scan-panel.ota.bin")
		if err != nil {
			t.Fatalf("Open during a refresh: %v", err)
		}
		got, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if want := string(otaBytesFor(from.Tag)); string(got) != want {
			t.Fatalf("Open reported generation %q but returned %q, want %q",
				from.Tag, got, want)
		}
	}

	cancel()
	<-publishing
}
