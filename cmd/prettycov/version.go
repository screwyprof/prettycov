package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

var version string // set by the linker

func printVersion(w io.Writer) {
	// Installed with `go install prettycov@v1.2.3` the ldflags are not passed and version is
	// empty, so fall back to the build info.
	if version == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			version = info.Main.Version
		}
	}

	_, _ = fmt.Fprintln(w, version)
}
