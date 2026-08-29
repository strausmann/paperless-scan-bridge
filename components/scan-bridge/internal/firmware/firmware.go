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
// The one thing the mirror does not pass through untouched is the
// manifest's `ota.path`, which Manifest() rewrites to a
// generation-qualified route so an install clicked hours after a check
// still gets the binary that check's MD5 describes. The `md5` itself is
// never rewritten — see Manifest() for why that distinction is the
// whole of ADR 0024's constraint.
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
	"syscall"
	"time"
)

// renameFile is os.Rename behind a seam. The two renames that swap a
// generation into place are the only steps in this package that can
// destroy a working cache, and there is no other way to exercise the
// restore path -- os.Rename simply does not fail on a healthy
// filesystem. Tests replace it; nothing else ever does.
var renameFile = os.Rename

// ErrNotCached is returned by Open when the mirror holds no release, or
// holds one that does not carry the requested file. The two cases are
// deliberately one error: a caller can only either serve the file or
// not, and distinguishing them would leak which names exist.
var ErrNotCached = errors.New("firmware: not cached")

// ThrottledError reports that a refresh was refused by the API-call
// floor, and how long remains. It exists so a refused refresh can be
// re-armed instead of silently dropped: a person pressing "Check for
// Update" four minutes after the scheduled poll must not have to wait
// out the next five-hour tick.
type ThrottledError struct {
	RetryAfter time.Duration
}

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("firmware: refresh throttled, retry in %s", e.RetryAfter.Round(time.Second))
}

const (
	// DefaultRepo is the GitHub repository the releases come from.
	DefaultRepo = "strausmann/paperless-scan-bridge"
	// DefaultAPIBase is GitHub's API root. Overridable so tests can
	// point the mirror at an httptest server.
	DefaultAPIBase = "https://api.github.com"
	// DefaultRefreshInterval is how often the mirror asks GitHub for a
	// newer release.
	//
	// It is NOT paired with the panel's own poll. The panel reads this
	// bridge's cache, not GitHub, so the two cadences are independent
	// and the panel may poll as often as it likes. What this number
	// answers is a different question: how long an unattended
	// deployment may lag a release. Five hours means at most half a
	// working day, at one anonymous API call apiece -- nowhere near
	// GitHub's 60-per-hour unauthenticated limit, which is why the
	// mirror needs no token.
	//
	// (An earlier comment justified it as "slightly shorter than the
	// panel's 6h check". That pairing is gone; the panel now polls
	// every 30 minutes and would out-run any bridge interval.)
	DefaultRefreshInterval = 5 * time.Hour

	// ManifestName is the ESP Web Tools / ESPHome update manifest. It
	// is the one mirrored file the bridge does not serve byte-for-byte:
	// Manifest() rewrites each build's `ota.path` to the
	// generation-qualified route, because ESPHome resolves a relative path
	// against the manifest's own URL and would otherwise send every
	// panel to the newest generation regardless of which manifest it
	// read. The `md5` beside it is never touched. See Manifest().
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

	// Two generations stay on disk: the one being served and the one it
	// replaced. That is a correctness requirement, not caution. The
	// panel reads the manifest on its own schedule and installs when a
	// person clicks, which can be hours later; it carries the MD5 it
	// read at check time. If the newer release had replaced the older
	// one in between, that click would download a binary the held MD5
	// does not describe and fail -- safely, but visibly, and exactly in
	// the moments right after a release. Keeping the predecessor,
	// together with the generation-qualified path the served manifest
	// points at, makes the URL the panel captured stay valid and keep
	// returning the same bytes. See prune, which names both tags
	// explicitly rather than inferring them.
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
	// Sums is the SHA-256 of every file, as the release's own
	// SHA256SUMS gave it. Kept so the cache can be re-verified later:
	// without it, a file truncated or emptied after it was mirrored
	// would be served forever. The panel would discard it on the MD5
	// check every time, and the mirror would never notice, because the
	// GitHub tag has not changed.
	Sums map[string]string `json:"sums,omitempty"`
	// ReleaseURL is the human-facing page for this tag, taken from the
	// API response, so a log line or an operator's curl leads
	// somewhere readable.
	ReleaseURL string `json:"release_url,omitempty"`
}

// Dir is the cache directory this release occupies, and the first
// segment of the generation-qualified URLs its manifest points at.
//
// Tag plus a digest of the release's own checksums, because a tag does
// not identify bytes. `release.yml` attaches assets with
// `gh release upload --clobber`, which deletes the existing ones before
// uploading, so re-running that job replaces the binaries under an
// unchanged tag — and an ESPHome build is not bit-reproducible, so the
// replacement genuinely differs. Keyed by tag alone, the new bytes
// would land on top of the old ones, and a panel that had already read
// the previous manifest would download them under the URL it saved and
// discard them on the MD5 check.
//
// Content-addressed, the two generations simply coexist and the normal
// retention rule keeps the predecessor reachable until it ages out.
func (r Release) Dir() string {
	names := make([]string, 0, len(r.Sums))
	for name := range r.Sums {
		names = append(names, name)
	}
	sort.Strings(names)

	h := sha256.New()
	for _, name := range names {
		_, _ = fmt.Fprintf(h, "%s:%s\n", name, r.Sums[name])
	}
	return r.Tag + "-" + hex.EncodeToString(h.Sum(nil))[:12]
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
	// minInterval and lastAttempt implement the API-call floor.
	// lastAttempt records attempts, not successes: a caller who can
	// make the mirror fail must not thereby be able to make it retry
	// faster. It is guarded by mu rather than refreshMu so Run can ask
	// how long the floor still has to run without blocking behind a
	// refresh that may take minutes.
	minInterval time.Duration

	// mu guards the published pointer only — the short critical
	// section every request takes, distinct from the minutes-long
	// refreshMu.
	mu          sync.RWMutex
	current     *Release
	lastAttempt time.Time

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
	// Unconditionally, and before anything can return: a process killed
	// during its very FIRST download leaves a .staging-* directory and
	// no state file at all, so a cleanup that ran only after a
	// successful adopt would never reach it. On a box with unreliable
	// power that is one near-complete directory per boot, on a
	// persistent volume, until some refresh finally succeeds.
	m.pruneStaging()

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

	for _, name := range rel.Files {
		if err := validateAssetName(name); err != nil {
			return err
		}
	}
	// Content, not just presence. A file that is there but truncated
	// would otherwise be adopted and served, and the panel would fail
	// its MD5 check on every attempt with nothing here to explain it.
	if err := m.verifyCached(rel); err != nil {
		return fmt.Errorf("firmware: cached release %s is unusable: %w", rel.Tag, err)
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

// Open returns a file from the cached release, the release it came from,
// and its modification time (for http.ServeContent's caching headers).
//
// The release is returned rather than left to a separate Current() call
// because the two would not be atomic: a refresh landing between them
// would let a handler label v2's bytes with v1's tag, or 404 a file the
// generation it asked about does carry. One read of the published
// pointer, one answer.
//
// name is matched against the cached release's file list rather than
// being joined onto a path. That allowlist is what makes traversal
// impossible: every name in it was validated as a plain base name when
// it was downloaded, so a request for "../../etc/passwd" fails the
// membership test long before it reaches the filesystem.
func (m *Mirror) Open(name string) (io.ReadSeekCloser, Release, time.Time, error) {
	m.mu.RLock()
	cur := m.current
	m.mu.RUnlock()

	if cur == nil || !slices.Contains(cur.Files, name) {
		return nil, Release{}, time.Time{}, ErrNotCached
	}

	p := filepath.Join(m.cacheDir, cur.Dir(), name)
	f, err := os.Open(p) //nolint:gosec // name is allowlisted above
	if err != nil {
		return nil, Release{}, time.Time{}, fmt.Errorf("firmware: open %q: %w", p, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, Release{}, time.Time{}, fmt.Errorf("firmware: stat %q: %w", p, err)
	}
	return f, *cur, info.ModTime(), nil
}

// OpenAt returns a file from a specific mirrored generation, which is
// what the generation-qualified paths in the served manifest point at.
//
// Unlike Open it is not restricted to the current release: the whole
// point is that a manifest a panel read hours ago keeps resolving to
// the bytes it described. It is bounded by what is on disk instead, and
// the mirror keeps the current and the previous one on disk.
//
// Both components are validated with the same rules that governed them
// on the way in, so a request cannot name a directory or a file the
// mirror would not itself have created.
func (m *Mirror) OpenAt(generation, name string) (io.ReadSeekCloser, time.Time, error) {
	if validateTag(generation) != nil || validateAssetName(name) != nil {
		return nil, time.Time{}, ErrNotCached
	}
	p := filepath.Join(m.cacheDir, generation, name)
	f, err := os.Open(p) //nolint:gosec // both path components validated above
	if err != nil {
		// Only "it is not there" is a 404. A permission problem or a
		// failing disk is a broken cache, and reporting it as
		// not-found would hide it completely: the panel's update would
		// fail with nothing in the log to explain why.
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return nil, time.Time{}, ErrNotCached
		}
		return nil, time.Time{}, fmt.Errorf("firmware: open %q: %w", p, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, time.Time{}, fmt.Errorf("firmware: stat %q: %w", p, err)
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, time.Time{}, ErrNotCached
	}
	return f, info.ModTime(), nil
}

// Manifest returns the update manifest as the bridge publishes it: the
// mirrored file with each build's `ota.path` rewritten to the absolute,
// generation-qualified path this bridge serves that generation at.
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

	raw, err := os.ReadFile(filepath.Join(m.cacheDir, rel.Dir(), ManifestName)) //nolint:gosec // rel.Tag was validated on the way in
	if err != nil {
		return nil, Release{}, fmt.Errorf("firmware: read manifest of %s: %w", rel.Tag, err)
	}
	out, err := renderManifest(raw, rel)
	if err != nil {
		return nil, Release{}, err
	}
	return out, rel, nil
}

// renderManifest rewrites every build's `ota.path` to the
// generation-qualified route and fails if it cannot.
//
// Strict on purpose. A best-effort rewrite that skipped a build it did
// not recognise would publish a manifest whose paths are still relative
// — silently breaking the one invariant this whole mechanism exists for,
// with no error anywhere. Everything below is therefore a hard failure:
// no builds, a build without an `ota.path`, a path naming a file this
// release does not carry, or an absolute URL pointing somewhere the
// bridge does not control.
//
// Refresh runs this before publishing, so a release whose manifest
// cannot be rendered never becomes current in the first place — the
// same "verify, then publish" ordering as the checksums.
func renderManifest(raw []byte, rel Release) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("firmware: decode manifest of %s: %w", rel.Tag, err)
	}

	builds, ok := doc["builds"].([]any)
	if !ok || len(builds) == 0 {
		return nil, fmt.Errorf("firmware: manifest of %s has no builds", rel.Tag)
	}

	for i, b := range builds {
		build, ok := b.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("firmware: manifest of %s: builds[%d] is not an object", rel.Tag, i)
		}
		ota, ok := build["ota"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("firmware: manifest of %s: builds[%d] has no ota block", rel.Tag, i)
		}
		p, ok := ota["path"].(string)
		if !ok || p == "" {
			return nil, fmt.Errorf("firmware: manifest of %s: builds[%d].ota has no path", rel.Tag, i)
		}
		if strings.Contains(p, "://") {
			return nil, fmt.Errorf(
				"firmware: manifest of %s: builds[%d].ota.path %q is an absolute URL; the bridge only serves what it mirrored",
				rel.Tag, i, p)
		}
		// The path has to name a file this release actually carries, or
		// the manifest would advertise a download that 404s -- the same
		// failure the staging-then-publish ordering exists to prevent,
		// arriving through the manifest instead.
		name := path.Base(p)
		if !slices.Contains(rel.Files, name) {
			return nil, fmt.Errorf(
				"firmware: manifest of %s: builds[%d].ota.path names %q, which the release does not carry",
				rel.Tag, i, name)
		}
		ota["path"] = "/firmware/" + rel.Dir() + "/" + name
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("firmware: encode manifest of %s: %w", rel.Tag, err)
	}
	return out, nil
}

// checksumsUnchanged reports whether the release still ships the exact
// checksums the mirror recorded. It is the upstream half of the
// same-tag question; verifyCached is the local half.
func (m *Mirror) checksumsUnchanged(ctx context.Context, rel Release, assets map[string]string) (bool, error) {
	url, ok := assets[ChecksumsName]
	if !ok {
		return false, fmt.Errorf("firmware: release %s carries no %s asset", rel.Tag, ChecksumsName)
	}
	sums, names, err := m.fetchChecksums(ctx, url)
	if err != nil {
		return false, err
	}
	if len(names) != len(rel.Files) {
		return false, nil
	}
	for _, name := range names {
		if !strings.EqualFold(sums[name], rel.Sums[name]) {
			return false, nil
		}
	}
	return true, nil
}

// verifyCached re-hashes every file of rel against the checksums the
// release shipped with. It is what makes "the tag has not changed" a
// safe reason to skip a download.
//
// A release adopted from a state file written before Sums existed
// carries none, and is treated as unverifiable rather than as fine —
// the mirror then re-downloads once, which is the cheap and correct
// direction to fail in.
func (m *Mirror) verifyCached(rel Release) error {
	if len(rel.Sums) == 0 {
		return errors.New("firmware: cached release carries no checksums")
	}
	dir := filepath.Join(m.cacheDir, rel.Dir())
	for _, name := range rel.Files {
		want, ok := rel.Sums[name]
		if !ok {
			return fmt.Errorf("firmware: no checksum recorded for %q", name)
		}
		f, err := os.Open(filepath.Join(dir, name)) //nolint:gosec // both components validated on the way in
		if err != nil {
			return fmt.Errorf("firmware: open cached %q: %w", name, err)
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("firmware: read cached %q: %w", name, err)
		}
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
			return fmt.Errorf("firmware: cached %q is %s, recorded as %s", name, got, want)
		}
	}
	return nil
}

// throttleRemaining reports how much of the API-call floor is left,
// or zero when a refresh may proceed.
func (m *Mirror) throttleRemaining() time.Duration {
	if m.minInterval <= 0 {
		return 0
	}
	m.mu.RLock()
	last := m.lastAttempt
	m.mu.RUnlock()
	if last.IsZero() {
		return 0
	}
	if remaining := m.minInterval - time.Since(last); remaining > 0 {
		return remaining
	}
	return 0
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
//
// A trigger that lands inside the API-call floor is re-armed for the
// moment the floor expires rather than dropped. Dropping it would mean
// a "Check for Update" pressed four minutes after the scheduled poll
// did nothing at all until the next five-hourly tick, while the route
// had already answered 202 — late is a delay, never is a bug.
func (m *Mirror) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Created stopped; armed below when a wake-up has to happen later
	// than now -- either because the API-call floor is still running or
	// because the attempt failed and deserves a prompt retry.
	deferred := time.NewTimer(time.Hour)
	deferred.Stop()
	defer deferred.Stop()
	deferredPending := false

	arm := func(wait time.Duration, msg string) {
		if deferredPending {
			return
		}
		deferredPending = true
		deferred.Reset(wait)
		m.logger.Info(msg, slog.Duration("retry_in", wait.Round(time.Second)))
	}

	// A failed refresh is retried a few times before falling back to
	// the ordinary interval. The window that motivates it is routine:
	// release.yml creates the release first and uploads the firmware in
	// a following job, so a refresh landing in between finds no
	// SHA256SUMS and fails. Waiting five hours for the next tick would
	// mean a "Check for Update" pressed during a release sees the old
	// build despite all three of its manifest reads. Bounded, so a
	// genuinely unreachable GitHub does not turn into a poll every five
	// minutes forever.
	const maxRetries = 3
	failures := 0

	// The refresh at startup. Its failure is handled exactly like any
	// other -- an earlier version ran it before the retry machinery
	// existed, so a daemon started during a release waited a full
	// interval before trying again, which is the very window the retry
	// was added for.
	if err := m.refreshLogged(ctx); err != nil {
		failures++
		arm(m.retryDelay(), "firmware refresh failed, retrying")
	}

	for {
		// A wake-up that came from the deferred timer continues
		// whatever series armed it; anything else is a fresh event and
		// starts the retry budget over. Without that, four consecutive
		// failures pinned `failures` at maxRetries forever, so every
		// later tick or press got a single attempt and no retry -- and
		// a second pass through the release window would go unnoticed
		// for five hours.
		continuation := false

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-deferred.C:
			deferredPending = false
			continuation = true
		case <-m.trigger:
		}

		if !continuation {
			failures = 0
		}

		// One rule for every wake-up, not just the button. A manual
		// refresh shortly before the five-hourly tick would otherwise
		// leave that tick to hit the floor, get a ThrottledError, and
		// be dropped -- so a release published in between would go
		// unnoticed for another five hours, despite a check having
		// been due.
		//
		// arm() ignores a second call while one is pending, and only
		// for redundancy's sake -- not for correctness.
		// throttleRemaining counts down from lastAttempt, which does
		// not move while the floor runs, so re-arming would land on
		// the same instant anyway; a caller in a loop cannot push the
		// refresh out.
		if wait := m.throttleRemaining(); wait > 0 {
			arm(wait, "firmware refresh deferred by the API-call floor")
			continue
		}

		if err := m.refreshLogged(ctx); err != nil {
			if failures < maxRetries {
				failures++
				arm(m.retryDelay(), "firmware refresh failed, retrying")
			} else {
				// Otherwise the log shows three retries and then
				// silence for five hours, and nothing says which of
				// those two things happened.
				m.logger.Warn("firmware refresh gave up after its retries; waiting for the next scheduled check",
					slog.Int("attempts", failures+1),
					slog.Duration("next_check_in", m.interval))
			}
			continue
		}
		failures = 0
	}
}

// refreshLogged runs one attempt and reports whether it failed, so Run
// can decide to retry. A throttled attempt is not a failure.
func (m *Mirror) refreshLogged(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()

	rel, err := m.Refresh(ctx)
	if err != nil {
		var throttled *ThrottledError
		if errors.As(err, &throttled) {
			// Run checks the floor before calling this, so anything
			// reaching here came from a direct Refresh call.
			m.logger.Debug("firmware refresh throttled",
				slog.Duration("retry_in", throttled.RetryAfter.Round(time.Second)))
			return nil
		}
		// Warn, not Error: an unreachable GitHub is an ordinary
		// condition for a home-lab box, and the mirror keeps serving
		// whatever it already has.
		m.logger.Warn("firmware refresh failed", slog.Any("err", err))
		return err
	}
	m.logger.Info("firmware mirror current",
		slog.String("tag", rel.Tag),
		slog.Int("files", len(rel.Files)))
	return nil
}

// retryDelay is how long Run waits before retrying a failed refresh:
// the earliest the API-call floor allows, and never zero, so a mirror
// configured without a floor still cannot spin.
func (m *Mirror) retryDelay() time.Duration {
	if wait := m.throttleRemaining(); wait > 0 {
		return wait
	}
	if m.minInterval > 0 {
		return m.minInterval
	}
	return time.Second
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
	//
	// Returning ErrThrottled rather than the current release: a caller
	// that hit the floor did NOT get a fresh check, and Run needs to
	// know that so it can re-arm rather than drop the request.
	if wait := m.throttleRemaining(); wait > 0 {
		return Release{}, &ThrottledError{RetryAfter: wait}
	}
	m.mu.Lock()
	m.lastAttempt = time.Now()
	m.mu.Unlock()

	latest, err := m.latestRelease(ctx)
	if err != nil {
		return Release{}, err
	}
	if err := validateTag(latest.TagName); err != nil {
		return Release{}, err
	}

	assets := make(map[string]string, len(latest.Assets))
	for _, a := range latest.Assets {
		assets[a.Name] = a.BrowserDownloadURL
	}

	cur, haveCurrent := m.Current()
	previousDir := ""
	if haveCurrent {
		previousDir = cur.Dir()
	}

	if haveCurrent && cur.Tag == latest.TagName {
		// Same tag is not the same thing as "same bytes", in either
		// direction.
		//
		// Locally: a file deleted or truncated after it was mirrored
		// would be served until a new release happened to appear -- the
		// panel discarding it on the MD5 check every single time while
		// the mirror short-circuited before it could notice.
		//
		// Upstream: release.yml attaches assets with `gh release upload
		// --clobber`, which deletes the existing assets before
		// uploading. Re-running that job replaces the binaries under an
		// unchanged tag, so the tag alone cannot say whether the mirror
		// is current. Fetching SHA256SUMS is a few hundred bytes and
		// settles it.
		//
		// The cheap path is taken only when both agree.
		if err := m.verifyCached(cur); err != nil {
			m.logger.Warn("cached firmware failed verification, re-downloading",
				slog.String("tag", cur.Tag), slog.Any("err", err))
		} else if same, err := m.checksumsUnchanged(ctx, cur, assets); err != nil {
			m.logger.Warn("could not compare the release checksums, re-downloading",
				slog.String("tag", cur.Tag), slog.Any("err", err))
		} else if same {
			m.pruneStaging()
			return cur, nil
		} else {
			m.logger.Info("release assets changed under an unchanged tag, re-downloading",
				slog.String("tag", cur.Tag))
		}
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
	defer func() {
		// Logged, not dropped: a staging directory holds up to
		// maxAssetBytes, and a cleanup that keeps failing silently
		// fills the volume one failed refresh at a time.
		if err := os.RemoveAll(staging); err != nil {
			m.logger.Warn("firmware staging cleanup failed",
				slog.String("entry", filepath.Base(staging)), slog.Any("err", err))
		}
	}()

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

	rel := Release{
		Tag:         latest.TagName,
		Files:       names,
		Sums:        sums,
		RetrievedAt: time.Now().UTC(),
		ReleaseURL:  latest.HTMLURL,
	}

	// Render the manifest while everything is still in staging. A
	// release whose manifest cannot be rewritten to generation-qualified
	// paths is one the panel could not safely install from, so it must
	// not reach the cache at all — the same "verify, then publish"
	// ordering as the checksums, one level up. The rendered bytes are
	// discarded; Manifest() rebuilds them per request, which is cheap
	// and keeps a single source of truth.
	rawManifest, err := os.ReadFile(filepath.Join(staging, ManifestName)) //nolint:gosec // staging is ours, ManifestName is a constant
	if err != nil {
		return Release{}, fmt.Errorf("firmware: read manifest of %s: %w", rel.Tag, err)
	}
	if _, err := renderManifest(rawManifest, rel); err != nil {
		return Release{}, err
	}

	dest := filepath.Join(m.cacheDir, rel.Dir())

	// A directory may already exist here, and it may well be the one
	// being served right now.
	//
	// That is not the crash-leftover case; it is the ordinary repair
	// path. verifyCached failing sends an unchanged tag down the full
	// download, and the bytes it fetches are the ones the checksums
	// describe -- so Dir(), which is derived from those checksums,
	// comes out identical to the generation already published. dest and
	// the live generation are then the same directory.
	//
	// (An earlier version of this comment claimed the opposite, on the
	// grounds that identical content would have short-circuited. It
	// does not, when the local copy is damaged -- which is precisely
	// when this path runs.)
	//
	// Hence: move aside, rename, and only then discard; restore if the
	// rename fails. Deleting first would destroy a served generation on
	// any rename error. The parking name carries the staging prefix so
	// prune sweeps it up if the process dies mid-swap; validateTag
	// rejects leading dots, so it can never collide with a real
	// generation.
	//nolint:gocritic // the seam is deliberate; see renameFile
	parked := filepath.Join(m.cacheDir, stagingPrefix+"replaced-"+rel.Dir())
	if err := os.RemoveAll(parked); err != nil {
		return Release{}, fmt.Errorf("firmware: clear the parking slot %q: %w", parked, err)
	}
	hadOld := false
	if _, err := os.Stat(dest); err == nil {
		if err := renameFile(dest, parked); err != nil {
			return Release{}, fmt.Errorf("firmware: set aside %q: %w", dest, err)
		}
		hadOld = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return Release{}, fmt.Errorf("firmware: stat %q: %w", dest, err)
	}
	if err := renameFile(staging, dest); err != nil {
		if hadOld {
			// Put it back. Serving the old release is strictly better
			// than serving nothing.
			if restoreErr := os.Rename(parked, dest); restoreErr != nil {
				m.logger.Error("firmware cache left without its published release",
					slog.String("tag", latest.TagName), slog.Any("err", restoreErr))
			}
		}
		return Release{}, fmt.Errorf("firmware: publish %q: %w", dest, err)
	}
	if hadOld {
		if err := os.RemoveAll(parked); err != nil {
			m.logger.Warn("firmware cache cleanup failed",
				slog.String("entry", filepath.Base(parked)), slog.Any("err", err))
		}
	}
	// MkdirTemp creates 0700. The daemon is the only reader, but an
	// operator inspecting the volume should not need root.
	if err := os.Chmod(dest, 0o755); err != nil {
		return Release{}, fmt.Errorf("firmware: chmod %q: %w", dest, err)
	}

	// Publish first, record afterwards. The bytes are downloaded,
	// checksum-verified and in place; refusing to serve them because a
	// bookkeeping file could not be written would strand a good release
	// on disk -- and the next refresh would hit the same failure, so it
	// would stay stranded. state.json only governs what a restart
	// adopts before the first refresh completes; a stale one means the
	// daemon comes back on the previous generation, which is still
	// cached and still valid, and the first refresh corrects it.
	m.publish(&rel)
	if err := m.writeState(rel); err != nil {
		m.logger.Error("firmware cache state not recorded; a restart will fall back to the previous release",
			slog.String("tag", rel.Tag), slog.Any("err", err))
	}

	// Only now is the old directory unreferenced.
	//
	// An in-place repair (previousDir == rel.Dir()) added no directory,
	// so there is nothing to evict -- and pruning anyway would pass the
	// same name as both survivors and delete the genuine predecessor,
	// dropping the mirror to one generation and 404ing a URL some panel
	// may be holding. Staging leftovers are still swept; a crash orphan
	// waits for the next real release.
	if previousDir == rel.Dir() {
		m.pruneStaging()
	} else {
		m.prune(rel.Dir(), previousDir)
	}

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

	// +1 so an over-long body is detected rather than silently cut: a
	// truncated payload that happens to end on a syntactically valid
	// boundary would otherwise be decoded as a release with fewer
	// assets, and the mirror would report "no SHA256SUMS asset" for a
	// release that has one.
	body := io.LimitReader(resp.Body, maxReleaseJSONBytes+1)
	raw, err := io.ReadAll(body)
	if err != nil {
		return ghRelease{}, fmt.Errorf("firmware: read release payload from %s: %w", url, err)
	}
	if len(raw) > maxReleaseJSONBytes {
		return ghRelease{}, fmt.Errorf(
			"firmware: release payload from %s exceeds the %d-byte limit", url, maxReleaseJSONBytes)
	}

	var rel ghRelease
	if err := json.Unmarshal(raw, &rel); err != nil {
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

	// +1, for the same reason as the asset download: SHA256SUMS is
	// line-oriented, so a body cut at the limit can still parse -- as a
	// SHORTER list. The mirror would then download a subset, and the
	// failure would surface later as "the manifest names a file the
	// release does not carry", which points at the wrong thing.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumsBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("firmware: read %s from %s: %w", ChecksumsName, url, err)
	}
	if len(raw) > maxChecksumsBytes {
		return nil, nil, fmt.Errorf(
			"firmware: %s from %s exceeds the %d-byte limit", ChecksumsName, url, maxChecksumsBytes)
	}

	sums, names, err := parseChecksums(strings.NewReader(string(raw)))
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

// prune removes every cached directory except the two generations that
// are allowed to exist: keep (the one just published) and previous (the
// one it replaced). Staging leftovers always go.
//
// Explicit tags rather than "the N newest by mtime". A daemon killed
// between the rename and the publish leaves a fully downloaded but
// never-advertised directory behind, and it is NEWER than the
// generation panels actually hold a versioned URL for -- so an
// mtime-based rule would keep the orphan and delete the one still being
// installed from, turning that install into a 404. Which two matter is
// something the mirror knows; it should not have to infer it.
//
// Failures are logged, not returned: stale bytes cost disk, but the
// refresh they follow already succeeded and reporting it as failed
// would be a lie.
func (m *Mirror) prune(keep, previous string) {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.logger.Warn("firmware cache prune failed", slog.Any("err", err))
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keep || (previous != "" && e.Name() == previous) {
			continue
		}
		m.remove(e.Name())
	}
}

// pruneStaging removes abandoned download and parking directories and
// nothing else.
//
// Called on the paths prune does not reach: adopting a cache at
// startup, and a refresh that short-circuits on an unchanged tag. A
// process killed mid-download leaves a .staging-* directory in a
// persistent volume, and without this those accumulate -- on a Pi, one
// per interrupted refresh until the next release happens to land.
func (m *Mirror) pruneStaging() {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.logger.Warn("firmware staging cleanup failed", slog.Any("err", err))
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), stagingPrefix) {
			m.remove(e.Name())
		}
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
