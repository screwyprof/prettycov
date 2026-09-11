package prettycov

import (
	"fmt"
	"strconv"
	"strings"
)

type CoverageStats struct {
	Covered   int
	Uncovered int
}

// Total is the statements a node holds, covered or not. Named rather than added up at each use,
// because it is the denominator of the percentage beside it and the two must be the same number:
// -counts prints the fraction the percentage stands for, so a second expression for it could drift
// from the one Percentage divides by.
//
// No overflow check: this is the raw sum, and Percentage is what refuses one that has wrapped.
func (c CoverageStats) Total() int { return c.Covered + c.Uncovered }

// Add takes in another node's statements. Both sides move together or the percentage is drawn from
// counts that were never summed the same way.
func (c *CoverageStats) Add(other CoverageStats) {
	c.Covered += other.Covered
	c.Uncovered += other.Uncovered
}

// Percentage reports the share of statements covered. The bool is false when there are none to
// cover, which is not 0% — there is nothing to report.
//
// Derived rather than stored: a stored percentage can disagree with the counts beside it, which is
// exactly how the roll-up used to go wrong.
func (c CoverageStats) Percentage() (Percentage, bool) {
	total := c.Total()

	// Both counts are statement totals, so they are non-negative and covered is at most total.
	// Breaking either means the sum overflowed, which a profile can arrange by declaring blocks
	// of billions of statements. Report nothing rather than a number: one such profile printed
	// "-461168601842738790400.00", and another 100.00, its uncovered statements having wrapped
	// past zero and taken the shortfall with them.
	if total <= 0 || c.Covered < 0 || c.Covered > total {
		return Percentage{}, false
	}

	return Percentage{
		value:    float64(c.Covered) / float64(total) * 100,
		complete: c.Uncovered == 0,
	}, true
}

// Percentage is a share of statements covered. Build one with CoverageStats.Percentage, which
// reports whether there was anything to cover; a Percentage that came from there always has a
// number to show, so no caller carries that question further.
//
// The zero value is not one of those and means nothing — it renders 0.00, which is a real and
// terrible coverage figure rather than a visible mistake. Do not declare a Percentage and use it.
type Percentage struct {
	value float64
	// complete is carried rather than derived from value, because whether every statement is
	// covered is a fact about the counts and 99.9986% rounds to 100.00 either way.
	complete bool
}

// Float is the unrounded percentage, for comparing against a threshold.
func (p Percentage) Float() float64 { return p.value }

// String renders the ratio to two decimals, and never reads 100.00 for code that is not fully
// covered. Rounding to nearest would print 100.00 for 73999 of 74000 statements, and 100% is what
// a badge shows and what stops someone writing another test.
func (p Percentage) String() string {
	text := fmt.Sprintf("%.2f", p.value)
	if text == "100.00" && !p.complete {
		return "99.99"
	}

	return text
}

type FileCoverage struct {
	File     string
	Coverage CoverageStats
	// Blocks is where the file's statements are, in the order the profile listed them. Optional:
	// a FileCoverage assembled by a caller may leave it empty. Coverage is the sum and stays the
	// authority; only Exclude reads this.
	Blocks []Block
}

// Block is one of a file's basic blocks: where it starts, and the statements it holds. Exactly one
// side of Coverage is non-zero — a block is run or not run, never partly.
//
// The position is where cmd/cover opens the block, which is not where a reader would point:
// `if !ok {` on line 32 owns the `return` on line 33.
type Block struct {
	Line, Col int
	Coverage  CoverageStats
}

// at names the block the way a compiler names a position, and again without the column. -exclude
// matches a pattern against both, so "a.go:3$" anchors on line 3 rather than never matching: the
// column is what a reader leaves off, and a pattern ending at the line has nowhere to stop without
// this.
//
// Neither spelling depends on the pattern, so callers build them once per block and ask every
// pattern about the pair.
func (b Block) at(file string) (withCol, toLine string) {
	toLine = file + ":" + strconv.Itoa(b.Line)

	return toLine + ":" + strconv.Itoa(b.Col), toLine
}

// Process turns per-file coverage into a tree in which every node reports its own statements plus
// those of everything beneath it. The files argument is not modified.
//
// The profile's files are the leaves, so a directory's total is exactly the sum of what hangs
// below it and a report can be checked by adding it up. Whether the file rows are drawn is
// Options.Files; whether they exist is not a rendering question.
func Process(files []FileCoverage, curRoot, newRoot string) *PathTree {
	tree := &PathTree{}
	for _, f := range shortenPaths(files, curRoot, newRoot) {
		tree.add(f.File, f.Coverage)
	}

	rollUp(tree)

	return tree
}

// rollUp gives every node the statements of everything beneath it, counted exactly once, and
// reports the node's own new total. Only the profile's files arrive carrying statements, so a
// directory's total is entirely this sum: counting its own as well is what made a node's totals
// grow by a factor of its child count.
//
// In place, because Process builds the tree and the tree never leaves it before this runs. The
// copy this used to return doubled a node count that the profile's files, now leaves of their own,
// had already multiplied several times over.
func rollUp(node *PathTree) CoverageStats {
	for _, below := range []map[string]*PathTree{node.Files, node.Children} {
		for _, child := range below {
			node.Coverage.Add(rollUp(child))
		}
	}

	return node.Coverage
}

// shortenPaths rewrites the leading oldRoot of each path to newRoot. It has to be leading, and it
// has to end on a separator: replacing the first match anywhere rewrote "github.com/rapid/api" to
// "github.com/rcored/api" for -old=api, and a bare prefix rewrote the unrelated
// "github.com/foobar" to "xbar" for -old=github.com/foo. An empty oldRoot matches at position 0,
// so -new alone prepended itself to every path instead of replacing anything. The separator is
// implied, so a trailing slash on oldRoot is trimmed rather than left to fail every match.
func shortenPaths(items []FileCoverage, oldRoot, newRoot string) []FileCoverage {
	oldRoot = strings.TrimSuffix(oldRoot, "/")
	if oldRoot == "" || newRoot == "" {
		return items
	}

	shortened := make([]FileCoverage, len(items))

	for i, item := range items {
		if rest, found := strings.CutPrefix(item.File, oldRoot); found && strings.HasPrefix(rest, "/") {
			item.File = newRoot + rest
		}

		shortened[i] = item
	}

	return shortened
}
