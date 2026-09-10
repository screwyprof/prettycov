package prettycov_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// A package with no statements has no percentage to report, so it renders as "n/a". It used to
// render as the literal "NaN", which is what 0/0 gives in float division.
func TestDisplayTreeRendersBothRatioBranches(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/empty/doc.go", 0, 0),
		file("m/real/a.go", 3, 1),
	}, "", "")

	out := render(t, tree, 2)

	assert.Contains(t, out, "empty - n/a")
	assert.Contains(t, out, "real - 75.00")
	assert.NotContains(t, out, "NaN")
}

// Paths reach the terminal, so a control character in one must not. Cursor movement is the case
// that goes past garbled output: "\x1b[1A\x1b[2K" erases the row above and writes over it, and
// above the first child is the total.
func TestDisplayTreeNeutralisesEscapesFromTheProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pkg  string
	}{
		{name: "cursor up and erase", pkg: "m/\x1b[1A\x1b[2Kforged"},
		{name: "colour", pkg: "m/\x1b[31mred"},
		{name: "carriage return", pkg: "m/\roverwritten"},
		{name: "bell", pkg: "m/\anoisy"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := prettycov.Process([]prettycov.FileCoverage{file(tc.pkg+"/a.go", 1, 1)}, "", "")

			out := render(t, tree, 3)

			assert.NotContains(t, out, "\x1b", "escape reached the terminal")
			assert.NotContains(t, out, "\r")
			assert.NotContains(t, out, "\a")
		})
	}
}

// Only control characters are touched. A path is allowed to be non-ASCII.
func TestDisplayTreeKeepsPrintableUnicode(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{file("m/héllo-世界/a.go", 1, 1)}, "", "")

	assert.Contains(t, render(t, tree, 2), "héllo-世界")
}

// A nil tree is nothing to draw, not a crash. Nothing in the CLI passes one, so only a caller of
// the library would find this — gobco reported the condition as never once true.
func TestRowsHandlesANilTree(t *testing.T) {
	t.Parallel()

	assert.Empty(t, prettycov.Rows(nil, prettycov.Options{Depth: 3}))
	assert.Empty(t, render(t, nil, 3))
}

// Coverage output gets diffed between CI runs, so the same tree must render byte-identically
// every time. Ranging over a map does not give that.
func TestDisplayTreeIsDeterministic(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process(printerFiles(), "", "")
	first := render(t, tree, 4)

	for range 50 {
		assert.Equal(t, first, render(t, tree, 4))
	}
}

func TestDisplayTreeSortsChildren(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process(printerFiles(), "", "")

	assert.Equal(t, []string{"m", "alpha/deep", "beta", "gamma"}, nodeNames(t, tree, 1))
}

// A run of directories that each hold nothing but the next one is one row, not one row each.
// Without this the default view of any real module is three wasted levels of import path:
// "github.com" then "screwyprof" then "delegator", each repeating the same percentage.
func TestDisplayTreeCollapsesPassThroughDirs(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("github.com/o/repo/pkg/a.go", 3, 1),
		file("github.com/o/repo/web/b.go", 1, 1),
	}, "", "")

	assert.Equal(t, []string{"github.com/o/repo", "pkg", "web"}, nodeNames(t, tree, 1))
}

// A directory that is a package in its own right keeps its own row even with a single child,
// otherwise its coverage disappears into the child's label.
func TestDisplayTreeKeepsDirsThatAreAlsoPackages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files []prettycov.FileCoverage
	}{
		{
			name: "own file with statements",
			files: []prettycov.FileCoverage{
				file("m/x/own.go", 4, 0),
				file("m/x/sub/s.go", 0, 4),
			},
		},
		{
			// A doc.go holding only a package comment has no statements, so m/x's totals equal
			// its child's. It is still a package and still gets a row.
			name: "own file with no statements",
			files: []prettycov.FileCoverage{
				file("m/x/doc.go", 0, 0),
				file("m/x/sub/s.go", 3, 1),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := prettycov.Process(tc.files, "", "")

			assert.Equal(t, []string{"m/x", "sub"}, nodeNames(t, tree, 1))
		})
	}
}

// The files are the leaves, so a package's own files sit beside its subpackages and every parent
// is the sum of what is drawn below it. That is the property the report can be checked by, and it
// is why the files are real rows rather than one row standing in for them.
func TestDisplayTreeFilesAreLeavesThatSumToTheirPackage(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/x/own.go", 4, 0),
		file("m/x/more.go", 2, 0),
		file("m/x/sub/s.go", 0, 4),
	}, "", "")

	out := renderOpts(t, tree, prettycov.Options{Depth: prettycov.DepthAll, Counts: true, Files: true})

	// 2 + 4 + 4 = 10, and the three rows below m/x are every statement it holds.
	assert.Contains(t, out, "m/x - 60.00  4/10 uncovered\n")
	assert.Contains(t, out, "more.go - 100.00  0/2 uncovered\n")
	assert.Contains(t, out, "own.go - 100.00  0/4 uncovered\n")
	assert.Contains(t, out, "s.go - 0.00  4/4 uncovered\n")
}

// Files sort among the packages beside them rather than before or after them, as ls, tree and
// du -a list a directory's entries. The extension is what tells them apart.
func TestDisplayTreeFilesSortAmongPackages(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/x/service.go", 1, 1),
		file("m/x/subscriber.go", 1, 1),
		file("m/x/config/c.go", 1, 1),
		file("m/x/store/s.go", 1, 1),
	}, "", "")

	assert.Equal(t, []string{"m/x", "config", "service.go", "store", "subscriber.go"},
		namesWith(t, tree, prettycov.Options{Depth: 1, Files: true}))
}

// Without -files the tree is exactly what it was: packages only, and a package holding nothing but
// files is a leaf. The file nodes are still there, so this is the check that they cost no row and
// no level when they are not asked for.
func TestDisplayTreeHidesFilesUnlessAsked(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/x/own.go", 4, 0),
		file("m/x/sub/s.go", 0, 4),
	}, "", "")

	out := renderOpts(t, tree, prettycov.Options{Depth: prettycov.DepthAll})

	assert.NotContains(t, out, ".go")
	assert.Equal(t, []string{"m/x", "sub"}, namesWith(t, tree, prettycov.Options{Depth: prettycov.DepthAll}))
}

// One name can be both a file and a directory: "m/a.go" beside "m/a.go/b.go" names a file and a
// package called the same thing. No filesystem allows it, so no single `go test` run produces it,
// but merging two profiles or rewriting a root with -old/-new can. Such a node is drawn as the
// directory it also is — hiding it as a file took its whole subtree with it while every ancestor
// went on counting the statements.
func TestDisplayTreeKeepsANameThatIsBothAFileAndADirectory(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/a.go", 5, 0),
		file("m/a.go/b.go", 0, 7),
	}, "", "")

	assert.Equal(t, []string{"m", "a.go"}, nodeNames(t, tree, prettycov.DepthAll),
		"the subtree survives with the files hidden")

	// One row for the two things sharing the name, carrying both: 5 covered in the file and 7
	// uncovered in the package. The file's own statements therefore get no row of their own even
	// with -files, which is the one place a parent is more than what is drawn beneath it — a name
	// cannot be two rows, and no profile a single `go test` run produces asks it to be.
	out := renderOpts(t, tree, prettycov.Options{Depth: prettycov.DepthAll, Counts: true, Files: true})

	assert.Contains(t, out, "a.go - 41.67  7/12 uncovered\n")
	assert.Contains(t, out, "b.go - 0.00  7/7 uncovered\n")

	// Get is how a library caller reaches it, and skipping IsFile nodes to enumerate packages must
	// not drop the packages underneath.
	assert.False(t, tree.Get("m/a.go").IsFile(), "it is also a directory")
}

// The same collision one level deeper, where the shared name holds a package rather than a file.
// Only the immediate parent of a file is marked as a package, so a.go here is a file with a child
// and no package of its own — which collapse used to fold away, leaving m claiming twelve
// statements above a single row reporting seven.
func TestDisplayTreeDoesNotCollapseAwayAFileWithASubtree(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/a.go", 5, 0),
		file("m/a.go/sub/b.go", 0, 7),
	}, "", "")

	assert.Equal(t, []string{"m", "a.go", "sub"}, nodeNames(t, tree, prettycov.DepthAll))

	out := renderOpts(t, tree, prettycov.Options{Depth: prettycov.DepthAll, Counts: true})

	assert.Contains(t, out, "m - 41.67  7/12 uncovered\n")
	assert.Contains(t, out, "a.go - 41.67  7/12 uncovered\n")
	assert.Contains(t, out, "sub - 0.00  7/7 uncovered\n")
}

// A file is one level below the package holding it, exactly as a subdirectory is — -depth counts
// levels the way tree -L does, and a file is one of a directory's entries like any other.
func TestDisplayTreeFilesCountAsALevel(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/x/own.go", 1, 1),
		file("m/x/sub/s.go", 1, 1),
	}, "", "")

	assert.Equal(t, []string{"m/x"}, namesWith(t, tree, prettycov.Options{Depth: 0, Files: true}))
	assert.Equal(t, []string{"m/x", "own.go", "sub"}, namesWith(t, tree, prettycov.Options{Depth: 1, Files: true}))
	assert.Equal(t, []string{"m/x", "own.go", "sub", "s.go"},
		namesWith(t, tree, prettycov.Options{Depth: 2, Files: true}))
}

// -depth counts levels below the root row, exactly as `tree -L` does: `tree -L 1` prints the root
// and one level under it. A collapsed run counts as the single row it renders as.
func TestDisplayTreeDepthCountsLevels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		depth prettycov.Depth
		want  []string
	}{
		{name: "root only", depth: 0, want: []string{"m"}},
		{name: "one level down", depth: 1, want: []string{"m", "alpha/deep", "beta", "gamma"}},
		{name: "beyond the tree", depth: 9, want: []string{"m", "alpha/deep", "beta", "gamma"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := prettycov.Process(printerFiles(), "", "")

			assert.Equal(t, tc.want, nodeNames(t, tree, tc.depth))
		})
	}
}

// Only the base ANSI colours, so the user's own theme decides what red and green look like.
// Anything from the 256-colour or truecolor range would name an exact shade and override it.
func TestDisplayTreeGradesByThreshold(t *testing.T) {
	t.Parallel()

	files := []prettycov.FileCoverage{
		file("m/bad/a.go", 1, 9),    // 10%  -> red
		file("m/edge/e.go", 5, 5),   // 50%  -> yellow, the boundary belongs to the upper band
		file("m/mid/b.go", 6, 4),    // 60%  -> yellow
		file("m/ok/o.go", 8, 2),     // 80%  -> green, likewise
		file("m/good/c.go", 10, 0),  // 100% -> green
		file("m/none/doc.go", 0, 0), // nothing to grade
	}

	out := renderColor(t, prettycov.Process(files, "", ""), 1)

	assert.Contains(t, out, "\x1b[31m10.00\x1b[0m", "red below 50")
	assert.Contains(t, out, "\x1b[33m50.00\x1b[0m", "50 is yellow, not red")
	assert.Contains(t, out, "\x1b[33m60.00\x1b[0m", "yellow in between")
	assert.Contains(t, out, "\x1b[32m80.00\x1b[0m", "80 is green, not yellow")
	assert.Contains(t, out, "\x1b[32m100.00\x1b[0m", "green at the top")
	assert.Contains(t, out, "none - n/a", "nothing to cover is not a grade, so no colour")
	assert.NotContains(t, out, "\x1b[38;", "no 256-colour or truecolor: that overrides the theme")
	assert.NotContains(t, out, "\x1b[4", "no background colours")
}

// Uncovered over total: the percentage hides size, and uncovered is the number acted on.
func TestDisplayTreeCountsShowUncoveredOverTotal(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/big/a.go", 115, 5),
		file("m/small/b.go", 18, 3),
		file("m/none/doc.go", 0, 0),
	}, "", "")

	out := renderOpts(t, tree, prettycov.Options{Depth: 1, Counts: true})

	assert.Contains(t, out, "big - 95.83  5/120 uncovered\n")
	assert.Contains(t, out, "small - 85.71  3/21 uncovered\n")
	assert.Contains(t, out, "none - n/a\n", "nothing to cover is nothing to count")
	assert.NotContains(t, render(t, tree, 1), "uncovered", "off unless asked for")

	// The counts are not part of the grade, so they land after the reset.
	colored := renderOpts(t, tree, prettycov.Options{Depth: 1, Color: prettycov.ANSI, Counts: true})
	assert.Contains(t, colored, "\x1b[0m  5/120 uncovered\n")
}

// m/
//
//	├ alpha/deep/   (alpha holds only deep, so the two collapse into one row)
//	├ beta/
//	└ gamma/
func printerFiles() []prettycov.FileCoverage {
	return []prettycov.FileCoverage{
		file("m/gamma/g.go", 1, 1),
		file("m/alpha/deep/d.go", 1, 1),
		file("m/beta/b.go", 1, 1),
	}
}

// Colour is a property of the terminal, not of the tree, so it is off in these tests unless a
// test is specifically about it.
func render(t *testing.T, tree *prettycov.PathTree, depth prettycov.Depth) string {
	t.Helper()

	return renderOpts(t, tree, prettycov.Options{Depth: depth})
}

func renderColor(t *testing.T, tree *prettycov.PathTree, depth prettycov.Depth) string {
	t.Helper()

	return renderOpts(t, tree, prettycov.Options{Depth: depth, Color: prettycov.ANSI})
}

func renderOpts(t *testing.T, tree *prettycov.PathTree, opts prettycov.Options) string {
	t.Helper()

	var buf bytes.Buffer

	prettycov.DisplayTree(&buf, tree, opts)

	return buf.String()
}

// nodeNames is the labels a tree renders to, in order. Read off Rows rather than scraped back
// out of the rendered text, so a change to the glyphs cannot break a test about ordering.
func nodeNames(t *testing.T, tree *prettycov.PathTree, depth prettycov.Depth) []string {
	t.Helper()

	return namesWith(t, tree, prettycov.Options{Depth: depth})
}

func namesWith(t *testing.T, tree *prettycov.PathTree, opts prettycov.Options) []string {
	t.Helper()

	rows := prettycov.Rows(tree, opts)

	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Label)
	}

	return names
}

// Only a ratio that is exactly 100% may render as 100.00. Every other value still rounds to
// nearest, so the cap is confined to (99.995, 100).
func TestPercentageNeverClaimsFullCoverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stats prettycov.CoverageStats
		want  string
	}{
		{name: "everything covered", stats: prettycov.CoverageStats{Covered: 74000}, want: "100.00"},
		{
			// go tool cover -func rounds at one decimal and reports this as 100.0%.
			name:  "one statement short of 74000",
			stats: prettycov.CoverageStats{Covered: 73999, Uncovered: 1}, want: "99.99",
		},
		{
			name:  "far enough short to round there anyway",
			stats: prettycov.CoverageStats{Covered: 52919, Uncovered: 3}, want: "99.99",
		},
		{
			name:  "an ordinary value still rounds up",
			stats: prettycov.CoverageStats{Covered: 2, Uncovered: 1}, want: "66.67",
		},
		{name: "nothing covered", stats: prettycov.CoverageStats{Uncovered: 4}, want: "0.00"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pct, ok := tc.stats.Percentage()

			require.True(t, ok)
			assert.Equal(t, tc.want, pct.String())
		})
	}
}

// Nothing to cover is not 0%, and a caller has to be able to tell the two apart.
func TestPercentageReportsNothingToCover(t *testing.T) {
	t.Parallel()

	pct, ok := prettycov.CoverageStats{}.Percentage()

	assert.False(t, ok)
	assert.Zero(t, pct.Float())
}
