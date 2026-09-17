package cli

// Everything prettycov says about a run that is not the report itself. All on stderr, so the report
// stays pipeable, and none of it deciding an exit code. The domain supplies the facts; this file
// decides only the words, and is the one place a flag is named in a sentence.

import (
	"fmt"

	"github.com/screwyprof/prettycov"
)

// sayNothingShown reports a printer that came up empty, without asking which one — the filters
// emptied it either way.
//
// The two causes are opposite news and the tree's count separates them: "nothing left to cover" for
// the second is a false all-clear, and `misses --hide-covered=0` reported completion on 34
// uncovered statements with exit 0.
func (d drawn) sayNothingShown(tree *prettycov.PathTree, s Streams) {
	if tree.Uncovered() == 0 {
		_, _ = fmt.Fprintln(s.Err, "nothing left to cover")

		return
	}

	_, _ = fmt.Fprintf(s.Err, "nothing to show at %s; %s left\n",
		d.filters(), plural(tree.Uncovered(), "uncovered statement"))
}

// filters names the flags that could have emptied the output, as typed — the one place a filter
// added later has to be named. Not --files, which cannot empty either output, and not --exclude,
// which acts on the profile and is refused further up with its own message.
func (d drawn) filters() string {
	filters := "--depth=" + d.Depth.String()

	if d.HideCovered != nil {
		filters += fmt.Sprintf(", --hide-covered=%v", d.HideCovered.Float())
	}

	return filters
}

// reportExclusions says what each pattern took. Not behind a verbose flag: exclusion moves the
// denominator. A pattern that matched nothing is said, not refused — unlike --old.
func reportExclusions(excluded []prettycov.Exclusion, s Streams) {
	for _, ex := range excluded {
		took := ex.Files > 0 || ex.Blocks > 0

		// Distinct from matching nothing: the pattern works, an earlier one got there first. Saying
		// "matched nothing" invites deleting one that is holding the line.
		if !took && ex.Overlapped() > 0 {
			_, _ = fmt.Fprintf(s.Err, "--exclude %q took nothing out, %s already excluded\n",
				ex.Pattern, units(ex.OverlappedFiles, ex.OverlappedBlocks))

			continue
		}

		if !took {
			_, _ = fmt.Fprintf(s.Err, "--exclude %q matched nothing\n", ex.Pattern)

			continue
		}

		// Charged and overlapping at once: say both, or it reads as barely earning its keep — the
		// same trap as above, sprung on a pattern that did take something.
		overlap := ""
		if ex.Overlapped() > 0 {
			overlap = ", and " + units(ex.OverlappedFiles, ex.OverlappedBlocks) + " already excluded"
		}

		_, _ = fmt.Fprintf(s.Err, "--exclude %q left out %s in %s%s\n",
			ex.Pattern, plural(ex.Statements, "statement"), units(ex.Files, ex.Blocks), overlap)
	}
}

// units names a count of files and a count of blocks. A pattern can reach both at once, so both are
// said rather than whichever came first.
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
// generated file.
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}

	return fmt.Sprintf("%d %ss", n, thing)
}
