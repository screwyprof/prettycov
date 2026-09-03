package prettycov_test

import (
	"bytes"
	"os"
	"path/filepath"
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

// -depth counts levels below the root row, exactly as `tree -L` does: `tree -L 1` prints the root
// and one level under it. A collapsed run counts as the single row it renders as.
func TestDisplayTreeDepthCountsLevels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		depth uint
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

//nolint:paralleltest // t.Setenv cannot be combined with t.Parallel.
func TestDisplayTreeColorMode(t *testing.T) {
	tree := prettycov.Process(printerFiles(), "", "")

	tests := []struct {
		name      string
		mode      prettycov.ColorMode
		env       map[string]string
		wantColor bool
	}{
		{name: "always, even into a pipe", mode: prettycov.ColorAlways, wantColor: true},
		{name: "never", mode: prettycov.ColorNever, wantColor: false},
		{name: "auto into a pipe stays clean", mode: prettycov.ColorAuto, wantColor: false},
		{
			name: "always ignores NO_COLOR, the caller asked",
			mode: prettycov.ColorAlways, env: map[string]string{"NO_COLOR": "1"}, wantColor: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			var buf bytes.Buffer

			prettycov.DisplayTree(&buf, tree, prettycov.Options{Depth: 1, Color: tc.mode})

			if tc.wantColor {
				assert.Contains(t, buf.String(), "\x1b[")
			} else {
				assert.NotContains(t, buf.String(), "\x1b[")
			}
		})
	}
}

// The auto heuristic's guards. A terminal cannot be faked here, so these pin the branches that
// say no; the branch that says yes is only reachable against a real tty.
//
//nolint:paralleltest // t.Setenv cannot be combined with t.Parallel.
func TestDisplayTreeAutoColorGuards(t *testing.T) {
	tree := prettycov.Process(printerFiles(), "", "")

	tests := []struct {
		name string
		key  string
		val  string
	}{
		{name: "NO_COLOR set", key: "NO_COLOR", val: "1"},
		{name: "NO_COLOR set but empty still counts", key: "NO_COLOR", val: ""},
		{name: "dumb terminal", key: "TERM", val: "dumb"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.val)

			var buf bytes.Buffer

			prettycov.DisplayTree(&buf, tree, prettycov.Options{Depth: 1})

			assert.NotContains(t, buf.String(), "\x1b[")
		})
	}
}

// A real file is not a terminal, so auto must stay clean writing to one. Covers the branch a
// bytes.Buffer cannot reach: the destination is an *os.File and gets stat'd.
func TestDisplayTreeAutoColorToRegularFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "report.txt")

	out, err := os.Create(path)
	require.NoError(t, err)

	prettycov.DisplayTree(out, prettycov.Process(printerFiles(), "", ""), prettycov.Options{Depth: 1})
	require.NoError(t, out.Close())

	written, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.NotEmpty(t, written)
	assert.NotContains(t, string(written), "\x1b[")
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
func render(t *testing.T, tree *prettycov.PathTree, depth uint) string {
	t.Helper()

	return renderWith(t, tree, depth, prettycov.ColorNever)
}

func renderColor(t *testing.T, tree *prettycov.PathTree, depth uint) string {
	t.Helper()

	return renderWith(t, tree, depth, prettycov.ColorAlways)
}

func renderWith(t *testing.T, tree *prettycov.PathTree, depth uint, color prettycov.ColorMode) string {
	t.Helper()

	var buf bytes.Buffer

	prettycov.DisplayTree(&buf, tree, prettycov.Options{Depth: depth, Color: color})

	return buf.String()
}

// nodeNames is the labels a tree renders to, in order. Read off Rows rather than scraped back
// out of the rendered text, so a change to the glyphs cannot break a test about ordering.
func nodeNames(t *testing.T, tree *prettycov.PathTree, depth uint) []string {
	t.Helper()

	rows := prettycov.Rows(tree, depth)

	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Label)
	}

	return names
}
