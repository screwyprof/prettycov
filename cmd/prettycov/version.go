package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

var version string // set by the linker

func printVersion(w io.Writer) {
	_, _ = fmt.Fprintln(w, buildVersion())
}

// buildVersion reads the version rather than caching it back into the linker variable. Writing to
// that variable made two concurrent callers a data race, which main never hit because it only ever
// asked once — the tests did.
func buildVersion() string {
	if version != "" {
		return version
	}

	// Installed with `go install prettycov@v1.2.3` the ldflags are not passed, so fall back to
	// what the build recorded.
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}

	// Never a bare newline: a script reading the version would take that as a version.
	return "(devel)"
}
