package scanner

import "strconv"

// buildArgv translates Params into the scanimage(1) argument list.
// outputTemplate is the --batch pattern (e.g. "<scratch>/page-%d.tiff")
// that scanimage expands per page; the scratch directory and its
// cleanup are ExecScanner's responsibility, not this pure function's.
//
// -d is omitted when Device is empty so scanimage auto-selects the
// (single) attached device; ExecScanner handles the zero/one/many
// device-count cases before calling this.
func buildArgv(p Params, outputTemplate string) []string {
	args := []string{}
	if p.Device != "" {
		args = append(args, "-d", p.Device)
	}
	args = append(args,
		"--format="+p.Format,
		"--mode="+p.Mode,
		"--resolution="+strconv.Itoa(p.Resolution),
		"--source="+p.Source,
		"--batch="+outputTemplate,
	)
	if p.MaxPages > 0 {
		args = append(args, "--batch-count="+strconv.Itoa(p.MaxPages))
	}
	return args
}
