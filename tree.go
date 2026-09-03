package prettycov

import "strings"

type PathTree struct {
	Coverage CoverageStats
	Children map[string]*PathTree

	// isPkg marks a node the profile named directly, as opposed to one created only to hold a
	// child. It cannot be inferred from Coverage: a package whose files declare no statements
	// contributes nothing, so its totals equal its child's.
	isPkg bool
}

func (n *PathTree) Put(key string, value CoverageStats) {
	node := n
	parts := strings.SplitSeq(key, "/")

	for part := range parts {
		child, ok := node.Children[part]
		if !ok {
			if node.Children == nil {
				node.Children = map[string]*PathTree{}
			}

			child = &PathTree{}
			node.Children[part] = child
		}

		node = child
	}

	node.Coverage = value
	node.isPkg = true
}

func (n *PathTree) Get(key string) *PathTree {
	node := n
	parts := strings.SplitSeq(key, "/")

	for part := range parts {
		if node = node.Children[part]; node == nil {
			return nil
		}
	}

	return node
}
