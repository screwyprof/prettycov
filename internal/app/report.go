package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"

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

	// Asked before any flag is judged. A profile with nothing in it has nothing for a pattern or a
	// root to match, so every one of them would be reported stale — a good `-old=$(MODULE)` named
	// as the fault when the profile is what is empty, and exit 2 where the gate below says 1.
	if !anyStatements(items) {
		return refuseEmpty(cfg, "no statements to cover", stderr)
	}

	kept, excluded := prettycov.Exclude(items, cfg.Exclude)
	reportExclusions(excluded, stderr)

	shortened, renamed := prettycov.Shorten(kept, cfg.CurrentRoot, cfg.NewRoot)

	// A root that matched nothing did not rename, which is the argument mistake parseFlags refuses
	// -old alone for — found a step later only because the profile is what answers it. No report
	// with it, as for any other argument mistake: the labels would not be the ones asked for.
	//
	// -exclude is not held to this. A rename transforms the output, so one that does not happen
	// leaves a report nobody asked for; a pattern is a filter, and "drop this if it is here" is a
	// reasonable thing to write. A defensive `-exclude='\.pb\.go$'`, or one config shared by
	// several repositories, is right to match nothing in a repository that generates nothing —
	// .gitignore, codecov's ignore list and golangci-lint's exclusions all take the same view.
	// It still says so on stderr, which is how a typo shows up.
	if reportRename(cfg, items, renamed, stderr) {
		return exitFailed
	}

	tree := prettycov.Process(shortened)

	// Settled once here, so the tree and -total cannot answer it differently.
	//
	// -exclude is named without asking which flag did it. Three things make that safe together: the
	// profile held statements or the guard above would have returned, only Exclude takes any away,
	// and ParseProfile refuses a profile whose counts overflow — which is Percentage's other way of
	// answering !ok, and the one that would put a message here about a flag nobody passed. So there
	// is no other way to arrive, and weighing the exclusions could only reach the same answer.
	if _, ok := tree.Coverage.Percentage(); !ok {
		return refuseEmpty(cfg, "-exclude left nothing to report", stderr)
	}

	if cfg.Total {
		return showTotal(cfg, tree, stdout, stderr)
	}

	// The destination is asked about here and nowhere earlier: parsing argv is too early to know
	// where the report goes, and no other flag needs to.
	// checkThreshold below reads the tree, not the rows, so -hide-covered cannot move the gate.
	opts := prettycov.Options{
		Depth:       cfg.Depth,
		Color:       cfg.Color.palette(stdout),
		Counts:      cfg.Counts,
		Files:       cfg.Files,
		HideCovered: cfg.HideCovered,
	}

	// -hide-covered can take the whole report, when nothing in the profile is below the threshold.
	// That is the honest answer and the exit code is unchanged, but a command that prints nothing
	// reads as one that failed, so it says which flag emptied it and what to read that as.
	if cfg.HideCovered != nil && len(prettycov.Rows(tree, opts)) == 0 {
		_, _ = fmt.Fprintf(stderr, "-hide-covered=%s hid the whole report; nothing is below it\n",
			strconv.FormatFloat(*cfg.HideCovered, 'f', -1, 64))

		return checkThreshold(cfg.FailUnder, tree, stderr)
	}

	prettycov.DisplayTree(stdout, tree, opts)

	return checkThreshold(cfg.FailUnder, tree, stderr)
}

// reportRename reports whether -old named a package the profile does not hold — a typo, or a module
// path that has moved — and says so. parseFlags has already refused a root that names no package at
// all; this is one that names the wrong one, which only the matching can catch.
//
// Asked of the whole profile rather than of what survived -exclude. Shorten runs on what is left,
// so a pattern that took every file under a perfectly good root would otherwise be reported as a
// bad root — sending someone to fix a flag that is already right, which is the confusion the
// overlap branch in reportExclusions exists to prevent.
func reportRename(cfg config, items []prettycov.FileCoverage, renamed int, stderr io.Writer) bool {
	// A root alone is refused by parseFlags, which TestRunRefusesHalfARename drives end to end, so
	// a root that is set means a target came with it. Testing NewRoot here as well would be a guard
	// no invocation can reach, and an uncoverable branch in a tool that reports coverage.
	//
	// renamed is a shortcut, not a second reason: Exclude only drops files, never renames them, so
	// anything it left that matched the root is in the profile too and HasRoot would agree. It
	// keeps even that scan off the path where the rename worked, which is every run not a mistake.
	if cfg.CurrentRoot == "" || renamed > 0 {
		return false
	}

	if prettycov.HasRoot(items, cfg.CurrentRoot) {
		return false
	}

	_, _ = fmt.Fprintf(stderr, "-old %q matched nothing, so no label was shortened\n", cfg.CurrentRoot)

	return true
}

// anyStatements reports whether the profile holds anything to cover. Statements rather than files:
// cmd/cover emits blocks declaring none, so a profile can name files and still be empty.
func anyStatements(files []prettycov.FileCoverage) bool {
	for _, f := range files {
		if f.Coverage.Total() > 0 {
			return true
		}
	}

	return false
}

// refuseEmpty says why there is nothing to report and grades the absence.
//
// Refused rather than drawn, because an empty report exits 0 and turns a coverage gate into a green
// no-op. With -fail-under it is a failed gate instead: exit 2 would read as "prettycov could not
// run" when the truth is that coverage was too low.
//
// The reason is the caller's, and the gate only adds what it wanted — being told to check
// `go test -coverprofile` for a report your own pattern emptied is the confusion it exists to
// prevent.
func refuseEmpty(cfg config, reason string, stderr io.Writer) int {
	if cfg.FailUnder != nil {
		_, _ = fmt.Fprintf(stderr, "%s, wanted at least %.2f%%\n", reason, *cfg.FailUnder)

		return exitBelow
	}

	_, _ = fmt.Fprintln(stderr, reason)

	return exitFailed
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
// Not behind a verbose flag: exclusion moves the denominator. The one run it says nothing on is an
// empty profile, which showReport answers before it gets here — there every pattern took nothing,
// so the accounting is a column of zeroes under a line already saying why.
//
// A pattern that matched nothing is said, not refused: see showReport for why -old is and this is
// not.
func reportExclusions(excluded []prettycov.Exclusion, stderr io.Writer) {
	for _, ex := range excluded {
		// Distinct from matching nothing: the pattern works, an earlier one just got there first.
		// Saying "matched nothing" here sends someone to fix a pattern that is already right, and
		// deleting it stops working the day such a file lands outside the earlier pattern's reach.
		if ex.Files == 0 && ex.Blocks == 0 && ex.Overlapped() > 0 {
			_, _ = fmt.Fprintf(stderr, "-exclude %q took nothing out, %s already excluded\n",
				ex.Pattern, units(ex.OverlappedFiles, ex.OverlappedBlocks))

			continue
		}

		if ex.Files == 0 && ex.Blocks == 0 {
			_, _ = fmt.Fprintf(stderr, "-exclude %q matched nothing\n", ex.Pattern)

			continue
		}

		// Charged and overlapping at once: say both. Reporting only what it took reads as a pattern
		// barely earning its keep, and deleting it gives back everything an earlier pattern happens
		// to be covering for it — which is the same trap the message above exists to avoid, sprung
		// on a pattern that did take something.
		overlap := ""
		if ex.Overlapped() > 0 {
			overlap = ", and " + units(ex.OverlappedFiles, ex.OverlappedBlocks) + " already excluded"
		}

		_, _ = fmt.Fprintf(stderr, "-exclude %q left out %s in %s%s\n",
			ex.Pattern, plural(ex.Statements, "statement"), units(ex.Files, ex.Blocks), overlap)
	}
}

// units names a count of files and a count of blocks. A pattern can reach both at once — one that
// takes whole files and blocks out of others is unlikely but legal — so both are said when both
// happened rather than reporting whichever came first.
func units(files, blocks int) string {
	switch {
	case blocks == 0:
		return plural(files, "file")
	case files == 0:
		return plural(blocks, "block")
	default:
		return plural(files, "file") + " and " + plural(blocks, "block")
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
//
// There is always a number by here: showReport refuses a profile with nothing to cover before
// either caller reaches this, and says what emptied it, which this cannot.
func checkThreshold(want *float64, tree *prettycov.PathTree, stderr io.Writer) int {
	if want == nil {
		return exitOK
	}

	pct, _ := tree.Coverage.Percentage()

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
