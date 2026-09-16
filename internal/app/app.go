package app

import (
	"errors"
	"fmt"
	"io"

	"github.com/alecthomas/kong"
)

// Exit codes. Below is distinct from failed so a CI step can tell "coverage dropped" from
// "prettycov could not run".
const (
	exitOK     = 0
	exitBelow  = 1
	exitFailed = 2
)

// exitError carries a status out through the error a Run method returns. A failed --fail-under gate
// is not a usage mistake: reported as one it would be printed and counted as exit 2.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func status(code int) error {
	if code == exitOK {
		return nil
	}

	return exitError{code: code}
}

// Run is the whole command apart from exiting: args excludes the program name, and the int is the
// status to exit with. Nothing it calls exits — kong.Exit is handed a no-op, so --help and a bad
// flag return here rather than calling os.Exit themselves.
func Run(args []string, stdout, stderr io.Writer) int {
	var cli CLI

	// --help and --version print and then ask kong to exit. Nothing here exits, so the request is
	// recorded and answered below; without it kong printed the help and then ran the command.
	done := -1

	parser, err := kong.New(&cli,
		kong.Name("prettycov"),
		kong.Description("Given a coverage profile produced by 'go test', draw the packages and what they cover.\n\n"+
			"\tgo test -covermode=atomic -coverprofile=coverage.out ./...\n\tprettycov"),
		kong.Writers(stdout, stderr),
		kong.Vars{"profile": defaultProfile, "depth": defaultDepth, "version": buildVersion()},
		kong.Exit(func(code int) { done = code }),
		kong.NamedMapper("hidecovered", optionalPercentage{}),
	)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)

		return exitFailed
	}

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

		return exitFailed
	}

	return report(ctx.Run(&streams{out: stdout, err: stderr}, &cli.measured, ctx), stderr)
}

// report turns what a command returned into a status, printing anything that is a real error.
func report(err error, stderr io.Writer) int {
	var exit exitError

	switch {
	case errors.As(err, &exit):
		return exit.code
	case err != nil:
		_, _ = fmt.Fprintf(stderr, "%v\n", err)

		return exitFailed
	}

	return exitOK
}
