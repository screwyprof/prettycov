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

	// Color is how percentages are written. Resolving -color=auto against a destination is the
	// caller's, since that is a question about the world rather than about coverage.
	Color Palette

	// Counts writes uncovered/total statements after each percentage, which hides size on its own.
	Counts bool

	// HideCovered leaves out every subtree covered to this percentage or above, so what is left is
	// what there is still work in. Nil is the whole report; the CLI's -hide-covered defaults it to
	// 100, where nothing hidden holds an uncovered statement and absence means "nothing to do here".
	//
	// A threshold below 100 hides misses along with the rows — at 90 on the delegator profile, 9 of
	// its 34 — which is the caller's to decide and worth knowing. Said here rather than refused:
	// -fail-under already takes a number, and this one only shapes the report.
	//
	// Shaping, never measuring. The tree keeps every statement it had, so -total, -fail-under and
	// the top row read the same with this set as without — as with Depth, which hides far more.
	HideCovered *float64

	// Files draws the profile's files as well as its packages, as entries of the package holding
	// them the way tree -L counts a directory's, so a package's own files and its subpackages
	// appear side by side and every parent is the sum of what is drawn beneath it. A file costs a
	// level like any other entry, unless it is all its package holds and the two merge into one
	// row. Off by default: the report is about packages, and a file row per source file buries
	// that.
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
// which rows there are and ignores the rest. Pure: no writer, no colour, no terminal. DisplayTree
// is the one that decides how a Row looks.
func Rows(tree *PathTree, opts Options) []Row {
	shown := prepare(tree, opts, opts.Files)

	rows := make([]Row, 0, len(shown))
	for _, d := range shown {
		rows = append(rows, Row{
			Prefix:   d.Prefix,
			Label:    d.Label,
			Level:    int(d.Level),
			Coverage: d.Coverage,
		})
	}

	return rows
}

// prepare is the whole of the output filtering: it reads -depth, -files and -hide-covered and
// returns the nodes left, in the order a report draws them, each carrying what a renderer needs.
//
// The one place those are read. A renderer takes this list and shapes it — it filters nothing, so
// two renderers cannot answer differently about what the report contains. Both bugs this seam was
// built for were that: a file above the bar listed by one and left out by the other, and a merged
// row drawn by one and lost by the other.
//
// withFiles is whether the output this is for includes files. The tree always holds them; what
// varies is the output. -files puts them in the tree's, and a list of positions is made of them, so
// the two renderers ask for different things without either of them filtering.
func prepare(tree *PathTree, opts Options, withFiles bool) []drawn {
	b := walker{depth: opts.Depth, withFiles: withFiles}
	if opts.HideCovered != nil {
		b.hiding, b.hideAt = true, *opts.HideCovered
	}

	// One leading space, with room to grow two bytes per level. Deep enough for any real path;
	// append handles a deeper one correctly if it comes.
	padding := make([]byte, 1, 128)
	padding[0] = ' '

	b.walk(tree, 0, "", padding)

	return b.out
}

// DisplayTree writes tree as an indented report and reports how many rows it drew. A collapsed run
// of directories is the one row it renders as.
//
// The count is there so a caller can tell an empty report from a full one without building every
// row a second time to ask — which is what asking Rows first amounted to, and it doubled the work
// of the run on the one path where a caller cares.
func DisplayTree(w io.Writer, tree *PathTree, opts Options) int {
	buf := bufio.NewWriter(w)

	rows := Rows(tree, opts)
	for _, row := range rows {
		_, _ = buf.WriteString(row.Prefix)
		_, _ = buf.WriteString(row.Label)
		_, _ = buf.WriteString(" - ")
		_, _ = buf.WriteString(formatCoverage(row.Coverage, opts))
		_ = buf.WriteByte('\n')
	}

	_ = buf.Flush()

	return len(rows)
}

// drawn is one row the traversal decided on, and everything a renderer needs to shape it.
//
// What the renderers read, not the node they read it from: Rows takes Coverage, Misses takes
// Blocks. Carrying the *PathTree instead would hand every renderer the Children and Files maps
// below it, so re-deriving the subtree stage 4 has already decided against would be one field
// access away and nothing would catch it — which is the way the two of them came to disagree twice.
type drawn struct {
	// Coverage is the node's, rolled up. Blocks is empty unless the row stands for a file.
	Coverage CoverageStats
	Blocks   []Block
	Label    string
	// Path is the node's whole path, which Label alone is not: a row is drawn with its own segment,
	// so `httpkit` says nothing about the `pkg` above it. Misses needs all of it, because what it
	// prints has to name a file an editor can open — and for the same reason it is built from the
	// unsanitised segments, where Label is the drawn one.
	Path  string
	Level Depth
	// Prefix is the indent and glyph placing the row.
	Prefix string
}

// walker holds what stays the same for the whole traversal, so the recursion carries only what
// actually varies: the node, how deep it is, and the indent it sits behind.
//
// It decides which nodes are visited and nothing about what they look like. One implementation of
// that decision, because two would drift — collapse and the depth cut-off already disagreed once,
// and every emitter added is another chance at it.
// Every field is one the traversal reads. Options is deliberately not among them: -files must be
// asked as withFiles, which is the output's question rather than the flag's, and leaving the struct
// in scope would put the wrong answer one field access away on every line of the walk.
type walker struct {
	out []drawn

	depth Depth
	// withFiles is the output's, not the options': see prepare.
	withFiles bool
	// hiding says -hide-covered was given at all and hideAt is the threshold, read once rather than
	// through the pointer at every node. What "at least this much" means is CoverageStats', not
	// ours: the answer at 100 is about the counts, not the ratio.
	hiding bool
	hideAt float64
}

// entry is one row to draw: the node, and the label it will carry once any run below it has been
// merged in. A name can belong to a file and a directory at once, so the two cannot be told apart
// by name alone.
type entry struct {
	label string
	// raw is label before sanitize, which is what a position has to carry: the replacement is for a
	// terminal to draw, and no editor opens `a/a�b.go`. -exclude matches its patterns against
	// the profile's own path, so a printed position only pastes back as a pattern if this is it.
	raw string
	// name is what the map called this before any merging, kept only to break a tie between two
	// labels that came out the same. Within one map it is unique, so it is a total order there.
	name string
	node *PathTree
}

// visible is what to draw below tree: the entries, minus the ones already at the bar, sorted — map
// order is randomised and this output gets diffed between runs.
func (b *walker) visible(tree *PathTree, level Depth) []entry {
	entries := b.entries(tree)

	if b.hiding {
		// Asked of the node the row draws, which is the one collapse merged to, so a run judged
		// here is judged by the number the reader would have seen.
		entries = slices.DeleteFunc(entries, func(e entry) bool { return b.allCovered(e.node, level) })
	}

	// Sorted by the label the reader sees rather than by the name it started as, or a merged row
	// lands where its first component would have put it: "api/errors.go" before "api.go", which
	// reads out of order because "/" sorts after ".".
	//
	// Sanitised above for the same reason, rather than at the row: a replaced rune sorts where the
	// replacement does, not where the original did. "a\x01" precedes "ab" and draws after it.
	//
	// Merging is what makes two labels able to tie, since it renames a row to something a sibling
	// may already be called: a profile naming "a.go", "a.go/b.go" and "a.go/c.go" gives the bare
	// file a "." directory that merges to "a.go", beside the directory of that name. Map order
	// decided which came first, and this output gets diffed between runs. The name each started as
	// breaks it, being unique within a map.
	//
	// Stable for the tie that leaves: a name in both maps is the same in both. Directories are
	// gathered first, so that one puts the directory above the file.
	slices.SortStableFunc(entries, func(x, y entry) int {
		return cmp.Or(strings.Compare(x.label, y.label), strings.Compare(x.name, y.name))
	})

	return entries
}

// allCovered reports whether a node says nothing the report was asked to show: it is at the bar,
// and so is every row drawn beneath it.
//
// What is drawn, not what the tree holds. -depth is a filter as much as -hide-covered is, so the
// two compose: a row -depth already cut cannot be the reason its parent survives. Judging the whole
// subtree kept `pkg - 96.41` above `logger - 96.88` at -depth=2, two rows both above the bar,
// explained only by a logger.go at 86.67 the depth had already removed.
//
// The subtree, not the node alone: coverage is not monotonic downwards below 100, so a package at
// 91 can hold one at 88. Judging the top row by itself hid the branch with the work in it.
//
// An ancestor re-walks what its child just did, so this is O(n·depth) along the surviving path
// rather than O(n) — a hidden subtree leaves the walk at once and a node under the bar stops at its
// own check. On a 30,000-file tree the re-walk costs 0.09ms against 0.29ms of rendering saved at 85%
// coverage, and 3.7ms against about 7ms at 99%: it earns its keep where there is most to hide, which
// is where the flag is for. At the default threshold it barely recurses at all — Coverage is rolled
// up, so a node with no uncovered statement has no descendant with one.
func (b *walker) allCovered(node *PathTree, level Depth) bool {
	if !node.Coverage.AtLeast(b.hideAt) {
		return false
	}

	// Past the last level the report draws, so nothing below can speak for itself.
	if !b.drawsAt(level + 1) {
		return true
	}

	// The same rows the report would draw here, so a level spent below is a level the report spends
	// too. Walking the tree a node at a time instead spent the budget several levels early wherever
	// a run of directories collapses into one row — and single-child directories are the norm in Go
	// — so the recursion stopped above rows the report does draw and called the subtree covered.
	//
	// A file among them has nothing under it, so the recursion stops at its own bar check.
	for child := range b.below(node) {
		if !b.allCovered(child.node, level+1) {
			return false
		}
	}

	return true
}

// draws reports whether the tree puts a row where this entry is. Every entry is visited; only a
// file needs asking about, and only because -files is what turns one into a row.
// drawsAt reports whether the report puts rows at this level. One definition, because walk asks it
// of the level it is about to draw and allCovered of the level below the one it is judging, and the
// two drifting is how a collapsed run came to cost allCovered more levels than it cost the report.
func (b *walker) drawsAt(level Depth) bool { return level <= b.depth }

// entries is everything below tree that could be a row: its directories with any run below them
// merged in, and its files when -files draws them. In map order, and without the bar — whether a
// row is worth drawing is allCovered's question, and asking it here would recurse back through it.
//
// The one place collapse and -files are read. They were read here and again inside allCovered, and
// the same decision written twice is how the depth cut-off and collapse came to disagree once
// already: allCovered spent a level per directory where the report spends one per row.
func (b *walker) entries(tree *PathTree) []entry {
	size := len(tree.Children)
	if b.withFiles {
		size += len(tree.Files)
	}

	out := make([]entry, 0, size)

	for e := range b.below(tree) {
		// Sanitised here and not in below: a replaced rune sorts where the replacement does, so the
		// label has to be the drawn one before visible sorts it. allCovered reads no label, and the
		// scan is per rune of every name in the profile.
		e.raw = e.label
		e.label = sanitize(e.raw)
		out = append(out, e)
	}

	return out
}

// below yields what the node holds that could be a row: its directories with any run beneath them
// merged in, and its files when the output includes them.
//
// The one place collapse and the files gate are read. An iterator rather than a slice because
// allCovered walks this for every node it judges and reads only the nodes — materialising there
// cost twelve times the bytes of the walk it was judging, for labels it never looks at.
func (b *walker) below(tree *PathTree) iter.Seq[entry] {
	return func(yield func(entry) bool) {
		for name, node := range tree.Children {
			label, merged := collapse(name, node, b.withFiles)

			// The filesystem root is the one node with no name of its own: an absolute path splits
			// to a leading empty component, which collapse turns back into the "/" of "/home/x"
			// whenever there is something below to fold. When there is not — a root holding two
			// files — the label is left empty, and a blank row says nothing. Decided here rather
			// than at the row, so the sort in visible sees the label the reader will: "/" belongs
			// after ".", and sorting on the empty string put it first.
			if label == "" {
				label = "/"
			}

			if !yield(entry{label: label, name: name, node: merged}) {
				return
			}
		}

		// Files are simply not enumerated when they are not being shown, so they cost no level and
		// no glyph rather than being filtered out later: without that the last package under a
		// directory would draw the branch glyph of a middle one whenever a file sorted after it.
		if !b.withFiles {
			return
		}

		for name, node := range tree.Files {
			if !yield(entry{label: name, name: name, node: node}) {
				return
			}
		}
	}
}

// walk adds one row per child of tree, then recurses. The top row carries no glyph, which is what
// level 0 means — it is not tracked separately, since a second flag can only drift from it.
func (b *walker) walk(tree *PathTree, level Depth, parent string, padding []byte) {
	if tree == nil || !b.drawsAt(level) {
		return
	}

	entries := b.visible(tree, level)
	root := level == 0

	for at, e := range entries {
		here := path.Join(parent, e.raw)

		b.out = append(b.out, drawn{
			Coverage: e.node.Coverage,
			Blocks:   e.node.Blocks,
			Label:    e.label,
			Path:     here,
			Level:    level,
			// string() copies, so the visitor owns its prefix and padding stays reusable. Built
			// here even for a visitor that ignores it: a row's indent depends on whether every
			// ancestor was a last child, which cannot be rebuilt from the level alone.
			Prefix: string(append(padding, symbol(root, getBoxType(at, len(entries)))...)),
		})

		// Grown for the child and truncated after it, so siblings share one buffer instead of each
		// concatenating a string. The walk is depth-first, so the child is done with its padding
		// before the next sibling overwrites it.
		depth := len(padding)
		padding = append(padding, symbol(root, childSymbol(at, len(entries)))...)
		b.walk(e.node, level+1, here, padding)
		padding = padding[:depth]
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
	for len(node.Files) == 0 && len(node.Children) == 1 {
		for name, child := range node.Children {
			label, node = join(label, name), child
		}
	}

	// A file has nothing below it, so this is where the run ends either way.
	if mergeFiles && len(node.Files) == 1 && len(node.Children) == 0 {
		for name, file := range node.Files {
			return join(label, name), file
		}
	}

	return label, node
}

// join names something inside label. Cleaned, because "." is a directory of this tree and of no
// profile — it is where a file the profile gave no directory of its own lands — and putting a
// separator after it printed "./printer.go", a path no profile contained, beside siblings written
// plainly.
//
// path.Clean rather than path.Join, which has that rule and one more: the filesystem root is the
// empty label, where the separator is the whole name, and Join cleans "/a.go" down to "a.go".
func join(label, name string) string {
	return path.Clean(label + "/" + name)
}

// sanitize replaces the characters in a label that a terminal would obey rather than draw.
// Chiefly hygiene — a stray control byte in a path garbles the report, which is why ls and git
// quote them too. It also stops a spoof: a package named "\x1b[1A\x1b[2Kforged" erases the row
// above and writes over it, and above the first child is the total.
//
// Rows only. A position carries the profile's own spelling — see entry.raw — because the
// replacement gives a path no editor resolves and an -exclude pattern that cannot match what it was
// copied from. go vet and gopls do not sanitize their positions either. So a profile naming
// "\x1b[1A\x1b[2Kforged/a.go" draws as a scrubbed row and prints raw under -misses, which is the
// trade: the report is the thing read by eye, and a location has to stay a location.
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

// symbol is the glyph that places a row, and the space after it. Constants rather than a glyph
// concatenated with " ", which allocated on every row, twice. The top row carries none — that is
// what level 0 means. An unrecognised box type draws blank rather than panicking: this draws a
// report, and nothing here is worth taking the process down for.
func symbol(root bool, b boxType) string {
	if root {
		return ""
	}

	// blank is the two columns a glyph would have taken: below a last child, and for a box type
	// nothing recognises.
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
