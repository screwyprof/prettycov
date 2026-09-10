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

	// Settled once here, so the tree and -total cannot answer it differently. Refused rather than
	// drawn, because an empty report exits 0 and turns a coverage gate into a green no-op. With
	// -fail-under it is a failed gate instead: exit 2 would read as "prettycov could not run"
	// when the truth is that coverage was too low.
	if _, ok := tree.Coverage.Percentage(); !ok {
		if cfg.FailUnder != nil {
			return checkThreshold(cfg.FailUnder, tree, stderr)
		}

		_, _ = fmt.Fprintln(stderr, emptyReason(excluded))

		return exitFailed
	}

	if cfg.Total {
		return showTotal(cfg, tree, stdout, stderr)
	}

	// The destination is asked about here and nowhere earlier: parsing argv is too early to know
	// where the report goes, and no other flag needs to.
	prettycov.DisplayTree(stdout, tree, prettycov.Options{
		Depth:  cfg.Depth,
		Color:  cfg.Color.palette(stdout),
		Counts: cfg.Counts,
		Files:  cfg.Files,
	})

	return checkThreshold(cfg.FailUnder, tree, stderr)
}

// emptyReason blames -exclude when it is what took the statements out, and the profile otherwise.
// Statements rather than surviving files: a leftover file declaring none would otherwise send the
// reader to check `go test -coverprofile` for a report a pattern emptied.
func emptyReason(excluded []prettycov.Exclusion) string {
	for _, ex := range excluded {
		if ex.Statements > 0 {
			return "-exclude left nothing to report"
		}
	}

	return "no statements to cover"
}

// showTotal writes the total percentage and nothing else, for a caller reading it into a variable.
// There is always a number by here: showReport has already refused a profile with nothing to
// cover, so `COVERAGE := $(shell prettycov -total)` never picks up an "n/a" or a bare 0.00.
func showTotal(cfg config, tree *prettycov.PathTree, stdout, stderr io.Writer) int {
	pct, _ := tree.Coverage.Percentage()

	_, _ = fmt.Fprintln(stdout, pct)

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
	pct, ok := tree.Coverage.Percentage()
	if !ok {
		_, _ = fmt.Fprintf(stderr, "no statements to cover, wanted at least %.2f%%\n", *want)

		return exitBelow
	}

	if pct.Float() < *want {
		// Percentage renders the coverage figure, as it does everywhere else, so this message and
		// the report cannot show different numbers for the same thing.
		//
		// The threshold is rounded instead, which is not free of trouble: -fail-under=99.99999
		// reads back as 100.00%, a figure Percentage will never print, and at 79.999% against
		// -fail-under=80 both sides round to 80.00 and the line contradicts itself. Printing the
		// threshold as typed would fix both, and would change this message for everyone.
		_, _ = fmt.Fprintf(stderr, "total coverage %s%% is below %.2f%%\n", pct, *want)

		return exitBelow
	}

	return exitOK
}
