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

	// -misses replaces the report rather than decorating it, so this is a choice of printer and not
	// a second path through the report. The positions are the drill-down and the tree is the
	// summary: printing both would answer the question the default invocation already answered, and
	// a tree row can be read as a location by whatever parses this, since a label may carry a colon.
	//
	// Both printers have the same shape — (writer, tree, options) returning what they drew — so the
	// call below does not know which one it is holding. What they return is each one's own unit,
	// rows against statements, and the only thing they promise in common is that it is zero exactly
	// when nothing was written.
	draw := prettycov.DisplayTree
	if cfg.Misses {
		draw = prettycov.DisplayMisses
	}

	shown := draw(stdout, tree, opts)

	// A command that prints nothing, or less than everything, reads as one that ran clean.
	cfg.reportOutput(tree, shown, stderr)

	return checkThreshold(cfg.FailUnder, tree, stderr)
}

// reportOutput says what the printer produced when that is not evident from the output itself. The
// exit code is unchanged and the report is correct either way.
//
// Printing nothing is the shared case and stays printer-blind: both can be emptied, and by the same
// filters. Being a fragment is not shared — a tree carries its subtree's count on every row, so the
// rows it draws account for every statement at any depth, which is what makes a shallow one a
// summary rather than a short list. So that is asked of the one printer it can happen to, and shown
// is read as that printer's own unit rather than as a count the two have to agree on.
//
// Short is the quieter mistake of the two. `file:line:col` is the shape every linter and compiler
// emits, and in all of them it is everything they found, so a list that stops early reads as a clean
// bill: pipe eight of thirty-four into `vim -q -`, fix them, and the quickfix says there is nothing
// left. On stderr, so the pipe carries only the list.
func (c config) reportOutput(tree *prettycov.PathTree, shown int, stderr io.Writer) {
	switch {
	case shown == 0:
		_, _ = fmt.Fprintln(stderr, c.whyNothingShown(tree))
	case c.Misses && shown < tree.Coverage.Uncovered:
		_, _ = fmt.Fprintf(stderr, "%s lists %d of %s\n",
			c.outputFilters(), shown, plural(tree.Coverage.Uncovered, "uncovered statement"))
	}
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

// whyNothingShown says why a printer came up empty.
//
// It asks nothing about which printer that was. One drawing rows and one printing positions would
// need a message each, and a third would need a third — and every one of them would be a second
// place holding an opinion about what the output filters do, which is the drift the single prepare
// seam exists to prevent. The filters emptied it; naming them is the whole answer either way.
//
// Two causes, and they are opposite news. The tree's own count separates them, which is a number
// already to hand rather than a second pass over the filtering: no uncovered statement anywhere is
// an all-clear worth printing, and uncovered statements the filters leave out is the opposite.
// Saying the first when the second happened is a false all-clear on a profile with work left in it —
// `-misses -hide-covered=0` over a fully drawn tree reported completion on 34 statements, exit 0.
//
// It does not say what one level deeper would have shown, which nothing here knows. Re-deriving it
// would put the filtering in a second place.
func (c config) whyNothingShown(tree *prettycov.PathTree) string {
	if tree.Coverage.Uncovered == 0 {
		return "nothing left to cover"
	}

	// "left" rather than "remain", which would need a second spelling for the singular that plural
	// already handles for the count itself.
	return fmt.Sprintf("nothing to show at %s; %s left",
		c.outputFilters(), plural(tree.Coverage.Uncovered, "uncovered statement"))
}

// outputFilters names the flags that shape the output, as typed — the one place a filter added
// later has to be named, which is what keeps whyNothingShown out of the business of diagnosing
// which one did it.
//
// -depth is always in play and always has a value, so it is always named. -hide-covered is named
// when it was given, which is the only time it can have taken anything.
//
// -files is not one of these, not because it shapes nothing — it decides which rows exist, and
// reaches -hide-covered's judgement through the same gate — but because it cannot be the one that
// emptied the output. A list of positions is made of files whatever it says, and a tree keeps the
// top row -depth always draws. -exclude is not either: it acts on the profile, and a report it
// emptied is refused further up with a message of its own.
func (c config) outputFilters() string {
	filters := "-depth=" + c.Depth.String()

	if c.HideCovered != nil {
		filters += fmt.Sprintf(", -hide-covered=%v", *c.HideCovered)
	}

	return filters
}

// showTotal writes one percentage and nothing else, for a caller reading it into a variable.
//
// -total=path reports a node of the tree rather than the whole of it, which is the one number the
// report shows and nothing could hand back: `prettycov -depth=max` draws `web/handler - 89.66` and
// there was no way to get 89.66 out. The path is spelled as the report prints it, because the tree
// is built from shortened paths and this looks the node up in that tree.
//
// The same node is printed and graded. Two lookups would be two chances to disagree, which is
// exactly how -fail-under=100 came to pass a run whose own report read 99.99.
func showTotal(cfg config, tree *prettycov.PathTree, stdout, stderr io.Writer) int {
	node := tree
	if cfg.TotalOf != "" {
		if node = tree.Get(cfg.TotalOf); node == nil {
			_, _ = fmt.Fprintf(stderr, "%v: %s\n", errNoSuchPath, cfg.TotalOf)

			return exitFailed
		}
	}

	// A node with no statements has no percentage, which the whole tree cannot be by here — an empty
	// profile is refused above — but one package can: a directory of files that declare none.
	// Printing "n/a" into a variable is worse than saying so.
	pct, ok := node.Coverage.Percentage()
	if !ok {
		_, _ = fmt.Fprintf(stderr, "%v: %s\n", errNothingToCover, cfg.TotalOf)

		return exitFailed
	}

	_, _ = fmt.Fprintln(stdout, pct)

	return checkThreshold(cfg.FailUnder, node, stderr)
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
// CoverageStats answers whether the total is at the bar, rather than this comparing the ratio
// itself: at 100 the two differ. A profile one statement short of complete divides to exactly 100
// in float64 once the counts are large enough, so comparing ratios passed -fail-under=100 for a
// report that reads 99.99 — the gate and the figure beside it disagreeing about the same run.
//
// There is always a number by here: showReport refuses a profile with nothing to cover before
// either caller reaches this, and says what emptied it, which this cannot.
func checkThreshold(want *float64, tree *prettycov.PathTree, stderr io.Writer) int {
	if want == nil {
		return exitOK
	}

	if !tree.Coverage.AtLeast(*want) {
		pct, _ := tree.Coverage.Percentage()

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
