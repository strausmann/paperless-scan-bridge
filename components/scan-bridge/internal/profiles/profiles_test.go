package profiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations"
)

const validProfile = `
profiles:
  - name: private-duplex
    description: "Private documents, duplex, color, 300 DPI"
    source: "ADF Duplex"
    resolution: 300
    mode: "Color"
    format: "pdf"
    target_subdir: "private/"
    deskew: true
    remove_blank: true
    rotate_pages: true
    page_size: "A4"
    timeout_seconds: 300
    metadata_template:
      paperless_tags: ["private"]
      paperless_correspondent: null
`

// pipelineProfile exercises the ocr/assembly/document_type/destinations
// extension (docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
// sec. 6, ADR 0016, ADR 0017) using the design doc's own example shape.
const pipelineProfile = `
profiles:
  - name: private-duplex
    source: "ADF Duplex"
    resolution: 300
    mode: "Color"
    format: "pdf"
    target_subdir: "private/"
    page_size: "A4"
    timeout_seconds: 120
    ocr:
      enabled: true
      languages: [deu, eng]
    assembly:
      page_grouping: combined
    document_type: eingangsrechnung
    destinations:
      - target: paperless
        storage_first: false
        config:
          base_url: "https://paperless.example.com"
          token_secret: paperless_api_token
          tag_ids: [3]
          tag_strategy: add
          correspondent_id: 12
          document_type_id: 3
          document_type_map:
            eingangsrechnung:
              document_type_id: 3
              tag_ids: [7]
            post:
              tag_ids: [4]
`

func TestParsePipelineProfile(t *testing.T) {
	t.Parallel()

	set, err := Parse(strings.NewReader(pipelineProfile))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, ok := set.Get("private-duplex")
	if !ok {
		t.Fatal("profile not found by name")
	}

	if !p.OCR.Enabled {
		t.Error("OCR.Enabled = false, want true")
	}
	if got, want := p.OCR.Languages, []string{"deu", "eng"}; !equalStrings(got, want) {
		t.Errorf("OCR.Languages = %v, want %v", got, want)
	}
	if p.Assembly.PageGrouping != PageGroupingCombined {
		t.Errorf("Assembly.PageGrouping = %q, want %q", p.Assembly.PageGrouping, PageGroupingCombined)
	}
	if p.DocumentType != "eingangsrechnung" {
		t.Errorf("DocumentType = %q, want %q", p.DocumentType, "eingangsrechnung")
	}
	if len(p.Destinations) != 1 {
		t.Fatalf("len(Destinations) = %d, want 1", len(p.Destinations))
	}
	dest := p.Destinations[0]
	if dest.Target != "paperless" {
		t.Errorf("Destinations[0].Target = %q, want %q", dest.Target, "paperless")
	}
	if dest.StorageFirst {
		t.Error("Destinations[0].StorageFirst = true, want false")
	}
	if dest.Config["base_url"] != "https://paperless.example.com" {
		t.Errorf("Destinations[0].Config[base_url] = %v, want %q", dest.Config["base_url"], "https://paperless.example.com")
	}
	// tag_ids decodes as []any under a map[string]any leaf (yaml.v3
	// has no static type to decode into here) -- assert the shape a
	// caller actually gets, not a wished-for []int.
	tagIDs, ok := dest.Config["tag_ids"].([]any)
	if !ok || len(tagIDs) != 1 {
		t.Fatalf("Destinations[0].Config[tag_ids] = %#v, want a one-element slice", dest.Config["tag_ids"])
	}
	docTypeMap, ok := dest.Config["document_type_map"].(map[string]any)
	if !ok {
		t.Fatalf("Destinations[0].Config[document_type_map] = %#v, want a map", dest.Config["document_type_map"])
	}
	if _, ok := docTypeMap["eingangsrechnung"]; !ok {
		t.Errorf("document_type_map missing key %q", "eingangsrechnung")
	}
}

func TestParseAppliesPageGroupingDefault(t *testing.T) {
	t.Parallel()

	// validProfile sets neither ocr nor assembly at all.
	set, err := Parse(strings.NewReader(validProfile))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, ok := set.Get("private-duplex")
	if !ok {
		t.Fatal("profile not found by name")
	}
	if p.Assembly.PageGrouping != PageGroupingCombined {
		t.Errorf("Assembly.PageGrouping = %q, want default %q", p.Assembly.PageGrouping, PageGroupingCombined)
	}
	if p.OCR.Enabled {
		t.Error("OCR.Enabled = true, want false (no ocr block given)")
	}
	if len(p.OCR.Languages) != 0 {
		t.Errorf("OCR.Languages = %v, want empty (OCR disabled, no default applied)", p.OCR.Languages)
	}
	if len(p.Destinations) != 0 {
		t.Errorf("Destinations = %v, want empty (no destinations block given)", p.Destinations)
	}
}

func TestParseOCREnabledDefaultsLanguages(t *testing.T) {
	t.Parallel()

	body := `
profiles:
  - name: ocr-no-langs
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
    ocr:
      enabled: true
`
	set, err := Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, ok := set.Get("ocr-no-langs")
	if !ok {
		t.Fatal("profile not found by name")
	}
	if want := []string{"deu", "eng"}; !equalStrings(p.OCR.Languages, want) {
		t.Errorf("OCR.Languages = %v, want default %v", p.OCR.Languages, want)
	}
}

func TestParseOCREnabledDefaultsMinConfidence(t *testing.T) {
	t.Parallel()

	body := `
profiles:
  - name: ocr-no-min-confidence
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
    ocr:
      enabled: true
`
	set, err := Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, ok := set.Get("ocr-no-min-confidence")
	if !ok {
		t.Fatal("profile not found by name")
	}
	if p.OCR.MinConfidence != defaultMinOCRConfidence {
		t.Errorf("OCR.MinConfidence = %v, want default %v", p.OCR.MinConfidence, defaultMinOCRConfidence)
	}
}

func TestParseOCRMinConfidenceExplicitValueNotOverwritten(t *testing.T) {
	t.Parallel()

	body := `
profiles:
  - name: ocr-explicit-min-confidence
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
    ocr:
      enabled: true
      min_confidence: 55
`
	set, err := Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, ok := set.Get("ocr-explicit-min-confidence")
	if !ok {
		t.Fatal("profile not found by name")
	}
	if p.OCR.MinConfidence != 55 {
		t.Errorf("OCR.MinConfidence = %v, want 55 (explicit value preserved)", p.OCR.MinConfidence)
	}
}

func TestParseOCRAutoLanguageAccepted(t *testing.T) {
	t.Parallel()

	body := `
profiles:
  - name: ocr-auto
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
    ocr:
      enabled: true
      languages: ["auto"]
`
	set, err := Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, ok := set.Get("ocr-auto")
	if !ok {
		t.Fatal("profile not found by name")
	}
	if want := []string{"auto"}; !equalStrings(p.OCR.Languages, want) {
		t.Errorf("OCR.Languages = %v, want %v (default must not overwrite an explicit [auto])", p.OCR.Languages, want)
	}
}

func TestParseRejectsUnknownOCRField(t *testing.T) {
	t.Parallel()

	body := `
profiles:
  - name: bad
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
    ocr:
      enabled: true
      not_a_real_field: true
`
	_, err := Parse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected rejection of unknown ocr.* field")
	}
}

func TestParseRejectsUnknownAssemblyField(t *testing.T) {
	t.Parallel()

	body := `
profiles:
  - name: bad
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
    assembly:
      page_grouping: combined
      not_a_real_field: true
`
	_, err := Parse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected rejection of unknown assembly.* field")
	}
}

func TestParseRejectsUnknownDestinationField(t *testing.T) {
	t.Parallel()

	body := `
profiles:
  - name: bad
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
    destinations:
      - target: paperless
        not_a_real_field: true
`
	_, err := Parse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected rejection of unknown destinations[].* field")
	}
}

func TestDestinationConfigsMapsProfileDestinations(t *testing.T) {
	t.Parallel()

	p := Profile{
		Destinations: []ProfileDestination{
			{
				Target:       "paperless",
				StorageFirst: false,
				Config:       map[string]any{"base_url": "https://paperless.example.com"},
			},
			{
				Target:       "nfs",
				StorageFirst: true,
				Config:       nil,
			},
		},
	}

	got := p.DestinationConfigs()
	want := []destinations.ProfileDestinationConfig{
		{Target: "paperless", StorageFirst: false, Config: map[string]any{"base_url": "https://paperless.example.com"}},
		{Target: "nfs", StorageFirst: true, Config: nil},
	}

	if len(got) != len(want) {
		t.Fatalf("len(DestinationConfigs()) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Target != want[i].Target {
			t.Errorf("[%d].Target = %q, want %q", i, got[i].Target, want[i].Target)
		}
		if got[i].StorageFirst != want[i].StorageFirst {
			t.Errorf("[%d].StorageFirst = %v, want %v", i, got[i].StorageFirst, want[i].StorageFirst)
		}
		if got[i].Config["base_url"] != want[i].Config["base_url"] {
			t.Errorf("[%d].Config[base_url] = %v, want %v", i, got[i].Config["base_url"], want[i].Config["base_url"])
		}
	}
}

func TestDestinationConfigsEmptyForProfileWithoutDestinations(t *testing.T) {
	t.Parallel()

	p := Profile{}
	got := p.DestinationConfigs()
	if len(got) != 0 {
		t.Errorf("DestinationConfigs() = %v, want empty", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseValidProfile(t *testing.T) {
	t.Parallel()

	set, err := Parse(strings.NewReader(validProfile))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if set.Len() != 1 {
		t.Fatalf("Len = %d, want 1", set.Len())
	}
	p, ok := set.Get("private-duplex")
	if !ok {
		t.Fatal("profile not found by name")
	}
	if p.Resolution != 300 {
		t.Errorf("Resolution = %d, want 300", p.Resolution)
	}
	if p.MetadataTemplate.PaperlessCorrespondent != nil {
		t.Errorf("PaperlessCorrespondent = %v, want nil", *p.MetadataTemplate.PaperlessCorrespondent)
	}
	if got := set.Names(); len(got) != 1 || got[0] != "private-duplex" {
		t.Errorf("Names = %v, want [private-duplex]", got)
	}
}

func TestLoadDefaultsYAML(t *testing.T) {
	t.Parallel()

	set, err := Load("defaults.yaml")
	if err != nil {
		t.Fatalf("Load defaults.yaml: %v", err)
	}
	if set.Len() < 1 {
		t.Fatal("defaults.yaml must contain at least one profile")
	}
	for _, name := range set.Names() {
		if _, ok := set.Get(name); !ok {
			t.Errorf("Get(%q) = false after Names listed it", name)
		}
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	t.Parallel()

	_, err := Parse(strings.NewReader("profiles: []\n"))
	if err == nil || !strings.Contains(err.Error(), "no profiles") {
		t.Fatalf("expected empty-list rejection, got %v", err)
	}
}

func TestParseRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	body := `
profiles:
  - name: dup
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
  - name: dup
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
`
	_, err := Parse(strings.NewReader(body))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-name rejection, got %v", err)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	body := `
profiles:
  - name: bad
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
    not_a_real_field: true
`
	_, err := Parse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected rejection of unknown YAML field")
	}
}

func TestValidateBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		mut          func(p *Profile)
		errSubstring string
	}{
		{
			name:         "missing name",
			mut:          func(p *Profile) { p.Name = "" },
			errSubstring: "name",
		},
		{
			name:         "missing source",
			mut:          func(p *Profile) { p.Source = "" },
			errSubstring: "source",
		},
		{
			name:         "resolution too low",
			mut:          func(p *Profile) { p.Resolution = 50 },
			errSubstring: "resolution",
		},
		{
			name:         "resolution too high",
			mut:          func(p *Profile) { p.Resolution = 5000 },
			errSubstring: "resolution",
		},
		{
			name:         "unknown mode",
			mut:          func(p *Profile) { p.Mode = "Sepia" },
			errSubstring: "mode",
		},
		{
			name:         "unknown format",
			mut:          func(p *Profile) { p.Format = "bmp" },
			errSubstring: "format",
		},
		{
			name:         "unknown page_size",
			mut:          func(p *Profile) { p.PageSize = "tabloid" },
			errSubstring: "page_size",
		},
		{
			name:         "non-positive timeout",
			mut:          func(p *Profile) { p.TimeoutSeconds = 0 },
			errSubstring: "timeout_seconds",
		},
		{
			name:         "unknown page_grouping",
			mut:          func(p *Profile) { p.Assembly.PageGrouping = "shuffled" },
			errSubstring: "page_grouping",
		},
		{
			name:         "ocr enabled with empty language entry",
			mut:          func(p *Profile) { p.OCR = OCRConfig{Enabled: true, Languages: []string{"deu", ""}} },
			errSubstring: "ocr",
		},
		{
			name:         "ocr auto mixed with a real language",
			mut:          func(p *Profile) { p.OCR = OCRConfig{Enabled: true, Languages: []string{"auto", "deu"}} },
			errSubstring: "ocr.languages",
		},
		{
			name:         "ocr min_confidence negative",
			mut:          func(p *Profile) { p.OCR = OCRConfig{Enabled: true, Languages: []string{"deu"}, MinConfidence: -1} },
			errSubstring: "min_confidence",
		},
		{
			name:         "ocr min_confidence above 100",
			mut:          func(p *Profile) { p.OCR = OCRConfig{Enabled: true, Languages: []string{"deu"}, MinConfidence: 101} },
			errSubstring: "min_confidence",
		},
		{
			name: "destination with empty target",
			mut: func(p *Profile) {
				p.Destinations = []ProfileDestination{{Target: "  "}}
			},
			errSubstring: "destinations",
		},
	}

	base := Profile{
		Name:           "ok",
		Source:         "ADF Front",
		Resolution:     300,
		Mode:           ColorModeColor,
		Format:         FormatPDF,
		PageSize:       PageSizeA4,
		TimeoutSeconds: 60,
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := base
			tc.mut(&p)
			err := validateProfile(p)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errSubstring)
			}
			if !strings.Contains(err.Error(), tc.errSubstring) {
				t.Errorf("error %q did not contain %q", err.Error(), tc.errSubstring)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error loading missing file")
	}
}

func TestLoadFromDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	if err := os.WriteFile(path, []byte(validProfile), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	set, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if set.Len() != 1 {
		t.Errorf("Len = %d, want 1", set.Len())
	}
}

func TestAllReturnsDeterministicOrder(t *testing.T) {
	t.Parallel()

	body := `
profiles:
  - name: zeta
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
  - name: alpha
    source: "ADF Front"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
`
	set, err := Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	all := set.All()
	if len(all) != 2 || all[0].Name != "alpha" || all[1].Name != "zeta" {
		t.Errorf("All() not sorted: %+v", all)
	}
}

// TestValidateProfileRejectsUnknownSource locks in a bug found against
// the real Kodak ScanMate i1120 on 2026-08-26: validateProfile only
// checked that source was non-empty, so a profile could ship a source
// string the scanner does not offer. The i1120 advertises exactly
// "ADF Front|ADF Duplex" (scanimage -A), and sane-runtime's allowlist
// rejects anything else with 400 invalid_request — so a bad source
// passed profile validation at startup and only blew up later, at scan
// time, on the caller.
func TestValidateProfileRejectsUnknownSource(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"ADF", "adf front", "Duplex", "Feeder"} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()

			p := validProfileFixture()
			p.Source = src

			if err := validateProfile(p); err == nil {
				t.Fatalf("validateProfile(source=%q) = nil, want rejection", src)
			}
		})
	}
}

// TestValidateProfileAcceptsKnownSources guards the other direction:
// the sources the hardware actually offers must keep validating.
func TestValidateProfileAcceptsKnownSources(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"ADF Front", "ADF Duplex", "Flatbed"} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()

			p := validProfileFixture()
			p.Source = src

			if err := validateProfile(p); err != nil {
				t.Fatalf("validateProfile(source=%q) = %v, want nil", src, err)
			}
		})
	}
}

// TestShippedDefaultsUseSupportedSources asserts the profiles we ship
// are actually scannable: every source in defaults.yaml must be one the
// scanner offers. This is the regression guard for the two profiles
// (private-simplex, receipts) that shipped source "ADF".
func TestShippedDefaultsUseSupportedSources(t *testing.T) {
	t.Parallel()

	set, err := Load("defaults.yaml")
	if err != nil {
		t.Fatalf("Load defaults.yaml: %v", err)
	}
	for _, name := range set.Names() {
		p, ok := set.Get(name)
		if !ok {
			t.Fatalf("profile %q vanished from set", name)
		}
		if !supportedSources[p.Source] {
			t.Errorf("profile %q ships source %q, which the scanner does not offer", name, p.Source)
		}
	}
}

// validProfileFixture returns a minimal profile that passes
// validateProfile, so a test can mutate exactly one field and attribute
// the resulting error to that field alone.
func validProfileFixture() Profile {
	return Profile{
		Name:           "ok",
		Source:         "ADF Front",
		Resolution:     300,
		Mode:           ColorModeColor,
		Format:         FormatPDF,
		PageSize:       PageSizeA4,
		TimeoutSeconds: 60,
	}
}

// TestValidateProfileAcceptsEveryFormat guards the format allowlist in
// both directions. png was added last (roadmap Epic A3) and is the one
// most likely to be dropped by a future refactor of the switch, because
// unlike pdf/jpeg/tiff it has no separate assembly branch downstream --
// it falls through the same single-image path JPEG uses.
func TestValidateProfileAcceptsEveryFormat(t *testing.T) {
	t.Parallel()

	for _, f := range []Format{FormatPDF, FormatJPEG, FormatTIFF, FormatPNG} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()

			p := validProfileFixture()
			p.Format = f

			if err := validateProfile(p); err != nil {
				t.Fatalf("validateProfile(format=%q) = %v, want nil", f, err)
			}
		})
	}
}

func TestValidateProfileRejectsUnknownFormat(t *testing.T) {
	t.Parallel()

	// "PNG" and "jpg" are the plausible typos: the allowlist is
	// case-sensitive and spells JPEG out.
	for _, f := range []Format{"PNG", "jpg", "webp", "bmp"} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()

			p := validProfileFixture()
			p.Format = f

			if err := validateProfile(p); err == nil {
				t.Fatalf("validateProfile(format=%q) = nil, want rejection", f)
			}
		})
	}
}

// TestValidateProfileMaxPages covers the feeder cap (roadmap Epic A5).
// 0 is not "unset and therefore invalid" -- it is the documented "drain
// the ADF" default every profile had before the field existed, so a
// test asserting it validates is what keeps a future "required field"
// tightening from silently breaking every shipped profile.
func TestValidateProfileMaxPages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		maxPages  int
		wantValid bool
	}{
		{"zero drains the feeder", 0, true},
		{"one is the single-sheet case", 1, true},
		{"a plausible cap", 25, true},
		{"negative is rejected", -1, false},
		{"a typo'd negative is rejected", -50, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := validProfileFixture()
			p.MaxPages = tc.maxPages

			err := validateProfile(p)
			if tc.wantValid && err != nil {
				t.Fatalf("validateProfile(max_pages=%d) = %v, want nil", tc.maxPages, err)
			}
			if !tc.wantValid && err == nil {
				t.Fatalf("validateProfile(max_pages=%d) = nil, want rejection", tc.maxPages)
			}
		})
	}
}
