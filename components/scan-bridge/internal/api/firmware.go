package api

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/firmware"
)

// FirmwareMirror is the slice of internal/firmware.Mirror the HTTP
// layer needs. An interface so handler tests do not need a cache
// directory and a fake GitHub.
type FirmwareMirror interface {
	// Current reports the release being served; ok is false on a cold
	// cache.
	Current() (firmware.Release, bool)
	// Open returns a file of the current release, the release it
	// actually came from, and its modification time — or
	// firmware.ErrNotCached. The release comes back with the file
	// rather than from a second Current() call so a refresh landing
	// between the two cannot label one generation's bytes with
	// another's tag.
	Open(name string) (io.ReadSeekCloser, firmware.Release, time.Time, error)
	// OpenAt does the same for a named generation, which may be older
	// than the current one. The generation is Release.Dir(), not the
	// bare tag.
	OpenAt(generation, name string) (io.ReadSeekCloser, time.Time, error)
	// Manifest returns the manifest as the bridge publishes it, with
	// generation-qualified binary paths, plus the release it describes.
	Manifest() ([]byte, firmware.Release, error)
	// TriggerRefresh queues a refresh and returns immediately.
	TriggerRefresh() bool
}

// firmwareRefreshResponse is the 202 body of POST /firmware/refresh.
//
// queued is false when a refresh was already pending — the request is
// satisfied either way, which is why the status is 202 in both cases
// rather than a conflict.
type firmwareRefreshResponse struct {
	Queued        bool   `json:"queued"`
	CachedVersion string `json:"cached_version,omitempty"`
}

// handleFirmwareManifest serves the update manifest of the current
// release, with each build's `ota.path` rewritten to the
// generation-qualified path below. Only the path — never the MD5 beside
// it, which is the digest CI computed from the binary it shipped.
func (s *Server) handleFirmwareManifest(w http.ResponseWriter, r *http.Request) {
	body, rel, err := s.Firmware.Manifest()
	if err != nil {
		if errors.Is(err, firmware.ErrNotCached) {
			s.writeFirmwareNotCached(w, r)
			return
		}
		s.Logger.LogAttrs(r.Context(), slog.LevelError, "firmware manifest unreadable",
			slog.Any("err", err))
		s.writeJSON(w, r, http.StatusInternalServerError, errorResponse{
			Error: "firmware_unreadable",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	setFirmwareHeaders(w, rel.Tag, rel.Dir())
	http.ServeContent(w, r, firmware.ManifestName, rel.RetrievedAt, bytes.NewReader(body))
}

// handleFirmwareVersionedFile serves a file of a named generation.
//
// This is what the manifest points at, and it is the route that makes an
// update survive the gap between check and click. A panel reads the
// manifest on its own schedule and installs when a person presses the
// button, which can be hours later, carrying the MD5 it read back then.
// An unversioned path would hand it whatever the newest generation holds
// by that point — a different binary, failing the MD5 check for no
// reason the operator can see. The mirror keeps the previous generation
// on disk precisely so this URL keeps returning the same bytes.
func (s *Server) handleFirmwareVersionedFile(w http.ResponseWriter, r *http.Request) {
	gen, name := r.PathValue("generation"), r.PathValue("name")

	f, modTime, err := s.Firmware.OpenAt(gen, name)
	if err != nil {
		if errors.Is(err, firmware.ErrNotCached) {
			s.writeFirmwareFileNotFound(w, r)
			return
		}
		// A cache that exists but cannot be read is a broken bridge,
		// not a missing file. Reporting it as 404 would hide a failing
		// disk behind an update that simply never arrives.
		s.Logger.LogAttrs(r.Context(), slog.LevelError, "firmware file unreadable",
			slog.String("generation", gen), slog.String("name", name), slog.Any("err", err))
		s.writeJSON(w, r, http.StatusInternalServerError, errorResponse{
			Error: "firmware_unreadable",
		})
		return
	}
	defer func() { _ = f.Close() }()

	w.Header().Set("Content-Type", firmwareContentType(name))
	// Only the generation is known here -- this route is reachable for
	// a generation that is no longer current, and nothing on disk
	// records which tag it belonged to. The tag is the prefix of the
	// generation, so a reader loses nothing.
	setFirmwareHeaders(w, "", gen)
	http.ServeContent(w, r, name, modTime, f)
}

// handleFirmwareFile serves one file of the *current* release by bare
// name. Kept alongside the versioned route for anything that just wants
// "the newest" — an operator with curl, and the manifest itself, which
// the panel is configured to read from a stable URL.
func (s *Server) handleFirmwareFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	// Open first, ask questions afterwards. Probing Current() up front
	// would answer 503 for a request that a refresh completing
	// mid-flight could have served, and a 503 costs the panel a whole
	// poll interval — up to half an hour once its checks are
	// succeeding. Open also returns the release it read, from
	// one lock acquisition, so a refresh landing mid-request cannot put
	// one generation's tag on another generation's bytes.
	f, rel, modTime, err := s.Firmware.Open(name)
	if err != nil {
		if errors.Is(err, firmware.ErrNotCached) {
			// Now the distinction matters: nothing mirrored at all is
			// "come back later", a name this release does not carry is
			// "that does not exist".
			if _, cached := s.Firmware.Current(); !cached {
				s.writeFirmwareNotCached(w, r)
				return
			}
			s.writeFirmwareFileNotFound(w, r)
			return
		}
		s.Logger.LogAttrs(r.Context(), slog.LevelError, "firmware file unreadable",
			slog.String("name", name), slog.Any("err", err))
		s.writeJSON(w, r, http.StatusInternalServerError, errorResponse{
			Error: "firmware_unreadable",
		})
		return
	}
	defer func() { _ = f.Close() }()

	w.Header().Set("Content-Type", firmwareContentType(name))
	setFirmwareHeaders(w, rel.Tag, rel.Dir())
	http.ServeContent(w, r, name, modTime, f)
}

// handleFirmwareRefresh queues a refresh and answers at once.
//
// The panel calls this from its "Check for update" button, and
// ESPHome's http_request is synchronous on the device's main loop: a
// handler that waited for GitHub plus a 1.7 MB download would hold that
// loop past the 60s task watchdog and reboot the panel on the press.
// So this returns 202 immediately and the background loop does the
// work; the result shows up on the next manifest read.
func (s *Server) handleFirmwareRefresh(w http.ResponseWriter, r *http.Request) {
	resp := firmwareRefreshResponse{Queued: s.Firmware.TriggerRefresh()}
	if rel, ok := s.Firmware.Current(); ok {
		resp.CachedVersion = rel.Tag
	}
	s.writeJSON(w, r, http.StatusAccepted, resp)
}

// 503, not 404: the resource exists in principle and the mirror is
// expected to have it shortly. A panel that saw 404 would have no reason
// to come back.
func (s *Server) writeFirmwareNotCached(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusServiceUnavailable, errorResponse{
		Error: "firmware_not_cached",
		Hint:  "The bridge has not mirrored a release yet. POST /firmware/refresh, or wait for the periodic refresh.",
	})
}

func (s *Server) writeFirmwareFileNotFound(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusNotFound, errorResponse{
		Error: "firmware_file_not_found",
		Hint:  "GET /firmware/manifest.json names the files of the mirrored release.",
	})
}

// setFirmwareHeaders labels a response with what it is, so `curl -I`
// answers "which build is this bridge handing out?" without moving
// 1.7 MB.
//
// Two headers, because one cannot say it. A tag does not identify
// bytes: re-running the release workflow replaces the assets under the
// same tag (`gh release upload --clobber`), which is why the cache is
// keyed by generation in the first place. X-Firmware-Version is always
// the human tag and X-Firmware-Generation always the exact content,
// with the same meaning on every route -- an earlier version put the
// generation under the Version header on one route only, which made
// the two responses disagree about what the header meant.
//
// tag may be empty where only the generation is known.
func setFirmwareHeaders(w http.ResponseWriter, tag, generation string) {
	if tag != "" {
		w.Header().Set("X-Firmware-Version", tag)
	}
	w.Header().Set("X-Firmware-Generation", generation)
}

func firmwareContentType(name string) string {
	switch path.Ext(name) {
	case ".json":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
