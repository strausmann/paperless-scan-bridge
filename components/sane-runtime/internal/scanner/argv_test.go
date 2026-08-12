package scanner

import (
	"reflect"
	"testing"
)

func TestBuildArgv(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		params         Params
		outputTemplate string
		want           []string
	}{
		{
			name: "full params with device and max pages",
			params: Params{
				Device:     "avision:libusb:001:002",
				Source:     "ADF Duplex",
				Resolution: 300,
				Mode:       "Color",
				Format:     "tiff",
				MaxPages:   0,
			},
			outputTemplate: "/scratch/page-%d.tiff",
			want: []string{
				"-d", "avision:libusb:001:002",
				"--format=tiff", "--mode=Color", "--resolution=300",
				"--source=ADF Duplex", "--batch=/scratch/page-%d.tiff",
			},
		},
		{
			name: "no device auto-selects (no -d flag)",
			params: Params{
				Source:     "ADF Front",
				Resolution: 200,
				Mode:       "Gray",
				Format:     "pnm",
			},
			outputTemplate: "/scratch/page-%d.pnm",
			want: []string{
				"--format=pnm", "--mode=Gray", "--resolution=200",
				"--source=ADF Front", "--batch=/scratch/page-%d.pnm",
			},
		},
		{
			name: "max pages appends --batch-count",
			params: Params{
				Source:     "ADF Duplex",
				Resolution: 300,
				Mode:       "Color",
				Format:     "tiff",
				MaxPages:   5,
			},
			outputTemplate: "/scratch/page-%d.tiff",
			want: []string{
				"--format=tiff", "--mode=Color", "--resolution=300",
				"--source=ADF Duplex", "--batch=/scratch/page-%d.tiff",
				"--batch-count=5",
			},
		},
		{
			name: "max pages zero omits --batch-count",
			params: Params{
				Source:     "Flatbed",
				Resolution: 75,
				Mode:       "Lineart",
				Format:     "tiff",
				MaxPages:   0,
			},
			outputTemplate: "/scratch/page-%d.tiff",
			want: []string{
				"--format=tiff", "--mode=Lineart", "--resolution=75",
				"--source=Flatbed", "--batch=/scratch/page-%d.tiff",
			},
		},
		{
			name: "device and max pages both present",
			params: Params{
				Device:     "avision:libusb:001:002",
				Source:     "ADF Duplex",
				Resolution: 600,
				Mode:       "Color",
				Format:     "tiff",
				MaxPages:   2,
			},
			outputTemplate: "/scratch/page-%d.tiff",
			want: []string{
				"-d", "avision:libusb:001:002",
				"--format=tiff", "--mode=Color", "--resolution=600",
				"--source=ADF Duplex", "--batch=/scratch/page-%d.tiff",
				"--batch-count=2",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildArgv(tc.params, tc.outputTemplate)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildArgv(%+v, %q) = %v, want %v",
					tc.params, tc.outputTemplate, got, tc.want)
			}
		})
	}
}
