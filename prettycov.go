package prettycov

import (
	"fmt"
	"path"
	"strings"
)

type CoverageStats struct {
	Covered   int
	Uncovered int
}

// Percentage reports the share of statements covered. ok is false when there are none to cover,
// which is not 0% — there is nothing to report.
//
// Derived rather than stored: a stored percentage can disagree with the counts beside it, which is
// exactly how the roll-up used to go wrong.
func (c CoverageStats) Percentage() (Percentage, bool) {
	total := c.Covered + c.Uncovered

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

// Float is the unrounded ratio, for comparing against a threshold.
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
}

// Process turns per-file coverage into a tree in which every node reports its own statements plus
// those of everything beneath it. The files argument is not modified.
func Process(files []FileCoverage, curRoot, newRoot string) *PathTree {
	tree := &PathTree{}
	for pkg, stats := range mergePackages(shortenPaths(files, curRoot, newRoot)) {
		tree.put(pkg, stats)
	}

	return rollUp(tree)
}

// rollUp returns a copy of n in which every node's coverage is its own statements plus those of
// each descendant, counted exactly once. A directory can be both a package and the parent of
// packages, so the two contributions are summed rather than conflated: that conflation is what
// made a node's totals grow by a factor of its child count.
func rollUp(node *PathTree) *PathTree {
	covered, uncovered := node.Coverage.Covered, node.Coverage.Uncovered

	var children map[string]*PathTree

	if len(node.Children) > 0 {
		children = make(map[string]*PathTree, len(node.Children))

		for name, child := range node.Children {
			rolled := rollUp(child)
			children[name] = rolled

			covered += rolled.Coverage.Covered
			uncovered += rolled.Coverage.Uncovered
		}
	}

	return &PathTree{
		Coverage: CoverageStats{
			Covered:   covered,
			Uncovered: uncovered,
		},
		Children: children,
		isPkg:    node.isPkg,
	}
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

// mergePackages totals each file's statements against the directory that holds it. Totalling by
// filename first would change nothing: addition is associative, so grouping by name and then by
// directory gives the same per-directory totals as grouping by directory alone.
func mergePackages(files []FileCoverage) map[string]CoverageStats {
	packages := make(map[string]CoverageStats, len(files))

	for _, f := range files {
		pkg := path.Dir(f.File)

		stats := packages[pkg]
		stats.Covered += f.Coverage.Covered
		stats.Uncovered += f.Coverage.Uncovered
		packages[pkg] = stats
	}

	return packages
}
