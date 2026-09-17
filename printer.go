package prettycov

import (
	"bufio"
	"cmp"
	"io"
	"iter"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Options controls how a tree is rendered. The zero value prints the top row alone, in plain text.
type Options struct {
	// Depth is how many levels to show below the top row. DepthAll shows all of them.
	Depth Depth

	// Color is how percentages are written. Resolving --color=auto against a destination is the
	// caller's, since that is a question about the world rather than about coverage.
	Color Palette

	// Counts writes uncovered/total statements after each percentage, which hides size on its own.
	Counts bool

	// HideCovered leaves out every subtree at this percentage or above. Nil is the whole report.
	// Below 100 it hides misses along with the rows, which is the caller's to decide.
	//
	// Shaping, never measuring: the tree keeps every statement, so total and --fail-under are
	// unaffected.
	HideCovered *Threshold

	// Files draws the profile's files beside its packages, each costing a level the way `tree -L`
	// counts one — unless a file is all its package holds, and the two merge into one row.
	Files bool
}

// Row is one line of the report: the indent and glyph that place it in the tree, the label of the
// node, and that node's coverage. The percentage is not here — it is a rendering choice, and the
// counts it comes from are.
type Row struct {
	Prefix string
	Label  string
	// Level is how far below the top row this sits. Prefix says the same in box-drawing characters,
	// but reading it back means trusting a glyph to stay two runes wide.
	Level    int
	Coverage CoverageStats
}

// Rows flattens tree into the lines a report prints, in order. It reads the options that decide
// which rows there are and ignores the rest. Pure: no writer, no colour, no terminal. DisplayTree
// is the one that decides how a Row looks.
func Rows(tree *PathTree, opts Options) []Row {
	// Nil, not empty, when there is nothing to draw: exported behaviour, and what Misses and Exclude
	// return beside it.
	var rows []Row

	for d := range prepare(tree, opts, shape{files: opts.Files}) {
		rows = append(rows, d.Row)
	}

	return rows
}

// shape is what a renderer reads off each row. The halves are disjoint — a tree row is placed by
// Prefix, a position named by Path — and building both cost a 30,000-file tree 137% of its memory.
type shape struct {
	// files draws the profile's files. --files for a tree; always, for a list of positions.
	files bool
	// positions builds Path and leaves Prefix empty, rather than the other way about.
	positions bool
}

// prepare is the whole of the output filtering: it reads --depth, --files and --hide-covered and
// yields the nodes left, in draw order.
//
// The one place those are read. Renderers receive and shape; they cannot filter, so two of them
// cannot disagree about what the report contains — which is what both bugs this seam was built for
// were. want is the output's question, not a flag's: a list of positions is made of files either way.
func prepare(tree *PathTree, opts Options, want shape) iter.Seq[drawn] {
	return func(yield func(drawn) bool) {
		b := walker{shape: want, depth: opts.Depth, yield: yield}
		if opts.HideCovered != nil {
			b.hiding, b.hideAt = true, *opts.HideCovered
		}

		// One leading space, with room to grow two bytes per level. Deep enough for any real path;
		// append handles a deeper one correctly if it comes.
		padding := make([]byte, 1, 128)
		padding[0] = ' '

		b.walk(tree, 0, "", padding)
	}
}

// DisplayTree writes tree as an indented report and returns the rows drawn and the destination's
// error. A collapsed run of directories is one row.
//
// bufio holds the first write failure until Flush; discarding it reported a full report over a full
// disk. Counted while writing rather than via Rows, which would hold the whole report to return a
// length.
func DisplayTree(w io.Writer, tree *PathTree, opts Options) (int, error) {
	buf := bufio.NewWriter(w)
	drawn := 0

	for d := range prepare(tree, opts, shape{files: opts.Files}) {
		_, _ = buf.WriteString(d.Prefix)
		_, _ = buf.WriteString(d.Label)
		_, _ = buf.WriteString(" - ")
		_, _ = buf.WriteString(formatCoverage(d.Coverage, opts))
		_ = buf.WriteByte('\n')

		drawn++
	}

	//nolint:wrapcheck // the writer's own error; this adds no context the caller lacks.
	return drawn, buf.Flush()
}

// drawn is one row the traversal decided on, and everything a renderer needs to shape it.
//
// What the renderers read, not the node they read it from: Rows takes Coverage, Misses takes
// Blocks. Carrying the *PathTree instead would hand every renderer the Children and Files maps
// below it, so re-deriving the subtree stage 4 has already decided against would be one field
// access away and nothing would catch it — which is the way the two of them came to disagree twice.
type drawn struct {
	Row

	// Blocks is empty unless the row stands for a file.
	Blocks []Block
	// Path is the whole path, which Label is not — a row carries only its own segment. Built from
	// sanitised segments, so both printers spell one path the same way.
	Path string
}

// walker holds what stays the same for the whole traversal. It decides which nodes are visited and
// nothing about how they look.
//
// Options is deliberately not a field: --files must be asked as shape, the output's question rather
// than the flag's, and holding the struct would put the wrong answer one field access away.
type walker struct {
	// shape is the output's question, not the options'.
	shape

	// yield takes each row as it is decided, and reports false when the consumer has stopped.
	yield func(drawn) bool

	depth Depth
	// hiding says --hide-covered was given; hideAt is the threshold, read once rather than through
	// the pointer at every node.
	hiding bool
	hideAt Threshold
}

// entry is one row to draw: the node, and the label it carries once any run below it is merged in.
type entry struct {
	label string
	// name is what the map called this before merging, kept to break a tie between two labels that
	// came out the same. Unique within one map, so it is a total order there.
	name string
	node *PathTree
}

// visible is what to draw below tree: everything it holds that could be a row, minus the ones
// already at the bar, sanitised and sorted — map order is randomised and this output gets diffed
// between runs.
func (b *walker) visible(tree *PathTree, level Depth) []entry {
	size := len(tree.Children)
	if b.files {
		size += len(tree.Files)
	}

	entries := make([]entry, 0, size)

	for e := range b.below(tree) {
		// The bar first, so a row nobody draws is never scanned — and asked of the node collapse
		// merged to, which is the number the reader would have seen.
		if b.hiding && b.allCovered(e.node, level) {
			continue
		}

		// Before the sort, since a replaced rune sorts where the replacement does. After the bar,
		// since allCovered reads no label and the scan is per rune.
		e.label = sanitize(e.label)
		entries = append(entries, e)
	}

	// By the drawn label, not the name it started as: "api/errors.go" sorts before "api.go" because
	// "/" follows ".". Merging is what lets two labels tie — a bare "a.go" merges to the same label
	// as a directory called "a.go" — and the original name breaks it, being unique within a map.
	// Stable for the tie left when a name is in both maps; directories are gathered first.
	slices.SortStableFunc(entries, func(x, y entry) int {
		return cmp.Or(strings.Compare(x.label, y.label), strings.Compare(x.name, y.name))
	})

	return entries
}

// allCovered reports whether a node and every row drawn beneath it are at the bar.
//
// What is drawn, not what the tree holds: a row --depth already cut cannot be why its parent
// survives. And the subtree, not the node alone — coverage is not monotonic downwards below 100, so
// a package at 91 can hold one at 88.
//
// O(n·depth), since an ancestor re-walks what its child did. Measured worth it: 3.7ms of re-walk
// against 7ms of rendering saved at 99% coverage.
func (b *walker) allCovered(node *PathTree, level Depth) bool {
	if !node.AtLeast(b.hideAt) {
		return false
	}

	// Past the last level the report draws, so nothing below can speak for itself.
	if !b.drawsAt(level + 1) {
		return true
	}

	// The rows the report would draw, so a level spent here is one the report spends. Walking node
	// by node spent the budget early wherever a run collapses, and called a subtree covered above
	// rows the report does draw.
	for child := range b.below(node) {
		if !b.allCovered(child.node, level+1) {
			return false
		}
	}

	return true
}

// drawsAt reports whether the report puts rows at this level. One definition, because walk asks it
// of the level it is about to draw and allCovered of the level below the one it is judging, and the
// two drifting is how a collapsed run came to cost allCovered more levels than it cost the report.
func (b *walker) drawsAt(level Depth) bool { return level <= b.depth }

// below yields what the node holds that could be a row: directories with any run beneath them
// merged in, and files when the output includes them. The one place collapse and the files gate are
// read. An iterator, since allCovered walks it per node judged and reads only the nodes.
func (b *walker) below(tree *PathTree) iter.Seq[entry] {
	return func(yield func(entry) bool) {
		for name, node := range tree.Children {
			label, merged := collapse(name, node, b.files)

			// The filesystem root has no name: an absolute path splits to a leading empty component.
			// Named here, not at the row, so visible sorts on what the reader sees — "/" belongs
			// after ".", and the empty string sorted first.
			if label == "" {
				label = "/"
			}

			if !yield(entry{label: label, name: name, node: merged}) {
				return
			}
		}

		// Not enumerated rather than filtered later, so they cost no level and no glyph — else the
		// last package would draw a middle one's branch glyph whenever a file sorted after it.
		if !b.files {
			return
		}

		for name, node := range tree.Files {
			if !yield(entry{label: name, name: name, node: node}) {
				return
			}
		}
	}
}

// walk adds one row per child of tree, then recurses. Level 0 is the top row, which carries no glyph.
//
// Returns whether to carry on: range-over-func panics on a yield after the body has been left, so a
// consumer that stops must stop every ancestor's loop, and the return value makes each call site ask.
func (b *walker) walk(tree *PathTree, level Depth, parent string, padding []byte) bool {
	if tree == nil || !b.drawsAt(level) {
		return true
	}

	entries := b.visible(tree, level)
	root := level == 0

	for at, e := range entries {
		// Each is read by one renderer only, so the one nobody asked for is not built.
		var here, prefix string

		if b.positions {
			// join, not path.Join: one allocation, and right at the filesystem root, where
			// join("", "a.go") is "/a.go".
			here = e.label
			if parent != "" {
				here = join(parent, e.label)
			}
		} else {
			// Copied, so the row owns its prefix and padding stays reusable. The indent depends on
			// whether every ancestor was a last child, which the level alone cannot say.
			prefix = string(append(padding, symbol(root, getBoxType(at, len(entries)))...))
		}

		if !b.yield(drawn{
			Row: Row{
				Coverage: e.node.Coverage,
				Label:    e.label,
				Level:    int(level),
				Prefix:   prefix,
			},
			Blocks: e.node.Blocks,
			Path:   here,
		}) {
			return false
		}

		// One buffer shared by siblings: depth-first, so the child is done before the next overwrites.
		depth := len(padding)
		padding = append(padding, symbol(root, childSymbol(at, len(entries)))...)
		carryOn := b.walk(e.node, level+1, here, padding)
		padding = padding[:depth]

		if !carryOn {
			return false
		}
	}

	return true
}

// collapse merges a run of nodes that each hold nothing but the next into one row, so a module path
// does not spend three levels on "github.com", "owner", "repo".
//
// Files of its own stop the run — unless mergeFiles and that one file is all the directory holds,
// where the two rows would carry the same number twice.
func collapse(label string, node *PathTree, mergeFiles bool) (string, *PathTree) {
	// Bounded like Get's descent: Children is exported, so a hand-built tree can point at itself.
	for range maxRootDepth {
		name, child, ok := node.onlyChild()
		if !ok {
			break
		}

		label, node = join(label, name), child
	}

	// A file has nothing below it, so this is where the run ends either way.
	if mergeFiles && len(node.Files) == 1 && len(node.Children) == 0 {
		for name, file := range node.Files {
			return join(label, name), file
		}
	}

	return label, node
}

// join names something inside label. Cleaned, so the "." a bare file lands under does not print as
// "./printer.go". Clean rather than Join, which also strips the filesystem root's leading separator.
func join(label, name string) string {
	return path.Clean(label + "/" + name)
}

// sanitize replaces the characters a terminal would obey rather than draw. A package named
// "\x1b[1A\x1b[2Kforged" erases the row above and writes over it, and above the first child is the
// total. Positions too: the same escape erases a miss, and "real\revil/b.go" draws as "evil/b.go".
//
// The cost is that such a path loses its round trip — it will not open in an editor or match as an
// --exclude pattern. Worth it: it is corrupted visibly rather than silently, most of this set breaks
// a line-oriented consumer anyway, and Go forbids all of it in a module path.
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

// formatCoverage renders a statement-less package as "n/a" rather than the "NaN" 0/0 gives, with no
// grade and no counts. Percentage also refuses overflowed counts.
//
// Counts read uncovered over total, with the word kept: codecov prints the same fraction the other
// way round, and a row gets pasted where no flag name travels with it.
func formatCoverage(stats CoverageStats, opts Options) string {
	pct, ok := stats.Percentage()
	if !ok {
		return "n/a"
	}

	text := pct.String()

	// Only the one value that asks for it: Palette is an exported int, and escapes into a file are
	// worse than a missing colour.
	if opts.Color == ANSI {
		text = grade(pct.Float()) + text + reset
	}

	if opts.Counts {
		text += "  " + strconv.Itoa(stats.Uncovered) + "/" + strconv.Itoa(stats.Total()) + " uncovered"
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

// symbol is the glyph placing a row, with its trailing space — constants, since concatenating one
// allocated twice per row. An unrecognised box type draws blank rather than panicking.
func symbol(root bool, b boxType) string {
	if root {
		return ""
	}

	// The two columns a glyph would have taken.
	const blank = "  "

	switch b {
	case regular:
		return "\u251c " // ├
	case last:
		return "\u2514 " // └
	case afterLast:
		return blank
	case between:
		return "\u2502 " // │
	}

	return blank
}
