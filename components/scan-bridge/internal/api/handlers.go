package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"syscall"
)

// writeJSON serialises body as JSON and writes it with the supplied
// status code. Encode errors are logged at warn level — by the time
// json.Encoder fails we have already written the status line, so we
// cannot recover the response, but the failure must not be silent
// (per the project's "no swallowed errors" rule). EPIPE / connection-
// reset errors are common when a client hangs up mid-response and
// are noisy; we log them at debug.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		level := slog.LevelWarn
		if errors.Is(err, syscall.EPIPE) ||
			errors.Is(err, syscall.ECONNRESET) {
			level = slog.LevelDebug
		}
		s.Logger.LogAttrs(r.Context(), level, "json encode failed",
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Any("err", err))
	}
}

// healthResponse is the small payload returned by /health. The schema
// is intentionally minimal so the endpoint is cheap and stable.
type healthResponse struct {
	Status string `json:"status"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, healthResponse{Status: "ok"})
}

// versionResponse mirrors the /version contract from CONTAINER_SUITE.md
// sec. 4.4.
type versionResponse struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, versionResponse{
		Version:   s.Build.Version,
		Commit:    s.Build.Commit,
		BuildDate: s.Build.BuildDate,
	})
}

// profileSummary is the list-shape of /profiles. Detail is served by
// handleProfileDetail.
type profileSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleProfilesList(w http.ResponseWriter, r *http.Request) {
	all := s.Profiles.All()
	out := make([]profileSummary, 0, len(all))
	for _, p := range all {
		out = append(out, profileSummary{
			Name:        p.Name,
			Description: p.Description,
		})
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"profiles": out})
}

func (s *Server) handleProfileDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, ok := s.Profiles.Get(name)
	if !ok {
		s.writeJSON(w, r, http.StatusNotFound, errorResponse{
			Error: "profile_not_found",
			Hint:  "GET /profiles to list configured profile names.",
		})
		return
	}
	s.writeJSON(w, r, http.StatusOK, p)
}

// errorResponse is the canonical error envelope used by 4xx and 5xx
// responses. Subsystems can extend this without breaking the contract.
type errorResponse struct {
	Error string `json:"error"`
	Hint  string `json:"hint,omitempty"`
}

// notImplemented is wired to every endpoint whose backing subsystem
// has not landed yet. It returns 501 with a uniform body so clients
// can rely on the schema even before the full daemon is feature
// complete.
func (s *Server) notImplemented(reason string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.writeJSON(w, r, http.StatusNotImplemented, errorResponse{
			Error: "not_implemented",
			Hint:  reason,
		})
	}
}
