package prettycov_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

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

	assert.Equal(t, []string{"m", "alpha/deep", "beta", "gamma"}, nodeNames(render(t, tree, 1)))
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

	assert.Equal(t, []string{"github.com/o/repo", "pkg", "web"}, nodeNames(render(t, tree, 1)))
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

			assert.Equal(t, []string{"m/x", "sub"}, nodeNames(render(t, tree, 1)))
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

			assert.Equal(t, tc.want, nodeNames(render(t, tree, tc.depth)))
		})
	}
}

// Drawing a report is not worth a panic, so an unrecognised box type is a blank.
func TestBoxTypeStringNeverPanics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		box  prettycov.BoxType
		want string
	}{
		{name: "regular", box: prettycov.Regular, want: "├"},
		{name: "last", box: prettycov.Last, want: "└"},
		{name: "between", box: prettycov.Between, want: "│"},
		{name: "after last", box: prettycov.AfterLast, want: " "},
		{name: "out of range", box: prettycov.BoxType(99), want: " "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.NotPanics(t, func() { assert.Equal(t, tc.want, tc.box.String()) })
		})
	}
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

func render(t *testing.T, tree *prettycov.PathTree, depth uint) string {
	t.Helper()

	var buf bytes.Buffer

	prettycov.DisplayTree(&buf, tree, depth)

	return buf.String()
}

// nodeNames pulls the label out of each rendered line, in the order printed.
func nodeNames(out string) []string {
	var names []string

	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		idx := strings.LastIndex(line, " - ")
		if idx < 0 {
			continue
		}

		if name := strings.TrimLeft(line[:idx], " ├└│"); name != "" {
			names = append(names, name)
		}
	}

	return names
}
