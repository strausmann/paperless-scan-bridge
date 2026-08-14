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

func TestBuildOCRArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		languages []string
		wantPDF   bool
		want      []string
	}{
		{
			name:      "deu+eng default, pdf+tsv",
			languages: []string{"deu", "eng"},
			wantPDF:   true,
			want:      []string{"in.tiff", "out", "-l", "deu+eng", "pdf", "tsv"},
		},
		{
			name:      "single language, pdf+tsv",
			languages: []string{"eng"},
			wantPDF:   true,
			want:      []string{"in.tiff", "out", "-l", "eng", "pdf", "tsv"},
		},
		{
			name:      "no languages: tesseract falls back to its own default",
			languages: nil,
			wantPDF:   true,
			want:      []string{"in.tiff", "out", "pdf", "tsv"},
		},
		{
			name:      "tsv only, no pdf (jpeg/tiff OCR-check callers)",
			languages: []string{"deu", "eng"},
			wantPDF:   false,
			want:      []string{"in.tiff", "out", "-l", "deu+eng", "tsv"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := buildOCRArgs("in.tiff", "out", tc.languages, tc.wantPDF)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildOCRArgs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseOCRTSV(t *testing.T) {
	t.Parallel()

	header := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext"

	cases := []struct {
		name          string
		tsv           string
		wantMean      float64
		wantWordCount int
		wantWords     []string
	}{
		{
			name: "two words, mean of their confidences",
			tsv: header + "\n" +
				"5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t90\thello\n" +
				"5\t1\t1\t1\t1\t2\t10\t0\t10\t10\t70\tworld\n",
			wantMean:      80,
			wantWordCount: 2,
			wantWords:     []string{"hello", "world"},
		},
		{
			name: "non-word levels (page/block/par/line) excluded via conf=-1",
			tsv: header + "\n" +
				"1\t1\t0\t0\t0\t0\t0\t0\t100\t100\t-1\t\n" +
				"5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t100\tonly\n",
			wantMean:      100,
			wantWordCount: 1,
			wantWords:     []string{"only"},
		},
		{
			name:          "header only: no words",
			tsv:           header + "\n",
			wantMean:      0,
			wantWordCount: 0,
			wantWords:     nil,
		},
		{
			name:          "empty input",
			tsv:           "",
			wantMean:      0,
			wantWordCount: 0,
			wantWords:     nil,
		},
		{
			name: "malformed conf column skipped, not fatal",
			tsv: header + "\n" +
				"5\t1\t1\t1\t1\t1\t0\t0\t10\t10\tnot-a-number\tbad\n" +
				"5\t1\t1\t1\t1\t2\t0\t0\t10\t10\t50\tgood\n",
			wantMean:      50,
			wantWordCount: 1,
			wantWords:     []string{"good"},
		},
		{
			name: "short row (below tsvMinColumns) skipped",
			tsv: header + "\n" +
				"5\t1\t1\n" +
				"5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t60\tword\n",
			wantMean:      60,
			wantWordCount: 1,
			wantWords:     []string{"word"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotMean, gotWordCount, gotWords := parseOCRTSV(tc.tsv)
			if gotMean != tc.wantMean {
				t.Errorf("mean = %v, want %v", gotMean, tc.wantMean)
			}
			if gotWordCount != tc.wantWordCount {
				t.Errorf("wordCount = %v, want %v", gotWordCount, tc.wantWordCount)
			}
			if !reflect.DeepEqual(gotWords, tc.wantWords) {
				t.Errorf("words = %v, want %v", gotWords, tc.wantWords)
			}
		})
	}
}

func TestIsLowConfidence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mean, threshold float64
		want            bool
	}{
		{90, 80, false},
		{80, 80, false}, // exactly at threshold is not "below"
		{79.9, 80, true},
		{0, 80, true},
		{100, 0, false},
	}
	for _, tc := range cases {
		if got := isLowConfidence(tc.mean, tc.threshold); got != tc.want {
			t.Errorf("isLowConfidence(%v, %v) = %v, want %v", tc.mean, tc.threshold, got, tc.want)
		}
	}
}

func TestMeanFloat64(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		vals []float64
		want float64
	}{
		{"empty", nil, 0},
		{"single", []float64{42}, 42},
		{"multiple", []float64{90, 70, 80}, 80},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := meanFloat64(tc.vals); got != tc.want {
				t.Errorf("meanFloat64(%v) = %v, want %v", tc.vals, got, tc.want)
			}
		})
	}
}

func TestIsAutoLanguageRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		languages []string
		want      bool
	}{
		{"exactly auto", []string{"auto"}, true},
		{"auto mixed with another entry", []string{"auto", "deu"}, false},
		{"regular languages", []string{"deu", "eng"}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isAutoLanguageRequest(tc.languages); got != tc.want {
				t.Errorf("isAutoLanguageRequest(%v) = %v, want %v", tc.languages, got, tc.want)
			}
		})
	}
}

func TestContainsLanguage(t *testing.T) {
	t.Parallel()

	if !containsLanguage([]string{"deu", "eng"}, "eng") {
		t.Error("containsLanguage([deu eng], eng) = false, want true")
	}
	if containsLanguage([]string{"deu", "eng"}, "fra") {
		t.Error("containsLanguage([deu eng], fra) = true, want false")
	}
	if containsLanguage(nil, "eng") {
		t.Error("containsLanguage(nil, eng) = true, want false")
	}
}

func TestDetectLanguage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "clearly english",
			text: "the quick brown fox and this is a test with the dog",
			want: "eng",
		},
		{
			name: "clearly french",
			text: "bonjour le monde et la vie pour vous dans ce",
			want: "fra",
		},
		{
			name: "clearly german",
			text: "der und ist nicht mit den von ein eine für im auf sich",
			want: "deu",
		},
		{
			name: "no recognizable stopwords: no guess",
			text: "xkq zzqx qxkz plonk fizzbuzz",
			want: "",
		},
		{
			name: "empty text: no guess",
			text: "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := detectLanguage(tc.text, autoDetectCandidateLanguages); got != tc.want {
				t.Errorf("detectLanguage(%q) = %q, want %q", tc.text, got, tc.want)
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
