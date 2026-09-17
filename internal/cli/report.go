package cli

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/screwyprof/prettycov"
)

// A gate is --fail-under: the only thing turning what was measured into a status.
type gate struct{ want *prettycov.Threshold }

// treeOf measures and says on stderr what it found. The writers are here, not in Measure, because
// saying is the only thing that needs them.
func treeOf(req prettycov.Request, g gate, s Streams) (*prettycov.PathTree, error) {
	res, err := prettycov.Measure(req)
	if err != nil {
		_, _ = fmt.Fprintf(s.Err, "%v\n", err)

		// A first run is one command away, and file-not-found does not say which.
		if errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(s.Err, "run: go test -coverprofile=%s ./...\n", req.Profile)
		}

		return nil, exitError{code: ExitFailed}
	}

	// Before the failures below: a pattern that took everything is how a run ends up with nothing.
	reportExclusions(res.Exclusions, s)

	// The bool, not the Outcome, so a caller cannot read a nil tree without being told. Before the
	// switch, which is then only the ways a run comes back empty.
	if tree, ok := res.Tree(); ok {
		return tree, nil
	}

	// One switch over every outcome, so exhaustive asks when a fifth is added.
	switch res.Outcome() {
	// Refused, not merely said: it falls through to ExitFailed below. A rename transforms the
	// output, so one that did not happen leaves a report nobody asked for. --exclude is a filter,
	// where "drop this if it is here" is reasonable.
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

// cannotWrite reports a destination that would not take the report. Exit 2, never the gate's 1: what
// failed is writing it down, not the coverage. A closed pipe does not reach here, since Go raises SIGPIPE
// for stdout and stderr.
func cannotWrite(err error, s Streams) error {
	_, _ = fmt.Fprintf(s.Err, "cannot write the report: %v\n", err)

	return exitError{code: ExitFailed}
}

// refuse grades an absence. Exit 1 under a gate, not 2, which reads as "could not run". Without a
// gate an empty report would exit 0 and turn a CI check into a green no-op.
func (g gate) refuse(reason string, s Streams) error {
	if g.want != nil {
		_, _ = fmt.Fprintf(s.Err, "%s, wanted at least %.2f%%\n", reason, g.want.Float())

		return exitError{code: ExitBelow}
	}

	_, _ = fmt.Fprintln(s.Err, reason)

	return exitError{code: ExitFailed}
}

// total writes one percentage and nothing else. The path is spelled as the report prints it, since
// the tree is built from shortened paths; --exclude is the other way round. The same node is printed
// and graded. Two lookups disagreed once, and --fail-under=100 passed a report reading 99.99.
func total(g gate, tree *prettycov.PathTree, want string, s Streams) error {
	node := tree

	if want != "" {
		// Get also resolves under the collapsed root, so "pkg/logger" works.
		if node = tree.Get(want); node == nil {
			// Quoted: a path off argv can be empty-looking or carry a control byte.
			_, _ = fmt.Fprintf(s.Err, "total: no such package or file in the profile: %q\n", want)

			return exitError{code: ExitFailed}
		}
	}

	// No percentage without statements. Through the gate, so a node reads as the tree does.
	pct, ok := node.Percentage()
	if !ok {
		return g.refuse(fmt.Sprintf("total names nothing with statements to cover: %q", want), s)
	}

	// Checked, where the stderr messages above are not, because this is the answer and
	// `COVERAGE := $(shell prettycov total)` on a full disk took an empty string. Before grade, so a
	// write failure is exit 2 rather than exit 1 for a number nobody received.
	if _, err := fmt.Fprintln(s.Out, pct); err != nil {
		return cannotWrite(err, s)
	}

	return g.grade(node, s)
}

// grade compares a node against the bar. AtLeast rather than the ratio: at 100 they differ, since a
// profile one statement short divides to exactly 100 in float64 once the counts are large enough.
func (g gate) grade(node *prettycov.PathTree, s Streams) error {
	if g.want == nil {
		return nil
	}

	if !node.AtLeast(*g.want) {
		pct, _ := node.Percentage()

		// Percentage renders the figure, so this and the report cannot disagree. The threshold is
		// rounded: --fail-under=99.99999 would read back as a 100.00% Percentage never prints.
		_, _ = fmt.Fprintf(s.Err, "total coverage %s%% is below %.2f%%\n", pct, g.want.Float())

		return exitError{code: ExitBelow}
	}

	return nil
}
