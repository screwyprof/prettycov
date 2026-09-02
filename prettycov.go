package prettycov

import (
	"path"
	"strings"
)

type CoverageStats struct {
	Covered   int
	Uncovered int
	Ratio     float64
}

type FileCoverage struct {
	File     string
	Coverage CoverageStats
}

type PkgCoverage struct {
	Pkg      string
	Coverage CoverageStats
}

// ratio returns the percentage of covered statements, or NaN when there are none.
func ratio(covered, uncovered int) float64 {
	return float64(covered) / float64(covered+uncovered) * 100
}

// Process turns per-file coverage into a tree in which every node reports its own statements plus
// those of everything beneath it. The files argument is not modified.
func Process(files []FileCoverage, curRoot, newRoot string) *PathTree {
	packages := mergePackages(mergeFiles(shortenPaths(files, curRoot, newRoot)))

	tree := &PathTree{}
	for _, pkg := range packages {
		tree.Put(pkg.Pkg, pkg.Coverage)
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
			Ratio:     ratio(covered, uncovered),
		},
		Children: children,
	}
}

func shortenPaths(items []FileCoverage, oldRoot, newRoot string) []FileCoverage {
	if newRoot == "" {
		return items
	}

	shortened := make([]FileCoverage, len(items))

	for i, item := range items {
		item.File = strings.Replace(item.File, oldRoot, newRoot, 1)
		shortened[i] = item
	}

	return shortened
}

func mergeFiles(files []FileCoverage) []FileCoverage {
	covered := map[string]int{}
	uncovered := map[string]int{}
	uniqueFiles := make(map[string]FileCoverage, len(files))

	for _, f := range files {
		covered[f.File] += f.Coverage.Covered
		uncovered[f.File] += f.Coverage.Uncovered
		uniqueFiles[f.File] = FileCoverage{File: f.File}
	}

	merged := make([]FileCoverage, 0, len(uniqueFiles))

	for _, f := range uniqueFiles {
		f.Coverage.Covered = covered[f.File]
		f.Coverage.Uncovered = uncovered[f.File]
		f.Coverage.Ratio = ratio(covered[f.File], uncovered[f.File])

		merged = append(merged, f)
	}

	return merged
}

func mergePackages(files []FileCoverage) []PkgCoverage {
	covered := map[string]int{}
	uncovered := map[string]int{}
	uniquePackages := make(map[string]PkgCoverage, len(files))

	for _, f := range files {
		pkg := path.Dir(f.File)

		covered[pkg] += f.Coverage.Covered
		uncovered[pkg] += f.Coverage.Uncovered
		uniquePackages[pkg] = PkgCoverage{Pkg: pkg}
	}

	merged := make([]PkgCoverage, 0, len(uniquePackages))

	for _, p := range uniquePackages {
		p.Coverage.Covered = covered[p.Pkg]
		p.Coverage.Uncovered = uncovered[p.Pkg]
		p.Coverage.Ratio = ratio(covered[p.Pkg], uncovered[p.Pkg])

		merged = append(merged, p)
	}

	return merged
}
