// Package app is the composition root: it builds the parser, hands the command line its
// dependencies, and turns what a handler returned into a status for the process.
//
// It knows nothing about what any command does. The router and the handlers are internal/cli; this
// decides only what they are wired to and what their errors mean to an exit code.
package app

import (
	"fmt"
	"io"

	"github.com/alecthomas/kong"

	"github.com/screwyprof/prettycov/internal/cli"
)

// Run is the whole command apart from exiting: args excludes the program name, and the int is the
// status to exit with. Nothing it calls exits — kong.Exit is handed a recorder, so --help and a bad
// flag return here rather than calling os.Exit themselves.
func Run(args []string, stdout, stderr io.Writer) int {
	var root cli.CLI

	// Recorded rather than obeyed: --help and --version print and then ask kong to exit.
	done := -1

	// Must, not New: the only thing that fails here is a malformed CLI struct, which is a constant
	// of this package. That is a broken build rather than a runtime condition — every test run
	// builds this, so a mistake in it cannot reach a user — and an error branch for it would be one
	// no test could take.
	parser := kong.Must(&root,
		kong.Name("prettycov"),
		kong.Description("Given a coverage profile produced by 'go test', draw the packages and what they cover.\n\n"+
			"\tgo test -covermode=atomic -coverprofile=coverage.out ./...\n\tprettycov report"),
		kong.Writers(stdout, stderr),
		kong.Vars{"profile": cli.DefaultProfile, "depth": cli.DefaultDepth, "version": buildVersion()},
		kong.Exit(func(code int) { done = code }),
		kong.NamedMapper("hidecovered", cli.OptionalPercentage{}),
	)

	// No command at all is not a mistake, it is someone finding out what this does. Kong would
	// answer "expected one of ..."; the help says that and more, through kong's own printer.
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

	return status(
		ctx.Run(&cli.Streams{Out: stdout, Err: stderr}, &root.Measured, ctx, cli.Version(buildVersion())),
		stderr,
	)
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
