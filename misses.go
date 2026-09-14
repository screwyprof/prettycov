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

	// Sorted by position, so the list is diffable between runs and reads down a file the way the
	// file does. Map order over Files is randomised, and a caller pipes this into a tool that will
	// not sort it back.
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
	start := len(out)

	// Where the last covered block begins. cmd/cover nests them — a run block spans the branch
	// blocks inside it — so one that opened before the region did says nothing about what is between
	// the region and the next block, and only a start at or past the region's end can bridge them.
	covered := 0

	for _, block := range blocks {
		if block.Coverage.Uncovered == 0 {
			covered = max(covered, block.Line)

			continue
		}

		last := len(out) - 1
		if last >= start && block.Line <= out[last].EndLine+1 && covered < out[last].EndLine {
			out[last].EndLine = max(out[last].EndLine, block.EndLine)
			out[last].Statements += block.Coverage.Uncovered

			continue
		}

		out = append(out, Miss{
			File:       file,
			Line:       block.Line,
			Col:        block.Col,
			EndLine:    block.EndLine,
			Statements: block.Coverage.Uncovered,
		})
	}

	return out
}

// DisplayMisses writes one position per line, as a compiler does, and reports how many it wrote.
//
// The same shape as DisplayTree — (writer, tree, options) returning what it drew — so a caller picks
// a printer and calls it without knowing which it holds. That is the whole of the -misses mode: one
// selection, not a second path through the report.
//
// `file:line:col: message`, which is the shape go vet emits and an editor's error format parses.
// Every part of it is load-bearing:
//
//   - the position rather than the range. A range is not a location to that format and is dropped
//     without a word, taking the miss with it.
//   - the message. Without one the format cannot match and falls back to `file:line:message`, which
//     reads the column as the text: `a.go:62:2` opens line 62 at column 1, and the column is gone.
//     That is why go vet always has something to say.
//
// The statements are what the message says, because that is the number the position cannot carry —
// one uncovered branch and a whole untested function look alike until you see it.
func DisplayMisses(w io.Writer, tree *PathTree, opts Options) int {
	buf := bufio.NewWriter(w)

	misses := Misses(tree, opts)
	for _, m := range misses {
		// position, not a spelling of its own: -exclude matches its patterns against exactly this,
		// so the two have to agree for a line printed here to work as a pattern there.
		_, _ = buf.WriteString(position(m.File, m.Line, m.Col))
		_, _ = buf.WriteString(": ")
		_, _ = buf.WriteString(strconv.Itoa(m.Statements))
		_, _ = buf.WriteString(" uncovered\n")
	}

	_ = buf.Flush()

	return len(misses)
}
