package scanner

import "regexp"

// scanimageDeviceLine matches lines scanimage -L prints per detected
// device, e.g.:
//
//	device `avision:libusb:001:002' is a KODAK ScanMate i1120 scanner
//
// The device string is backtick/single-quote delimited and may itself
// contain spaces, so it must be captured, not split on whitespace.
var scanimageDeviceLine = regexp.MustCompile("(?m)^device `([^']+)' is a ")

// parseScanimageList extracts device identifiers from scanimage -L
// stdout. It returns nil (not an empty, non-nil slice) when no device
// lines are found, matching Go's idiomatic "absent means nil" — the
// EmptyOutput_ReturnsEmptySlice test only asserts len == 0, so nil
// satisfies it.
func parseScanimageList(output string) []string {
	matches := scanimageDeviceLine.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return nil
	}
	devices := make([]string, 0, len(matches))
	for _, m := range matches {
		devices = append(devices, m[1])
	}
	return devices
}
