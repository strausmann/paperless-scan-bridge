package scanner

import (
	"reflect"
	"testing"
)

func TestParsesScanimageDashLOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name: "single device, quoted string with embedded spaces",
			output: "\n" +
				"device `avision:libusb:001:002' is a KODAK ScanMate i1120 scanner\n",
			want: []string{"avision:libusb:001:002"},
		},
		{
			name: "multiple devices",
			output: "" +
				"device `avision:libusb:001:002' is a KODAK ScanMate i1120 scanner\n" +
				"device `genesys:libusb:001:003' is a Canon LiDE 220 flatbed scanner\n",
			want: []string{
				"avision:libusb:001:002",
				"genesys:libusb:001:003",
			},
		},
		{
			name:   "no devices attached message",
			output: "\nNo scanners were identified.\n",
			want:   nil,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseScanimageList(tc.output)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseScanimageList(%q) = %v, want %v",
					tc.output, got, tc.want)
			}
		})
	}
}

func TestEmptyOutput_ReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	got := parseScanimageList("")
	if len(got) != 0 {
		t.Errorf("parseScanimageList(\"\") = %v, want empty", got)
	}
}
