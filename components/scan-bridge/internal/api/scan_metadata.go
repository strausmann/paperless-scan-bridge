package api

import (
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/tag"
)

// destinationMetadataDefaults holds the profile-config-level
// tag/correspondent/document-type defaults one destination's config
// block carries (design doc
// docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
// sec. 5.3/6: destinations[].config.tag_ids/tag_strategy/
// correspondent_id/document_type_id/document_type_map).
//
// This is deliberately decoded here, in the dispatch core, rather than
// in the paperless module: the design's Paperless Config type (sec.
// 5.3) narrows to only what Deliver itself needs to make its HTTP
// call (base_url/token_secret) -- "profile-level tag/correspondent/
// document-type defaults and doc-type-map merging are resolved
// upstream of Deliver, into the Metadata this module receives, not
// decoded here" (design doc sec. 5.3, and confirmed by
// destinations/paperless/paperless.go's actual Config struct, which
// has no tag/correspondent/document-type fields). resolveMetadata is
// where that upstream resolution happens -- Task 3/4's deferred work,
// picked up here in Task 7.
type destinationMetadataDefaults struct {
	tagIDs          []int
	tagStrategy     tag.Strategy
	correspondentID *int
	documentTypeID  *int
	docTypeMap      map[string]docTypeMapping
}

// docTypeMapping is one entry of a destination config's
// document_type_map (ADR 0017): a profile's free-form document_type
// key mapped to that destination's own document_type_id/tag_ids
// override.
type docTypeMapping struct {
	documentTypeID *int
	tagIDs         []int
}

// resolveMetadata builds the destinations.Metadata for one document
// delivered to one destination, merging that destination's own
// profile-config tag/correspondent/document-type defaults (parsed from
// cfg.Config, an untyped map[string]any -- ProfileDestinationConfig's
// shape, ADR 0016) with the caller-supplied tag IDs/strategy from the
// POST /scan body, via the existing tag.Merge algebra (internal/tag,
// unchanged).
//
// Doc-type-map resolution (ADR 0017, design doc sec. 5.3): when
// profile.DocumentType is non-empty and cfg.Config's
// document_type_map has a matching entry, that entry's document_type_id
// (if set) overrides the destination config's own document_type_id,
// and its tag_ids are appended to the destination's own tag_ids before
// the caller-tag merge runs. A miss (no document_type_map entry, or no
// profile.DocumentType at all) falls back to the destination config's
// own document_type_id/correspondent_id/tag_ids unchanged -- matching
// the design doc's documented "a miss falls back to
// cfg.CorrespondentID/cfg.DocumentTypeID/cfg.DefaultTagIDs only (no
// error)" contract.
//
// FLAGGED GAP (see PR description): the design doc does not fully
// specify whether a document_type_map HIT's tag_ids replace or add to
// the destination's base tag_ids -- its own TypeMapping struct sketch
// (sec. 5.3) only has DocumentTypeID and TagIDs fields, with no
// explicit merge rule stated. This implementation treats a hit's
// tag_ids as ADDITIVE (base cfg.tag_ids + mapping.tag_ids, then both
// merged against the caller's tags via tag.Merge) -- the interpretation
// that matches the design's own YAML example (sec. 6: a profile-level
// "post" tag plus a per-type tag), but it is an implementation choice,
// not a verified contract. Needs explicit operator confirmation before
// being relied on for anything where "replace" vs. "add" changes
// behaviour.
func resolveMetadata(profile profiles.Profile, cfg destinations.ProfileDestinationConfig, callerTagIDs []int, callerStrategy tag.Strategy, scanID string, now time.Time) destinations.Metadata {
	defaults := parseDestinationMetadataDefaults(cfg.Config)

	tagIDs := defaults.tagIDs
	documentTypeID := defaults.documentTypeID

	if profile.DocumentType != "" {
		if mapping, ok := defaults.docTypeMap[profile.DocumentType]; ok {
			if mapping.documentTypeID != nil {
				documentTypeID = mapping.documentTypeID
			}
			tagIDs = append(append([]int(nil), defaults.tagIDs...), mapping.tagIDs...)
		}
	}

	return destinations.Metadata{
		Title:         renderTitle(profile.TitleTemplate, profile, scanID, now),
		TagIDs:        tag.Merge(tagIDs, defaults.tagStrategy, callerTagIDs, callerStrategy),
		Correspondent: defaults.correspondentID,
		DocumentType:  documentTypeID,
	}
}

// parseDestinationMetadataDefaults decodes the generic subset of a
// destination's profile-config block (map[string]any --
// ProfileDestinationConfig.Config) that resolveMetadata needs:
// tag_ids, tag_strategy, correspondent_id, document_type_id, and
// document_type_map. Unknown/malformed values are silently skipped
// (left at the zero value) rather than erroring -- cfg.Config's shape
// beyond base_url/token_secret is not validated at profile-load time
// today (only the paperless module's own decodeConfig validates its
// own subset), so a destination config missing these optional fields
// is a normal, unremarkable case, not a failure.
func parseDestinationMetadataDefaults(raw map[string]any) destinationMetadataDefaults {
	out := destinationMetadataDefaults{}

	if raw == nil {
		return out
	}

	out.tagIDs = intsFromAny(raw["tag_ids"])
	if v, ok := raw["tag_strategy"].(string); ok {
		out.tagStrategy = tag.Strategy(v)
	}
	out.correspondentID = intPtrFromAny(raw["correspondent_id"])
	out.documentTypeID = intPtrFromAny(raw["document_type_id"])

	rawMap, ok := raw["document_type_map"].(map[string]any)
	if !ok {
		return out
	}
	out.docTypeMap = make(map[string]docTypeMapping, len(rawMap))
	for key, v := range rawMap {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		out.docTypeMap[key] = docTypeMapping{
			documentTypeID: intPtrFromAny(entry["document_type_id"]),
			tagIDs:         intsFromAny(entry["tag_ids"]),
		}
	}
	return out
}

// intFromAny extracts an int from a decoded YAML/JSON value. YAML
// (gopkg.in/yaml.v3, what profiles.Profile is actually parsed from)
// decodes scalar integers into Go's native int; the float64/int64
// cases are handled defensively in case cfg.Config is ever populated
// from a JSON-sourced map instead.
func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// intPtrFromAny returns a pointer to the int value of v, or nil if v
// is not a recognized numeric type (including v == nil, the "key
// absent from the map" case, since a missing map key reads back as a
// nil any).
func intPtrFromAny(v any) *int {
	n, ok := intFromAny(v)
	if !ok {
		return nil
	}
	return &n
}

// intsFromAny extracts a []int from a decoded YAML/JSON sequence
// value. yaml.v3 decodes a YAML sequence of scalars into []any (each
// element itself an any, per intFromAny's cases above); a Go-native
// []int is also accepted directly for tests/callers that build
// ProfileDestinationConfig.Config by hand rather than via YAML
// decoding. Elements that are not a recognized numeric type are
// skipped, not errored -- mirrors parseDestinationMetadataDefaults'
// "malformed optional field is not a failure" stance.
func intsFromAny(v any) []int {
	switch vv := v.(type) {
	case []int:
		return append([]int(nil), vv...)
	case []any:
		out := make([]int, 0, len(vv))
		for _, e := range vv {
			if n, ok := intFromAny(e); ok {
				out = append(out, n)
			}
		}
		return out
	default:
		return nil
	}
}
