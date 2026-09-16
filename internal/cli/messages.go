package cli

// Everything prettycov says about a run that is not the report itself: what a pattern took, what a
// root did not match, why a printer came up empty, which path was probably meant.
//
// All of it on stderr, so the report stays pipeable, and none of it deciding an exit code — that is
// the gate's. The facts arrive from the domain; this file decides only the words, which is why it
// is the one place a flag is named in a sentence.

import (
	"fmt"

	"github.com/screwyprof/prettycov"
)

// reasonFor is how an Outcome without a tree reads. Here rather than beside the constant, because Measure
// states the fact and only a command line has an opinion about the words.
func reasonFor(o prettycov.Outcome) string {
	if o == prettycov.NoStatements {
		return "no statements to cover"
	}

	return "--exclude left nothing to report"
}

// sayNothingShown reports a printer that came up empty, which both drawing commands can be.
//
// It asks nothing about which printer that was. One drawing rows and one printing positions would
// need a message each, and a third would need a third — every one of them a second place holding an
// opinion about what the filters do. The filters emptied it; naming them is the answer either way.
func sayNothingShown(cfg config, tree *prettycov.PathTree, s Streams) {
	_, _ = fmt.Fprintln(s.Err, cfg.whyNothingShown(tree))
}

// suggest offers the path under the module root when what was typed is not there. The tree answers
// whether such a path exists — see PathTree.UnderRoot — and this decides only the words.
func suggest(tree *prettycov.PathTree, want string) string {
	full, ok := tree.UnderRoot(want)
	if !ok {
		return ""
	}

	return fmt.Sprintf(", did you mean %q?", full)
}

// whyNothingShown says why a printer came up empty. Printer-blind: a message per printer would be a
// second place with an opinion about what the filters do.
//
// The two causes are opposite news, and the tree's count separates them. Saying "nothing left to
// cover" for the second is a false all-clear — `misses --hide-covered=0` over a fully drawn tree
// reported completion on 34 statements, exit 0.
func (c config) whyNothingShown(tree *prettycov.PathTree) string {
	if tree.Uncovered() == 0 {
		return "nothing left to cover"
	}

	return fmt.Sprintf("nothing to show at %s; %s left",
		c.outputFilters(), plural(tree.Uncovered(), "uncovered statement"))
}

// outputFilters names the flags that could have emptied the output, as typed. The one place a
// filter added later has to be named.
//
// Not --files: a list of positions is made of files whatever it says, and a tree keeps the top row
// --depth always draws. Not --exclude: it acts on the profile, and a report it emptied is refused
// further up with its own message.
func (c config) outputFilters() string {
	filters := "--depth=" + c.Depth.String()

	if c.HideCovered != nil {
		filters += fmt.Sprintf(", --hide-covered=%v", *c.HideCovered)
	}

	return filters
}

// reportExclusions says what each pattern took. Not behind a verbose flag: exclusion moves the
// denominator.
//
// A pattern that matched nothing is said, not refused — unlike --old, which transforms the output,
// so one that did nothing leaves a report nobody asked for.
func reportExclusions(excluded []prettycov.Exclusion, s Streams) {
	for _, ex := range excluded {
		// Distinct from matching nothing: the pattern works, an earlier one got there first. Saying
		// "matched nothing" sends someone to delete a pattern that is holding the line for the day
		// such a file lands outside the earlier one's reach.
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
