package prettycov_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// The properties in crosscheck_test.go are checked against a table of shapes someone thought of.
// These are the same properties against shapes nobody did.
//
// That distinction has already cost us once: sorting rows by their label stopped being a total
// order the moment a merged row could take a sibling's name, and every hand-written case missed it
// because the shape it needs — a file with no directory of its own, named for a directory beside
// it — is one no `go test` run produces. Three hundred made-up profiles find it in seconds.
//
// Seeded, so a failure reproduces: the subtest name is the seed, and the profile is logged.
const (
	propertyRuns = 300
	propertySeed = 0x9E3779B97F4A7C15
)

// TestTreePropertiesHoldForAnyProfile checks what a report claims against what the profile said,
// for profiles made up on the spot. The assertions are crosscheck_test.go's, which never cared
// where the files came from, plus the three that only a generator makes worth stating.
func TestTreePropertiesHoldForAnyProfile(t *testing.T) {
	t.Parallel()

	rnd := rand.New(rand.NewPCG(propertySeed, 0))

	for run := range propertyRuns {
		files := randomProfile(rnd)

		t.Run(fmt.Sprintf("profile-%d", run), func(t *testing.T) {
			t.Parallel()

			// Printed only when the test fails, which is when the shape is the whole question.
			t.Log(profilePaths(files))

			tree := prettycov.Process(files, "", "")
			totals := nodeTotals(files)

			for _, withFiles := range []bool{false, true} {
				for _, depth := range []prettycov.Depth{0, 1, 2, 3, prettycov.DepthAll} {
					assertRowsMatchTheProfile(t, tree, totals,
						prettycov.Options{Depth: depth, Files: withFiles, Counts: true})
				}

				assertRowsHoldEveryStatement(t, tree, files, withFiles)
				assertDepthOnlyAddsRows(t, tree, withFiles)
				assertTopRowsSumToTheTotal(t, tree, withFiles)
				assertRenderIsDeterministic(t, tree, withFiles)
			}
		})
	}
}

// assertDepthOnlyAddsRows checks that raising -depth adds rows below and leaves every row already
// drawn exactly as it was — same label, same glyphs, same numbers. Stated in prose on the merge
// tests, and the reason a one-file package merges at every depth rather than only where its file
// would have been drawn: a row's label is a property of its node, not of where the cut falls.
//
// Rows are compared whole, so a merge that renamed a row only at some depths fails here even if
// the arithmetic still balances.
func assertDepthOnlyAddsRows(t *testing.T, tree *prettycov.PathTree, withFiles bool) {
	t.Helper()

	// Past the deepest shape randomProfile makes, so the last cut is always the whole tree.
	for cut := range prettycov.Depth(6) {
		shallow := prettycov.Rows(tree, prettycov.Options{Depth: cut, Files: withFiles})
		deeper := prettycov.Rows(tree, prettycov.Options{Depth: cut + 1, Files: withFiles})

		kept := make([]prettycov.Row, 0, len(shallow))

		for _, row := range deeper {
			if row.Level <= int(cut) {
				kept = append(kept, row)
			}
		}

		assert.Equalf(t, shallow, kept, "-depth=%d against the rows -depth=%d draws at that level",
			cut, cut+1)
	}
}

// assertTopRowsSumToTheTotal checks the report against the one number most people read. -total
// prints the tree's own coverage without drawing anything, so nothing else in the report is in a
// position to disagree with it — except the rows at the top, which are every statement there is.
func assertTopRowsSumToTheTotal(t *testing.T, tree *prettycov.PathTree, withFiles bool) {
	t.Helper()

	var top prettycov.CoverageStats

	for _, row := range prettycov.Rows(tree, prettycov.Options{Depth: 0, Files: withFiles}) {
		top.Covered += row.Coverage.Covered
		top.Uncovered += row.Coverage.Uncovered
	}

	assert.Equalf(t, tree.Coverage, top, "the top rows against what -total prints, files=%v", withFiles)
}

// assertRenderIsDeterministic renders the same tree repeatedly. Map order is randomised per range
// statement, so a sort that is not a total order comes out differently between runs — and this
// output gets diffed. TestDisplayTreeIsDeterministic pins the two shapes that have gone wrong;
// this asks the same of shapes nobody has thought about.
func assertRenderIsDeterministic(t *testing.T, tree *prettycov.PathTree, withFiles bool) {
	t.Helper()

	opts := prettycov.Options{Depth: prettycov.DepthAll, Files: withFiles, Counts: true}
	first := renderOpts(t, tree, opts)

	for range 20 {
		assert.Equal(t, first, renderOpts(t, tree, opts), "the same tree rendered twice")
	}
}

// randomProfile makes up a profile. Every component is drawn from four names, two of which look
// like files, so the collisions live in almost every profile rather than in the one case someone
// remembered: a name used as both a file and a directory, a file with no directory of its own, a
// package whose whole content is one file. Real profiles reach these through a merge of two runs
// or an -old/-new rewrite.
//
// Duplicates are left in. x/tools merges a repeated filename before we see it, so only a caller
// assembling its own slice gets one — which is the case PathTree.add accumulates for.
func randomProfile(rnd *rand.Rand) []prettycov.FileCoverage {
	names := []string{"a", "b", "a.go", "b.go"}

	files := make([]prettycov.FileCoverage, rnd.IntN(8)+1)

	for i := range files {
		parts := make([]string, rnd.IntN(3)+1)
		for j := range parts {
			parts[j] = names[rnd.IntN(len(names))]
		}

		// A leading empty component is an absolute path, which is what a CI checkout writes.
		if rnd.IntN(6) == 0 {
			parts = append([]string{""}, parts...)
		}

		// Zero statements is an ordinary file: a doc.go declaring none.
		files[i] = file(strings.Join(parts, "/"), rnd.IntN(4), rnd.IntN(4))
	}

	return files
}

// profilePaths is the generated profile in one line, for the log a failing subtest prints.
func profilePaths(files []prettycov.FileCoverage) string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = fmt.Sprintf("%s:%d/%d", f.File, f.Coverage.Uncovered, f.Coverage.Total())
	}

	return strings.Join(paths, " ")
}

// TestPercentagePropertiesHoldForAnyCounts checks the rendered percentage against the counts it
// came from, for counts no profile should hold as well as ones it should. Overflow is not
// hypothetical: a profile declaring blocks of billions of statements once printed
// "-461168601842738790400.00", and another read 100.00 with statements left uncovered.
func TestPercentagePropertiesHoldForAnyCounts(t *testing.T) {
	t.Parallel()

	rnd := rand.New(rand.NewPCG(propertySeed, 1))

	for range propertyRuns {
		stats := prettycov.CoverageStats{Covered: drawCount(rnd), Uncovered: drawCount(rnd)}

		pct, ok := stats.Percentage()
		if !ok {
			// Nothing to report is not 0%, and the zero Percentage renders as one.
			assert.Equalf(t, prettycov.Percentage{}, pct, "refused %d covered of %d",
				stats.Covered, stats.Total())

			continue
		}

		text := pct.String()

		require.Truef(t, pct.Float() >= 0 && pct.Float() <= 100,
			"%d covered of %d is %v%%", stats.Covered, stats.Total(), pct.Float())

		value, err := strconv.ParseFloat(text, 64)
		require.NoErrorf(t, err, "%d covered of %d rendered as %q", stats.Covered, stats.Total(), text)

		// Two decimals, so the text is within half a place of the number it stands for — except at
		// the very top, where the cap spends a whole one. It is a round down, not a round to
		// nearest: 73999 of 74000 statements is 99.9986%, which renders "99.99" rather than the
		// "100.00" that stops someone writing another test.
		tolerance := 0.005
		if stats.Uncovered > 0 && text == "99.99" {
			tolerance = 0.01 + 1e-9 // the whole place, and float64's own error at these magnitudes
		}

		assert.InDeltaf(t, pct.Float(), value, tolerance, "%q against %v", text, pct.Float())

		if text == "100.00" {
			assert.Zerof(t, stats.Uncovered, "%q with statements left uncovered", text)
		}
	}
}

// drawCount draws a statement count, including the counts no profile holds: negative, and large
// enough that two of them overflow.
func drawCount(rnd *rand.Rand) int {
	switch rnd.IntN(4) {
	case 0:
		return 0
	case 1:
		return math.MaxInt - rnd.IntN(1000)
	case 2:
		return -rnd.IntN(1000)
	default:
		return rnd.IntN(1000)
	}
}
