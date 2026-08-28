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

	// padReleaseJSON, when non-zero, appends that many bytes of filler
	// to the release payload -- the only way to exercise the
	// size-limit branch without a real GitHub.
	padReleaseJSON int

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
		Padding string  `json:"padding,omitempty"`
	}{
		TagName: g.tag,
		HTMLURL: "https://example.invalid/releases/" + g.tag,
		Padding: strings.Repeat("x", g.padReleaseJSON),
	}
	for name := range g.assets {
		if name == g.omitFromRelease {
			continue
		}
		payload.Assets = append(payload.Assets, asset{
			Name: name, URL: g.srv.URL + "/download/" + name,
		})
	}
	if g.omitFromRelease != ChecksumsName {
		payload.Assets = append(payload.Assets, asset{
			Name: ChecksumsName, URL: g.srv.URL + "/download/" + ChecksumsName,
		})
	}

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

// genDir returns the on-disk directory of the release the mirror is
// currently serving. Generations are content-addressed (Release.Dir),
// so a test cannot spell the path from the tag alone.
func genDir(t *testing.T, m *Mirror) string {
	t.Helper()
	cur, ok := m.Current()
	if !ok {
		t.Fatal("no release is being served")
	}
	return filepath.Join(m.cacheDir, cur.Dir())
}

// runMirror starts m.Run and guarantees it has returned before the test
// ends.
//
// Not tidiness: t.TempDir registers its cleanup when the directory is
// created, and t.Cleanup runs LIFO, so this one fires first and joins
// the goroutine while the directory still exists. Without the join, a
// Run still inside Refresh keeps creating and removing a staging
// directory under a tree TempDir is already deleting, and the test
// fails with "directory not empty" -- intermittently, which is the
// worst kind.
func runMirror(t *testing.T, m *Mirror) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { m.Run(ctx); close(stopped) }()

	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Error("Run did not return after its context was cancelled")
		}
	})
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
	// One extra fetch, and exactly one: SHA256SUMS. The tag alone
	// cannot answer whether the mirror is current, because
	// `gh release upload --clobber` replaces assets under an unchanged
	// tag. The binaries must NOT be re-fetched.
	if extra := g.downloads.Load() - after; extra != 1 {
		t.Errorf("second refresh fetched %d files, want exactly 1 (%s)", extra, ChecksumsName)
	}
	if g.apiCalls.Load() != 2 {
		t.Errorf("apiCalls = %d, want 2", g.apiCalls.Load())
	}
}

// Re-running the release workflow replaces the assets of an existing
// tag (`gh release upload --clobber` deletes before it uploads), so a
// mirror that trusted the tag alone would serve the superseded bytes
// until some later release appeared.
func TestRefreshDetectsReplacedAssetsAtTheSameTag(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	// Same tag, different bytes -- exactly what --clobber produces.
	g.assets["cyd-scan-panel.ota.bin"] = []byte("re-uploaded-ota-bytes")
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	f, _, _, err := m.Open("cyd-scan-panel.ota.bin")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "re-uploaded-ota-bytes" {
		t.Errorf("serving %q; the replaced asset was not picked up", got)
	}
}

// A staging directory left by a killed download must be swept even when
// the refresh short-circuits on an unchanged tag -- that path never
// reaches prune, so without this they accumulate one per interruption.
func TestStagingLeftoversAreSweptOnAShortCircuitAndOnLoad(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	orphan := filepath.Join(m.cacheDir, stagingPrefix+"abandoned")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Unchanged tag: the short-circuit path.
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("staging leftover survived a short-circuiting refresh: %v", err)
	}

	// And on the startup path.
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	restarted, err := New(Options{CacheDir: m.cacheDir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := restarted.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("staging leftover survived Load: %v", err)
	}
}

// The cold-cache variant: killed during the very first download, so
// there is no state file for Load to adopt and the early return would
// skip the sweep. On a box with unreliable power that is one
// near-complete directory per boot until a refresh finally succeeds.
func TestStagingLeftoversAreSweptWithNoStateFile(t *testing.T) {
	dir := t.TempDir()
	orphan := filepath.Join(dir, stagingPrefix+"first-attempt")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m, err := New(Options{CacheDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Fatalf("Load on a cold cache = %v, want nil", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("staging leftover survived a cold-cache Load: %v", err)
	}
}

// Re-running the release workflow replaces the assets of an existing
// tag, and an ESPHome build is not bit-reproducible, so the replacement
// genuinely differs. A generation keyed by the tag alone would take the
// new bytes in place -- and a panel that had already read the previous
// manifest would fetch them under the URL it saved and discard them on
// the MD5 check. Content-addressed, both generations coexist.
func TestReplacedAssetsAtTheSameTagGetTheirOwnGeneration(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	first, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	oldBytes := string(g.assets["cyd-scan-panel.ota.bin"])

	// Same tag, different bytes: `gh release upload --clobber`.
	g.assets["cyd-scan-panel.ota.bin"] = []byte("clobbered-ota-bytes")
	second, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	if second.Tag != first.Tag {
		t.Fatalf("tags differ (%q vs %q); this test is about an UNCHANGED tag", first.Tag, second.Tag)
	}
	if second.Dir() == first.Dir() {
		t.Fatalf("both generations share the directory %q; the old bytes were overwritten", first.Dir())
	}

	// The URL a panel saved before the clobber still returns what its
	// manifest described.
	f, _, err := m.OpenAt(first.Dir(), "cyd-scan-panel.ota.bin")
	if err != nil {
		t.Fatalf("OpenAt on the pre-clobber generation: %v", err)
	}
	got, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != oldBytes {
		t.Errorf("pre-clobber URL returned %q, want %q", got, oldBytes)
	}

	// And the current one serves the replacement.
	f, _, _, err = m.Open("cyd-scan-panel.ota.bin")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err = io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "clobbered-ota-bytes" {
		t.Errorf("current generation returned %q", got)
	}
}

// Retention is by tag, not by mtime. A daemon killed between the rename
// and the publish leaves a fully downloaded but never-advertised
// directory that is NEWER than the generation panels actually hold a
// versioned URL for; an mtime rule would keep the orphan and delete the
// one still being installed from.
func TestPruneKeepsThePublishedPredecessorNotTheNewestOrphan(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	first, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	// An orphan from a crashed refresh, newer than the predecessor.
	orphan := filepath.Join(m.cacheDir, "v1.5.0-000000000000")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("third-generation-bytes")
	second, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	if _, err := os.Stat(filepath.Join(m.cacheDir, first.Dir())); err != nil {
		t.Errorf("the published predecessor was pruned: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("the never-published orphan survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.cacheDir, second.Dir())); err != nil {
		t.Errorf("the current generation is missing: %v", err)
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
	if err := os.Remove(filepath.Join(genDir(t, m), "cyd-scan-panel.ota.bin")); err != nil {
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
// the prune comment. Eviction is covered by
// TestPruneKeepsTheCurrentAndPreviousGeneration.
func TestRefreshKeepsThePreviousRelease(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	first, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("newer-ota-image-bytes")
	second, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	for _, dir := range []string{first.Dir(), second.Dir()} {
		if _, err := os.Stat(filepath.Join(m.cacheDir, dir)); err != nil {
			t.Errorf("%s missing after the second refresh: %v", dir, err)
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
	if want := "/firmware/" + rel.Dir() + "/cyd-scan-panel.ota.bin"; doc.Builds[0].OTA.Path != want {
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
	first, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	oldBytes := string(g.assets["cyd-scan-panel.ota.bin"])

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("second-generation-ota-bytes")
	second, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	f, _, err := m.OpenAt(first.Dir(), "cyd-scan-panel.ota.bin")
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
	f, _, err = m.OpenAt(second.Dir(), "cyd-scan-panel.ota.bin")
	if err != nil {
		t.Fatalf("OpenAt on the current generation: %v", err)
	}
	_ = f.Close()
}

// Two generations, not more: the third release evicts the first.
func TestPruneKeepsTheCurrentAndPreviousGeneration(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)

	var dirs []string
	for i, tag := range []string{"v1.0.0", "v2.0.0", "v3.0.0"} {
		g.tag = tag
		g.assets["cyd-scan-panel.ota.bin"] = fmt.Appendf(nil, "ota-generation-%d", i)
		rel, err := m.Refresh(t.Context())
		if err != nil {
			t.Fatalf("Refresh %s: %v", tag, err)
		}
		dirs = append(dirs, rel.Dir())
	}

	if _, err := os.Stat(filepath.Join(m.cacheDir, dirs[0])); !os.IsNotExist(err) {
		t.Errorf("the first generation survived two later releases: %v", err)
	}
	for _, dir := range dirs[1:] {
		if _, err := os.Stat(filepath.Join(m.cacheDir, dir)); err != nil {
			t.Errorf("%s missing: %v", dir, err)
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
	if _, _, err := m.OpenAt("v9.9.9-000000000000", ManifestName); !errors.Is(err, ErrNotCached) {
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

	runMirror(t, m)

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

	runMirror(t, m)
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
	victim := filepath.Join(genDir(t, m), "cyd-scan-panel.ota.bin")
	if err := os.Chmod(victim, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(victim, 0o644) })

	cur, _ := m.Current()
	_, _, err = m.OpenAt(cur.Dir(), "cyd-scan-panel.ota.bin")
	if err == nil {
		t.Fatal("OpenAt on an unreadable file succeeded")
	}
	if errors.Is(err, ErrNotCached) {
		t.Errorf("OpenAt on an unreadable file = %v; a permission error is a broken cache, not a missing file", err)
	}
}

// A best-effort rewrite is worse than none: it would publish a manifest
// whose paths are still relative, silently breaking the one invariant
// the generation-qualified route exists for. Every shape below is a hard
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

// "The tag has not changed" is not "the cache is still intact". A file
// truncated or emptied after it was mirrored would otherwise be served
// forever: the panel discards it on the MD5 check every time, and the
// mirror short-circuits before it could notice, because GitHub still
// reports the same tag.
func TestRefreshRepairsACorruptedCacheAtTheSameTag(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	victim := filepath.Join(genDir(t, m), "cyd-scan-panel.ota.bin")
	if err := os.WriteFile(victim, []byte("truncated"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	before := g.downloads.Load()

	// Same tag upstream, so the cheap path would normally be taken.
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if g.downloads.Load() == before {
		t.Error("a corrupted cache was short-circuited past; nothing was re-downloaded")
	}

	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(g.assets["cyd-scan-panel.ota.bin"]) {
		t.Errorf("file not repaired: %q", got)
	}
}

// The same corruption must not be adopted across a restart either.
func TestLoadRejectsACorruptedCache(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	victim := filepath.Join(genDir(t, m), "cyd-scan-panel.ota.bin")
	if err := os.WriteFile(victim, []byte("truncated"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	restarted, err := New(Options{CacheDir: m.cacheDir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := restarted.Load(); err == nil {
		t.Fatal("Load adopted a cache whose contents do not match their checksums")
	}
	if _, ok := restarted.Current(); ok {
		t.Error("a corrupted cache became current")
	}
}

// A state file written before checksums were recorded cannot be
// verified, so it must be treated as unusable rather than as fine.
func TestLoadRejectsAStateFileWithoutChecksums(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	statePath := filepath.Join(m.cacheDir, stateFile)
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var rel Release
	if err := json.Unmarshal(raw, &rel); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	rel.Sums = nil
	out, err := json.Marshal(rel)
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}
	if err := os.WriteFile(statePath, out, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	restarted, err := New(Options{CacheDir: m.cacheDir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := restarted.Load(); err == nil {
		t.Fatal("Load adopted a release it has no way to verify")
	}
}

// Re-mirroring a generation must not be able to leave the cache with
// nothing at all.
//
// This matters because of verifyCached: a corrupted cache at an
// UNCHANGED tag now falls through the short-circuit into a full
// re-download, so the directory being replaced can be the one currently
// being served. Removing it before the rename -- which is what the code
// did first -- destroys a working cache the moment the rename fails,
// leaving m.current pointing at nothing and every request 500ing.
//
// os.Rename does not fail on a healthy filesystem, so the failure is
// injected through the renameFile seam.
func TestRefreshRestoresTheOldGenerationIfThePublishFails(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	dir := genDir(t, m)

	// Corrupt a file so the same tag takes the full download path and
	// the swap therefore targets the generation being served.
	if err := os.WriteFile(filepath.Join(dir, "cyd-scan-panel.ota.bin"), []byte("truncated"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	// Snapshot AFTER the corruption: a failed publish has to restore
	// what was actually there, corrupt file included. Serving a stale
	// release the panel rejects on its MD5 check is bad; serving
	// nothing at all, with Current() still pointing at it, is worse.
	before := readDirContents(t, dir)

	// Fail only the second rename -- the one that publishes staging --
	// so the old directory has already been moved aside.
	real := renameFile
	calls := 0
	renameFile = func(oldpath, newpath string) error {
		calls++
		if calls == 2 {
			return errors.New("injected rename failure")
		}
		return real(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = real })

	if _, err := m.Refresh(t.Context()); err == nil {
		t.Fatal("Refresh reported success despite a failed publish")
	}
	if calls != 2 {
		t.Fatalf("renameFile called %d times, want 2 (set aside, then publish)", calls)
	}

	// The served generation must be back, complete.
	after := readDirContents(t, dir)
	if len(after) != len(before) {
		t.Fatalf("generation has %d files after a failed publish, had %d", len(after), len(before))
	}
	for name, content := range before {
		if after[name] != content {
			t.Errorf("%q changed across a failed publish", name)
		}
	}
	if cur, ok := m.Current(); !ok || cur.Tag != "v1.0.0" {
		t.Errorf("Current() = %+v, %v; the served release must survive", cur, ok)
	}
}

func readDirContents(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %q: %v", dir, err)
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %q: %v", e.Name(), err)
		}
		out[e.Name()] = string(b)
	}
	return out
}

// A successful same-tag re-mirror repairs the corruption and leaves no
// parking directory behind.
func TestRefreshRepairsInPlaceWithoutLeavingLeftovers(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	dir := genDir(t, m)
	original := readDirContents(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "cyd-scan-panel.ota.bin"), []byte("truncated"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	if got := readDirContents(t, dir); len(got) != len(original) {
		t.Errorf("generation has %d files after repair, had %d", len(got), len(original))
	} else if got["cyd-scan-panel.ota.bin"] != original["cyd-scan-panel.ota.bin"] {
		t.Error("the corrupted file was not repaired")
	}

	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), stagingPrefix) {
			t.Errorf("staging leftover %q after a successful refresh", e.Name())
		}
	}
}

// prune must sweep a parking directory left behind by a process that
// died mid-swap, exactly as it does an abandoned staging download.
func TestPruneRemovesAParkedGeneration(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	first, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	parked := filepath.Join(m.cacheDir, stagingPrefix+"replaced-v1.0.0-abcdef012345")
	if err := os.MkdirAll(parked, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("second-generation-bytes")
	second, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	if _, err := os.Stat(parked); !os.IsNotExist(err) {
		t.Errorf("parked generation survived prune: %v", err)
	}
	// The two real generations are untouched.
	for _, dir := range []string{first.Dir(), second.Dir()} {
		if _, err := os.Stat(filepath.Join(m.cacheDir, dir)); err != nil {
			t.Errorf("%s missing: %v", dir, err)
		}
	}
}

// A bookkeeping failure must not strand a good release. The bytes are
// downloaded and verified and in place; state.json only decides what a
// restart adopts before the first refresh lands. Refusing to publish
// over it would leave the release unserved -- and the next refresh
// would hit the same failure, so it would stay that way.
func TestRefreshPublishesEvenIfTheStateFileCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory does not stop a write")
	}

	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	if _, err := m.Refresh(t.Context()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	// A directory where the state file's temp path has to go makes the
	// write fail without touching anything else.
	statePath := filepath.Join(m.cacheDir, stateFile)
	if err := os.Remove(statePath); err != nil {
		t.Fatalf("remove state: %v", err)
	}
	if err := os.Mkdir(statePath+".tmp", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(statePath + ".tmp") })

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("second-generation-bytes")
	rel, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("Refresh refused to publish over a state-write failure: %v", err)
	}
	if rel.Tag != "v2.0.0" {
		t.Errorf("returned tag = %q, want v2.0.0", rel.Tag)
	}
	cur, ok := m.Current()
	if !ok || cur.Tag != "v2.0.0" {
		t.Fatalf("Current() = %+v, %v; the verified release must be served", cur, ok)
	}
	f, from, _, err := m.Open("cyd-scan-panel.ota.bin")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = f.Close()
	if from.Tag != "v2.0.0" {
		t.Errorf("Open served %q", from.Tag)
	}
}

// The floor applies to the scheduled tick as well, and a tick it
// refuses must be re-armed like a button press. Without that, a manual
// refresh shortly before the tick leaves the tick to be dropped, and a
// release published in between goes unseen for another full interval
// despite a check having been due.
//
// Asserted on WHEN the second API call arrives, not on whether the new
// tag turns up: a repeating ticker gets another chance on its own, so
// eventual success proves nothing. The interval sits just under the
// floor, which puts exactly one tick inside it and the next one far
// afterwards:
//
//	call 1   t=0      Run's initial refresh; the floor runs to t=2000ms
//	tick 1   t=1800   inside the floor -> deferred to t=2000
//	call 2   t=2000   if the tick was re-armed
//	tick 2   t=3600   when it was dropped instead
//
// The deadline sits between the two, 800ms clear of each. An earlier
// draft put it exactly on the expected time and went flaky in CI.
func TestRunReArmsAScheduledTickThatHitsTheFloor(t *testing.T) {
	const (
		floor    = 2000 * time.Millisecond
		interval = 1800 * time.Millisecond
		deadline = 2800 * time.Millisecond
	)

	g := newFakeGitHub(t)
	m, err := New(Options{
		CacheDir:           t.TempDir(),
		APIBase:            g.srv.URL,
		Interval:           interval,
		MinRefreshInterval: floor,
		HTTPClient:         g.srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runMirror(t, m)

	waitFor(t, func() bool { return g.apiCalls.Load() >= 1 }, "the initial refresh")
	start := time.Now()

	for time.Since(start) < deadline {
		if g.apiCalls.Load() >= 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("only %d API calls after %v; the throttled tick was dropped instead of re-armed",
		g.apiCalls.Load(), deadline)
}

// A failed refresh must be retried promptly, not five hours later.
//
// The window is routine rather than exotic: release.yml creates the
// release first and uploads the firmware in a following job, so a
// refresh landing in between finds no SHA256SUMS and fails. Waiting for
// the next tick would mean a "Check for Update" pressed during a
// release sees the old build despite all three of its manifest reads.
func TestRunRetriesAFailedRefreshBeforeTheNextTick(t *testing.T) {
	g := newFakeGitHub(t)
	// The release exists but its assets do not yet -- exactly the gap
	// between the release job and the attach job.
	g.omitFromRelease = ChecksumsName

	m, err := New(Options{
		CacheDir: t.TempDir(),
		APIBase:  g.srv.URL,
		// An interval far beyond the test: only the retry may fire.
		Interval:           time.Hour,
		MinRefreshInterval: 200 * time.Millisecond,
		HTTPClient:         g.srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runMirror(t, m)

	// Three failing attempts: the one at startup plus two retries. Two
	// matters -- one would also happen if only the startup attempt were
	// retried, which would leave the loop's retry path untested.
	waitFor(t, func() bool { return g.apiCalls.Load() >= 3 }, "the failure to be retried twice")
	if _, ok := m.Current(); ok {
		t.Fatal("a release with no SHA256SUMS became current")
	}

	// The attach job finishes.
	g.mu.Lock()
	g.omitFromRelease = ""
	g.mu.Unlock()

	waitFor(t, func() bool {
		cur, ok := m.Current()
		return ok && cur.Tag == "v1.0.0"
	}, "the retry to pick the release up")
}

// The retry is bounded, so an upstream that stays broken does not turn
// into a poll every floor-length forever.
func TestRunStopsRetryingAfterAFewFailures(t *testing.T) {
	g := newFakeGitHub(t)
	g.omitFromRelease = ChecksumsName

	m, err := New(Options{
		CacheDir:           t.TempDir(),
		APIBase:            g.srv.URL,
		Interval:           time.Hour,
		MinRefreshInterval: 100 * time.Millisecond,
		HTTPClient:         g.srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runMirror(t, m)

	// One initial attempt plus at most three retries. Give it well over
	// the time four attempts need, then check it has stopped.
	time.Sleep(1200 * time.Millisecond)
	settled := g.apiCalls.Load()
	if settled > 4 {
		t.Fatalf("apiCalls = %d after the retries should have stopped, want at most 4", settled)
	}
	if settled < 2 {
		t.Fatalf("apiCalls = %d; the failure was not retried at all", settled)
	}

	time.Sleep(600 * time.Millisecond)
	if got := g.apiCalls.Load(); got != settled {
		t.Errorf("apiCalls grew from %d to %d; the retry is unbounded", settled, got)
	}
}

// The retry budget belongs to one series, not to the process. Pinned at
// maxRetries after an early run of failures, every later tick or press
// would get a single attempt and no retry -- so a second pass through
// the release window would go unnoticed until the next interval.
func TestRunGivesAFreshTriggerAFreshRetryBudget(t *testing.T) {
	g := newFakeGitHub(t)
	g.omitFromRelease = ChecksumsName

	m, err := New(Options{
		CacheDir:           t.TempDir(),
		APIBase:            g.srv.URL,
		Interval:           time.Hour, // only presses may drive this
		MinRefreshInterval: 100 * time.Millisecond,
		HTTPClient:         g.srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runMirror(t, m)

	// Exhaust the budget: the startup attempt plus its retries.
	waitFor(t, func() bool { return g.apiCalls.Load() >= 4 }, "the retry budget to be spent")
	time.Sleep(400 * time.Millisecond)
	spent := g.apiCalls.Load()

	// A press much later. It must get its own budget, so a failure now
	// is still retried.
	m.TriggerRefresh()
	waitFor(t, func() bool { return g.apiCalls.Load() >= spent+2 },
		"a fresh press to be retried rather than tried once")
}

// An in-place repair must not evict the predecessor.
//
// The repair path runs with an UNCHANGED tag and unchanged checksums,
// so the freshly downloaded generation has the same Dir() as the one it
// replaces -- which made prune receive the same name as both survivors
// and delete the real predecessor, dropping the mirror to a single
// generation and 404ing a URL a panel may still be holding.
func TestRepairingInPlaceKeepsThePredecessor(t *testing.T) {
	g := newFakeGitHub(t)
	m := newTestMirror(t, g)
	first, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	g.tag = "v2.0.0"
	g.assets["cyd-scan-panel.ota.bin"] = []byte("second-generation-bytes")
	second, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	// Damage the current generation so the next refresh repairs it in
	// place rather than publishing anything new.
	victim := filepath.Join(m.cacheDir, second.Dir(), "cyd-scan-panel.ota.bin")
	if err := os.WriteFile(victim, []byte("truncated"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	repaired, err := m.Refresh(t.Context())
	if err != nil {
		t.Fatalf("repair Refresh: %v", err)
	}
	if repaired.Dir() != second.Dir() {
		t.Fatalf("repair produced generation %q, want the same %q", repaired.Dir(), second.Dir())
	}

	if _, err := os.Stat(filepath.Join(m.cacheDir, first.Dir())); err != nil {
		t.Errorf("the predecessor was evicted by an in-place repair: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.cacheDir, second.Dir())); err != nil {
		t.Errorf("the repaired generation is missing: %v", err)
	}
	// The repair still fixed the file.
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "second-generation-bytes" {
		t.Errorf("file not repaired: %q", got)
	}
}

// Both size limits detect an over-long body rather than cutting it.
//
// SHA256SUMS is line-oriented, so a body cut at the limit still parses
// -- as a SHORTER list. The mirror would download a subset and the
// failure would surface later as "the manifest names a file the release
// does not carry", pointing at the wrong thing entirely.
func TestOverLongBodiesAreRejectedNotTruncated(t *testing.T) {
	t.Run("SHA256SUMS", func(t *testing.T) {
		g := newFakeGitHub(t)
		valid := hex.EncodeToString(hashOf("x"))
		var b strings.Builder
		for i := range 3000 {
			fmt.Fprintf(&b, "%s  filler-%04d.bin\n", valid, i)
		}
		g.sums = []byte(b.String())
		if len(g.sums) <= maxChecksumsBytes {
			t.Fatalf("fixture is only %d bytes, under the %d-byte limit", len(g.sums), maxChecksumsBytes)
		}
		m := newTestMirror(t, g)

		_, err := m.Refresh(t.Context())
		if err == nil {
			t.Fatal("Refresh accepted an over-long SHA256SUMS")
		}
		if !strings.Contains(err.Error(), "exceeds") {
			t.Errorf("error %q does not say the body was too large", err)
		}
	})

	t.Run("release payload", func(t *testing.T) {
		g := newFakeGitHub(t)
		g.padReleaseJSON = maxReleaseJSONBytes + 1
		m := newTestMirror(t, g)

		_, err := m.Refresh(t.Context())
		if err == nil {
			t.Fatal("Refresh accepted an over-long release payload")
		}
		if !strings.Contains(err.Error(), "exceeds") {
			t.Errorf("error %q does not say the body was too large", err)
		}
	})
}
