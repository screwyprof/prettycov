package prettycov

import (
	"fmt"
	"io"
	"maps"
	"slices"
)

// DisplayTree writes tree as an indented report, depth levels deep, counting levels the way
// `tree -L` does. A collapsed run of directories is the one row it renders as.
func DisplayTree(w io.Writer, tree *PathTree, depth uint) {
	displayTree(w, tree, depth, " ", true)
}

func displayTree(w io.Writer, tree *PathTree, depth uint, padding string, root bool) {
	if tree == nil || depth == 0 {
		return
	}

	// Sorted, because this output gets diffed between runs and map order is randomised.
	names := slices.Sorted(maps.Keys(tree.Children))

	for i, name := range names {
		label, node := collapse(name, tree.Children[name])

		_, _ = fmt.Fprintf(w, "%s%s - %s\n",
			padding+symbol(root, getBoxType(i, len(names))), label, formatRatio(node.Coverage))
		displayTree(w, node, depth-1, padding+symbol(root, childSymbol(i, len(names))), false)
	}
}

// collapse folds a run of directories that each hold nothing but the next one into a single row,
// so a module path does not spend three levels on "github.com", "owner", "repo" before reaching
// anything worth reading. A directory that is a package in its own right is never folded away:
// it contributes statements, so its totals differ from its child's.
func collapse(label string, node *PathTree) (string, *PathTree) {
	for len(node.Children) == 1 {
		name := slices.Collect(maps.Keys(node.Children))[0]

		child := node.Children[name]
		if child.Coverage != node.Coverage {
			break
		}

		label, node = label+"/"+name, child
	}

	return label, node
}

// formatRatio renders a package with no statements as "n/a" rather than a percentage. It used to
// print "NaN", which is what 0/0 produces in float division.
func formatRatio(stats CoverageStats) string {
	pct, ok := stats.Ratio()
	if !ok {
		return "n/a"
	}

	return fmt.Sprintf("%.2f", pct)
}

type BoxType int

const (
	Regular BoxType = iota
	Last
	AfterLast
	Between
)

func (boxType BoxType) String() string {
	switch boxType {
	case Regular:
		return "\u251c" // ├
	case Last:
		return "\u2514" // └
	case AfterLast:
		return " "
	case Between:
		return "\u2502" // │
	default:
		panic("invalid box type")
	}
}

func getBoxType(index int, length int) BoxType {
	if index+1 == length {
		return Last
	} else if index+1 > length {
		return AfterLast
	}

	return Regular
}

func childSymbol(index int, length int) BoxType {
	if index+1 == length {
		return AfterLast
	}

	return Between
}

func symbol(root bool, boxType BoxType) string {
	if root {
		return ""
	}

	return boxType.String() + " "
}
