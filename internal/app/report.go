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

	if cfg.Total {
		return showTotal(cfg, tree, stdout, stderr)
	}

	prettycov.DisplayTree(stdout, tree, prettycov.Options{Depth: cfg.Depth, Color: cfg.Color})

	return checkThreshold(cfg.FailUnder, tree, stderr)
}

// showTotal writes the total percentage and nothing else, for a caller reading it into a variable.
// It grades against -fail-under exactly as the report does.
func showTotal(cfg config, tree *prettycov.PathTree, stdout, stderr io.Writer) int {
	text, ok := prettycov.Percentage(tree.Coverage)

	// Nothing to cover is refused rather than printed: "n/a" is what the tree shows, and a caller
	// reading `COVERAGE := $(shell prettycov -total)` would carry it into a comparison, while 0.00
	// reads as a real and terrible number. With a gate it is checkThreshold's call, which already
	// refuses it — reporting a failed threshold as exit 2 would say prettycov could not run, and a
	// step branching on the two codes would take the infrastructure path.
	if !ok && cfg.FailUnder == nil {
		_, _ = fmt.Fprintln(stderr, "no statements to cover")

		return exitFailed
	}

	if ok {
		_, _ = fmt.Fprintln(stdout, text)
	}

	return checkThreshold(cfg.FailUnder, tree, stderr)
}

// reportExclusions says what each pattern took out, on stderr so the report itself stays pipeable.
// Always, not behind a verbose flag: exclusion moves the denominator.
func reportExclusions(excluded []prettycov.Exclusion, stderr io.Writer) {
	for _, ex := range excluded {
		// Distinct from matching nothing: the pattern works, an earlier one just got there first.
		// Saying "matched nothing" here sends someone to fix a pattern that is already right, and
		// deleting it stops working the day such a file lands outside the earlier pattern's reach.
		if ex.Files == 0 && ex.Overlapped > 0 {
			_, _ = fmt.Fprintf(stderr, "-exclude %q took nothing out, %s already excluded\n",
				ex.Pattern, plural(ex.Overlapped, "file"))

			continue
		}

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
		// The measured figure goes through Percentage, like every other one, so this message and
		// the report it refers to cannot disagree. The threshold does not: it is a number the
		// caller typed, not a coverage ratio, and rounding it is all it needs.
		text, _ := prettycov.Percentage(tree.Coverage)
		_, _ = fmt.Fprintf(stderr, "total coverage %s%% is below %.2f%%\n", text, *want)

		return exitBelow
	}

	return exitOK
}
