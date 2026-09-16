package cli

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/screwyprof/prettycov"
)

// A gate is --fail-under: the only thing that turns what was measured into a status, rather than
// turning how the run went into one. A type rather than a *float64 threaded through four
// signatures, and narrow on purpose — refuseEmpty and total each read this and nothing else, where
// they used to take the whole config to reach one field.
type gate struct{ want *float64 }

// treeOf is what the commands call: it measures, says on stderr what the measuring found, and
// hands back the tree. The writers are here and not in measure, because saying is the only reason
// they are needed at all.
func treeOf(req prettycov.Request, g gate, s Streams) (*prettycov.PathTree, error) {
	res, err := prettycov.Measure(req)
	if err != nil {
		_, _ = fmt.Fprintf(s.Err, "%v\n", err)

		// Someone running prettycov for the first time, in a repo with no profile yet, is one
		// command away. Say which, rather than leaving them a bare file-not-found.
		if errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(s.Err, "run: go test -coverprofile=%s ./...\n", req.Profile)
		}

		return nil, exitError{code: ExitFailed}
	}

	// Said before the two failures below, because a pattern that took everything is how a run ends
	// up with nothing to report and the accounting is what shows it.
	reportExclusions(res.Exclusions, s)

	switch {
	// Refused, not merely said: a rename transforms the output, so one that did not happen leaves a
	// report nobody asked for. --exclude is not held to this — a pattern is a filter, and "drop this
	// if it is here" is a reasonable thing to write.
	case res.RootMissed:
		_, _ = fmt.Fprintf(s.Err, "--old %q matched nothing, so no label was shortened\n", req.Rename.From)

		return nil, exitError{code: ExitFailed}
	case res.Empty != prettycov.NotEmpty:
		return nil, g.refuse(reasonFor(res.Empty), s)
	}

	return res.Tree, nil
}

// prepare retrieves; each command renders. They are composed in a command's Run and not bundled
// into one interface: what turns flags into a tree is the same for every command, and what turns a
// tree into output is the only thing that differs.

// refuse says why there is nothing to report and grades the absence.
//
// Refused rather than drawn, because an empty report exits 0 and turns a coverage gate into a green
// no-op. With --fail-under it is a failed gate instead: exit 2 would read as "prettycov could not
// run" when the truth is that coverage was too low.
//
// The reason is the caller's, and the gate only adds what it wanted — being told to check
// `go test -coverprofile` for a report your own pattern emptied is the confusion it exists to
// prevent.
func (g gate) refuse(reason string, s Streams) error {
	if g.want != nil {
		_, _ = fmt.Fprintf(s.Err, "%s, wanted at least %.2f%%\n", reason, *g.want)

		return exitError{code: ExitBelow}
	}

	_, _ = fmt.Fprintln(s.Err, reason)

	return exitError{code: ExitFailed}
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
func total(g gate, tree *prettycov.PathTree, want string, s Streams) error {
	node := tree

	if want != "" {
		// Quoted, as every message quoting something the reader typed is: a path can be
		// empty-looking or carry a control byte, and argv is where both arrive from.
		if node = tree.Get(want); node == nil {
			_, _ = fmt.Fprintf(s.Err, "total: no such package or file in the profile: %q%s\n",
				want, suggest(tree, want))

			return exitError{code: ExitFailed}
		}
	}

	// A node with no statements has no percentage. The whole tree cannot be one by here — an empty
	// profile is refused above — but one node can: a directory whose files declare none. Through
	// refuseEmpty rather than here, so a gate reads the same for a node as for the tree: exit 1 and
	// the shortfall, not exit 2 as though prettycov could not run.
	pct, ok := node.Percentage()
	if !ok {
		return g.refuse(fmt.Sprintf("total names nothing with statements to cover: %q", want), s)
	}

	_, _ = fmt.Fprintln(s.Out, pct)

	return g.grade(node, s)
}

// grade grades node's coverage against want, which is nil when no gate was asked for. The
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
func (g gate) grade(node *prettycov.PathTree, s Streams) error {
	if g.want == nil {
		return nil
	}

	if !node.AtLeast(*g.want) {
		pct, _ := node.Percentage()

		// Percentage renders the coverage figure, as it does everywhere else, so this message and
		// the report cannot show different numbers for the same thing.
		//
		// The threshold is rounded instead, which is not free of trouble: --fail-under=99.99999
		// reads back as 100.00%, a figure Percentage will never print, and at 79.999% against
		// --fail-under=80 both sides round to 80.00 and the line contradicts itself. Printing the
		// threshold as typed would fix both, and would change this message for everyone.
		_, _ = fmt.Fprintf(s.Err, "total coverage %s%% is below %.2f%%\n", pct, *g.want)

		return exitError{code: ExitBelow}
	}

	return nil
}
