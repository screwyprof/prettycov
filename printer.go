package prettycov

import (
	"fmt"
	"io"
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

	// Counts writes uncovered/total statements after each percentage, which hides size on its own.
	Counts bool

	// Files draws the profile's files as well as its packages. They sit one level below the
	// package that holds them, the way tree -L counts a directory's entries, so a package's own
	// files and its subpackages appear side by side and every parent is the sum of what is drawn
	// beneath it. Off by default: the report is about packages, and a file row per source file
	// buries that.
	Files bool
}

// Row is one line of the report: the indent and glyph that place it in the tree, the label of the
// node, and that node's coverage. The percentage is not here — it is a rendering choice, and the
// counts it comes from are.
type Row struct {
	Prefix   string
	Label    string
	Coverage CoverageStats
}

// Rows flattens tree into the lines a report prints, in order. It reads the options that decide
// which rows there are — Depth and Files — and ignores the rest. Pure: no writer, no colour, no
// terminal. DisplayTree is the one that decides how a Row looks.
func Rows(tree *PathTree, opts Options) []Row {
	b := rowBuilder{depth: opts.Depth, files: opts.Files}
	b.walk(tree, 0, " ")

	return b.rows
}

// DisplayTree writes tree as an indented report. A collapsed run of directories is the one row it
// renders as.
func DisplayTree(w io.Writer, tree *PathTree, opts Options) {
	for _, row := range Rows(tree, opts) {
		_, _ = fmt.Fprintf(w, "%s%s - %s\n", row.Prefix, row.Label, formatCoverage(row.Coverage, opts))
	}
}

// rowBuilder holds what stays the same for the whole traversal, so the recursion carries only
// what actually varies: the node, how deep it is, and the indent it sits behind.
type rowBuilder struct {
	depth Depth
	files bool
	rows  []Row
}

// visible is the children to draw, sorted — map order is randomised and this output gets diffed
// between runs. Files are dropped rather than skipped later, so they cost no level and no glyph
// when they are not being shown: without this the last package under a directory would draw the
// branch glyph of a middle one whenever a file sorted after it.
//
// Only a file with nothing beneath it is dropped. One name can be both — a profile naming
// "m/a.go" and "m/a.go/b.go" describes a file and a directory called the same thing, which no
// filesystem allows but merging two profiles, or an -old/-new rewrite, can produce. Dropping it
// took its whole subtree with it while every ancestor went on counting the statements.
func (b *rowBuilder) visible(tree *PathTree) []string {
	names := make([]string, 0, len(tree.Children))

	for name, child := range tree.Children {
		if child.IsFile() && !b.files {
			continue
		}

		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// walk adds one row per child of tree, then recurses. The top row carries no glyph, which is what
// level 0 means — it is not tracked separately, since a second flag can only drift from it.
func (b *rowBuilder) walk(tree *PathTree, level Depth, padding string) {
	if tree == nil || level > b.depth {
		return
	}

	names := b.visible(tree)

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
//
// Nor is a file, which carries statements of its own that the row it folded into would not report:
// "m/a.go" beside "m/a.go/sub/b.go" makes a.go a file with one child and no package of its own, and
// folding it left m claiming twelve statements above a single row showing seven.
func collapse(label string, node *PathTree) (string, *PathTree) {
	for !node.isPkg && !node.isFile && len(node.Children) == 1 {
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

// formatCoverage renders a package with no statements as "n/a" rather than a percentage — it used
// to print "NaN", which is what 0/0 produces in float division — and with no grade and no counts,
// since there is nothing to grade or count. Percentage also refuses overflowed counts, so a wrapped
// negative total never reaches the row either.
//
// Counts go as uncovered over total: the uncovered count is the one a reader acts on. The word
// stays because codecov prints the same fraction the other way round — 162/180 there is hits —
// and a row gets pasted into places where no flag name travels with it.
func formatCoverage(stats CoverageStats, opts Options) string {
	pct, ok := stats.Percentage()
	if !ok {
		return "n/a"
	}

	text := pct.String()

	// Colour only for the one value that asks for it: Palette is an exported int, so a caller can
	// hand over any number, and escapes into a file are worse than a missing colour.
	if opts.Color == ANSI {
		text = grade(pct.Float()) + text + reset
	}

	if opts.Counts {
		text += fmt.Sprintf("  %d/%d uncovered", stats.Uncovered, stats.Covered+stats.Uncovered)
	}

	return text
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
