package api

import (
	"strings"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
)

// renderTitle expands a profile's title_template into the document title
// sent to a destination.
//
// Why this exists: nothing ever populated destinations.Metadata.Title,
// so no title reached Paperless-ngx and it fell back to the uploaded
// filename -- which is the scan ID, a 32-character hex string. Every
// document arrived named like "7cc2ba0a36df384ca12f977b2bc64ddc".
//
// The substitution is deliberately literal rather than text/template:
// an operator writes these in a YAML profile, and a Go template's error
// modes (a stray brace, a missing field) would surface as a scan-time
// failure on a field that is cosmetic. Here the worst case is a title
// that still contains "{typo}", which is visible and self-explaining.
//
// An empty template returns an empty string, and the caller leaves
// Metadata.Title unset -- the destination keeps whatever default it had.
// That keeps this opt-in: profiles written before this feature behave
// exactly as before.
func renderTitle(tmpl string, p profiles.Profile, scanID string, now time.Time) string {
	if strings.TrimSpace(tmpl) == "" {
		return ""
	}

	// Colons are illegal in filenames on Windows and awkward in a
	// Paperless title, so the time uses hyphens.
	replacer := strings.NewReplacer(
		"{profile}", p.Name,
		"{document_type}", p.DocumentType,
		"{scan_id}", scanID,
		"{date}", now.Format("2006-01-02"),
		"{time}", now.Format("15-04"),
		"{datetime}", now.Format("2006-01-02 15-04"),
	)

	// An unset field (commonly document_type) would otherwise leave a
	// double space or a trailing separator mid-title, so collapse runs
	// of whitespace and trim the ends. Unknown placeholders survive
	// untouched on purpose -- see the doc comment.
	return strings.Join(strings.Fields(replacer.Replace(tmpl)), " ")
}
