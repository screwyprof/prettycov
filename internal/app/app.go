// Package app is the composition root: it builds the parser, wires the command line, and turns what
// a handler returned into a process status. It knows nothing about what any command does.
package app

import (
	"fmt"
	"io"

	"github.com/alecthomas/kong"

	"github.com/screwyprof/prettycov/internal/cli"
)

// Run is the whole command apart from exiting. Nothing it calls exits: kong.Exit is handed a
// recorder, so --help and a bad flag return here.
func Run(args []string, stdout, stderr io.Writer) int {
	var root cli.CLI

	// Recorded rather than obeyed: --help and --version print and then ask kong to exit.
	done := -1

	// Must, not New: only a malformed CLI struct fails here, and every test run builds one.
	parser := kong.Must(&root,
		kong.Name(cli.Name),
		kong.Description(cli.Description),
		kong.Writers(stdout, stderr),
		kong.Vars{"profile": cli.DefaultProfile, "depth": cli.DefaultDepth, "version": buildVersion()},
		kong.Exit(func(code int) { done = code }),
		kong.NamedMapper("hidecovered", cli.OptionalPercentage{}),
	)

	// No command at all is someone finding out what this does, not a mistake: kong would answer
	// "expected one of ...", where the help says that and more.
	if len(args) == 0 {
		args = []string{"--help"}
	}

	ctx, err := parser.Parse(args)

	if done >= 0 {
		return done
	}

	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\nrun \"prettycov --help\" for usage\n", err)

		return int(cli.ExitFailed)
	}

	return status(ctx.Run(&cli.Streams{Out: stdout, Err: stderr}), stderr)
}

// status turns what a handler returned into an exit code, printing anything that is a real error.
// A handler that already reported itself comes back carrying a code and nothing is printed twice.
func status(err error, stderr io.Writer) int {
	if code, reported := cli.ExitCodeOf(err); reported {
		return int(code)
	}

	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)

		return int(cli.ExitFailed)
	}

	return int(cli.ExitOK)
}
