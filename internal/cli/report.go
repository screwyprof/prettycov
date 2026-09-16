package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"path"

	"github.com/screwyprof/prettycov"
)

// prepare turns the profile into a tree, or an error
// carrying the status for something already reported.
//
// It does not know which command asked. What is done with the tree is the command's, which is why
// config no longer carries a flag saying which one ran.
//
// It does not change directory. It used to chdir to the profile's directory and then open the path
// it was given, which meant any relative path with a directory component failed to resolve.
func prepare(cfg config, s Streams) (*prettycov.PathTree, error) {
	items, err := prettycov.ParseProfile(cfg.Profile)
	if err != nil {
		_, _ = fmt.Fprintf(s.Err, "%v\n", err)

		// Someone running prettycov for the first time, in a repo with no profile yet, is one
		// command away. Say which, rather than leaving them a bare file-not-found.
		if errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(s.Err, "run: go test -coverprofile=%s ./...\n", cfg.Profile)
		}

		return nil, exitError{code: ExitFailed}
	}

	// Asked before any flag is judged. A profile with nothing in it has nothing for a pattern or a
	// root to match, so every one of them would be reported stale — a good `--old=$(MODULE)` named
	// as the fault when the profile is what is empty, and exit 2 where the gate below says 1.
	if !anyStatements(items) {
		return nil, refuseEmpty(cfg, "no statements to cover", s)
	}

	kept, excluded := prettycov.Exclude(items, cfg.Exclude)
	reportExclusions(excluded, s)

	shortened, renamed := prettycov.Shorten(kept, cfg.Rename.From, cfg.Rename.To)

	// A root that matched nothing did not rename, which is an argument mistake found a step later
	// only because the profile is what answers it. No report with it: the labels would not be the
	// ones asked for. --exclude is not held to this — a pattern is a filter, and "drop this if it
	// is here" is a reasonable thing to write.
	if reportRename(cfg, items, renamed, s) {
		return nil, exitError{code: ExitFailed}
	}

	tree := prettycov.Process(shortened)

	// Settled once here, so no two commands can answer it differently.
	if _, ok := tree.Coverage.Percentage(); !ok {
		return nil, refuseEmpty(cfg, "--exclude left nothing to report", s)
	}

	return tree, nil
}

// prepare retrieves; each command renders. They are composed in a command's Run and not bundled
// into one interface: what turns flags into a tree is the same for every command, and what turns a
// tree into output is the only thing that differs.

// sayNothingShown reports a printer that came up empty, which both drawing commands can be.
//
// It asks nothing about which printer that was. One drawing rows and one printing positions would
// need a message each, and a third would need a third — every one of them a second place holding an
// opinion about what the filters do. The filters emptied it; naming them is the answer either way.
func sayNothingShown(cfg config, tree *prettycov.PathTree, s Streams) {
	_, _ = fmt.Fprintln(s.Err, cfg.whyNothingShown(tree))
}

// underRoot suggests the path with the module root in front of it, when that is what resolves.
//
// A row is drawn with its own segment, so reading `pkg - 96.41` off a report and asking for "pkg"
// is the obvious next thing to type and the wrong one: the tree holds it under the whole module
// path. Saying so costs a lookup that has already failed once, and only fires when it turns the
// miss into a hit, so it can never send anyone somewhere that is not there.
//
// The root is a run of single-child directories rather than one node — a module path spends three
// of them on github.com, the owner and the repository — so this descends that run the way collapse
// does, which is exactly the run the report draws as its top row. It stops where the tree branches
// or holds a file, because past there is a choice and there is no one prefix to suggest.
func underRoot(tree *prettycov.PathTree, want string) string {
	root := ""

	for node := tree; len(node.Children) == 1 && len(node.Files) == 0; {
		for name, child := range node.Children {
			root, node = path.Join(root, name), child
		}

		if full := path.Join(root, want); tree.Get(full) != nil {
			return fmt.Sprintf(", did you mean %q?", full)
		}
	}

	return ""
}

// reportRename reports whether --old named a package the profile does not hold — a typo, or a module
// path that has moved — and says so. parseFlags has already refused a root that names no package at
// all; this is one that names the wrong one, which only the matching can catch.
//
// Asked of the whole profile rather than of what survived --exclude. Shorten runs on what is left,
// so a pattern that took every file under a perfectly good root would otherwise be reported as a
// bad root — sending someone to fix a flag that is already right, which is the confusion the
// overlap branch in reportExclusions exists to prevent.
func reportRename(cfg config, items []prettycov.FileCoverage, renamed int, s Streams) bool {
	// A root alone is refused by parseFlags, which TestRunRefusesHalfARename drives end to end, so
	// a root that is set means a target came with it. Testing NewRoot here as well would be a guard
	// no invocation can reach, and an uncoverable branch in a tool that reports coverage.
	//
	// renamed is a shortcut, not a second reason: Exclude only drops files, never renames them, so
	// anything it left that matched the root is in the profile too and HasRoot would agree. It
	// keeps even that scan off the path where the rename worked, which is every run not a mistake.
	if !cfg.Rename.asked() || renamed > 0 {
		return false
	}

	if prettycov.HasRoot(items, cfg.Rename.From) {
		return false
	}

	_, _ = fmt.Fprintf(s.Err, "--old %q matched nothing, so no label was shortened\n", cfg.Rename.From)

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
// no-op. With --fail-under it is a failed gate instead: exit 2 would read as "prettycov could not
// run" when the truth is that coverage was too low.
//
// The reason is the caller's, and the gate only adds what it wanted — being told to check
// `go test -coverprofile` for a report your own pattern emptied is the confusion it exists to
// prevent.
func refuseEmpty(cfg config, reason string, s Streams) error {
	if cfg.FailUnder != nil {
		_, _ = fmt.Fprintf(s.Err, "%s, wanted at least %.2f%%\n", reason, *cfg.FailUnder)

		return exitError{code: ExitBelow}
	}

	_, _ = fmt.Fprintln(s.Err, reason)

	return exitError{code: ExitFailed}
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
// `misses --hide-covered=0` over a fully drawn tree reported completion on 34 statements, exit 0.
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
// --depth is always in play and always has a value, so it is always named. --hide-covered is named
// when it was given, which is the only time it can have taken anything.
//
// --files is not one of these, not because it shapes nothing — it decides which rows exist, and
// reaches --hide-covered's judgement through the same gate — but because it cannot be the one that
// emptied the output. A list of positions is made of files whatever it says, and a tree keeps the
// top row --depth always draws. --exclude is not either: it acts on the profile, and a report it
// emptied is refused further up with a message of its own.
func (c config) outputFilters() string {
	filters := "--depth=" + c.Depth.String()

	if c.HideCovered != nil {
		filters += fmt.Sprintf(", --hide-covered=%v", *c.HideCovered)
	}

	return filters
}

// total writes one percentage and nothing else, for a caller reading it into a variable.
//
// total <path> reports a node of the tree rather than the whole of it, which is the one number the
// report shows and nothing could hand back: `prettycov --depth=max` draws `web/handler - 89.66` and
// there was no way to get 89.66 out. The path is spelled as the report prints it, because the tree
// is built from shortened paths and this looks the node up in that tree — the opposite of --exclude,
// which matches the profile's own paths, and the right way round here: you read a row, then ask for
// its number.
//
// The same node is printed and graded. Two lookups would be two chances to disagree, which is
// exactly how --fail-under=100 came to pass a run whose own report read 99.99.
func total(cfg config, tree *prettycov.PathTree, want string, s Streams) error {
	node := tree

	if want != "" {
		// Quoted, as every message quoting something the reader typed is: a path can be
		// empty-looking or carry a control byte, and argv is where both arrive from.
		if node = tree.Get(want); node == nil {
			_, _ = fmt.Fprintf(s.Err, "total: no such package or file in the profile: %q%s\n",
				want, underRoot(tree, want))

			return exitError{code: ExitFailed}
		}
	}

	// A node with no statements has no percentage. The whole tree cannot be one by here — an empty
	// profile is refused above — but one node can: a directory whose files declare none. Through
	// refuseEmpty rather than here, so a gate reads the same for a node as for the tree: exit 1 and
	// the shortfall, not exit 2 as though prettycov could not run.
	pct, ok := node.Coverage.Percentage()
	if !ok {
		return refuseEmpty(cfg, fmt.Sprintf("total names nothing with statements to cover: %q", want), s)
	}

	_, _ = fmt.Fprintln(s.Out, pct)

	return checkThreshold(cfg.FailUnder, node, s)
}

// reportExclusions says what each pattern took out, on stderr so the report itself stays pipeable.
// Not behind a verbose flag: exclusion moves the denominator. The one run it says nothing on is an
// empty profile, which showReport answers before it gets here — there every pattern took nothing,
// so the accounting is a column of zeroes under a line already saying why.
//
// A pattern that matched nothing is said, not refused: see showReport for why --old is and this is
// not.
func reportExclusions(excluded []prettycov.Exclusion, s Streams) {
	for _, ex := range excluded {
		// Distinct from matching nothing: the pattern works, an earlier one just got there first.
		// Saying "matched nothing" here sends someone to fix a pattern that is already right, and
		// deleting it stops working the day such a file lands outside the earlier pattern's reach.
		if ex.Files == 0 && ex.Blocks == 0 && ex.Overlapped() > 0 {
			_, _ = fmt.Fprintf(s.Err, "--exclude %q took nothing out, %s already excluded\n",
				ex.Pattern, units(ex.OverlappedFiles, ex.OverlappedBlocks))

			continue
		}

		if ex.Files == 0 && ex.Blocks == 0 {
			_, _ = fmt.Fprintf(s.Err, "--exclude %q matched nothing\n", ex.Pattern)

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

		_, _ = fmt.Fprintf(s.Err, "--exclude %q left out %s in %s%s\n",
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

// checkThreshold grades node's coverage against want, which is nil when no gate was asked for. The
// node is the whole tree for every caller but total <path>, which hands over the one it printed —
// grading a second lookup would be a second chance to disagree with the number on screen.
//
// CoverageStats answers whether the total is at the bar, rather than this comparing the ratio
// itself: at 100 the two differ. A profile one statement short of complete divides to exactly 100
// in float64 once the counts are large enough, so comparing ratios passed --fail-under=100 for a
// report that reads 99.99 — the gate and the figure beside it disagreeing about the same run.
//
// There is always a number by here: showReport refuses a profile with nothing to cover before any
// caller reaches this, and showTotal refuses a node with none, each saying what this cannot.
func checkThreshold(want *float64, node *prettycov.PathTree, s Streams) error {
	if want == nil {
		return nil
	}

	if !node.Coverage.AtLeast(*want) {
		pct, _ := node.Coverage.Percentage()

		// Percentage renders the coverage figure, as it does everywhere else, so this message and
		// the report cannot show different numbers for the same thing.
		//
		// The threshold is rounded instead, which is not free of trouble: --fail-under=99.99999
		// reads back as 100.00%, a figure Percentage will never print, and at 79.999% against
		// --fail-under=80 both sides round to 80.00 and the line contradicts itself. Printing the
		// threshold as typed would fix both, and would change this message for everyone.
		_, _ = fmt.Fprintf(s.Err, "total coverage %s%% is below %.2f%%\n", pct, *want)

		return exitError{code: ExitBelow}
	}

	return nil
}
