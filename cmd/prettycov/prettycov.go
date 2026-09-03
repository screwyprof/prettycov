package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// Exit codes. Below is distinct from failed so a CI step can tell "coverage dropped" from
// "prettycov could not run".
const (
	exitOK     = 0
	exitBelow  = 1
	exitFailed = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is everything main does apart from exiting, so it can be tested. Nothing it calls exits.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "help":
			printUsage(stderr)

			return exitOK
		case "version":
			printVersion(stdout)

			return exitOK
		}
	}

	cfg, err := parseFlags(args, stderr)
	if err != nil {
		// ContinueOnError already reported a bad flag, and -h prints usage itself.
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintf(stderr, "An error occurred: %v\n", err)
		}

		return exitFailed
	}

	switch {
	case cfg.Help:
		printUsage(stderr)

		return exitOK
	case cfg.Version:
		printVersion(stdout)

		return exitOK
	}

	return showReport(cfg, stdout, stderr)
}
