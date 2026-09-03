package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/screwyprof/prettycov"
)

// showReport renders the profile and, when -fail-under is set, reports whether the total cleared
// it. It does not change directory: it used to chdir to the profile's directory and then open the
// path it was given, which meant any relative path with a directory component failed to resolve.
func showReport(cfg config, stdout, stderr io.Writer) int {
	items, err := prettycov.ParseProfile(cfg.Profile)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)

		// Someone running prettycov for the first time, in a repo with no profile yet, is one
		// command away. Say which, rather than leaving them a bare file-not-found.
		if errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(stderr, "run: go test -coverprofile=%s ./...\n", cfg.Profile)
		}

		return exitFailed
	}

	tree := prettycov.Process(items, cfg.CurrentRoot, cfg.NewRoot)

	prettycov.DisplayTree(stdout, tree, prettycov.Options{Depth: cfg.Depth, Color: cfg.Color})

	return checkThreshold(cfg, tree, stderr)
}

func checkThreshold(cfg config, tree *prettycov.PathTree, stderr io.Writer) int {
	if !cfg.Gate {
		return exitOK
	}

	// A profile with nothing to cover cannot clear a threshold, and silently passing would make
	// the gate useless on an empty or mis-pointed profile.
	total, ok := tree.Coverage.Ratio()
	if !ok {
		_, _ = fmt.Fprintf(stderr, "no statements to cover, wanted at least %.2f%%\n", cfg.FailUnder)

		return exitBelow
	}

	if total < cfg.FailUnder {
		_, _ = fmt.Fprintf(stderr, "total coverage %.2f%% is below %.2f%%\n", total, cfg.FailUnder)

		return exitBelow
	}

	return exitOK
}
