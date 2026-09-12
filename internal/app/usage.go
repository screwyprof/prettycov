package app

import (
	"fmt"
	"io"
)

const usageMessage = "" +
	`Prettycov:
Given a coverage profile produced by 'go test'.
	go test -coverprofile=coverage.out ./...
Show the top level packages, reading ./coverage.out:
	prettycov
Read a profile elsewhere:
	prettycov path/to/coverage.out
Show another level down, or the whole tree:
	prettycov -depth=2
	prettycov -depth=max
Replace a long root package path:
	prettycov -old=gitlab.com/Company/Department/product/unicorn -new=unicorn
Fail when total coverage is below a threshold, for CI:
	prettycov -fail-under=80
Stop counting code you never meant to test, one pattern per flag:
	prettycov -exclude='/cmd/' -exclude='\.pb\.go$'
Or one block, by the position the profile gives it:
	prettycov -exclude='version\.go:32'
Show the statement counts behind each percentage:
	prettycov -counts
Show the files too, not only the packages:
	prettycov -files -depth=max
Leave out what is finished, or anything already above a bar (the bar needs '='):
	prettycov -hide-covered -depth=max
	prettycov -hide-covered=90 -depth=max
Print just the number, for a Makefile or a badge:
	prettycov -total
`

// printUsage writes the help text and the flag defaults.
func printUsage(w io.Writer) int {
	var cfg config

	_, _ = fmt.Fprint(w, usageMessage)
	_, _ = fmt.Fprintln(w, "\nFlags:")

	set := newFlagSet(&cfg)

	// newFlagSet silences the flag package for parsing; PrintDefaults writes to the same place,
	// so it has to be pointed back at w or the flag list comes out empty.
	set.SetOutput(w)
	set.PrintDefaults()

	return exitOK
}
