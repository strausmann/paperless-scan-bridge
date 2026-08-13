package pipeline

import (
	"reflect"
	"testing"
)

func TestBuildDeskewArgs(t *testing.T) {
	t.Parallel()

	got := buildDeskewArgs("in.tiff", "out.tiff")
	want := []string{"in.tiff", "-deskew", "40%", "+repage", "out.tiff"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildDeskewArgs = %v, want %v", got, want)
	}
}

func TestBuildMeanBrightnessArgs(t *testing.T) {
	t.Parallel()

	got := buildMeanBrightnessArgs("page.tiff")
	want := []string{"-format", "%[fx:mean]", "page.tiff"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildMeanBrightnessArgs = %v, want %v", got, want)
	}
}

func TestParseMeanBrightness(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		stdout  string
		want    float64
		wantErr bool
	}{
		{"plain decimal", "0.987654", 0.987654, false},
		{"leading/trailing whitespace", "  0.5  \n", 0.5, false},
		{"integer one", "1", 1, false},
		{"integer zero", "0", 0, false},
		{"garbage", "not-a-number", 0, true},
		{"empty", "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseMeanBrightness(tc.stdout)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseMeanBrightness(%q): expected error, got nil", tc.stdout)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMeanBrightness(%q): unexpected error: %v", tc.stdout, err)
			}
			if got != tc.want {
				t.Errorf("parseMeanBrightness(%q) = %v, want %v", tc.stdout, got, tc.want)
			}
		})
	}
}

func TestBuildOSDArgs(t *testing.T) {
	t.Parallel()

	got := buildOSDArgs("page.tiff")
	want := []string{"page.tiff", "stdout", "--psm", "0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildOSDArgs = %v, want %v", got, want)
	}
}

func TestParseOSDRotation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		stdout  string
		want    int
		wantErr bool
	}{
		{
			name: "typical tesseract osd output",
			stdout: "Page number: 0\n" +
				"Orientation in degrees: 90\n" +
				"Rotate: 270\n" +
				"Orientation confidence: 5.20\n" +
				"Script: Latin\n" +
				"Script confidence: 2.16\n",
			want: 270,
		},
		{
			name:   "zero rotation",
			stdout: "Page number: 0\nOrientation in degrees: 0\nRotate: 0\n",
			want:   0,
		},
		{
			name:   "no rotate line: not a failure, defaults to 0",
			stdout: "Detects no OSD\n",
			want:   0,
		},
		{
			name:   "empty stdout",
			stdout: "",
			want:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseOSDRotation(tc.stdout)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseOSDRotation(%q): expected error, got nil", tc.stdout)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOSDRotation(%q): unexpected error: %v", tc.stdout, err)
			}
			if got != tc.want {
				t.Errorf("parseOSDRotation(%q) = %d, want %d", tc.stdout, got, tc.want)
			}
		})
	}
}

func TestBuildRotateArgs(t *testing.T) {
	t.Parallel()

	got := buildRotateArgs("in.tiff", "out.tiff", 270)
	want := []string{"in.tiff", "-rotate", "270", "out.tiff"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildRotateArgs = %v, want %v", got, want)
	}
}

func TestBuildOCRPDFArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		languages []string
		want      []string
	}{
		{
			name:      "deu+eng default",
			languages: []string{"deu", "eng"},
			want:      []string{"in.tiff", "out", "-l", "deu+eng", "pdf"},
		},
		{
			name:      "single language",
			languages: []string{"eng"},
			want:      []string{"in.tiff", "out", "-l", "eng", "pdf"},
		},
		{
			name:      "no languages: tesseract falls back to its own default",
			languages: nil,
			want:      []string{"in.tiff", "out", "pdf"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := buildOCRPDFArgs("in.tiff", "out", tc.languages)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildOCRPDFArgs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildConvertFormatArgs(t *testing.T) {
	t.Parallel()

	got := buildConvertFormatArgs("in.tiff", "out.jpg")
	want := []string{"in.tiff", "out.jpg"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildConvertFormatArgs = %v, want %v", got, want)
	}
}

func TestBuildQpdfMergeArgs(t *testing.T) {
	t.Parallel()

	got := buildQpdfMergeArgs([]string{"p1.pdf", "p2.pdf", "p3.pdf"}, "combined.pdf")
	want := []string{"--empty", "--pages", "p1.pdf", "p2.pdf", "p3.pdf", "--", "combined.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildQpdfMergeArgs = %v, want %v", got, want)
	}
}

func TestBuildTIFFMergeArgs(t *testing.T) {
	t.Parallel()

	got := buildTIFFMergeArgs([]string{"p1.tiff", "p2.tiff"}, "combined.tiff")
	want := []string{"p1.tiff", "p2.tiff", "combined.tiff"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildTIFFMergeArgs = %v, want %v", got, want)
	}
}

func TestContentTypeForFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		format OutputFormat
		want   string
	}{
		{OutputFormatPDF, "application/pdf"},
		{OutputFormatJPEG, "image/jpeg"},
		{OutputFormatTIFF, "image/tiff"},
	}
	for _, tc := range cases {
		if got := contentTypeForFormat(tc.format); got != tc.want {
			t.Errorf("contentTypeForFormat(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
}

func TestFileExtensionForFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		format OutputFormat
		want   string
	}{
		{OutputFormatPDF, "pdf"},
		{OutputFormatJPEG, "jpg"},
		{OutputFormatTIFF, "tiff"},
	}
	for _, tc := range cases {
		if got := fileExtensionForFormat(tc.format); got != tc.want {
			t.Errorf("fileExtensionForFormat(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
}
