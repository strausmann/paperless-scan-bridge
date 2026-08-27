// Package firmware mirrors the CYD panel firmware from this project's
// GitHub Releases into a local cache the panel can fetch over plain
// HTTP on the LAN.
//
// Why the detour exists at all: the panel cannot reach GitHub itself.
// With Wi-Fi, the Bluedroid stack, LVGL and its own web_server already
// resident, the ESP32 has no heap left for a TLS session, and every
// attempt fails at setup:
//
//	E esp-tls-mbedtls: mbedtls_ssl_setup returned -0x7F00
//	                   (MBEDTLS_ERR_SSL_ALLOC_FAILED)
//
// ADR 0024 settled that the bridge would serve the firmware over HTTP
// instead, and deliberately left "how the bridge obtains the image" as
// an implementation question. This package is the answer (issue #111):
// the bridge polls the GitHub Releases API, verifies what it downloads
// against the release's own SHA256SUMS, and only then makes it visible.
//
// # The ordering invariant
//
// The manifest must never advertise a version the mirror cannot serve.
// If the manifest were swapped as soon as GitHub reported a new tag,
// and the binary fetched lazily on first request, the panel would offer
// an update whose download then 404s or runs past the panel's 55s
// client timeout while the bridge pulls ~1.7 MB. So Refresh downloads
// and verifies EVERY file first, into a staging directory, and swaps
// the cache and the published state only afterwards. A failed refresh
// leaves the previous release serving unchanged.
//
// # Trust
//
// SHA256SUMS is fetched over TLS from GitHub and is itself the trust
// anchor; the checksums it carries are what protect every other file.
// This is the same trust boundary a human downloading the release from
// a browser gets, and the reason the mirror does not need a signature
// scheme of its own.
package firmware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNotCached is returned by Open when the mirror holds no release, or
// holds one that does not carry the requested file. The two cases are
// deliberately one error: a caller can only either serve the file or
// not, and distinguishing them would leak which names exist.
var ErrNotCached = errors.New("firmware: not cached")

const (
	// DefaultRepo is the GitHub repository the releases come from.
	DefaultRepo = "strausmann/paperless-scan-bridge"
	// DefaultAPIBase is GitHub's API root. Overridable so tests can
	// point the mirror at an httptest server.
	DefaultAPIBase = "https://api.github.com"
	// DefaultRefreshInterval is how often the mirror asks GitHub for a
	// newer release. Deliberately shorter than the panel's own 6h
	// update_interval: the bridge should always know slightly before
	// the panel asks. One anonymous API call every five hours is
	// nowhere near GitHub's 60-per-hour unauthenticated limit, which
	// is why the mirror needs no token.
	DefaultRefreshInterval = 5 * time.Hour

	// ManifestName is the ESP Web Tools / ESPHome update manifest. It
	// is mirrored verbatim: its `ota.path` is relative, and ESPHome
	// resolves a relative path against the manifest's own URL, so
	// serving it from /firmware/ makes the binary resolve to
	// /firmware/<name>.bin without any rewriting.
	ManifestName = "manifest.json"
	// ChecksumsName is the release asset that lists every other file's
	// SHA-256. It drives the whole download: the mirror fetches what
	// this file names, and nothing else.
	ChecksumsName = "SHA256SUMS"

	// stateFile records which tag the cache currently holds, so a
	// restart serves the existing cache instead of a 503 until the
	// first refresh completes.
	stateFile = "state.json"

	stagingPrefix = ".staging-"

	// Bounds on everything read from the network. A mirror that
	// streams an unbounded body into a cache directory on a Pi is a
	// disk-filling primitive, and the real files are ~1.7 MB.
	maxReleaseJSONBytes = 1 << 20  // 1 MiB
	maxChecksumsBytes   = 64 << 10 // 64 KiB
	maxAssetBytes       = 16 << 20 // 16 MiB

	// refreshTimeout bounds one whole refresh attempt driven by Run:
	// the API call plus every download. Without it a stalled connection
	// would hold refreshMu forever and the mirror would never try
	// again.
	refreshTimeout = 5 * time.Minute

	// DefaultMinRefreshInterval is the floor between two GitHub API
	// calls, however often a refresh is asked for.
	//
	// POST /firmware/refresh is unauthenticated by design, so anyone on
	// the LAN can press the panel's button, or call the route in a
	// loop. Coalescing alone does not bound that: once Run takes the
	// queued token the next call queues immediately behind it, so a
	// persistent caller drives one API call per refresh duration
	// indefinitely and can exhaust the anonymous 60-per-hour quota --
	// which would stop real updates arriving. This caps trigger-driven
	// checks at 12 per hour. Nothing upstream changes in five minutes
	// anyway; the scheduled poll runs hours apart and never meets it.
	DefaultMinRefreshInterval = 5 * time.Minute

	// generationsKept is how many mirrored releases stay on disk.
	//
	// Two, not one, and this is a correctness requirement rather than
	// caution. The panel reads the manifest on its own schedule and
	// installs when a person clicks, which can be hours later; it
	// carries the MD5 it read at check time. If the newer release
	// replaced the older one in between, that click would download a
	// binary the held MD5 does not describe and fail -- safely, but
	// visibly, and exactly in the moments right after a release. Keeping
	// the previous generation, together with the version-qualified path
	// the served manifest points at, makes the URL the panel captured
	// stay valid and keep returning the same bytes.
	generationsKept = 2
)

// Release describes the firmware currently in the cache.
//
// Files is sorted and holds plain base names, validated at download
// time; it doubles as the allowlist Open checks against. It is never
// mutated after construction, so handing out shallow copies is safe.
type Release struct {
	Tag         string    `json:"tag"`
	Files       []string  `json:"files"`
	RetrievedAt time.Time `json:"retrieved_at"`
	// ReleaseURL is the human-facing page for this tag, taken from the
	// API response, so a log line or an operator's curl leads
	// somewhere readable.
	ReleaseURL string `json:"release_url,omitempty"`
}

// Options configures New. Every field except CacheDir has a default.
type Options struct {
	CacheDir string
	Repo     string
	APIBase  string
	Interval time.Duration
	// MinRefreshInterval is the floor between two GitHub API calls.
	// Zero means DefaultMinRefreshInterval; negative disables the
	// throttle, which only tests have a reason to do.
	MinRefreshInterval time.Duration
	HTTPClient         *http.Client
	Logger             *slog.Logger
	UserAgent          string
}

// Mirror is the local copy of the panel firmware. It is safe for
// concurrent use: HTTP handlers read through Current/Open while Run
// refreshes in the background.
type Mirror struct {
	cacheDir  string
	repo      string
	apiBase   string
	interval  time.Duration
	client    *http.Client
	logger    *slog.Logger
	userAgent string

	// refreshMu serialises whole refreshes. The ticker and the
	// /firmware/refresh trigger both call Refresh, and two concurrent
	// refreshes would race on the same staging and destination
	// directories.
	refreshMu sync.Mutex
	// minInterval and lastAttempt implement the API-call floor, both
	// under refreshMu. lastAttempt records attempts, not successes: a
	// caller who can make the mirror fail must not thereby be able to
	// make it retry faster.
	minInterval time.Duration
	lastAttempt time.Time

	// mu guards the published pointer only — the short critical
	// section every request takes, distinct from the minutes-long
	// refreshMu.
	mu      sync.RWMutex
	current *Release

	// trigger is capacity 1 and written non-blockingly, so
	// TriggerRefresh coalesces bursts and never blocks its caller.
	// That is what keeps POST /firmware/refresh a fast request: the
	// panel's http_request is synchronous on its main loop, and a
	// handler that waited for a GitHub round trip would hold that loop
	// past the task watchdog and reboot the device mid-press.
	trigger chan struct{}
}

// New builds a Mirror and creates its cache directory. It performs no
// network I/O; call Load to adopt an existing cache and Run to start
// refreshing.
func New(opts Options) (*Mirror, error) {
	if opts.CacheDir == "" {
		return nil, errors.New("firmware: cache dir is required")
	}
	m := &Mirror{
		cacheDir:    opts.CacheDir,
		repo:        cmpOr(opts.Repo, DefaultRepo),
		apiBase:     strings.TrimSuffix(cmpOr(opts.APIBase, DefaultAPIBase), "/"),
		interval:    opts.Interval,
		minInterval: opts.MinRefreshInterval,
		client:      opts.HTTPClient,
		logger:      opts.Logger,
		userAgent:   cmpOr(opts.UserAgent, "paperless-scan-bridge"),
		trigger:     make(chan struct{}, 1),
	}
	if m.interval <= 0 {
		m.interval = DefaultRefreshInterval
	}
	if m.minInterval == 0 {
		m.minInterval = DefaultMinRefreshInterval
	}
	if m.client == nil {
		m.client = &http.Client{Timeout: refreshTimeout}
	}
	if m.logger == nil {
		m.logger = slog.New(slog.DiscardHandler)
	}
	if err := os.MkdirAll(m.cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("firmware: create cache dir %q: %w", m.cacheDir, err)
	}
	return m, nil
}

func cmpOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// Load adopts a cache written by a previous run so a restart keeps
// serving instead of answering 503 until the first refresh lands.
//
// A missing state file is not an error — that is a cold cache. A state
// file that describes files which are not on disk IS an error, because
// publishing it would break the package's central invariant: the
// manifest must only ever name files the mirror can actually serve. The
// caller should log it and carry on; the next refresh rebuilds.
func (m *Mirror) Load() error {
	b, err := os.ReadFile(filepath.Join(m.cacheDir, stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("firmware: read cache state: %w", err)
	}

	var rel Release
	if err := json.Unmarshal(b, &rel); err != nil {
		return fmt.Errorf("firmware: decode cache state: %w", err)
	}
	if err := validateTag(rel.Tag); err != nil {
		return err
	}
	if len(rel.Files) == 0 || !slices.Contains(rel.Files, ManifestName) {
		return fmt.Errorf("firmware: cached release %s has no %s", rel.Tag, ManifestName)
	}

	dir := filepath.Join(m.cacheDir, rel.Tag)
	for _, name := range rel.Files {
		if err := validateAssetName(name); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("firmware: cached release %s is incomplete: %w", rel.Tag, err)
		}
	}

	m.publish(&rel)
	m.logger.Info("firmware cache adopted",
		slog.String("tag", rel.Tag),
		slog.Int("files", len(rel.Files)))
	return nil
}

// Current returns the release the mirror is serving. ok is false when
// the cache is cold.
func (m *Mirror) Current() (Release, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return Release{}, false
	}
	return *m.current, true
}

// Open returns a file from the cached release along with its
// modification time (for http.ServeContent's caching headers).
//
// name is matched against the cached release's file list rather than
// being joined onto a path. That allowlist is what makes traversal
// impossible: every name in it was validated as a plain base name when
// it was downloaded, so a request for "../../etc/passwd" fails the
// membership test long before it reaches the filesystem.
func (m *Mirror) Open(name string) (io.ReadSeekCloser, time.Time, error) {
	m.mu.RLock()
	cur := m.current
	m.mu.RUnlock()

	if cur == nil || !slices.Contains(cur.Files, name) {
		return nil, time.Time{}, ErrNotCached
	}

	path := filepath.Join(m.cacheDir, cur.Tag, name)
	f, err := os.Open(path) //nolint:gosec // name is allowlisted above
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("firmware: open %q: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, time.Time{}, fmt.Errorf("firmware: stat %q: %w", path, err)
	}
	return f, info.ModTime(), nil
}

// OpenAt returns a file from a specific mirrored generation, which is
// what the version-qualified paths in the served manifest point at.
//
// Unlike Open it is not restricted to the current release: the whole
// point is that a manifest a panel read hours ago keeps resolving to
// the bytes it described. It is bounded by what is on disk instead, and
// the mirror keeps generationsKept of those.
//
// Both components are validated with the same rules that governed them
// on the way in, so a request cannot name a directory or a file the
// mirror would not itself have created.
func (m *Mirror) OpenAt(tag, name string) (io.ReadSeekCloser, time.Time, error) {
	if validateTag(tag) != nil || validateAssetName(name) != nil {
		return nil, time.Time{}, ErrNotCached
	}
	p := filepath.Join(m.cacheDir, tag, name)
	f, err := os.Open(p) //nolint:gosec // both path components validated above
	if err != nil {
		return nil, time.Time{}, ErrNotCached
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		_ = f.Close()
		return nil, time.Time{}, ErrNotCached
	}
	return f, info.ModTime(), nil
}

// Manifest returns the update manifest as the bridge publishes it: the
// mirrored file with each build's `ota.path` rewritten to the absolute,
// version-qualified path this bridge serves that generation at.
//
// Only `path` is touched. The `md5` beside it is the digest CI computed
// from the binary it shipped, and rewriting a digest is precisely what
// ADR 0024 forbids — the mirror must publish the digest of the file it
// will actually serve, which is what leaving it alone guarantees.
//
// The rewrite exists because ESPHome resolves a relative path against
// the manifest's own URL, which would send every panel to
// /firmware/<name>.bin — always the newest generation, whatever
// manifest it happens to be holding. An install is a human click that
// can come hours after the check, so "newest" and "the one this
// manifest describes" are not the same file, and the MD5 the panel
// carries would not match. `parts` stays relative: it is read by ESP
// Web Tools during a USB install from the docs site, never from here.
func (m *Mirror) Manifest() ([]byte, Release, error) {
	rel, ok := m.Current()
	if !ok {
		return nil, Release{}, ErrNotCached
	}

	raw, err := os.ReadFile(filepath.Join(m.cacheDir, rel.Tag, ManifestName)) //nolint:gosec // rel.Tag was validated on the way in
	if err != nil {
		return nil, Release{}, fmt.Errorf("firmware: read manifest of %s: %w", rel.Tag, err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, Release{}, fmt.Errorf("firmware: decode manifest of %s: %w", rel.Tag, err)
	}
	builds, _ := doc["builds"].([]any)
	for _, b := range builds {
		build, ok := b.(map[string]any)
		if !ok {
			continue
		}
		ota, ok := build["ota"].(map[string]any)
		if !ok {
			continue
		}
		p, ok := ota["path"].(string)
		if !ok || p == "" || strings.Contains(p, "://") {
			continue
		}
		ota["path"] = "/firmware/" + rel.Tag + "/" + path.Base(p)
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, Release{}, fmt.Errorf("firmware: encode manifest of %s: %w", rel.Tag, err)
	}
	return out, rel, nil
}

// TriggerRefresh asks the background loop to refresh now and returns
// immediately. false means a refresh is already queued, so the request
// is satisfied either way — it never means "refused".
//
// It is deliberately only the coalescing half of the rate control, and
// the weaker half: a caller who keeps calling can queue another token
// the moment Run takes the previous one. What actually bounds the
// outbound API calls is the minInterval floor inside Refresh.
func (m *Mirror) TriggerRefresh() bool {
	select {
	case m.trigger <- struct{}{}:
		return true
	default:
		return false
	}
}

// Run refreshes once immediately, then on the configured interval and
// whenever TriggerRefresh fires, until ctx is cancelled. A failed
// refresh is logged and retried on the next tick; it never takes the
// currently served release away.
func (m *Mirror) Run(ctx context.Context) {
	m.refreshLogged(ctx)

	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.trigger:
		}
		m.refreshLogged(ctx)
	}
}

func (m *Mirror) refreshLogged(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()

	rel, err := m.Refresh(ctx)
	if err != nil {
		// Warn, not Error: an unreachable GitHub is an ordinary
		// condition for a home-lab box, and the mirror keeps serving
		// whatever it already has.
		m.logger.Warn("firmware refresh failed", slog.Any("err", err))
		return
	}
	m.logger.Info("firmware mirror current",
		slog.String("tag", rel.Tag),
		slog.Int("files", len(rel.Files)))
}

// Refresh brings the cache up to date with the repository's latest
// release and returns what the mirror serves afterwards.
//
// It is a no-op when the latest tag already matches the cached one, so
// the common case costs a single API call and no downloads.
func (m *Mirror) Refresh(ctx context.Context) (Release, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	// The API-call floor. Checked here rather than in TriggerRefresh
	// because this is the only place an outbound call is made, so it is
	// the only place that can actually bound them.
	if m.minInterval > 0 && !m.lastAttempt.IsZero() &&
		time.Since(m.lastAttempt) < m.minInterval {
		cur, _ := m.Current()
		return cur, nil
	}
	m.lastAttempt = time.Now()

	latest, err := m.latestRelease(ctx)
	if err != nil {
		return Release{}, err
	}
	if err := validateTag(latest.TagName); err != nil {
		return Release{}, err
	}

	if cur, ok := m.Current(); ok && cur.Tag == latest.TagName {
		return cur, nil
	}

	assets := make(map[string]string, len(latest.Assets))
	for _, a := range latest.Assets {
		assets[a.Name] = a.BrowserDownloadURL
	}

	sumsURL, ok := assets[ChecksumsName]
	if !ok {
		return Release{}, fmt.Errorf(
			"firmware: release %s carries no %s asset", latest.TagName, ChecksumsName)
	}
	sums, names, err := m.fetchChecksums(ctx, sumsURL)
	if err != nil {
		return Release{}, err
	}
	if !slices.Contains(names, ManifestName) {
		return Release{}, fmt.Errorf(
			"firmware: release %s lists no %s in %s",
			latest.TagName, ManifestName, ChecksumsName)
	}

	// Everything below lands in a staging directory. Nothing the panel
	// can see changes until the whole set is downloaded and verified.
	staging, err := os.MkdirTemp(m.cacheDir, stagingPrefix)
	if err != nil {
		return Release{}, fmt.Errorf("firmware: create staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	for _, name := range names {
		url, ok := assets[name]
		if !ok {
			return Release{}, fmt.Errorf(
				"firmware: %s of release %s lists %q but the release has no such asset",
				ChecksumsName, latest.TagName, name)
		}
		if err := m.download(ctx, url, filepath.Join(staging, name), sums[name]); err != nil {
			return Release{}, err
		}
	}

	dest := filepath.Join(m.cacheDir, latest.TagName)
	// A leftover directory for this tag can only come from a refresh
	// that died between rename and state write; its contents are
	// unverified, so it is replaced rather than reused.
	if err := os.RemoveAll(dest); err != nil {
		return Release{}, fmt.Errorf("firmware: clear %q: %w", dest, err)
	}
	if err := os.Rename(staging, dest); err != nil {
		return Release{}, fmt.Errorf("firmware: publish %q: %w", dest, err)
	}
	// MkdirTemp creates 0700. The daemon is the only reader, but an
	// operator inspecting the volume should not need root.
	if err := os.Chmod(dest, 0o755); err != nil {
		return Release{}, fmt.Errorf("firmware: chmod %q: %w", dest, err)
	}

	rel := Release{
		Tag:         latest.TagName,
		Files:       names,
		RetrievedAt: time.Now().UTC(),
		ReleaseURL:  latest.HTMLURL,
	}
	if err := m.writeState(rel); err != nil {
		return Release{}, err
	}
	m.publish(&rel)

	// Only now is the old directory unreferenced.
	m.prune(rel.Tag)

	m.logger.Info("firmware mirrored",
		slog.String("tag", rel.Tag),
		slog.Int("files", len(rel.Files)),
		slog.String("release_url", rel.ReleaseURL))
	return rel, nil
}

func (m *Mirror) publish(rel *Release) {
	m.mu.Lock()
	m.current = rel
	m.mu.Unlock()
}

// ghRelease is the subset of GitHub's release payload the mirror uses.
type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func (m *Mirror) latestRelease(ctx context.Context) (ghRelease, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", m.apiBase, m.repo)
	resp, err := m.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return ghRelease{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseJSONBytes)).Decode(&rel); err != nil {
		return ghRelease{}, fmt.Errorf("firmware: decode release payload from %s: %w", url, err)
	}
	if rel.TagName == "" {
		return ghRelease{}, fmt.Errorf("firmware: release payload from %s has no tag_name", url)
	}
	return rel, nil
}

func (m *Mirror) fetchChecksums(ctx context.Context, url string) (map[string]string, []string, error) {
	resp, err := m.get(ctx, url, "text/plain")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	sums, names, err := parseChecksums(io.LimitReader(resp.Body, maxChecksumsBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("firmware: %s from %s: %w", ChecksumsName, url, err)
	}
	return sums, names, nil
}

// download streams url into dest, hashing as it writes, and fails if
// the result does not match wantHex. A mismatch leaves dest in the
// staging directory, which the caller removes — nothing partial can
// reach the served cache.
func (m *Mirror) download(ctx context.Context, url, dest, wantHex string) error {
	resp, err := m.get(ctx, url, "application/octet-stream")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	f, err := os.Create(dest) //nolint:gosec // dest is staging + a validated base name
	if err != nil {
		return fmt.Errorf("firmware: create %q: %w", dest, err)
	}

	h := sha256.New()
	// maxAssetBytes+1 so an over-long body is detected rather than
	// silently truncated into a file whose checksum then "just"
	// mismatches with no explanation.
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxAssetBytes+1))
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("firmware: download %s: %w", url, err)
	}
	if n > maxAssetBytes {
		_ = f.Close()
		return fmt.Errorf("firmware: %s exceeds the %d-byte asset limit", url, maxAssetBytes)
	}
	// Not deferred and not ignored: a write-side Close is where the
	// final flush happens, and swallowing its error is how a truncated
	// file gets published as if it were whole.
	if err := f.Close(); err != nil {
		return fmt.Errorf("firmware: close %q: %w", dest, err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantHex) {
		return fmt.Errorf("firmware: checksum mismatch for %s: %s says %s, download is %s",
			filepath.Base(dest), ChecksumsName, wantHex, got)
	}
	return nil
}

func (m *Mirror) get(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("firmware: build request for %s: %w", url, err)
	}
	req.Header.Set("Accept", accept)
	// GitHub rejects unauthenticated API calls without a User-Agent.
	req.Header.Set("User-Agent", m.userAgent)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("firmware: GET %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("firmware: GET %s: unexpected status %s", url, resp.Status)
	}
	return resp, nil
}

func (m *Mirror) writeState(rel Release) error {
	b, err := json.Marshal(rel)
	if err != nil {
		return fmt.Errorf("firmware: encode cache state: %w", err)
	}
	final := filepath.Join(m.cacheDir, stateFile)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil { //nolint:gosec // the manifest it describes is public
		return fmt.Errorf("firmware: write cache state: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("firmware: publish cache state: %w", err)
	}
	return nil
}

// prune keeps the generationsKept most recent release directories —
// keep among them, whatever its mtime — and removes the rest, together
// with any staging directory a killed refresh left behind.
//
// Keeping more than one is what makes an install offered by an older
// manifest still work; see generationsKept. Failures are logged, not
// returned: stale bytes cost disk, but the refresh they follow already
// succeeded and reporting it as failed would be a lie.
func (m *Mirror) prune(keep string) {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.logger.Warn("firmware cache prune failed", slog.Any("err", err))
		return
	}

	type generation struct {
		name string
		mod  time.Time
	}
	var older []generation
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keep {
			continue
		}
		// A staging directory is never a generation: it holds an
		// abandoned, unverified download and always goes.
		if strings.HasPrefix(e.Name(), stagingPrefix) {
			m.remove(e.Name())
			continue
		}
		info, err := e.Info()
		if err != nil {
			m.logger.Warn("firmware cache prune failed",
				slog.String("entry", e.Name()), slog.Any("err", err))
			continue
		}
		older = append(older, generation{name: e.Name(), mod: info.ModTime()})
	}

	// Newest first, so the survivors are the ones a recently-read
	// manifest may still point at.
	sort.Slice(older, func(i, j int) bool { return older[i].mod.After(older[j].mod) })
	// keep already occupies one of the generationsKept slots.
	for _, g := range older[min(len(older), generationsKept-1):] {
		m.remove(g.name)
	}
}

func (m *Mirror) remove(name string) {
	if err := os.RemoveAll(filepath.Join(m.cacheDir, name)); err != nil {
		m.logger.Warn("firmware cache prune failed",
			slog.String("entry", name), slog.Any("err", err))
	}
}

// parseChecksums reads sha256sum output: a hex digest, whitespace, an
// optional "*" binary marker, then the file name. The release workflow
// runs `sha256sum ./*.bin ./manifest.json`, so the names arrive with a
// "./" prefix that is stripped here.
func parseChecksums(r io.Reader) (map[string]string, []string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, fmt.Errorf("read: %w", err)
	}

	sums := make(map[string]string)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		digest, rest, ok := strings.Cut(line, " ")
		if !ok {
			return nil, nil, fmt.Errorf("malformed line %q", line)
		}
		if _, err := hex.DecodeString(digest); err != nil || len(digest) != sha256.Size*2 {
			return nil, nil, fmt.Errorf("line %q does not start with a SHA-256 digest", line)
		}
		name := strings.TrimPrefix(strings.TrimSpace(rest), "*")
		name = strings.TrimPrefix(name, "./")
		if err := validateAssetName(name); err != nil {
			return nil, nil, err
		}
		if _, dup := sums[name]; dup {
			return nil, nil, fmt.Errorf("duplicate entry for %q", name)
		}
		sums[name] = strings.ToLower(digest)
	}
	if len(sums) == 0 {
		return nil, nil, errors.New("no entries")
	}

	names := make([]string, 0, len(sums))
	for name := range sums {
		names = append(names, name)
	}
	sort.Strings(names)
	return sums, names, nil
}

// validateAssetName rejects anything that is not a plain file name.
// SHA256SUMS comes off the network, and a name like "../../config.toml"
// would otherwise choose where the download lands.
func validateAssetName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return fmt.Errorf("firmware: %q is not a usable asset name", name)
	}
	return nil
}

// validateTag rejects a tag that could not be a directory name. It is
// the same argument as validateAssetName: the tag comes from the API
// response and names a directory under the cache. The leading-dot rule
// also keeps a tag from colliding with a staging directory.
func validateTag(tag string) error {
	if tag == "" || tag == "." || tag == ".." || tag == stateFile ||
		strings.ContainsAny(tag, `/\`) || strings.HasPrefix(tag, ".") {
		return fmt.Errorf("firmware: %q is not a usable release tag", tag)
	}
	return nil
}
