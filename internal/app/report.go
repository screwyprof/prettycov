package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/screwyprof/prettycov"
)

// showReport renders the profile and, when -fail-under was given, grades the total against it.
//
// It does not change directory. It used to chdir to the profile's directory and then open the
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

	kept, excluded := prettycov.Exclude(items, cfg.Exclude)
	reportExclusions(excluded, stderr)

	tree := prettycov.Process(kept, cfg.CurrentRoot, cfg.NewRoot)

	prettycov.DisplayTree(stdout, tree, prettycov.Options{Depth: cfg.Depth, Color: cfg.Color})

	return checkThreshold(cfg.FailUnder, tree, stderr)
}

// reportExclusions says what each pattern took out, on stderr so the report itself stays pipeable.
// Always, not behind a verbose flag: exclusion moves the denominator.
func reportExclusions(excluded []prettycov.Exclusion, stderr io.Writer) {
	for _, ex := range excluded {
		if ex.Files == 0 {
			_, _ = fmt.Fprintf(stderr, "-exclude %q matched nothing\n", ex.Pattern)

			continue
		}

		_, _ = fmt.Fprintf(stderr, "-exclude %q left out %s in %s\n",
			ex.Pattern, plural(ex.Statements, "statement"), plural(ex.Files, "file"))
	}
}

// plural counts n things. "1 statements in 1 files" is the common case for a pattern aimed at one
// generated file, so it is worth the three lines.
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}

	return fmt.Sprintf("%d %ss", n, thing)
}

// checkThreshold grades the total against want, which is nil when no gate was asked for.
func checkThreshold(want *float64, tree *prettycov.PathTree, stderr io.Writer) int {
	if want == nil {
		return exitOK
	}

	// A profile with nothing to cover cannot clear a threshold, and silently passing would make
	// the gate useless on an empty or mis-pointed profile.
	total, ok := tree.Coverage.Ratio()
	if !ok {
		_, _ = fmt.Fprintf(stderr, "no statements to cover, wanted at least %.2f%%\n", *want)

		return exitBelow
	}

	if total < *want {
		_, _ = fmt.Fprintf(stderr, "total coverage %.2f%% is below %.2f%%\n", total, *want)

		return exitBelow
	}

	return exitOK
}
