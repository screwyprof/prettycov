package cli

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/screwyprof/prettycov"
)

// A gate is --fail-under: the only thing that turns what was measured into a status, rather than
// turning how the run went into one.
type gate struct{ want prettycov.OptionalThreshold }

// treeOf measures and says on stderr what the measuring found. The writers are here rather than in
// Measure because saying is the only thing that needs them.
func treeOf(req prettycov.Request, g gate, s Streams) (*prettycov.PathTree, error) {
	res, err := prettycov.Measure(req)
	if err != nil {
		_, _ = fmt.Fprintf(s.Err, "%v\n", err)

		// A first run in an untested repo is one command away; a bare file-not-found does not say
		// which.
		if errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(s.Err, "run: go test -coverprofile=%s ./...\n", req.Profile)
		}

		return nil, exitError{code: ExitFailed}
	}

	// Before the failures below: a pattern that took everything is how a run ends up with nothing,
	// and this accounting is what shows it.
	reportExclusions(res.Exclusions, s)

	// The bool, not the Outcome: Tree hands it back so a caller cannot read a nil tree without being
	// told. Asked before the switch, which is then only the ways a run comes back empty.
	if tree, ok := res.Tree(); ok {
		return tree, nil
	}

	// One switch over every outcome, so exhaustive asks when a fifth is added. The if this replaced
	// handed anything it did not recognise whichever sentence happened to be last.
	switch res.Outcome() {
	// Refused, not merely said: a rename transforms the output, so one that did not happen leaves a
	// report nobody asked for. --exclude is not held to this — a pattern is a filter, and "drop this
	// if it is here" is a reasonable thing to write.
	case prettycov.RootMissed:
		_, _ = fmt.Fprintf(s.Err, "--old %q matched nothing, so no label was shortened\n", req.Rename.From)
	case prettycov.NoStatements:
		return nil, g.refuse("no statements to cover", s)
	case prettycov.ExcludedAway:
		return nil, g.refuse("--exclude left nothing to report", s)
	case prettycov.Measured: // returned above, where the tree is
	case prettycov.Unmeasured: // returned above, with the error that produced it
	}

	return nil, exitError{code: ExitFailed}
}

// wroteNothing reports a destination that would not take the report. Exit 2, never the gate's 1:
// the coverage is whatever it is, and what failed is writing it down — grading a report nobody
// received would answer a question that was not asked.
//
// A closed pipe does not reach here. Go raises SIGPIPE for stdout and stderr, so `prettycov report
// | head` dies before the write returns, which is what every other command-line tool does.
func wroteNothing(err error, s Streams) error {
	_, _ = fmt.Fprintf(s.Err, "cannot write the report: %v\n", err)

	return exitError{code: ExitFailed}
}

// refuse grades an absence. Exit 1 under a gate, not 2: exit 2 reads as "prettycov could not run",
// where the truth is that coverage was too low. Without a gate an empty report would exit 0, which
// turns a CI check into a green no-op.
func (g gate) refuse(reason string, s Streams) error {
	if bar, ok := g.want.Unwrap(); ok {
		_, _ = fmt.Fprintf(s.Err, "%s, wanted at least %.2f%%\n", reason, bar.Float())

		return exitError{code: ExitBelow}
	}

	_, _ = fmt.Fprintln(s.Err, reason)

	return exitError{code: ExitFailed}
}

// total writes one percentage and nothing else.
//
// The path is spelled as the report prints it, not as the profile does — the tree is built from
// shortened paths, so this looks a node up the way a reader copies it off a row. --exclude is the
// other way round.
//
// The same node is printed and graded: two lookups disagreed once, and --fail-under=100 passed a
// run whose report read 99.99.
func total(g gate, tree *prettycov.PathTree, want string, s Streams) error {
	node := tree

	if want != "" {
		// Get resolves a path spelled as the report draws it, including under the module root the
		// report collapsed away, so "pkg/logger" works as well as the profile's own spelling.
		if node = tree.Get(want); node == nil {
			// Quoted, as every message quoting something the reader typed is: a path can be
			// empty-looking or carry a control byte, and argv is where both arrive from.
			_, _ = fmt.Fprintf(s.Err, "total: no such package or file in the profile: %q\n", want)

			return exitError{code: ExitFailed}
		}
	}

	// A directory whose files declare no statements has no percentage. Through the gate, so this
	// reads the same for a node as for the tree.
	pct, ok := node.Percentage()
	if !ok {
		return g.refuse(fmt.Sprintf("total names nothing with statements to cover: %q", want), s)
	}

	_, _ = fmt.Fprintln(s.Out, pct)

	return g.grade(node, s)
}

// grade compares a node against the bar.
//
// AtLeast rather than comparing the ratio: at 100 they differ. A profile one statement short of
// complete divides to exactly 100 in float64 once the counts are large enough, so comparing ratios
// passed --fail-under=100 for a report that read 99.99.
func (g gate) grade(node *prettycov.PathTree, s Streams) error {
	bar, ok := g.want.Unwrap()
	if !ok {
		return nil
	}

	if !node.AtLeast(bar) {
		pct, _ := node.Percentage()

		// Percentage renders the figure, so this and the report cannot disagree. The threshold is
		// rounded instead: --fail-under=99.99999 reads back as 100.00%, which Percentage never
		// prints, and 79.999% against --fail-under=80 rounds both sides to 80.00.
		_, _ = fmt.Fprintf(s.Err, "total coverage %s%% is below %.2f%%\n", pct, bar.Float())

		return exitError{code: ExitBelow}
	}

	return nil
}
