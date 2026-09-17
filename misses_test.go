package prettycov_test

import (
	"bytes"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// missOpts is how the CLI runs this mode: a miss is a file position, and a list of them is made of
// files, so the output includes them whatever --files says about the tree's.
func missOpts(depth prettycov.Depth) prettycov.Options {
	return prettycov.Options{Depth: depth, Files: true}
}

func missPaths(misses []prettycov.Miss) []string {
	out := make([]string, 0, len(misses))
	for _, m := range misses {
		out = append(out, m.File)
	}

	return out
}

// covered is uncovered's other half.
func covered(line, col, endLine, statements int) prettycov.Block {
	return prettycov.Block{
		Line: line, Col: col, EndLine: endLine,
		Coverage: prettycov.CoverageStats{Covered: statements},
	}
}

// uncovered builds one unrun block the way cmd/cover writes it: a start position, an end line, and
// the statements between them. Distinct from exclude_test.go's block, which takes a covered count
// and no end, that one predates Misses needing to know where a block stops.
func uncovered(line, col, endLine, statements int) prettycov.Block {
	return prettycov.Block{
		Line: line, Col: col, EndLine: endLine,
		Coverage: prettycov.CoverageStats{Uncovered: statements},
	}
}

// Blocks that abut fold into one region, and the fold is decided by where the last one ended rather
// than where it began. A block running 44 to 51 reaches one opening on 52.
func TestMissesMergesAbuttingBlocks(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		withBlocks("m/pkg/a.go",
			uncovered(44, 35, 51, 2), // runs to 51, so it reaches the next
			uncovered(52, 16, 54, 1),
			uncovered(80, 16, 82, 1), // a gap, so its own region
		),
	})

	got := prettycov.Misses(tree, missOpts(prettycov.DepthAll))

	assert.Equal(t, []prettycov.Miss{
		{File: "m/pkg/a.go", Line: 44, Col: 35, EndLine: 54, Statements: 3},
		{File: "m/pkg/a.go", Line: 80, Col: 16, EndLine: 82, Statements: 1},
	}, got, "the first two blocks are one region, the third is its own")
}

// A covered block is not a miss either, and it does not join the regions on either side of it. The
// blocks abut, so only the covered one between them keeps this two regions. A gap would have done
// it on its own and tested nothing.
//
// delegator's pgxstore/store.go is this shape: 44.35,46.3 unrun, 47.2,47.16 run, 47.16,49.3 unrun.
// Folding across gave a region of 44-49 that claimed the covered statement on 47, which a GitHub
// annotation or an LSP diagnostic would have marked with the rest.
func TestMissesLeavesCoveredBlocksOut(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{{
		File:     "m/a.go",
		Coverage: prettycov.CoverageStats{Covered: 5, Uncovered: 2},
		Blocks: []prettycov.Block{
			uncovered(10, 2, 11, 1),
			covered(11, 2, 12, 5),
			uncovered(12, 2, 13, 1), // abuts 10-11, and would fold but for the block between
		},
	}})

	got := prettycov.Misses(tree, missOpts(prettycov.DepthAll))

	assert.Equal(t, []prettycov.Miss{
		{File: "m/a.go", Line: 10, Col: 2, EndLine: 11, Statements: 1},
		{File: "m/a.go", Line: 12, Col: 2, EndLine: 13, Statements: 1},
	}, got, "the covered block between them is not a bridge")
}

// Block is exported and EndLine is new, so a caller assembling its own FileCoverage, the
// documented way to use Exclude leaves it zero. A region ending before it starts is the one field
// a range consumer reads, and GitHub rejects an annotation with end_line below start_line outright.
// Folding died for such a caller too: nothing starts at or before line 1.
func TestMissesFloorTheEndAtTheBlocksOwnLine(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{{
		File:     "m/a.go",
		Coverage: prettycov.CoverageStats{Uncovered: 2},
		Blocks: []prettycov.Block{
			{Line: 62, Col: 2, Coverage: prettycov.CoverageStats{Uncovered: 1}},
			{Line: 63, Col: 2, Coverage: prettycov.CoverageStats{Uncovered: 1}},
		},
	}})

	got := prettycov.Misses(tree, missOpts(prettycov.DepthAll))

	assert.Equal(t, []prettycov.Miss{
		{File: "m/a.go", Line: 62, Col: 2, EndLine: 63, Statements: 2},
	}, got, "a single-line region each, and they abut, so they fold")
}

// A block cmd/cover declares with no statements is neither covered nor unrun, so it does not
// separate the regions on either side of it. It emits one per case expression of a type switch.
// delegator has nine, at subscriber.go:76-83, and they land exactly where an untested switch's
// misses abut, so reading them as covered takes that switch's regions apart.
func TestMissesFoldAcrossABlockWithNoStatements(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{{
		File:     "m/a.go",
		Coverage: prettycov.CoverageStats{Uncovered: 2},
		Blocks: []prettycov.Block{
			uncovered(10, 2, 11, 1),
			// `76.47,76.47 0 5` in the profile: a count, and nothing for it to have counted.
			{Line: 11, Col: 3, EndLine: 12},
			uncovered(12, 2, 13, 1),
		},
	}})

	got := prettycov.Misses(tree, missOpts(prettycov.DepthAll))

	assert.Equal(t, []prettycov.Miss{
		{File: "m/a.go", Line: 10, Col: 2, EndLine: 13, Statements: 2},
	}, got, "nothing to reach between them, so they are one region")
}

// cover bounds no line number, so a profile can name one that leaves no room to add to. Asking
// whether the next block starts at or before the end, rather than whether the end plus one reaches
// it, is the same test without the wrap, which turned every later block into a region nested
// inside the first.
func TestMissesFoldPastTheLargestLineNumber(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{{
		File:     "m/a.go",
		Coverage: prettycov.CoverageStats{Uncovered: 3},
		Blocks: []prettycov.Block{
			uncovered(10, 2, math.MaxInt, 1),
			uncovered(20, 2, 21, 1),
			uncovered(30, 2, 31, 1),
		},
	}})

	got := prettycov.Misses(tree, missOpts(prettycov.DepthAll))

	assert.Equal(t, []prettycov.Miss{
		{File: "m/a.go", Line: 10, Col: 2, EndLine: math.MaxInt, Statements: 3},
	}, got, "the first block runs to the end of everything, so the rest are inside it")
}

// A covered block inside an open region does not close it. The region came from a single block
// spanning its whole range, so it already holds that covered statement. Closing here would leave
// the next block to open a second region nested in the first, and Misses only sorts, so nothing
// downstream unpicks that. Nesting is the fault; sharing the line between two regions is not, and
// TestMissesShareTheLineBetweenTwoRegions has the case cmd/cover actually emits.
func TestMissesDoNotOverlapWhenACoveredBlockSitsInsideARegion(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{{
		File:     "m/a.go",
		Coverage: prettycov.CoverageStats{Covered: 1, Uncovered: 2},
		Blocks: []prettycov.Block{
			uncovered(10, 2, 20, 1), // one block, spanning ten lines
			covered(12, 4, 14, 1),   // inside it, so the region already holds it
			uncovered(15, 2, 16, 1),
		},
	}})

	got := prettycov.Misses(tree, missOpts(prettycov.DepthAll))

	assert.Equal(t, []prettycov.Miss{
		{File: "m/a.go", Line: 10, Col: 2, EndLine: 20, Statements: 2},
	}, got, "one region; the alternative is 10-20 with 15-16 nested inside it")
}

// `} else if d {` is the end of one block and the start of the next on one line, because a block's
// end is the coordinate after it. So two regions meet on that line, and neither contains the other.
// Closing only past the end instead would fold them into one region spanning the covered condition
// between them, which is the fault this guards; meeting on a line is not.
//
// From a profile go test -cover produced for an if/else-if/else where only the last arm runs.
func TestMissesShareTheLineBetweenTwoRegions(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{{
		File:     "m/o.go",
		Coverage: prettycov.CoverageStats{Covered: 1, Uncovered: 4},
		Blocks: []prettycov.Block{
			uncovered(4, 7, 7, 2),   // 4.7,7.3  , ends on the line the next opens
			covered(7, 8, 7, 1),     // 7.8,7.14: the else-if condition, run
			uncovered(7, 14, 10, 2), // 7.14,10.3
		},
	}})

	got := prettycov.Misses(tree, missOpts(prettycov.DepthAll))

	assert.Equal(t, []prettycov.Miss{
		{File: "m/o.go", Line: 4, Col: 7, EndLine: 7, Statements: 2},
		{File: "m/o.go", Line: 7, Col: 14, EndLine: 10, Statements: 2},
	}, got, "two regions meeting on line 7, not one spanning the covered condition")
}

// cmd/cover nests blocks: a function's run block spans the branch blocks inside it, so a covered
// block is nearly always open when an uncovered one starts. Only one that begins between two
// uncovered regions separates them; one that began before the first is what encloses it.
func TestMissesFoldAcrossAnEnclosingCoveredBlock(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{{
		File:     "m/a.go",
		Coverage: prettycov.CoverageStats{Covered: 3, Uncovered: 2},
		Blocks: []prettycov.Block{
			covered(41, 69, 44, 3), // the enclosing run block, open before either miss
			uncovered(44, 35, 46, 1),
			uncovered(47, 2, 49, 1),
		},
	}})

	got := prettycov.Misses(tree, missOpts(prettycov.DepthAll))

	assert.Equal(t, []prettycov.Miss{
		{File: "m/a.go", Line: 44, Col: 35, EndLine: 49, Statements: 2},
	}, got, "a block that opened above them says nothing about what is between them")
}

// --depth chooses which packages are visited, so it chooses which misses are listed. A miss inside a
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
		return missPaths(prettycov.Misses(tree, missOpts(d)))
	}

	// A file is an entry of the package holding it, so it sits one level below that package. The
	// same level --files gives it, since that is the mode this always runs in.
	assert.Empty(t, paths(0), "the top row alone, and a file is a level below one")
	assert.Equal(t, []string{"m/own.go"}, paths(1))
	// deeper/ holds one file, so the two merge into a single row and b.go arrives at level 2 with
	// a.go rather than a level below it.
	assert.Equal(t, []string{"m/deep/a.go", "m/deep/deeper/b.go", "m/own.go"}, paths(2))
	assert.Equal(t, []string{"m/deep/a.go", "m/deep/deeper/b.go", "m/own.go"}, paths(prettycov.DepthAll))
}

// --hide-covered leaves out subtrees already at the bar, so it leaves out their misses too, which is
// the point of it: do not show me work in what is already done.
func TestMissesFollowHideCovered(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		{
			File: "m/nearly/a.go", Coverage: prettycov.CoverageStats{Covered: 19, Uncovered: 1},
			Blocks: []prettycov.Block{
				covered(3, 2, 4, 19),
				uncovered(9, 2, 10, 1),
			},
		},
		withBlocks("m/bad/b.go", uncovered(5, 2, 6, 4)),
	})

	all := prettycov.Misses(tree, missOpts(prettycov.DepthAll))
	require.Len(t, all, 2)

	// nearly/ is 95%, so a bar of 90 takes it and the miss inside it.
	bar := missOpts(prettycov.DepthAll)
	bar.HideCovered = new(prettycov.MustThreshold(90.0))

	focused := prettycov.Misses(tree, bar)
	require.Len(t, focused, 1)
	assert.Equal(t, "m/bad/b.go", focused[0].File)
}

// A file already at the bar has no misses worth listing, even when the package holding it is below
// the bar and so is visited. delegator's logger/ is 96.88 over a logger.go at 86.67 and a
// middleware.go at 98.77: at --hide-covered=90 the tree draws logger.go alone, and the misses have to
// agree: listing middleware.go's one uncovered statement contradicts the report beside it.
//
// The bar is asked of each file, not only of the package holding it.
func TestMissesSkipFilesAlreadyAtTheBar(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		// 86.67, below the bar, so its miss is listed.
		{
			File: "m/logger/logger.go", Coverage: prettycov.CoverageStats{Covered: 13, Uncovered: 2},
			Blocks: []prettycov.Block{
				covered(3, 2, 4, 13),
				uncovered(23, 3, 24, 2),
			},
		},
		// 98.77, above it, so its miss is not.
		{
			File: "m/logger/middleware.go", Coverage: prettycov.CoverageStats{Covered: 80, Uncovered: 1},
			Blocks: []prettycov.Block{
				covered(3, 2, 4, 80),
				uncovered(90, 2, 91, 1),
			},
		},
	})

	opts := missOpts(prettycov.DepthAll)
	opts.HideCovered = new(prettycov.MustThreshold(90.0))

	assert.Equal(t, []string{"m/logger/logger.go"}, missPaths(prettycov.Misses(tree, opts)))
}

// A package holding one file merges into a single row, and that row is the file, so the node the
// traversal hands over has the blocks on it rather than in a Files map below it. Emitting only what
// a node's Files hold lost those: delegator's `store/pgxstore/store.go` is drawn at 75.47 and its
// misses went unlisted, which contradicts the row beside them.
func TestMissesIncludeACollapsedFileRow(t *testing.T) {
	t.Parallel()

	// pgxstore/ holds one file, so with --files the two draw as one row.
	tree := prettycov.Process([]prettycov.FileCoverage{
		withBlocks("m/store/pgxstore/store.go", uncovered(44, 35, 45, 1)),
		withBlocks("m/other.go", uncovered(9, 2, 10, 1)),
	})

	got := missPaths(prettycov.Misses(tree, missOpts(prettycov.DepthAll)))

	assert.Equal(t, []string{"m/other.go", "m/store/pgxstore/store.go"}, got,
		"the merged row carries its own blocks; the bare file is listed by the package holding it")
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

	got := missPaths(prettycov.Misses(tree, missOpts(prettycov.DepthAll)))

	assert.Equal(t, []string{"m/a.go", "m/a.go", "m/z.go"}, got,
		"a.go's two blocks are 30 lines apart, so they stay two regions despite arriving reversed")
}

// A position goes to the same terminal a row does, so it is scrubbed the same way. Printing the
// profile's own spelling let a crafted one erase the miss above it -- \x1b[1A\x1b[2K is cursor up
// and erase line -- and made "real\revil/b.go" read as "evil/b.go". A coverage tool that can be
// made to drop a line of its own output is failing at the one thing it is for.
//
// The cost is that such a path no longer opens in an editor or matches as an --exclude pattern. No
// Go repository holds one: a module path cannot carry any of this set, so only a file name could.
func TestMissesScrubTheFileAsTheRowIs(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		withBlocks("m/\x1b[1A\x1b[2Kforged/a.go", uncovered(3, 2, 4, 1)),
		withBlocks("m/real\revil/b.go", uncovered(9, 2, 10, 1)),
	})

	var buf bytes.Buffer
	require.Equal(t, 2, displayMisses(t, &buf, tree, missOpts(prettycov.DepthAll)))

	// real\u2026 sorts first: the replacement is U+FFFD, which outranks every ASCII letter, so scrubbing
	// moves a row as well as redrawing it \u2014 the reason visible sorts on the label as drawn.
	assert.Equal(t,
		"m/real\ufffdevil/b.go:9:2: 1 uncovered\n"+
			"m/\ufffd[1A\ufffd[2Kforged/a.go:3:2: 1 uncovered\n",
		buf.String())

	// One spelling between the two printers, which is the point: a path the report draws one way and
	// a position names another is the disagreement that has bitten this pair twice already. A row
	// carries its own segment, so these are the halves of the paths above.
	assert.Equal(t,
		[]string{"m", "real\ufffdevil/b.go", "\ufffd[1A\ufffd[2Kforged/a.go"},
		namesWith(t, tree, missOpts(prettycov.DepthAll)))
}

// The joiners are not obeyed, so a path spelling a word in Persian or Devanagari survives both
// printers intact -- the scrubbed set is the one a terminal acts on, not everything invisible.
func TestMissesKeepAZeroWidthJoiner(t *testing.T) {
	t.Parallel()

	const name = "m/pkg/a\u200db.go"

	tree := prettycov.Process([]prettycov.FileCoverage{withBlocks(name, uncovered(3, 2, 4, 1))})

	var buf bytes.Buffer
	require.Equal(t, 1, displayMisses(t, &buf, tree, missOpts(prettycov.DepthAll)))

	assert.Equal(t, name+":3:2: 1 uncovered\n", buf.String())
}

// `file:line:col: message`, as go vet prints it. The message is not decoration: without one an
// editor's error format cannot match and falls back to file:line:message, reading the column as the
// text: `a.go:9:2` opens line 9 at column 1 and the column is lost.
// displayMisses is DisplayMisses where the destination cannot fail, which is every test writing to
// a bytes.Buffer. The error is the writer's, and a buffer has none.
func displayMisses(t *testing.T, w io.Writer, tree *prettycov.PathTree, opts prettycov.Options) int {
	t.Helper()

	listed, err := prettycov.DisplayMisses(w, tree, opts)
	require.NoError(t, err)

	return listed
}

func TestDisplayMissesWritesOnePositionPerLine(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		withBlocks("m/a.go", uncovered(9, 2, 11, 3)),
		withBlocks("m/b.go", uncovered(40, 16, 40, 1)),
	})

	var buf bytes.Buffer

	n, err := prettycov.DisplayMisses(&buf, tree, missOpts(prettycov.DepthAll))
	require.NoError(t, err)

	assert.Equal(t, "m/a.go:9:2: 3 uncovered\nm/b.go:40:16: 1 uncovered\n", buf.String())

	// Statements, not lines: two positions here and four statements between them. It is what a
	// caller weighs against the tree's own count to tell a whole list from a shallow one, and every
	// region holds at least one, so it is still zero exactly when nothing was written.
	assert.Equal(t, 4, n)
}

// Zero when there is nothing to list, which is the other thing the count is for: a command printing
// nothing reads as one that failed, and only this tells the caller it happened.
func TestDisplayMissesCountsNothingWhenFullyCovered(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{{
		File:     "m/a.go",
		Coverage: prettycov.CoverageStats{Covered: 3},
		Blocks:   []prettycov.Block{covered(9, 2, 11, 3)},
	}})

	var buf bytes.Buffer

	assert.Equal(t, 0, displayMisses(t, &buf, tree, missOpts(prettycov.DepthAll)))
	assert.Empty(t, buf.String())
}
