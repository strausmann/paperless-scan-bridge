package api

import (
	"strings"
	"testing"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
)

// fixedTime is a stable stand-in for "now" so a title test asserts on
// formatting rather than on the clock.
var fixedTime = time.Date(2026, 3, 14, 9, 5, 0, 0, time.UTC)

// TestRenderTitleSubstitutesPlaceholders covers the whole placeholder
// set in one realistic template, which is how an operator will actually
// write one.
func TestRenderTitleSubstitutesPlaceholders(t *testing.T) {
	t.Parallel()

	p := profiles.Profile{Name: "receipts", DocumentType: "kassenbon"}

	got := renderTitle("{profile} {document_type} {date} {time}", p, "abc123", fixedTime)

	if want := "receipts kassenbon 2026-03-14 09-05"; got != want {
		t.Fatalf("renderTitle = %q, want %q", got, want)
	}
}

// TestRenderTitleScanID exists because the scan ID is the one field that
// makes a title unique when everything else collides.
func TestRenderTitleScanID(t *testing.T) {
	t.Parallel()

	got := renderTitle("scan-{scan_id}", profiles.Profile{Name: "x"}, "abc123", fixedTime)

	if want := "scan-abc123"; got != want {
		t.Fatalf("renderTitle = %q, want %q", got, want)
	}
}

// TestRenderTitleEmptyTemplateYieldsEmpty locks in the opt-in: a profile
// without title_template must send no title at all, so the destination
// keeps whatever default it had (Paperless derives one from the
// filename). Returning a made-up title here would be a silent behaviour
// change for every existing profile.
func TestRenderTitleEmptyTemplateYieldsEmpty(t *testing.T) {
	t.Parallel()

	if got := renderTitle("", profiles.Profile{Name: "x"}, "abc123", fixedTime); got != "" {
		t.Fatalf("renderTitle(\"\") = %q, want empty", got)
	}
}

// TestRenderTitleUnknownPlaceholderIsLeftAlone: a typo must be visible in
// the result rather than silently deleted, so the operator can see what
// they wrote.
func TestRenderTitleUnknownPlaceholderIsLeftAlone(t *testing.T) {
	t.Parallel()

	got := renderTitle("{profile} {nope}", profiles.Profile{Name: "x"}, "id", fixedTime)

	if want := "x {nope}"; got != want {
		t.Fatalf("renderTitle = %q, want %q", got, want)
	}
}

// TestRenderTitleCollapsesWhitespace: an unset document_type would
// otherwise leave a double space or a dangling separator in the middle
// of the title.
func TestRenderTitleCollapsesWhitespace(t *testing.T) {
	t.Parallel()

	p := profiles.Profile{Name: "scan"} // DocumentType deliberately empty

	got := renderTitle("{profile} {document_type} {date}", p, "id", fixedTime)

	if want := "scan 2026-03-14"; got != want {
		t.Fatalf("renderTitle = %q, want %q", got, want)
	}
	if strings.Contains(got, "  ") {
		t.Errorf("result contains a double space: %q", got)
	}
}

// TestResolveMetadataSetsTitleFromProfile wires the renderer into the
// metadata the destination actually receives — the renderer being
// correct is worth nothing if nobody calls it.
func TestResolveMetadataSetsTitleFromProfile(t *testing.T) {
	t.Parallel()

	p := profiles.Profile{Name: "receipts", TitleTemplate: "{profile} {date}"}

	meta := resolveMetadata(p, destinations.ProfileDestinationConfig{}, nil, "", "abc123", fixedTime)

	if want := "receipts 2026-03-14"; meta.Title != want {
		t.Fatalf("meta.Title = %q, want %q", meta.Title, want)
	}
}

// TestResolveMetadataLeavesTitleEmptyWithoutTemplate is the regression
// guard for existing profiles: no title_template must mean no title
// field on the wire, not an invented one.
func TestResolveMetadataLeavesTitleEmptyWithoutTemplate(t *testing.T) {
	t.Parallel()

	meta := resolveMetadata(profiles.Profile{Name: "receipts"},
		destinations.ProfileDestinationConfig{}, nil, "", "abc123", fixedTime)

	if meta.Title != "" {
		t.Fatalf("meta.Title = %q, want empty", meta.Title)
	}
}
