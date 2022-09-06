package main

import (
	"fmt"
	"os"
	"runtime/debug"
)

var version string // set by the linker

func showVersion() {
	// When app is being installed using `go install prettycov@v1.2.3`, the ldflags won't be passed
	// and the version will be empty. In this case, we try to populate version using build info.
	if version == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			version = info.Main.Version
		}
	}

	fmt.Println(version)
	os.Exit(0)
}
