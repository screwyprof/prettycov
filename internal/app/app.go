package app

import (
	"fmt"
	"io"
)

// Exit codes. Below is distinct from failed so a CI step can tell "coverage dropped" from
// "prettycov could not run".
const (
	exitOK     = 0
	exitBelow  = 1
	exitFailed = 2
)

// Run is the whole command apart from exiting: args excludes the program name, and the int is the
// status to exit with. Nothing it calls exits, so a caller keeps control.
func Run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\nrun \"prettycov -help\" for usage\n", err)

		return exitFailed
	}

	// Every handler returns its own status, so this only picks one.
	switch {
	case cfg.Help:
		return printUsage(stdout)
	case cfg.Version:
		return printVersion(stdout)
	default:
		return showReport(cfg, stdout, stderr)
	}
}
