package prettycov_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// withBlocks lives in exclude_test.go, which needed the same fixture first.
//
// uncovered builds one unrun block of the profile, the way cmd/cover writes it: a start position,
// an end line, and the statements between them. Distinct from exclude_test.go's block, which takes
// a covered count and no end — that one predates Misses needing to know where a block stops.
func uncovered(line, col, endLine, statements int) prettycov.Block {
	return prettycov.Block{
		Line: line, Col: col, EndLine: endLine,
		Coverage: prettycov.CoverageStats{Uncovered: statements},
	}
}

// Blocks that abut fold into one region, and the fold is decided by where the last one ended rather
// than where it began — a block running 44 to 51 reaches one opening on 52.
func TestMissesMergesAbuttingBlocks(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		withBlocks("m/pkg/a.go",
			uncovered(44, 35, 51, 2), // runs to 51, so it reaches the next
			uncovered(52, 16, 54, 1),
			uncovered(80, 16, 82, 1), // a gap, so its own region
		),
	})

	got := prettycov.Misses(tree, prettycov.Options{Depth: prettycov.DepthAll})

	assert.Equal(t, []prettycov.Miss{
		{File: "m/pkg/a.go", Line: 44, Col: 35, EndLine: 54, Statements: 3},
		{File: "m/pkg/a.go", Line: 80, Col: 16, EndLine: 82, Statements: 1},
	}, got, "the first two blocks are one region, the third is its own")
}

// A block cmd/cover emits with no statements in it is not a miss: there is nothing there to cover.
func TestMissesSkipsBlocksWithNoStatements(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		withBlocks("m/a.go", uncovered(10, 2, 12, 0), uncovered(20, 2, 22, 1)),
	})

	got := prettycov.Misses(tree, prettycov.Options{Depth: prettycov.DepthAll})

	require.Len(t, got, 1)
	assert.Equal(t, 20, got[0].Line)
}

// A covered block is not a miss either, and it does not join the regions on either side of it.
func TestMissesLeavesCoveredBlocksOut(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{{
		File:     "m/a.go",
		Coverage: prettycov.CoverageStats{Covered: 5, Uncovered: 2},
		Blocks: []prettycov.Block{
			uncovered(10, 2, 11, 1),
			{Line: 12, Col: 2, EndLine: 13, Coverage: prettycov.CoverageStats{Covered: 5}},
			uncovered(14, 2, 15, 1),
		},
	}})

	got := prettycov.Misses(tree, prettycov.Options{Depth: prettycov.DepthAll})

	assert.Len(t, got, 2, "the covered block between them is not a bridge")
}

// -depth chooses which packages are visited, so it chooses which misses are listed. A miss inside a
// package the report does not draw belongs to something the reader did not ask to see; raising the
// depth reveals it, the same gesture as with the tree.
func TestMissesFollowTheDepth(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		withBlocks("m/own.go", uncovered(5, 2, 6, 1)),
		withBlocks("m/deep/a.go", uncovered(9, 2, 10, 1)),
		withBlocks("m/deep/deeper/b.go", uncovered(13, 2, 14, 1)),
	})

	paths := func(d prettycov.Depth) []string {
		found := prettycov.Misses(tree, prettycov.Options{Depth: d})

		out := make([]string, 0, len(found))
		for _, m := range found {
			out = append(out, m.File)
		}

		return out
	}

	assert.Equal(t, []string{"m/own.go"}, paths(0), "only the top row's own files")
	assert.Equal(t, []string{"m/deep/a.go", "m/own.go"}, paths(1))
	assert.Equal(t, []string{"m/deep/a.go", "m/deep/deeper/b.go", "m/own.go"}, paths(prettycov.DepthAll))
}

// -hide-covered leaves out subtrees already at the bar, so it leaves out their misses too — which is
// the point of it: do not show me work in what is already done.
func TestMissesFollowHideCovered(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		{
			File: "m/nearly/a.go", Coverage: prettycov.CoverageStats{Covered: 19, Uncovered: 1},
			Blocks: []prettycov.Block{
				{Line: 3, Col: 2, EndLine: 4, Coverage: prettycov.CoverageStats{Covered: 19}},
				uncovered(9, 2, 10, 1),
			},
		},
		withBlocks("m/bad/b.go", uncovered(5, 2, 6, 4)),
	})

	at := func(pct float64) *float64 { return &pct }

	all := prettycov.Misses(tree, prettycov.Options{Depth: prettycov.DepthAll})
	require.Len(t, all, 2)

	// nearly/ is 95%, so a bar of 90 takes it and the miss inside it.
	focused := prettycov.Misses(tree, prettycov.Options{Depth: prettycov.DepthAll, HideCovered: at(90)})
	require.Len(t, focused, 1)
	assert.Equal(t, "m/bad/b.go", focused[0].File)
}

// The list is sorted by position, not by map order, or it would differ between runs and a consumer
// would have to sort it back. A file whose own blocks arrive out of order is sorted before merging,
// or a block listed early would fold into a region it does not touch.
func TestMissesAreSortedByPosition(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		withBlocks("m/z.go", uncovered(9, 2, 10, 1)),
		withBlocks("m/a.go", uncovered(40, 2, 41, 1), uncovered(9, 2, 10, 1)),
	})

	found := prettycov.Misses(tree, prettycov.Options{Depth: prettycov.DepthAll})

	got := make([]string, 0, len(found))
	for _, m := range found {
		got = append(got, m.File)
	}

	assert.Equal(t, []string{"m/a.go", "m/a.go", "m/z.go"}, got,
		"a.go's two blocks are 30 lines apart, so they stay two regions despite arriving reversed")
}

// `file:line:col: message`, as go vet prints it. The message is not decoration: without one an
// editor's error format cannot match and falls back to file:line:message, reading the column as the
// text — `a.go:9:2` opens line 9 at column 1 and the column is lost.
func TestDisplayMissesWritesOnePositionPerLine(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		withBlocks("m/a.go", uncovered(9, 2, 11, 3)),
		withBlocks("m/b.go", uncovered(40, 16, 40, 1)),
	})

	var buf bytes.Buffer

	n := prettycov.DisplayMisses(&buf, tree, prettycov.Options{Depth: prettycov.DepthAll})

	assert.Equal(t, "m/a.go:9:2: 3 uncovered\nm/b.go:40:16: 1 uncovered\n", buf.String())
	assert.Equal(t, 2, n, "the count is what tells a caller the list was empty")
}
