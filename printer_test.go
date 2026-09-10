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
		// Not control characters — these are category Cf, so unicode.IsControl says no — but a
		// terminal obeys them just the same and reverses everything after them, which is how a
		// file gets drawn under a name it does not have. Written as escapes rather than as the
		// characters themselves, which is what gosec's G116 asks of Go source for this very reason.
		{name: "right-to-left override", pkg: "m/\u202egps.go"},
		{name: "right-to-left isolate", pkg: "m/\u2067gps.go"},
		// Not a terminal spoof, but the report is read a line at a time and these end one for a
		// log viewer or a JSON consumer, exactly as the carriage return above does for a terminal.
		{name: "line separator", pkg: "m/z\u2028y.go"},
		{name: "paragraph separator", pkg: "m/p\u2029q.go"},
		// Draws as nothing, so two labels differing only by one look identical.
		{name: "zero-width no-break space", pkg: "m/w\ufeffv.go"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := prettycov.Process([]prettycov.FileCoverage{file(tc.pkg+"/a.go", 1, 1)}, "", "")

			out := render(t, tree, 3)

			assert.NotContains(t, out, "\x1b", "escape reached the terminal")
			assert.NotContains(t, out, "\r")
			assert.NotContains(t, out, "\a")
			assert.NotContains(t, out, "\u202e", "bidi override reached the terminal")
			assert.NotContains(t, out, "\u2067")
			assert.NotContains(t, out, "\u2028", "line separator reached the report")
			assert.NotContains(t, out, "\u2029")
			assert.NotContains(t, out, "\ufeff")
		})
	}
}

// The joiners share a category with the bidi controls and are how several scripts spell ordinary
// words, so they are drawn rather than replaced. Blanking every Cf rune would mangle a real path.
func TestDisplayTreeKeepsZeroWidthJoiners(t *testing.T) {
	t.Parallel()

	// U+200D between the Devanagari letters is part of the spelling, not a control.
	joined := "\u0915\u094d\u200d\u0937"

	tree := prettycov.Process([]prettycov.FileCoverage{file("m/"+joined+"/a.go", 1, 1)}, "", "")

	assert.Contains(t, render(t, tree, 2), joined)
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
			// A doc.go holding only a package comment has no statements. Both cases take the
			// same path now that holding a file is what makes a directory a package — this one
			// is here so that inferring it from Coverage again would have to delete a test.
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

	// Two files in each subpackage, so neither merges into one of them and the order stays the
	// subject.
	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/x/service.go", 1, 1),
		file("m/x/subscriber.go", 1, 1),
		file("m/x/config/c.go", 1, 1),
		file("m/x/config/d.go", 1, 1),
		file("m/x/store/s.go", 1, 1),
		file("m/x/store/t.go", 1, 1),
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
		"with the files hidden only the directory is drawn, and it keeps its subtree")

	// Sorted by the label each row ends up with, so the file comes first: "a.go" < "a.go/b.go".
	// Sorting by the name they started as would put the directory first, since both begin as
	// "a.go" and directories are gathered before files.
	assert.Equal(t, []string{"m", "a.go", "a.go/b.go"},
		namesWith(t, tree, prettycov.Options{Depth: prettycov.DepthAll, Files: true}))

	// Two nodes, so two rows, and m is exactly the sum of them: the file's 5 and the directory's
	// 7. The directory holds one file and nothing else, so it merges into it and the two rows end
	// up telling apart by more than the number.
	out := renderOpts(t, tree, prettycov.Options{Depth: prettycov.DepthAll, Counts: true, Files: true})

	assert.Contains(t, out, "m - 41.67  7/12 uncovered\n")
	assert.Contains(t, out, "a.go - 100.00  0/5 uncovered\n", "the file")
	assert.Contains(t, out, "a.go/b.go - 0.00  7/7 uncovered\n", "the directory of the same name")
}

// Sorting by label separates a merged package from a file beside it, but a directory holding two
// files does not merge and keeps a name a file can also have. Nothing about the report requires one
// order over the other; it requires the same one every run, which the stable sort gives only
// because directories are gathered before files. Reversing those two loops is a plausible tidy-up
// and would change every report holding such a pair.
func TestDisplayTreeOrdersATieBetweenAFileAndADirectory(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/a.go", 5, 0),
		file("m/a.go/b.go", 0, 4),
		file("m/a.go/c.go", 0, 3),
	}, "", "")

	assert.Equal(t, []string{"m", "a.go", "b.go", "c.go", "a.go"},
		namesWith(t, tree, prettycov.Options{Depth: prettycov.DepthAll, Files: true}),
		"the directory and its files first, then the file of the same name")
}

// A file is one level below the package holding it, exactly as a subdirectory is — -depth counts
// levels the way tree -L does, and a file is one of a directory's entries like any other.
func TestDisplayTreeFilesCountAsALevel(t *testing.T) {
	t.Parallel()

	// Two files under sub, so it is never merged into one of them and the levels stay the subject.
	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/x/own.go", 1, 1),
		file("m/x/sub/s.go", 1, 1),
		file("m/x/sub/t.go", 1, 1),
	}, "", "")

	assert.Equal(t, []string{"m/x"}, namesWith(t, tree, prettycov.Options{Depth: 0, Files: true}))
	assert.Equal(t, []string{"m/x", "own.go", "sub"}, namesWith(t, tree, prettycov.Options{Depth: 1, Files: true}))
	assert.Equal(t, []string{"m/x", "own.go", "sub", "s.go", "t.go"},
		namesWith(t, tree, prettycov.Options{Depth: 2, Files: true}))
}

// A package whose whole content is one file says the same number twice, so the two rows become
// one and the label names both. A row's label is a property of the node, not of where the depth
// cut falls: raising -depth adds rows below, it does not rename the ones already drawn.
func TestDisplayTreeMergesAPackageThatIsOneFile(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/one/only.go", 3, 1),
		file("m/two/a.go", 1, 1),
		file("m/two/b.go", 1, 1),
	}, "", "")

	assert.Equal(t, []string{"m", "one/only.go", "two"},
		namesWith(t, tree, prettycov.Options{Depth: 1, Files: true}),
		"two has two files to keep apart, so only one merges")
	assert.Equal(t, []string{"m", "one/only.go", "two", "a.go", "b.go"},
		namesWith(t, tree, prettycov.Options{Depth: 2, Files: true}),
		"and a deeper cut adds rows without renaming one/only.go")

	// The numbers are what makes it a duplicate, and the merged row keeps them.
	out := renderOpts(t, tree, prettycov.Options{Depth: prettycov.DepthAll, Counts: true, Files: true})
	assert.Contains(t, out, "one/only.go - 75.00  1/4 uncovered\n")
	assert.NotContains(t, out, " one - ", "the package row it replaced is gone")

	// Without -files there is no file row to merge with, so the package keeps its own name.
	assert.Equal(t, []string{"m", "one", "two"}, nodeNames(t, tree, prettycov.DepthAll))
}

// The top row merges too, so a repository that is one package of one file reports a file path and
// no row names the package. Deliberate: -files asked for the files, the label still carries the
// whole package path, and refusing to merge at the top would be a rule about where a row sits
// rather than about what it holds. The default view is untouched, and -total reads the tree.
func TestDisplayTreeMergesTheTopRowToo(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{file("github.com/o/tool/main.go", 8, 1)}, "", "")

	assert.Equal(t, []string{"github.com/o/tool/main.go"},
		namesWith(t, tree, prettycov.Options{Depth: prettycov.DepthAll, Files: true}))
	assert.Equal(t, []string{"github.com/o/tool"}, nodeNames(t, tree, prettycov.DepthAll))
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
