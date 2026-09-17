package prettycov

import (
	"strconv"
	"strings"
)

// CoverageStats is a count of statements, split by whether the tests reached them. Statements, not
// lines: one line can hold several, and cmd/cover counts what it compiled.
type CoverageStats struct {
	Covered   int
	Uncovered int
}

// Total is the statements a node holds, covered or not. Named, because --counts prints the fraction
// the percentage stands for and a second expression could divide by a different number.
//
// No overflow check: this is the raw sum, and Percentage refuses one that has wrapped.
func (c CoverageStats) Total() int { return c.Covered + c.Uncovered }

// Plus is these statements and another's. Returns rather than mutates, so a caller cannot hold one
// that changes under it.
func (c CoverageStats) Plus(other CoverageStats) CoverageStats {
	return CoverageStats{Covered: c.Covered + other.Covered, Uncovered: c.Uncovered + other.Uncovered}
}

// Percentage reports the share of statements covered. False is not 0%; there is nothing to report.
// Derived rather than stored, or it could disagree with the counts beside it.
func (c CoverageStats) Percentage() (Percentage, bool) {
	total := c.Total()

	// Breaking either invariant means the sum overflowed, which a profile can arrange with blocks of
	// billions of statements. One printed "-461168601842738790400.00", another 100.00.
	if total <= 0 || c.Covered < 0 || c.Covered > total {
		return Percentage{}, false
	}

	return Percentage{
		value:    float64(c.Covered) / float64(total) * 100,
		complete: c.Uncovered == 0,
	}, true
}

// AtLeast reports whether this much is covered, which is not always what comparing the ratio says:
// at 100 the question is whether an uncovered statement is left, since 2^56-1 covered beside one
// uncovered divides to exactly 100.0 in float64.
//
// Nothing to cover is not "at least anything". Nothing above 100 can be asked, since Threshold is
// parsed.
func (c CoverageStats) AtLeast(bar Threshold) bool {
	share, ok := c.Percentage()
	if !ok {
		return false
	}

	// Exactly 100, since nothing above it can be asked.
	if bar.value == 100 {
		return share.complete
	}

	return share.Float() >= bar.value
}

// Percentage is a share of statements covered. Build one with CoverageStats.Percentage. Do not
// declare one and use it: the zero value renders 0.00, a real and terrible figure rather than a
// visible mistake.
type Percentage struct {
	value float64
	// complete is a fact about the counts, not about value: 99.9986% rounds to 100.00 either way.
	complete bool
}

// Float is the unrounded percentage, for comparing against a threshold.
func (p Percentage) Float() float64 { return p.value }

// String renders two decimals, and never 100.00 for code that is not fully covered. Rounding would
// print it for 73999 of 74000, and 100% is what stops someone writing another test.
func (p Percentage) String() string {
	text := strconv.FormatFloat(p.value, 'f', 2, 64)
	if text == "100.00" && !p.complete {
		return "99.99"
	}

	return text
}

// FileCoverage is one file of a profile: its path as the profile spells it, and what the tests
// reached in it. ParseProfile returns these; a caller can also build them to use Exclude or Process
// on coverage from somewhere else.
type FileCoverage struct {
	File     string
	Coverage CoverageStats
	// Blocks is where the file's statements are, in profile order. Optional, since Coverage is the sum
	// and stays the authority. Exclude matches against these, and Process carries them onto the
	// tree's leaves, which is what lets Misses say where rather than only how much.
	Blocks []Block
}

// Block is one of a file's basic blocks. Exactly one side of Coverage is non-zero, since a block is
// run or not run and never partly. The position is where cmd/cover opens it, so `if !ok {` on line 32
// owns the `return` on line 33.
//
// Line and Col are its identity, and all --exclude matches against. EndLine is what Misses folds on.
type Block struct {
	Line, Col int
	EndLine   int
	Coverage  CoverageStats
}

// at names the block as a compiler names a position, and again without the column, so "a.go:3$"
// anchors on the line a reader would leave the column off. Neither depends on the pattern, so a
// caller builds them once per block. Sliced, not built twice: 300,000 fewer allocations.
func (b Block) at(file string) (withCol, toLine string) {
	withCol = position(file, b.Line, b.Col)

	return withCol, withCol[:strings.LastIndexByte(withCol, ':')]
}

// position names a place in a file as a compiler does. One spelling, because --exclude matches
// against it and the misses command prints it, so a position pastes back as a pattern.
func position(file string, line, col int) string {
	return file + ":" + strconv.Itoa(line) + ":" + strconv.Itoa(col)
}

// Process turns per-file coverage into a tree where every node reports its own statements plus
// everything beneath it. The files argument is not modified.
//
// The profile's files are the leaves, so a report can be checked by adding it up. Whether file rows
// are drawn is Options.Files. Renaming a root is Shorten's, so a caller can see whether it did
// anything.
func Process(files []FileCoverage) *PathTree {
	tree, nodes := &PathTree{}, &arena{}
	for _, f := range files {
		tree.add(f.File, f.Coverage, f.Blocks, nodes)
	}

	rollUp(tree)

	return tree
}

// rollUp gives every node the statements beneath it, counted once. Only files arrive carrying
// statements, so a directory's total is entirely this sum. Counting its own as well grew a node's
// totals by a factor of its child count. In place: Process owns the tree until this returns.
func rollUp(node *PathTree) CoverageStats {
	for _, file := range node.Files {
		node.Coverage = node.Coverage.Plus(rollUp(file))
	}

	for _, child := range node.Children {
		node.Coverage = node.Coverage.Plus(rollUp(child))
	}

	return node.Coverage
}

// Shorten rewrites the leading oldRoot of each path to newRoot and reports how many it renamed. The
// files argument is never modified; with no rename asked for, the same slice comes back uncopied.
//
// The count is why this is its own step: a root naming no package rewrites nothing, which otherwise
// looks exactly like asking for no rename.
//
// The prefix must be leading and end on a separator. Matching anywhere rewrote
// "github.com/rapid/api" for --old=api, and a bare prefix rewrote "github.com/foobar" to "xbar".
// All trailing separators are trimmed, not one: `--old=$(MODULE)/` can spell "example.com/m//".
// Only trailing: a leading one is part of an absolute root.
func Shorten(files []FileCoverage, oldRoot, newRoot string) ([]FileCoverage, int) {
	oldRoot = strings.TrimRight(oldRoot, "/")
	if oldRoot == "" || newRoot == "" {
		return files, 0
	}

	shortened := make([]FileCoverage, len(files))
	renamed := 0

	for i, item := range files {
		if rest, ok := under(item.File, oldRoot); ok {
			item.File = newRoot + rest
			renamed++
		}

		shortened[i] = item
	}

	return shortened, renamed
}

// HasRoot reports whether any file sits under root, by the rule Shorten renames by. Shorten answers
// the same question, but only by building the renamed slice; this stops at the first match and
// allocates nothing. Exported so the rule lives in one place rather than being restated.
func HasRoot(files []FileCoverage, root string) bool {
	root = strings.TrimRight(root, "/")
	if root == "" {
		return false
	}

	for _, item := range files {
		if _, ok := under(item.File, root); ok {
			return true
		}
	}

	return false
}

// under returns the part of path below root, and whether it is below it at all. The one place the
// rule is written: leading, and ending on a separator, for the reasons Shorten sets out.
func under(path, root string) (string, bool) {
	rest, found := strings.CutPrefix(path, root)

	return rest, found && strings.HasPrefix(rest, "/")
}
