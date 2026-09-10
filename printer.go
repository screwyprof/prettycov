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
	Prefix string
	Label  string
	// Level is how far the row sits below the top one, which carries level 0. Prefix says the same
	// in box-drawing characters, and reading the shape back out of it means measuring a glyph
	// string and trusting it to stay two runes wide — which is what the reconciliation test did
	// before this existed, and what any other consumer of Rows would otherwise have to do.
	Level    int
	Coverage CoverageStats
}

// Rows flattens tree into the lines a report prints, in order. It reads the options that decide
// which rows there are — Depth and Files — and ignores the rest. Pure: no writer, no colour, no
// terminal. DisplayTree is the one that decides how a Row looks.
func Rows(tree *PathTree, opts Options) []Row {
	b := rowBuilder{opts: opts}
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
	opts Options
	rows []Row
}

// entry is one node to draw and the name it is drawn under. A name can belong to a file and a
// directory at once, so the two cannot be told apart by name alone.
type entry struct {
	name string
	node *PathTree
}

// visible is what to draw below tree, sorted — map order is randomised and this output gets diffed
// between runs. Files are simply not enumerated when they are not being shown, so they cost no
// level and no glyph rather than being filtered out later: without that the last package under a
// directory would draw the branch glyph of a middle one whenever a file sorted after it.
func (b *rowBuilder) visible(tree *PathTree) []entry {
	entries := make([]entry, 0, len(tree.Children)+len(tree.Files))

	for name, node := range tree.Children {
		entries = append(entries, entry{name: name, node: node})
	}

	if b.opts.Files {
		for name, node := range tree.Files {
			entries = append(entries, entry{name: name, node: node})
		}
	}

	// Stable, because a name can appear in both maps and comparing names alone leaves those two
	// tied: an unstable sort would order them differently between runs, and this output gets
	// diffed. Directories are appended first, so a tie puts the directory above the file.
	slices.SortStableFunc(entries, func(a, b entry) int { return strings.Compare(a.name, b.name) })

	return entries
}

// walk adds one row per child of tree, then recurses. The top row carries no glyph, which is what
// level 0 means — it is not tracked separately, since a second flag can only drift from it.
func (b *rowBuilder) walk(tree *PathTree, level Depth, padding string) {
	if tree == nil || level > b.opts.Depth {
		return
	}

	entries := b.visible(tree)

	for i, e := range entries {
		// A directory's own file is merged into it only where that file has a row of its own to
		// merge with: below the depth being drawn there is no second row, so merging would show a
		// filename the depth was asked to leave out, and put one branch at file granularity while
		// its siblings stayed at package granularity.
		label, node := collapse(e.name, e.node, b.opts.Files && level < b.opts.Depth)
		root := level == 0

		// The filesystem root is the one node with no name of its own: an absolute path splits to
		// a leading empty component, which collapse turns back into the "/" of "/home/x" whenever
		// there is something below to fold. When there is not — a profile holding "/a.go" — the
		// label is left empty, and a blank row says nothing.
		if label == "" {
			label = "/"
		}

		b.rows = append(b.rows, Row{
			Prefix: padding + symbol(root, getBoxType(i, len(entries))),
			// Sanitised here rather than at the writer, so no consumer of a Row has to remember to.
			Label:    sanitize(label),
			Level:    int(level),
			Coverage: node.Coverage,
		})

		b.walk(node, level+1, padding+symbol(root, childSymbol(i, len(entries))))
	}
}

// collapse merges a run of nodes that each hold nothing but the next one into a single row, so a
// module path does not spend three levels on "github.com", "owner", "repo" before reaching
// anything worth reading.
//
// A directory holding files of its own stops the run, however few statements they declare — a
// package whose files declare none still deserves its own row — unless mergeFiles and that one
// file is all it holds. Then the two rows carry the same number twice and the second says nothing
// the first does not: "tzkt/client.go" names the directory and the file in the row the directory
// had anyway. Two files, or a file beside a subdirectory, and it is left alone.
func collapse(label string, node *PathTree, mergeFiles bool) (string, *PathTree) {
	for {
		switch {
		case len(node.Files) == 0 && len(node.Children) == 1:
			label, node = descend(label, node.Children)
		case mergeFiles && len(node.Files) == 1 && len(node.Children) == 0:
			// A file has nothing below it, so this is where the run ends.
			return descend(label, node.Files)
		default:
			return label, node
		}
	}
}

// descend takes the sole entry of a map, which its callers have checked there is exactly one of.
func descend(label string, nodes map[string]*PathTree) (string, *PathTree) {
	for name, node := range nodes {
		return label + "/" + name, node
	}

	return label, nil
}

// sanitize replaces the characters in a label that a terminal would obey rather than draw.
// Chiefly hygiene — a stray control byte in a path garbles the report, which is why ls and git
// quote them too. It also stops a spoof: a package named "\x1b[1A\x1b[2Kforged" erases the row
// above and writes over it, and above the first child is the total.
//
// What counts as obeyed is wider than the control characters, and obeyed reports it.
func sanitize(label string) string {
	return strings.Map(func(r rune) rune {
		if obeyed(r) {
			return '�'
		}

		return r
	}, label)
}

// obeyed reports whether whatever renders the report would act on the rune rather than draw it.
// Not the same question as unicode.IsControl, which answers only for category Cc:
//
//   - the bidi overrides and isolates are Cf, and one in a path reverses the reading order of
//     everything after it, so a file is drawn under a name it does not have — the Trojan Source
//     trick, which gosec's G116 catches in Go source for the same reason;
//   - U+2028 and U+2029 end a line for a log viewer or a JSON consumer as surely as the carriage
//     return already handled here, and this report is read a line at a time;
//   - U+FEFF draws as nothing at all, so two labels differing only by one look identical.
//
// Everything else stays, so a path may be non-ASCII: Cf also holds the joiners U+200C and U+200D,
// which spell ordinary words in Persian and Devanagari.
func obeyed(r rune) bool {
	return unicode.IsControl(r) ||
		unicode.Is(unicode.Bidi_Control, r) ||
		r == '\u2028' || r == '\u2029' || r == '\ufeff'
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
		text += fmt.Sprintf("  %d/%d uncovered", stats.Uncovered, stats.Total())
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
