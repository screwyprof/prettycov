// Prettycov draws a Go coverage profile as a tree, with a total on every row.
//
//	go test -covermode=atomic -coverprofile=coverage.out ./...
//	prettycov report
//
// Exits 0, 1 when --fail-under was not met, and 2 when it could not do what was asked, so a CI step
// can tell a coverage drop from a broken invocation.
//
// Nothing but the exit lives here: internal/app builds the parser, internal/cli holds the commands,
// and both are testable without a process.
package main

import (
	"os"

	"github.com/screwyprof/prettycov/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdout, os.Stderr))
}
