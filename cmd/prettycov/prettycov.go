package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"

	"github.com/screwyprof/prettycov"
)

var version string // set by the linker

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

type params struct {
	Profile     string
	CurrentRoot string
	NewRoot     string
	Depth       uint
	Help        bool
	Info        bool
}

func parseParams() params {
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

	return params{
		Profile:     *profile,
		CurrentRoot: *curRoot,
		NewRoot:     *newRoot,
		Depth:       *depth,
		Help:        *help,
		Info:        *info,
	}
}

func showUsage() {
	_, _ = fmt.Fprint(os.Stderr, usageMessage)
	_, _ = fmt.Fprintln(os.Stderr, "\nFlags:")

	flag.PrintDefaults()
	os.Exit(2)
}

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

func showReport(params params) {
	dir := path.Dir(params.Profile)
	file := path.Base(params.Profile)

	curDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	failOnError(err)

	failOnError(os.Chdir(dir))

	res, err := prettycov.CoverHTML(file)
	failOnError(err)

	coverage, err := prettycov.ParseHTML(res)
	failOnError(err)

	tree := prettycov.Process(coverage.Items, params.CurrentRoot, params.NewRoot)
	prettycov.DisplayTree(os.Stdout, tree, params.Depth)

	failOnError(os.Chdir(curDir))
}

func main() {
	handleCommands(parseParams())
}

func handleCommands(params params) {
	// show usage info when no arguments or flags given.
	if flag.NFlag() == 0 && flag.NArg() == 0 {
		flag.Usage()
	}

	// show usage info when calling `prettycov -help or prettycov --help`
	if params.Help {
		flag.Usage()
	}

	// show version when calling `prettycov -version or prettycov --version`
	if params.Info {
		showVersion()
	}

	// handle commands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help":
			showUsage()
		case "version":
			showVersion()
		default:
			showReport(params)
		}
	}
}

func failOnError(err error) {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "An error occurred: %v\n", err)
		os.Exit(2)
	}
}
