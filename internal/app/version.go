package app

import (
	"fmt"
	"io"
	"runtime/debug"
)

var version string // set by the linker

func printVersion(w io.Writer) {
	_, _ = fmt.Fprintln(w, buildVersion())
}

// buildVersion reads the version rather than caching it back into the linker variable: writing to
// that variable made two concurrent callers a data race.
func buildVersion() string {
	if version != "" {
		return version
	}

	// `go install prettycov@v1.2.3` passes no ldflags, so fall back to what the build recorded.
	// The toolchain writes "(devel)" for a main module with no version, so this is empty only if
	// the binary carries no build info at all — which no ordinary build produces, and why the
	// last line is the one statement in this package no test reaches.
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}

	return "(devel)"
}
