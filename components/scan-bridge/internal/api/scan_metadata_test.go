package api

import (
	"slices"
	"testing"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/tag"
)

func TestResolveMetadataNoConfigReturnsCallerTagsOnly(t *testing.T) {
	t.Parallel()

	profile := profiles.Profile{}
	cfg := destinations.ProfileDestinationConfig{Target: "paperless", Config: nil}

	got := resolveMetadata(profile, cfg, []int{5, 6}, tag.StrategyAdd)

	if !slices.Equal(got.TagIDs, []int{5, 6}) {
		t.Errorf("TagIDs = %v, want [5 6]", got.TagIDs)
	}
	if got.Correspondent != nil {
		t.Errorf("Correspondent = %v, want nil", got.Correspondent)
	}
	if got.DocumentType != nil {
		t.Errorf("DocumentType = %v, want nil", got.DocumentType)
	}
}

func TestResolveMetadataMergesDestinationDefaultsWithCallerTags(t *testing.T) {
	t.Parallel()

	profile := profiles.Profile{} // no document_type set
	cfg := destinations.ProfileDestinationConfig{
		Target: "paperless",
		Config: map[string]any{
			"tag_ids":          []any{3},
			"tag_strategy":     "add",
			"correspondent_id": 12,
			"document_type_id": 3,
		},
	}

	got := resolveMetadata(profile, cfg, []int{7}, "")

	if !slices.Equal(got.TagIDs, []int{3, 7}) {
		t.Errorf("TagIDs = %v, want [3 7] (destination default + caller, add-merged)", got.TagIDs)
	}
	if got.Correspondent == nil || *got.Correspondent != 12 {
		t.Errorf("Correspondent = %v, want *12", got.Correspondent)
	}
	if got.DocumentType == nil || *got.DocumentType != 3 {
		t.Errorf("DocumentType = %v, want *3", got.DocumentType)
	}
}

// TestResolveMetadataDocumentTypeMapHit pins this implementation's
// documented (and explicitly flagged as an interpretation, not a
// verified spec) choice: a document_type_map hit's own document_type_id
// overrides the destination's base document_type_id, and its tag_ids
// are ADDED to the destination's base tag_ids -- matching the design
// doc's own YAML example (sec. 6) of a profile-level default tag plus
// a per-type tag.
func TestResolveMetadataDocumentTypeMapHit(t *testing.T) {
	t.Parallel()

	profile := profiles.Profile{DocumentType: "eingangsrechnung"}
	cfg := destinations.ProfileDestinationConfig{
		Target: "paperless",
		Config: map[string]any{
			"tag_ids":          []any{3},
			"tag_strategy":     "add",
			"document_type_id": 99, // overridden by the map entry below
			"document_type_map": map[string]any{
				"eingangsrechnung": map[string]any{
					"document_type_id": 3,
					"tag_ids":          []any{7},
				},
				"post": map[string]any{
					"tag_ids": []any{4},
				},
			},
		},
	}

	got := resolveMetadata(profile, cfg, nil, "")

	if got.DocumentType == nil || *got.DocumentType != 3 {
		t.Fatalf("DocumentType = %v, want *3 (from the document_type_map entry, not the base 99)", got.DocumentType)
	}
	if !slices.Equal(got.TagIDs, []int{3, 7}) {
		t.Errorf("TagIDs = %v, want [3 7] (base tag_ids + mapping tag_ids)", got.TagIDs)
	}
}

// TestResolveMetadataDocumentTypeMapMissFallsBackToBaseConfig pins the
// design doc's explicit "a miss falls back to
// cfg.CorrespondentID/cfg.DocumentTypeID/cfg.DefaultTagIDs only (no
// error)" contract (sec. 5.3).
func TestResolveMetadataDocumentTypeMapMissFallsBackToBaseConfig(t *testing.T) {
	t.Parallel()

	profile := profiles.Profile{DocumentType: "unmapped-type"}
	cfg := destinations.ProfileDestinationConfig{
		Target: "paperless",
		Config: map[string]any{
			"tag_ids":          []any{3},
			"document_type_id": 99,
			"document_type_map": map[string]any{
				"eingangsrechnung": map[string]any{"document_type_id": 3},
			},
		},
	}

	got := resolveMetadata(profile, cfg, nil, "")

	if got.DocumentType == nil || *got.DocumentType != 99 {
		t.Errorf("DocumentType = %v, want *99 (base config, unmapped type is not an error)", got.DocumentType)
	}
	if !slices.Equal(got.TagIDs, []int{3}) {
		t.Errorf("TagIDs = %v, want [3] (base tag_ids, unaffected by the miss)", got.TagIDs)
	}
}

func TestResolveMetadataCallerStrategyOverridesProfileStrategy(t *testing.T) {
	t.Parallel()

	profile := profiles.Profile{}
	cfg := destinations.ProfileDestinationConfig{
		Target: "paperless",
		Config: map[string]any{
			"tag_ids":      []any{3, 7},
			"tag_strategy": "add",
		},
	}

	got := resolveMetadata(profile, cfg, []int{7}, tag.StrategyOverride)

	if !slices.Equal(got.TagIDs, []int{7}) {
		t.Errorf("TagIDs = %v, want [7] (caller's override strategy discards destination defaults)", got.TagIDs)
	}
}

func TestParseDestinationMetadataDefaultsSkipsMalformedValues(t *testing.T) {
	t.Parallel()

	defaults := parseDestinationMetadataDefaults(map[string]any{
		"tag_ids":           "not-a-list",
		"correspondent_id":  "not-an-int",
		"document_type_map": "not-a-map",
	})

	if defaults.tagIDs != nil {
		t.Errorf("tagIDs = %v, want nil for a malformed tag_ids value", defaults.tagIDs)
	}
	if defaults.correspondentID != nil {
		t.Errorf("correspondentID = %v, want nil for a malformed correspondent_id value", defaults.correspondentID)
	}
	if defaults.docTypeMap != nil {
		t.Errorf("docTypeMap = %v, want nil for a malformed document_type_map value", defaults.docTypeMap)
	}
}

func TestIntsFromAnyAcceptsNativeIntSlice(t *testing.T) {
	t.Parallel()

	got := intsFromAny([]int{1, 2, 3})
	if !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("intsFromAny([]int{1,2,3}) = %v, want [1 2 3]", got)
	}
}

func TestIntsFromAnySkipsNonNumericElements(t *testing.T) {
	t.Parallel()

	got := intsFromAny([]any{1, "two", 3.0, nil})
	if !slices.Equal(got, []int{1, 3}) {
		t.Errorf("intsFromAny(...) = %v, want [1 3]", got)
	}
}

func TestIntPtrFromAnyNilForAbsentKey(t *testing.T) {
	t.Parallel()

	if got := intPtrFromAny(nil); got != nil {
		t.Errorf("intPtrFromAny(nil) = %v, want nil", got)
	}
}
