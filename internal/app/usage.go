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
Show another level down:
	prettycov -depth=2
Replace a long root package path:
	prettycov -old=gitlab.com/Company/Department/product/unicorn -new=unicorn
Fail when total coverage is below a threshold, for CI:
	prettycov -fail-under=80
Stop counting code you never meant to test, one pattern per flag:
	prettycov -exclude='/cmd/' -exclude='\.pb\.go$'

Run every module's tests, one go test each:
	prettycov measure -- -race
Join their coverage into one profile:
	prettycov measure -profile=coverage.out -- -race
Attribution is go's own: -coverpkg=./... within a module, =work across a workspace:
	prettycov measure -profile=coverage.out -- -coverpkg=work -covermode=atomic
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
