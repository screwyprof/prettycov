package prettycov

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
)

var ErrInvalidLineFormat = errors.New("invalid line format")

type Coverage struct {
	Items []CoverageItem
	Total float64
}

type CoverageItem struct {
	File     string
	Coverage float64
}

func Cover(profile string) (io.Reader, error) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	cmd := exec.Command("go", "tool", "cover", "-func", profile)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout, nil
}

func CoverHTML(profile string) (io.Reader, error) {
	stderr := &bytes.Buffer{}

	tmp, err := os.CreateTemp(os.TempDir(), "coverage-*.html")
	if err != nil {
		return nil, fmt.Errorf("cannot create temp file: %w", err)
	}

	output := tmp.Name()

	cmd := exec.Command("go", "tool", "cover", "-html", profile, "-o", output)
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}

	f, err := os.ReadFile(output)
	if err != nil {
		return nil, fmt.Errorf("cannot read coverage.html: %w", err)
	}

	if err := os.Remove(output); err != nil {
		return nil, fmt.Errorf("cannot remove temp file: %w", err)
	}

	return bytes.NewReader(f), nil
}

func Process(items []CoverageItem, curRoot, newRoot string) *PathTree {
	items = mergeByPackage(items)
	shortenPaths(items, curRoot, newRoot)

	return process(items)
}

func mergeByPackage(items []CoverageItem) []CoverageItem {
	coverage := map[string]float64{}
	count := map[string]int{}

	for _, item := range items {
		pkg := path.Dir(item.File)
		coverage[pkg] += item.Coverage
		count[pkg]++
	}

	res := make([]CoverageItem, 0, len(items))

	for pkg, cov := range coverage {
		res = append(res, CoverageItem{File: pkg, Coverage: cov / float64(count[pkg])})
	}

	return res
}

func shortenPaths(items []CoverageItem, oldRoot, newRoot string) {
	if newRoot == "" {
		return
	}

	for i := range items {
		items[i].File = strings.Replace(items[i].File, oldRoot, newRoot, 1)
	}
}

func process(items []CoverageItem) *PathTree {
	tree := &PathTree{}

	populateTree(items, tree)
	itemsMap := populateItemsMap(items)
	addMissingParents(items, itemsMap, tree)

	items = populateItems(itemsMap)
	sortByDepth(items)

	for _, item := range items {
		merge(tree, item.File)
	}

	return tree
}

func populateTree(items []CoverageItem, tree *PathTree) {
	for _, item := range items {
		if item.Coverage != 0.0 { // default golang behaviour
			tree.Put(item.File, item.Coverage)
		}
	}
}

func populateItemsMap(items []CoverageItem) map[string]CoverageItem {
	d := make(map[string]CoverageItem, len(items))
	for _, item := range items {
		d[item.File] = item
	}

	return d
}

func addMissingParents(items []CoverageItem, itemsMap map[string]CoverageItem, tree *PathTree) {
	var curPath string

	for _, item := range items {
		parts := strings.Split(item.File, "/")

		for i := 1; i < len(parts); i++ {
			curPath = strings.Join(parts[:i], "/")
			if _, ok := itemsMap[curPath]; !ok {
				itemsMap[curPath] = CoverageItem{File: curPath}

				n := tree.Get(curPath)
				n.Value = -1.
			}
		}
	}
}

func populateItems(itemsMap map[string]CoverageItem) []CoverageItem {
	items := make([]CoverageItem, 0, len(itemsMap))
	for _, item := range itemsMap {
		items = append(items, item)
	}

	return items
}

func sortByDepth(items []CoverageItem) {
	sort.Slice(items, func(i, j int) bool {
		f1 := strings.Count(items[i].File, "/")
		f2 := strings.Count(items[j].File, "/")

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
		total float64
		count int
	)

	if parent.Children != nil {
		for _, child := range parent.Children {
			count++

			total += child.Value
		}
	}

	if parent.Value >= 0 {
		count++

		total += parent.Value
	}

	parent.Value = total / float64(count)
}
