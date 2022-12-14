package prettycov

import "strings"

type Walker func(key string, value CoverageStats)

type PathTree struct {
	Coverage CoverageStats
	Children map[string]*PathTree
}

func (n *PathTree) Put(key string, value CoverageStats) bool {
	node := n
	isNew := false
	parts := strings.Split(key, "/")

	for _, part := range parts {
		child, ok := node.Children[part]
		if !ok {
			if node.Children == nil {
				node.Children = map[string]*PathTree{}
			}

			isNew = true
			child = &PathTree{}
			node.Children[part] = child
		}

		node = child
	}

	node.Coverage = value

	return isNew
}

func (n *PathTree) Get(key string) *PathTree {
	node := n
	parts := strings.Split(key, "/")

	for _, part := range parts {
		if node = node.Children[part]; node == nil {
			return nil
		}
	}

	return node
}

func (n *PathTree) Walk(walker Walker) {
	n.walk("", walker)
}

func (n *PathTree) walk(key string, walker Walker) {
	walker(key, n.Coverage)

	for part, child := range n.Children {
		child.walk(key+"/"+part, walker)
	}
}
