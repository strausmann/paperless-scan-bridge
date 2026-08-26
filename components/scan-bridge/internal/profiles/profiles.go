// Package profiles loads and validates scan-profile definitions.
//
// The on-disk schema is documented in CONTAINER_SUITE.md sec. 4.6.
// This package owns the in-process Go types and the validation that
// runs at daemon startup. Invalid profiles fail load with an error
// identifying the offending profile by name.
//
// TODO(phase 1.4): a JSON Schema mirror at api/schema/profile.json
// will become the canonical machine-readable spec once the OpenAPI
// surface lands; until then this package's struct tags are the
// reference.
//
// TODO(phase 1.4): surface YAML line numbers on validation errors
// per CONTAINER_SUITE.md sec. 4.6. Implementing that requires
// decoding into yaml.Node and threading position information through
// validateProfile, which is a larger refactor than this Phase 1.1
// session targets.
package profiles

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations"
)

// supportedSources is the allow-list of SANE source strings a profile
// may request. It mirrors sane-runtime's own allow-list
// (components/sane-runtime/internal/scanapi/handlers.go): validating
// here means a bad source fails fast at daemon startup instead of
// surfacing as a 400 from sane-runtime on the first scan.
//
// The values are the SANE option strings, matched exactly (SANE is
// case-sensitive). The reference Kodak ScanMate i1120 advertises
// "ADF Front|ADF Duplex" via `scanimage -A`, verified on the hardware
// on 2026-08-26; "Flatbed" is kept for flatbed-capable scanners.
var supportedSources = map[string]bool{
	"ADF Front":  true,
	"ADF Duplex": true,
	"Flatbed":    true,
}

// supportedSourceList renders the allow-list deterministically for
// error messages.
func supportedSourceList() string {
	names := make([]string, 0, len(supportedSources))
	for n := range supportedSources {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// ColorMode is the SANE-mode-equivalent enum.
type ColorMode string

const (
	ColorModeColor   ColorMode = "Color"
	ColorModeGray    ColorMode = "Gray"
	ColorModeLineart ColorMode = "Lineart"
)

// Format is the on-disk container the post-processor will produce.
type Format string

const (
	FormatPDF  Format = "pdf"
	FormatJPEG Format = "jpeg"
	FormatTIFF Format = "tiff"
)

// PageSize controls the SANE page-size option and the post-processor
// page-fit policy.
type PageSize string

const (
	PageSizeA4     PageSize = "A4"
	PageSizeLetter PageSize = "Letter"
	PageSizeA5     PageSize = "A5"
	PageSizeAuto   PageSize = "auto"
)

const (
	minResolutionDPI = 100
	maxResolutionDPI = 1200
)

// PageGrouping controls how scan-processor assembles a job's captured
// pages into the document(s) handed to destinations (ADR 0017 / design
// doc sec. 6). Combined merges all pages of a job into one document;
// PerPage emits one document per source page.
type PageGrouping string

const (
	PageGroupingCombined PageGrouping = "combined"
	PageGroupingPerPage  PageGrouping = "per_page"
)

// defaultOCRLanguages is applied by Parse when a profile enables OCR
// but omits languages (design doc sec. 6: "default when enabled and
// omitted"). Matches the HomeLab OCR grounding cited by the design
// (deu+eng).
var defaultOCRLanguages = []string{"deu", "eng"}

// autoOCRLanguage is the special OCRConfig.Languages value that
// requests scan-processor's two-pass auto-language-detection flow
// instead of a fixed language set (PR brief "pragmatische
// Auto-Language-Detection"). Declared independently from
// scan-processor's own copies of the same token
// (internal/pipeline/exec_argv.go, internal/procapi/handlers.go) —
// this package validates the profile-authoring shape at load time,
// scan-processor validates the wire request independently; see either
// package's doc comment for why the duplication is deliberate.
const autoOCRLanguage = "auto"

// defaultMinOCRConfidence is applied by Parse when a profile enables
// OCR but omits ocr.min_confidence, mirroring defaultOCRLanguages'
// same "zero/omitted means apply the documented default" contract.
// Matches scan-processor's own default
// (internal/pipeline.defaultMinOCRConfidence) — kept in sync by
// convention, not by import, for the same dependency-direction reason
// Languages' default is duplicated rather than shared.
const defaultMinOCRConfidence = 80.0

// Profile is a single named scan profile.
type Profile struct {
	Name           string    `yaml:"name"`
	Description    string    `yaml:"description"`
	Source         string    `yaml:"source"`
	Resolution     int       `yaml:"resolution"`
	Mode           ColorMode `yaml:"mode"`
	Format         Format    `yaml:"format"`
	TargetSubdir   string    `yaml:"target_subdir"`
	Deskew         bool      `yaml:"deskew"`
	RemoveBlank    bool      `yaml:"remove_blank"`
	RotatePages    bool      `yaml:"rotate_pages"`
	PageSize       PageSize  `yaml:"page_size"`
	TimeoutSeconds int       `yaml:"timeout_seconds"`

	// MetadataTemplate carries the original Paperless-only hint shape.
	// Superseded by Destinations for any profile that adopts the
	// destinations schema below (ADR 0016/0017): a Paperless
	// destination's Config carries tag_ids/correspondent_id/
	// document_type_id/document_type_map instead of
	// PaperlessTags/PaperlessCorrespondent. MetadataTemplate is not
	// removed -- no profile in production depends on its removal --
	// but new profiles should be authored against Destinations
	// directly (design doc sec. 6, migration note).
	MetadataTemplate MetadataTemplate `yaml:"metadata_template"`

	// OCR is the per-profile OCR toggle (Epic A2). Off by default,
	// matching ARCHITECTURE.md's existing documented default. When
	// Enabled is true and Languages is omitted, Parse fills in
	// defaultOCRLanguages.
	OCR OCRConfig `yaml:"ocr"`

	// Assembly controls scan-processor's multi-page result shape (ADR
	// 0017 / Epic A6). When PageGrouping is omitted, Parse defaults it
	// to PageGroupingCombined.
	Assembly AssemblyConfig `yaml:"assembly"`

	// DocumentType is a free-form, profile-defined key (ADR 0017) --
	// no central enum. Empty means no type-specific mapping is applied
	// at any destination.
	DocumentType string `yaml:"document_type"`

	// Destinations lists the destination-routing targets (ADR 0016)
	// this profile delivers to. Empty is valid: a profile that has not
	// adopted the new schema yet (e.g. still relying on TargetSubdir /
	// MetadataTemplate) has no Destinations entries.
	Destinations []ProfileDestination `yaml:"destinations"`
}

// MetadataTemplate carries the Paperless-side hints that scan-bridge
// passes through to scan-processor.
type MetadataTemplate struct {
	PaperlessTags          []string `yaml:"paperless_tags"`
	PaperlessCorrespondent *string  `yaml:"paperless_correspondent"`
}

// OCRConfig is a profile's per-profile OCR toggle (design doc sec. 6).
type OCRConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Languages []string `yaml:"languages"`
	// MinConfidence overrides scan-processor's confidence-gate
	// threshold (defaultMinOCRConfidence when omitted/zero) — PR
	// brief "Konfidenz-/Qualitäts-Gate". Carried through to
	// scan-processor verbatim (internal/api/scan.go's handleScan),
	// which never interprets it itself — the gate lives entirely in
	// scan-processor's internal/pipeline.
	MinConfidence float64 `yaml:"min_confidence"`
}

// AssemblyConfig controls the multi-page result shape scan-processor
// applies to a job's captured pages (design doc sec. 6, ADR 0017).
type AssemblyConfig struct {
	PageGrouping PageGrouping `yaml:"page_grouping"`
}

// ProfileDestination is one entry of a profile's destinations list
// (ADR 0016, design doc sec. 6). Target selects which registered
// destinations.Destination module handles delivery by name;
// StorageFirst chooses NFS/SMB-style intermediate storage vs. a direct
// API call (no storage-first module is built yet -- ADR 0016's and the
// design doc's scope note); Config carries the destination-specific
// block whose shape only that module's Constructor understands (e.g.
// a Paperless destination's base_url/token_secret/tag_ids/
// document_type_map) -- this package does not interpret it. See
// Profile.DestinationConfigs for the conversion to
// destinations.ProfileDestinationConfig the dispatch core (a later
// task) consumes.
type ProfileDestination struct {
	Target       string         `yaml:"target"`
	StorageFirst bool           `yaml:"storage_first"`
	Config       map[string]any `yaml:"config"`
}

// DestinationConfigs converts p.Destinations into the
// destinations.ProfileDestinationConfig shape the destination registry
// (ADR 0016, internal/destinations) consumes. This is the only place
// the profiles package depends on the destinations package; wiring
// that actually calls destinations.Build with these values is a later
// task (design doc sec. 9, Task 7), not built here. Returns an empty,
// non-nil slice when p.Destinations is empty.
func (p Profile) DestinationConfigs() []destinations.ProfileDestinationConfig {
	out := make([]destinations.ProfileDestinationConfig, len(p.Destinations))
	for i, d := range p.Destinations {
		out[i] = destinations.ProfileDestinationConfig{
			Target:       d.Target,
			StorageFirst: d.StorageFirst,
			Config:       d.Config,
		}
	}
	return out
}

// Set is a validated, named-keyed collection of profiles. Use Load or
// Parse to construct one; the zero value is intentionally not useful.
type Set struct {
	byName map[string]Profile
}

// Names returns the profile names in deterministic order.
func (s *Set) Names() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.byName))
	for n := range s.byName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Get returns the profile with the given name and a boolean
// indicating whether it was found.
func (s *Set) Get(name string) (Profile, bool) {
	if s == nil {
		return Profile{}, false
	}
	p, ok := s.byName[name]
	return p, ok
}

// Len reports how many profiles are in the set.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.byName)
}

// All returns a snapshot of the profiles in deterministic order.
func (s *Set) All() []Profile {
	if s == nil {
		return nil
	}
	names := s.Names()
	out := make([]Profile, 0, len(names))
	for _, n := range names {
		out = append(out, s.byName[n])
	}
	return out
}

type fileShape struct {
	Profiles []Profile `yaml:"profiles"`
}

// Load reads, parses, and validates the profile YAML at path.
func Load(path string) (*Set, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open profiles %q: %w", path, err)
	}
	defer f.Close()

	return Parse(f)
}

// Parse decodes the profile YAML from r and validates it.
func Parse(r io.Reader) (*Set, error) {
	var raw fileShape
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode profiles yaml: %w", err)
	}

	if len(raw.Profiles) == 0 {
		return nil, errors.New("no profiles defined; the daemon refuses to start without at least one")
	}

	set := &Set{byName: make(map[string]Profile, len(raw.Profiles))}
	for i := range raw.Profiles {
		p := raw.Profiles[i]
		applyProfileDefaults(&p)
		if err := validateProfile(p); err != nil {
			return nil, fmt.Errorf("profile %q: %w", p.Name, err)
		}
		if _, dup := set.byName[p.Name]; dup {
			return nil, fmt.Errorf("profile %q: duplicate name", p.Name)
		}
		set.byName[p.Name] = p
	}
	return set, nil
}

// applyProfileDefaults fills in the pipeline-extension fields'
// documented defaults (design doc sec. 6) before validateProfile runs.
// It only ever fills a zero value -- an explicit value from the YAML
// is never overwritten.
func applyProfileDefaults(p *Profile) {
	if p.Assembly.PageGrouping == "" {
		p.Assembly.PageGrouping = PageGroupingCombined
	}
	if p.OCR.Enabled && len(p.OCR.Languages) == 0 {
		p.OCR.Languages = append([]string(nil), defaultOCRLanguages...)
	}
	if p.OCR.Enabled && p.OCR.MinConfidence == 0 {
		p.OCR.MinConfidence = defaultMinOCRConfidence
	}
}

func validateProfile(p Profile) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name must be non-empty")
	}
	if strings.TrimSpace(p.Source) == "" {
		return errors.New("source must be non-empty (SANE source string)")
	}
	if !supportedSources[p.Source] {
		return fmt.Errorf("source %q: must be one of %s", p.Source, supportedSourceList())
	}

	if p.Resolution < minResolutionDPI || p.Resolution > maxResolutionDPI {
		return fmt.Errorf("resolution %d: must be between %d and %d DPI",
			p.Resolution, minResolutionDPI, maxResolutionDPI)
	}

	switch p.Mode {
	case ColorModeColor, ColorModeGray, ColorModeLineart:
	case "":
		return errors.New("mode is required")
	default:
		return fmt.Errorf("mode %q: must be Color, Gray, or Lineart", p.Mode)
	}

	switch p.Format {
	case FormatPDF, FormatJPEG, FormatTIFF:
	case "":
		return errors.New("format is required")
	default:
		return fmt.Errorf("format %q: must be pdf, jpeg, or tiff", p.Format)
	}

	switch p.PageSize {
	case PageSizeA4, PageSizeLetter, PageSizeA5, PageSizeAuto:
	case "":
		return errors.New("page_size is required")
	default:
		return fmt.Errorf("page_size %q: must be A4, Letter, A5, or auto", p.PageSize)
	}

	if p.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout_seconds %d: must be positive", p.TimeoutSeconds)
	}

	// "" is accepted here even though PageGroupingCombined/PerPage are
	// the only meaningful values: Parse always fills "" in via
	// applyProfileDefaults before this validation runs, but
	// validateProfile is also called directly (e.g. by tests) against
	// profiles that never went through that defaulting step.
	switch p.Assembly.PageGrouping {
	case "", PageGroupingCombined, PageGroupingPerPage:
	default:
		return fmt.Errorf("assembly.page_grouping %q: must be combined or per_page", p.Assembly.PageGrouping)
	}

	if p.OCR.Enabled {
		hasAuto := false
		for _, lang := range p.OCR.Languages {
			if strings.TrimSpace(lang) == "" {
				return errors.New("ocr.languages: must not contain empty entries")
			}
			if lang == autoOCRLanguage {
				hasAuto = true
			}
		}
		if hasAuto && len(p.OCR.Languages) != 1 {
			return fmt.Errorf("ocr.languages: %q must be the only entry when used", autoOCRLanguage)
		}
		if p.OCR.MinConfidence < 0 || p.OCR.MinConfidence > 100 {
			return fmt.Errorf("ocr.min_confidence %v: must be between 0 and 100", p.OCR.MinConfidence)
		}
	}

	for i, d := range p.Destinations {
		if strings.TrimSpace(d.Target) == "" {
			return fmt.Errorf("destinations[%d].target: must be non-empty", i)
		}
	}

	return nil
}
