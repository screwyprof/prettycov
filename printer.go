package prettycov

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"unicode"
)

// Options controls how a tree is rendered. The zero value prints the top row alone, in plain text.
type Options struct {
	// Depth is how many levels to show below the top row. DepthAll shows all of them.
	Depth Depth

	// Color is how percentages are written. Resolving -color=auto against a destination is the
	// caller's, since that is a question about the world rather than about coverage.
	Color Palette
}

// Row is one line of the report: the indent and glyph that place it in the tree, the label of the
// node, and that node's coverage. The percentage is not here — it is a rendering choice, and the
// counts it comes from are.
type Row struct {
	Prefix   string
	Label    string
	Coverage CoverageStats
}

// Rows flattens tree into the lines a report prints, in order, to the given depth. Pure: no
// writer, no colour, no terminal. DisplayTree is the one that decides how a Row looks.
func Rows(tree *PathTree, depth Depth) []Row {
	b := rowBuilder{depth: depth}
	b.walk(tree, 0, " ")

	return b.rows
}

// DisplayTree writes tree as an indented report. A collapsed run of directories is the one row it
// renders as.
func DisplayTree(w io.Writer, tree *PathTree, opts Options) {
	for _, row := range Rows(tree, opts.Depth) {
		_, _ = fmt.Fprintf(w, "%s%s - %s\n", row.Prefix, row.Label, formatRatio(row.Coverage, opts.Color))
	}
}

// rowBuilder holds what stays the same for the whole traversal, so the recursion carries only
// what actually varies: the node, how deep it is, and the indent it sits behind.
type rowBuilder struct {
	depth Depth
	rows  []Row
}

// walk adds one row per child of tree, then recurses. The top row carries no glyph, which is what
// level 0 means — it is not tracked separately, since a second flag can only drift from it.
func (b *rowBuilder) walk(tree *PathTree, level Depth, padding string) {
	if tree == nil || level > b.depth {
		return
	}

	// Sorted, because this output gets diffed between runs and map order is randomised.
	names := slices.Sorted(maps.Keys(tree.Children))

	for i, name := range names {
		label, node := collapse(name, tree.Children[name])
		root := level == 0

		b.rows = append(b.rows, Row{
			Prefix: padding + symbol(root, getBoxType(i, len(names))),
			// Sanitised here rather than at the writer, so no consumer of a Row has to remember to.
			Label:    sanitize(label),
			Coverage: node.Coverage,
		})

		b.walk(node, level+1, padding+symbol(root, childSymbol(i, len(names))))
	}
}

// collapse folds a run of directories that each hold nothing but the next one into a single row,
// so a module path does not spend three levels on "github.com", "owner", "repo" before reaching
// anything worth reading. A directory the profile named itself is never folded away, however few
// statements it holds — a package whose files declare none still deserves its own row.
func collapse(label string, node *PathTree) (string, *PathTree) {
	for !node.isPkg && len(node.Children) == 1 {
		for name, child := range node.Children {
			label, node = label+"/"+name, child
		}
	}

	return label, node
}

// sanitize replaces control characters in a label. Chiefly hygiene — a stray control byte in a
// path garbles the report, which is why ls and git quote them too. It also stops a spoof: a
// package named "\x1b[1A\x1b[2Kforged" erases the row above and writes over it, and above the
// first child is the total. Only control characters go; a path may be non-ASCII.
func sanitize(label string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}

		return r
	}, label)
}

// formatRatio renders a package with no statements as "n/a" rather than a percentage. It used to
// print "NaN", which is what 0/0 produces in float division.
func formatRatio(stats CoverageStats, palette Palette) string {
	pct, ok := stats.Percentage()
	if !ok {
		// Nothing to cover is not a grade, so it is not coloured either.
		return "n/a"
	}

	// Colour only for the one value that asks for it: Palette is an exported int, so a caller can
	// hand over any number, and escapes into a file are worse than a missing colour.
	if palette == ANSI {
		return grade(pct.Float()) + pct.String() + reset
	}

	return pct.String()
}

type boxType int

const (
	regular boxType = iota
	last
	afterLast
	between
)

// String renders the glyph for a box type. An unrecognised one is a blank rather than a panic:
// this draws a report, and nothing here is worth taking the process down for.
func (b boxType) String() string {
	switch b {
	case regular:
		return "\u251c" // ├
	case last:
		return "\u2514" // └
	case afterLast:
		return " "
	case between:
		return "\u2502" // │
	}

	return " "
}

func getBoxType(index int, length int) boxType {
	if index+1 == length {
		return last
	}

	return regular
}

func childSymbol(index int, length int) boxType {
	if index+1 == length {
		return afterLast
	}

	return between
}

func symbol(root bool, b boxType) string {
	if root {
		return ""
	}

	return b.String() + " "
}
