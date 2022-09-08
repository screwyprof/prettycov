package prettycov

import (
	"path"
	"sort"
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

func Process(files []FileCoverage, curRoot, newRoot string) *PathTree {
	shortenPaths(files, curRoot, newRoot)

	files = mergeFiles(files)
	packages := mergePackages(files)

	return process(packages)
}

func shortenPaths(items []FileCoverage, oldRoot, newRoot string) {
	if newRoot == "" {
		return
	}

	for i := range items {
		items[i].File = strings.Replace(items[i].File, oldRoot, newRoot, 1)
	}
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
		f.Coverage.Ratio = float64(covered[f.File]) / float64(covered[f.File]+uncovered[f.File]) * 100

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
		p.Coverage.Ratio = float64(covered[p.Pkg]) / float64(covered[p.Pkg]+uncovered[p.Pkg]) * 100

		merged = append(merged, p)
	}

	return merged
}

func process(items []PkgCoverage) *PathTree {
	tree := &PathTree{}

	populateTree(items, tree)
	itemsMap := populateItemsMap(items)
	addMissingParents(items, itemsMap, tree)

	items = populateItems(itemsMap)
	sortByDepth(items)

	for _, item := range items {
		merge(tree, item.Pkg)
	}

	return tree
}

func populateTree(packages []PkgCoverage, tree *PathTree) {
	for _, p := range packages {
		tree.Put(p.Pkg, p.Coverage)
	}
}

func populateItemsMap(items []PkgCoverage) map[string]PkgCoverage {
	d := make(map[string]PkgCoverage, len(items))
	for _, item := range items {
		d[item.Pkg] = item
	}

	return d
}

func addMissingParents(items []PkgCoverage, itemsMap map[string]PkgCoverage, tree *PathTree) {
	var curPath string

	for _, item := range items {
		parts := strings.Split(item.Pkg, "/")

		for i := 1; i < len(parts); i++ {
			curPath = strings.Join(parts[:i], "/")
			if _, ok := itemsMap[curPath]; !ok {
				itemsMap[curPath] = PkgCoverage{Pkg: curPath}

				if n := tree.Get(curPath); n != nil {
					n.Coverage.Ratio = -1.
				}
			}
		}
	}
}

func populateItems(itemsMap map[string]PkgCoverage) []PkgCoverage {
	items := make([]PkgCoverage, 0, len(itemsMap))
	for _, item := range itemsMap {
		items = append(items, item)
	}

	return items
}

func sortByDepth(items []PkgCoverage) {
	sort.Slice(items, func(i, j int) bool {
		f1 := strings.Count(items[i].Pkg, "/")
		f2 := strings.Count(items[j].Pkg, "/")

		return f1 > f2
	})
}

func merge(tree *PathTree, leaf string) {
	pkg := path.Dir(leaf)

	parent := tree.Get(pkg)
	if parent == nil {
		return
	}

	var (
		covered   int
		uncovered int
		ratio     float64
	)

	if parent.Children != nil {
		for _, child := range parent.Children {
			covered += child.Coverage.Covered
			uncovered += child.Coverage.Uncovered
		}

		ratio = float64(covered) / float64(covered+uncovered) * 100
	}

	stats := CoverageStats{
		Covered:   covered,
		Uncovered: uncovered,
		Ratio:     ratio,
	}

	if parent.Coverage.Ratio >= 0 {
		stats = CoverageStats{
			Covered:   parent.Coverage.Covered + covered,
			Uncovered: parent.Coverage.Uncovered + uncovered,
			Ratio:     float64(parent.Coverage.Covered) / float64(parent.Coverage.Covered+parent.Coverage.Uncovered) * 100,
		}
	}

	parent.Coverage = stats
}
