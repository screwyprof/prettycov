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
	// Installed with `go install prettycov@v1.2.3` the ldflags are not passed, so fall back to
	// what the build recorded.
	var recorded string
	if info, ok := debug.ReadBuildInfo(); ok {
		recorded = info.Main.Version
	}

	return pickVersion(version, recorded)
}

// pickVersion is split out because the two sources it chooses between are both fixed for the life
// of a process: under `go test` ReadBuildInfo always reports "(devel)", so the empty case — which
// is what a nix build with no VCS metadata produces — is unreachable through buildVersion.
func pickVersion(stamped, recorded string) string {
	switch {
	case stamped != "":
		return stamped
	case recorded != "":
		return recorded
	default:
		// Never a bare newline: a script reading the version would take that as a version.
		return "(devel)"
	}
}
