package prettycov

import (
	"path"
	"strings"
)

// PathTree is a directory and what the profile says about it: the files it holds, the directories
// below it, and the statements of everything under both once Process has rolled them up.
//
// Two maps rather than one, because a name can be a file and a directory at once — a profile
// naming "m/a.go" and "m/a.go/b.go" describes both, which no filesystem allows but merging two
// profiles can produce. One namespace made that a single node standing for two things, and every
// question about it had to be answered with a flag: whether it counted as a package, whether it
// counted as a file, whether it could be folded away. Here it is simply two nodes.
type PathTree struct {
	Coverage CoverageStats
	// Children is the directories below this one. Files is what the profile named here directly.
	// A node in Files never has anything below it; a directory of the same name is in Children.
	Children map[string]*PathTree
	Files    map[string]*PathTree
}

// add grafts one of the profile's files onto the tree, creating the directories along the way.
// Unexported: a tree is built by Process from a profile, and there is no reason to assemble one by
// hand. Get is the half a caller needs.
//
// Only the file carries the statements. Putting them on the directory as well — which is what
// totalling per directory before building the tree amounts to — makes rollUp count every statement
// twice, once on the directory and once beneath it.
func (n *PathTree) add(file string, stats CoverageStats) {
	// Split with path.Dir rather than by counting components, so a file with no directory at all
	// still lands somewhere: path.Dir gives it ".", which is the row it renders as. Reading the
	// directory off the second-to-last component instead left such a file hanging under the tree
	// root, which nothing draws — `prettycov -new=.` printed an empty report and exited 0.
	//
	// path.Dir cleans on the way, which the walk below relies on: splitting a path is not the same
	// as walking one, and "m//a/b.go" would otherwise give an empty component and read "m//a".
	// The trailing separator goes first, because splitting a string that is only the separator
	// yields two empty components where the filesystem root is one node: "/a.go" gave the root a
	// child of the same nameless kind, and both drew as a blank label. Every other directory
	// path.Dir returns has no trailing slash, so this touches nothing else.
	dir := n
	for part := range strings.SplitSeq(strings.TrimSuffix(path.Dir(file), "/"), "/") {
		dir = child(&dir.Children, part)
	}

	leaf := child(&dir.Files, path.Base(file))
	// Accumulated, not assigned, so a file named twice adds up rather than keeping the last one.
	// ParseProfile cannot deliver that — x/tools keys profiles by filename and merges their blocks
	// — so this is for a caller handing Process a slice of its own.
	leaf.Coverage.Covered += stats.Covered
	leaf.Coverage.Uncovered += stats.Uncovered
}

// child returns the node called name in the given map, creating both if this is the first time it
// is named. The map is taken by pointer so the nil one a fresh node starts with can be filled in.
func child(nodes *map[string]*PathTree, name string) *PathTree {
	if existing, ok := (*nodes)[name]; ok {
		return existing
	}

	if *nodes == nil {
		*nodes = map[string]*PathTree{}
	}

	created := &PathTree{}
	(*nodes)[name] = created

	return created
}

// Get returns the directory at key, or nil if the tree has no such path. Files are reached through
// the Files map of the directory holding them, so that a name which is both answers unambiguously.
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
