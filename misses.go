package prettycov

import (
	"bufio"
	"cmp"
	"io"
	"slices"
	"strconv"
)

// Miss is a run of statements the tests never reached, in one file: one per contiguous region, not
// per block, since cmd/cover emits a block per branch.
//
// Line and Col open the region's first block — `if !ok {` rather than the `return` inside it — and
// are the only spelling a compiler-style consumer reads. EndLine is for the consumers that do speak
// ranges (GitHub annotations, LSP). Statements is what the positions cannot say.
type Miss struct {
	File       string
	Line, Col  int
	EndLine    int
	Statements int
}

// Misses lists the statements the tests never reached, ordered by position.
//
// Same traversal as Rows, so --depth and --hide-covered narrow it the same way. --files is forced
// on: a miss is a file position whether or not a file is drawn, so the list narrows as the tree
// does *with* --files. --exclude and a renamed root acted on the profile before the tree was built.
func Misses(tree *PathTree, opts Options) []Miss {
	var misses []Miss

	for d := range prepare(tree, opts, shape{files: true, positions: true}) {
		misses = merge(misses, d.Path, d.Blocks)
	}

	// prepare yields depth-first, so a package's own files come after its subpackages. Flattened,
	// that puts b.go after deep/a.go and a reader loses their place; sort again.
	slices.SortFunc(misses, func(x, y Miss) int {
		return cmp.Or(cmp.Compare(x.File, y.File), cmp.Compare(x.Line, y.Line), cmp.Compare(x.Col, y.Col))
	})

	return misses
}

// byPosition orders two blocks the way a file reads.
func byPosition(x, y Block) int {
	return cmp.Or(cmp.Compare(x.Line, y.Line), cmp.Compare(x.Col, y.Col))
}

// merge folds a file's uncovered blocks into the fewest regions covering them.
//
// Abutting means opening no later than the line after the last region ended, with nothing covered
// between — a covered block in the gap stops the fold, or the region would claim a statement the
// tests reach.
//
// A profile is already sorted; Process also takes a caller's own FileCoverage, where an out-of-order
// block would fold into whatever region is open. Sorts a copy, so asking for misses does not reorder
// the caller's tree.
func merge(out []Miss, file string, blocks []Block) []Miss {
	if !slices.IsSortedFunc(blocks, byPosition) {
		blocks = slices.Clone(blocks)
		slices.SortFunc(blocks, byPosition)
	}

	// Index of the region still able to take a block, or -1. Starts closed so the previous file's
	// region cannot extend into this one.
	open := -1

	for _, block := range blocks {
		// Neither a miss nor a separator: cmd/cover emits one per type-switch case, exactly where an
		// untested switch's misses abut.
		if block.Coverage.Total() == 0 {
			continue
		}

		if block.Coverage.Uncovered == 0 {
			// Only a covered block at or past the region's end separates it from what follows. One
			// starting inside is cmd/cover's nesting and must not close. At or past, not past:
			// `} else if d {` ends one block and starts the next on one line, and 4-7 and 7-10 are
			// two regions, not 4-10 folded across the covered condition between them.
			if open >= 0 && block.Line >= out[open].EndLine {
				open = -1
			}

			continue
		}

		// Block is exported, so a caller's own FileCoverage leaves EndLine zero — an inverted region
		// a range consumer rejects.
		end := max(block.Line, block.EndLine)

		// Line-1, not EndLine+1: cover bounds no line number, and MaxInt+1 wrapped to MinInt.
		if open >= 0 && block.Line-1 <= out[open].EndLine {
			out[open].EndLine = max(out[open].EndLine, end)
			out[open].Statements += block.Coverage.Uncovered

			continue
		}

		out = append(out, Miss{
			File:       file,
			Line:       block.Line,
			Col:        block.Col,
			EndLine:    end,
			Statements: block.Coverage.Uncovered,
		})
		open = len(out) - 1
	}

	return out
}

// DisplayMisses writes one position per line and returns the uncovered statements it accounted for,
// which is zero exactly when nothing was written. Same shape as DisplayTree, so the misses command
// is one selection rather than a second path through the report.
//
// `file:line:col: message` is the GNU compiler format
// (https://www.gnu.org/prep/standards/html_node/Errors.html). Both halves are load-bearing: a range
// is not a location to that format and is dropped silently, and without a trailing message vim and
// Emacs fall back to `file:line:message` and read the column as the text.
//
// The count is the message because the position cannot carry it — one untaken branch and a whole
// untested function look alike without it. The column is cmd/cover's, a byte offset, which is what
// go vet prints; Emacs wants display width, and docs/reference.md names the setting.
func DisplayMisses(w io.Writer, tree *PathTree, opts Options) (int, error) {
	buf := bufio.NewWriter(w)

	listed := 0

	for _, m := range Misses(tree, opts) {
		// One definition of the format serves this and --exclude's matching, so a position pastes
		// back as a pattern. It matches the block opening there, not the region, and unanchored —
		// a.go:9:2 also matches a.go:9:24. docs/reference.md covers the round trip.
		_, _ = buf.WriteString(position(m.File, m.Line, m.Col))
		_, _ = buf.WriteString(": ")
		_, _ = buf.WriteString(strconv.Itoa(m.Statements))
		_, _ = buf.WriteString(" uncovered\n")

		listed += m.Statements
	}

	//nolint:wrapcheck // the writer's own error; this adds no context the caller lacks.
	return listed, buf.Flush()
}
