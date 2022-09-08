package prettycov

import (
	"fmt"
	"io"
	"strings"
)

func DisplayTree(w io.Writer, tree *PathTree, depth uint) {
	displayTree(w, tree, depth, " ", true, "")
}

func displayTree(w io.Writer, tree *PathTree, depth uint, padding string, root bool, key string) {
	curDepth := strings.Count(key, "/")
	if uint(curDepth) > depth {
		return
	}

	if tree == nil {
		return
	}

	index := 0
	for k, v := range tree.Children {
		_, _ = fmt.Fprintf(w, "%s%s - %.2f\n",
			padding+symbol(root, getBoxType(index, len(tree.Children))), k, v.Coverage.Ratio)
		displayTree(w, v, depth, padding+symbol(root, childSymbol(index, len(tree.Children))), false, key+"/"+k)
		index++
	}
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
