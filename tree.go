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

// put grafts value onto the node at key, creating the nodes along the way. Unexported: a tree is
// built by Process from a profile, and there is no reason to assemble one by hand. Get is the
// half a caller needs.
func (n *PathTree) put(key string, value CoverageStats) {
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

// Get returns the node at key, or nil if the tree has no such path.
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
