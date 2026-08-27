package api

import (
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
	// Open returns a file of that release and its modification time,
	// or firmware.ErrNotCached.
	Open(name string) (io.ReadSeekCloser, time.Time, error)
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

// handleFirmwareFile serves one file of the mirrored release, including
// manifest.json — the manifest is simply one of the release's assets,
// so it needs no route of its own.
func (s *Server) handleFirmwareFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	rel, ok := s.Firmware.Current()
	if !ok {
		// 503, not 404: the resource exists in principle and the
		// mirror is expected to have it shortly. A panel that saw 404
		// would have no reason to come back.
		s.writeJSON(w, r, http.StatusServiceUnavailable, errorResponse{
			Error: "firmware_not_cached",
			Hint:  "The bridge has not mirrored a release yet. POST /firmware/refresh, or wait for the periodic refresh.",
		})
		return
	}

	f, modTime, err := s.Firmware.Open(name)
	if err != nil {
		if errors.Is(err, firmware.ErrNotCached) {
			s.writeJSON(w, r, http.StatusNotFound, errorResponse{
				Error: "firmware_file_not_found",
				Hint:  "GET /firmware/manifest.json names the files of the mirrored release.",
			})
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
	// So `curl -I` answers "which build is this bridge handing out?"
	// without downloading 1.7 MB.
	w.Header().Set("X-Firmware-Version", rel.Tag)
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

func firmwareContentType(name string) string {
	switch path.Ext(name) {
	case ".json":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
