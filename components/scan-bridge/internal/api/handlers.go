package api

import (
	"encoding/json"
	"net/http"
)

// writeJSON serialises body as JSON and writes it with the supplied
// status code. The caller is responsible for not having already
// written headers.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

// healthResponse is the small payload returned by /health. The schema
// is intentionally minimal so the endpoint is cheap and stable.
type healthResponse struct {
	Status string `json:"status"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

// versionResponse mirrors the /version contract from CONTAINER_SUITE.md
// sec. 4.4.
type versionResponse struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, versionResponse{
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

func (s *Server) handleProfilesList(w http.ResponseWriter, _ *http.Request) {
	all := s.Profiles.All()
	out := make([]profileSummary, 0, len(all))
	for _, p := range all {
		out = append(out, profileSummary{
			Name:        p.Name,
			Description: p.Description,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out})
}

func (s *Server) handleProfileDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, ok := s.Profiles.Get(name)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{
			Error: "profile_not_found",
			Hint:  "GET /profiles to list configured profile names.",
		})
		return
	}
	writeJSON(w, http.StatusOK, p)
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
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotImplemented, errorResponse{
			Error: "not_implemented",
			Hint:  reason,
		})
	}
}
