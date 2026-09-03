package app

import (
	"fmt"
	"io"
	"runtime/debug"
)

var version string // set by the linker

func printVersion(w io.Writer) int {
	_, _ = fmt.Fprintln(w, buildVersion())

	return exitOK
}

// buildVersion reads the version rather than caching it back into the linker variable: writing to
// that variable made two concurrent callers a data race.
func buildVersion() string {
	if version != "" {
		return version
	}

	// `go install prettycov@v1.2.3` passes no ldflags, so fall back to what the build recorded.
	// Whatever that says is already the answer: the toolchain writes "(devel)" itself for a main
	// module with no version, so there is nothing to check it against.
	//
	// The guard is for the pointer, not the value — it is nil when ok is false, and a binary
	// carrying no build info at all is the only way there. No ordinary build produces one, which
	// is why this is the single statement in the package no test reaches.
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}

	return info.Main.Version
}
