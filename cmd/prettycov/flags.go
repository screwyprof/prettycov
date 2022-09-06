package main

import (
	"flag"
	"fmt"
	"os"
)

const usageMessage = "" +
	`Prettycov:
Given a coverage profile produced by 'go test'.
	go test -coveragepkg=coverage.out ./...
Show coverage summary of the top level packages:
	prettycov -profile=coverage.out
Show coverage summary for second level packages:
	prettycov -profile=coverage.out -depth=2
Replace a long root package path and show the report:
	prettycov -profile=coverage.out -old=gitlab.com/Company/Department/product/unicorn -new=unicorn
`

func showUsage() {
	_, _ = fmt.Fprint(os.Stderr, usageMessage)
	_, _ = fmt.Fprintln(os.Stderr, "\nFlags:")

	flag.PrintDefaults()
	os.Exit(2)
}

type flags struct {
	Profile     string
	CurrentRoot string
	NewRoot     string
	Depth       uint
	Help        bool
	Info        bool
}

func parseFlags() flags {
	var (
		profile = flag.String("profile", "", "coverage profile path")
		curRoot = flag.String("old", "", "old project's root package")
		newRoot = flag.String("new", "", "new project's root package")
		depth   = flag.Uint("depth", 1, "nesting to show from top to bottom starting from 0")
		help    = flag.Bool("help", false, "show help")
		info    = flag.Bool("version", false, "show version")
	)

	flag.Usage = showUsage
	flag.Parse()

	return flags{
		Profile:     *profile,
		CurrentRoot: *curRoot,
		NewRoot:     *newRoot,
		Depth:       *depth,
		Help:        *help,
		Info:        *info,
	}
}
