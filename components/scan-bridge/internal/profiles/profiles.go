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
)

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

// Profile is a single named scan profile.
type Profile struct {
	Name             string           `yaml:"name"`
	Description      string           `yaml:"description"`
	Source           string           `yaml:"source"`
	Resolution       int              `yaml:"resolution"`
	Mode             ColorMode        `yaml:"mode"`
	Format           Format           `yaml:"format"`
	TargetSubdir     string           `yaml:"target_subdir"`
	Deskew           bool             `yaml:"deskew"`
	RemoveBlank      bool             `yaml:"remove_blank"`
	RotatePages      bool             `yaml:"rotate_pages"`
	PageSize         PageSize         `yaml:"page_size"`
	TimeoutSeconds   int              `yaml:"timeout_seconds"`
	MetadataTemplate MetadataTemplate `yaml:"metadata_template"`
}

// MetadataTemplate carries the Paperless-side hints that scan-bridge
// passes through to scan-processor.
type MetadataTemplate struct {
	PaperlessTags          []string `yaml:"paperless_tags"`
	PaperlessCorrespondent *string  `yaml:"paperless_correspondent"`
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

func validateProfile(p Profile) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name must be non-empty")
	}
	if strings.TrimSpace(p.Source) == "" {
		return errors.New("source must be non-empty (SANE source string)")
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

	return nil
}
