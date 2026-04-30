package profiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
    source: "ADF"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
  - name: dup
    source: "ADF"
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
    source: "ADF"
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
		name string
		mut  func(p *Profile)
		errSubstring string
	}{
		{
			name: "missing name",
			mut: func(p *Profile) { p.Name = "" },
			errSubstring: "name",
		},
		{
			name: "missing source",
			mut: func(p *Profile) { p.Source = "" },
			errSubstring: "source",
		},
		{
			name: "resolution too low",
			mut: func(p *Profile) { p.Resolution = 50 },
			errSubstring: "resolution",
		},
		{
			name: "resolution too high",
			mut: func(p *Profile) { p.Resolution = 5000 },
			errSubstring: "resolution",
		},
		{
			name: "unknown mode",
			mut: func(p *Profile) { p.Mode = "Sepia" },
			errSubstring: "mode",
		},
		{
			name: "unknown format",
			mut: func(p *Profile) { p.Format = "bmp" },
			errSubstring: "format",
		},
		{
			name: "unknown page_size",
			mut: func(p *Profile) { p.PageSize = "tabloid" },
			errSubstring: "page_size",
		},
		{
			name: "non-positive timeout",
			mut: func(p *Profile) { p.TimeoutSeconds = 0 },
			errSubstring: "timeout_seconds",
		},
	}

	base := Profile{
		Name:           "ok",
		Source:         "ADF",
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
    source: "ADF"
    resolution: 300
    mode: "Color"
    format: "pdf"
    page_size: "A4"
    timeout_seconds: 60
  - name: alpha
    source: "ADF"
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
