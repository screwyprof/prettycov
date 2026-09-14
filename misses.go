package prettycov

import (
	"bufio"
	"cmp"
	"io"
	"slices"
	"strconv"
)

// Miss is a run of statements the tests never reached, in one file.
//
// One Miss per contiguous region rather than per block. cmd/cover emits a block per branch, so a
// function nothing covers arrives as many of them, each a line or two long and each abutting the
// next: 2,610 uncovered blocks on a profile at 1.7% coverage fold into 1,344 regions.
//
// The gain is all in the badly covered case, which is the one that needed it. At high coverage the
// two counts are nearly equal — misses there are scattered single statements, untaken error
// branches, with nothing adjacent to fold — so this is 4% on a profile at 91% and 0% on one at 99%.
//
// Line and Col are where cmd/cover opens the first block of the region, which is where a reader
// wants to be taken: `if !ok {` rather than the `return` inside it. That is also the one spelling a
// compiler-style consumer understands — `file:line:col` is what go vet emits and what an editor's
// error format parses, where a range is not a location and is silently dropped.
//
// EndLine is how far the region runs, for a consumer that does speak ranges: GitHub's annotations
// take endLine, and so do LSP diagnostics. Statements is how much is in it, which the positions
// cannot say and which decides whether a region is one branch or a whole function.
type Miss struct {
	File       string
	Line, Col  int
	EndLine    int
	Statements int
}

// Misses lists the statements the tests never reached, ordered by position so the list is diffable
// between runs and reads down a file the way the file does.
//
// It reads the same options Rows does and answers them the same way, being the same traversal:
// -depth decides which packages are visited and so which misses are listed, and -hide-covered
// leaves out the ones already at the bar. -files is not among them — a miss is a file position
// whether or not a file is drawn as a row.
//
// Which means the list narrows with -depth the way the tree does *with -files*, not the way the
// default tree does. Asking for files is also what lets collapse merge a package holding one into a
// single row, so `-misses -depth=2` reaches m/deep/deeper/b.go where `-depth=2` alone draws the
// deeper package and stops. Same traversal, one option set differently, and that option is the
// one the list has no choice about.
//
// -exclude and a renamed root are not read here. They act on the profile before the tree is built,
// so an excluded block is not a miss and a shortened path is what these carry — which is what makes
// them useful, since the profile's own module paths do not resolve on disk and `-new=.` makes them
// repository-relative.
//
// A block declaring no statements is not a miss. cmd/cover emits them, and there is nothing in one
// to cover: three of delegator's thirty-five uncovered blocks are of that kind, leaving 32.
func Misses(tree *PathTree, opts Options) []Miss {
	// Grown rather than sized. Counting the unrun blocks first saves twelve milliseconds and ninety
	// megabytes on a synthetic profile of 30,000 files at 5% coverage, and nothing on any real one —
	// the worst measured here was 1,344 regions, which is eleven regrowths. The count is only a
	// capacity, so nothing observes whether it is right, and a branch no test can be wrong about is
	// worse than the regrowths.
	var misses []Miss

	for _, d := range prepare(tree, opts, true) {
		misses = merge(misses, d.Path, d.Blocks)
	}

	// prepare hands these over in the order the report draws them, which is depth-first: a package's
	// own files come after the subpackages sorted above them. Flattening that into one list means
	// sorting it again, or b.go would follow deep/a.go and a reader would lose their place.
	slices.SortFunc(misses, func(x, y Miss) int {
		return cmp.Or(cmp.Compare(x.File, y.File), cmp.Compare(x.Line, y.Line), cmp.Compare(x.Col, y.Col))
	})

	return misses
}

// byPosition orders two blocks the way a file reads.
func byPosition(x, y Block) int {
	return cmp.Or(cmp.Compare(x.Line, y.Line), cmp.Compare(x.Col, y.Col))
}

// merge folds a file's uncovered blocks into the fewest regions that cover them, walking them in
// position order.
//
// Sorted first only when they are not already, which for a profile is never: cmd/cover writes them
// ascending and x/tools keeps that. But Process takes a caller's own FileCoverage, and one block
// listed before an earlier one folds into whatever region is open — a block on line 9 arriving after
// one that ended on 41 reads as abutting it, and two regions become one silently. Copied rather than
// sorted in place, so asking for the misses does not reorder the tree underneath the caller.
//
// Abutting means starting no later than the line after the last one ended, with nothing covered in
// between. Comparing against the end rather than the previous start is what does the work: a block
// spanning lines 44 to 51 reaches the one that opens on 52, where comparing openings would not. On a
// profile at 1.7% coverage that is 1,344 regions against 2,087.
//
// A covered block between two uncovered ones stops the fold, or the region would claim a statement
// the tests do reach. delegator's pgxstore/store.go has 44.35,46.3 unrun, 47.2,47.16 run and
// 47.16,49.3 unrun: line 47 opens where the region ended, so the two folded into 44-49 and a
// consumer that speaks ranges — a GitHub annotation, an LSP diagnostic — marked the covered line
// with them. Only the range was ever wrong: the statement counts were right either way, and the CLI
// prints the opening position alone, which is why nothing in the output showed it.
func merge(out []Miss, file string, blocks []Block) []Miss {
	if !slices.IsSortedFunc(blocks, byPosition) {
		// Cloned and sorted rather than collected from an iterator, which grows by doubling: one
		// allocation of the right size instead of fourteen.
		blocks = slices.Clone(blocks)
		slices.SortFunc(blocks, byPosition)
	}

	// Appended to the caller's slice rather than built and copied into it: one file's regions are
	// rarely many, and a profile's are.
	//
	// open is the region still able to take another block, or -1 for none. An index rather than a
	// line number and a length: it starts closed, so a region from the file before cannot be
	// extended into this one, and a covered block closes it outright rather than being remembered
	// and compared against every later block.
	open := -1

	for _, block := range blocks {
		// A block declaring no statements is neither covered nor unrun: there is nothing in it to
		// reach, so it is neither a miss nor a thing that separates two. cmd/cover emits one per
		// case expression of a type switch — delegator has nine, at subscriber.go:76-83 — and they
		// sit exactly where an untested switch's misses abut, so reading them as covered took the
		// regions of one apart.
		if block.Coverage.Total() == 0 {
			continue
		}

		if block.Coverage.Uncovered == 0 {
			// Closed by a covered block that starts at or past the region's end, which is what puts
			// it between this region and whatever comes next.
			//
			// Not by one that starts inside the region. cmd/cover nests blocks, and a region opened
			// by a single 10.2,20.x already spans its own whole range — closing there would leave
			// the next block to open a second region nested in the first, and nothing downstream
			// unpicks that. An enclosing run block opens at or before the region and is walked
			// before it exists, so it never reaches this at all.
			//
			// At or past, not past: two regions may share the line between them, and that is not
			// the same fault. A block's end is the coordinate after it, so `} else if d {` is the
			// end of one block and the start of the next on one line — 4.7,7.3 and 7.14,10.3 give
			// regions 4-7 and 7-10. Refusing to close there would fold them into 4-10 across the
			// covered condition between, which is the fault. Neither contains the other.
			if open >= 0 && block.Line >= out[open].EndLine {
				open = -1
			}

			continue
		}

		// Floored at the block's own opening. A profile always gives an end at or past it, but Block
		// is exported and EndLine is new, so a caller assembling its own FileCoverage — which is the
		// documented way to use Exclude — leaves it zero. That made an inverted Miss{Line: 62,
		// EndLine: 0}, which is the one field a range consumer reads and which GitHub rejects
		// outright, and it killed folding for such a caller besides: nothing starts at or before 1.
		end := max(block.Line, block.EndLine)

		// Line-1 rather than EndLine+1, which is the same test without the overflow: cover bounds no
		// line number, so a profile naming 9223372036854775807 wrapped the end to MinInt, failed the
		// abut test for every later block and returned regions nested inside the first one.
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

// DisplayMisses writes one position per line, as a compiler does, and reports how many uncovered
// statements it accounted for.
//
// The same shape as DisplayTree — (writer, tree, options) returning what it drew — so a caller picks
// a printer and calls it without a branch. That is the whole of the -misses mode: one selection,
// not a second path through the report. What each returns is its own unit, and all they promise in
// common is that it is zero exactly when nothing was written.
//
// Statements rather than lines, which is the number a caller can do something with. A tree says how
// much is uncovered in its top row whatever depth it is drawn at, so a reader can always tell a
// summary from the whole; a list has no such row, and eight positions look the same whether they are
// all of them or a quarter. Comparing this against the tree's own count is what answers that, and
// every region holds at least one statement, so it is still zero exactly when nothing was written.
//
// `file:line:col: message` is `sourcefile:lineno:column: message`, one of the two spellings the GNU
// coding standards give a compiler for naming a column, and the one go vet, gcc and golangci-lint
// all emit. See https://www.gnu.org/prep/standards/html_node/Errors.html. Every part of it is
// load-bearing:
//
//   - the position rather than the range. A range is not a location to that format and is dropped
//     without a word, taking the miss with it.
//   - the message. Without one the format cannot match and falls back to `file:line:message`, which
//     reads the column as the text: `a.go:62:2` opens line 62 at column 1, and the column is gone.
//     Vim tries %f:%l:%c:%m before %f:%l:%m and Emacs' gnu rule wants the same trailing colon, so
//     both need it. That is why go vet always has something to say.
//
// The statements are what the message says, because that is the number the position cannot carry —
// one uncovered branch and a whole untested function look alike until you see it.
//
// The column is whatever cmd/cover recorded, which go/token documents as a byte count with a tab
// worth one — https://pkg.go.dev/go/token#Position. That is what go vet prints and what
// golangci-lint byte-indexes to place its own caret, so this agrees with the tools a reader is
// already piping. It disagrees with the GNU text, which asks for display width with tab stops every
// 8 — and so with Emacs, whose compilation-error-screen-columns defaults to t. Converting would put
// prettycov alone among Go tools; the README names the setting instead.
func DisplayMisses(w io.Writer, tree *PathTree, opts Options) int {
	buf := bufio.NewWriter(w)

	listed := 0

	for _, m := range Misses(tree, opts) {
		// position, not a spelling of its own, so one definition of the format serves -exclude's
		// matching and this. The path is the drawn one, so a position pastes back as an -exclude
		// pattern for every path a Go repository actually holds — and for one carrying a rune a
		// terminal would obey it does not, which sanitize weighs and takes.
		//
		// What it matches is the block that opens there, not the region: this names the first
		// block of a fold and the count beside it is the whole region's, so excluding a position
		// reading "2 uncovered" takes one statement out and leaves the next block listed at its
		// own position. -exclude works in blocks, which is what makes a coordinate mean one thing.
		_, _ = buf.WriteString(position(m.File, m.Line, m.Col))
		_, _ = buf.WriteString(": ")
		_, _ = buf.WriteString(strconv.Itoa(m.Statements))
		_, _ = buf.WriteString(" uncovered\n")

		listed += m.Statements
	}

	_ = buf.Flush()

	return listed
}
