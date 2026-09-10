package prettycov

import (
	"path"
	"strings"
)

type PathTree struct {
	Coverage CoverageStats
	Children map[string]*PathTree

	// isPkg marks a directory the profile named directly, as opposed to one created only to hold
	// another. It cannot be inferred from Coverage: a package whose files declare no statements
	// contributes nothing, so its totals equal its child's.
	isPkg bool
	// isFile marks a node as one of the profile's files rather than a directory. Files are the
	// only nodes carrying coverage of their own; a directory's is entirely rolled up from these.
	isFile bool
}

// add grafts one of the profile's files onto the tree, creating the directories along the way and
// marking the one that holds it as a package. Unexported: a tree is built by Process from a
// profile, and there is no reason to assemble one by hand. Get is the half a caller needs.
//
// Only the file carries the statements. Putting them on the directory as well — which is what
// totalling per directory before building the tree amounts to — makes rollUp count every statement
// twice, once on the directory and once beneath it.
func (n *PathTree) add(file string, stats CoverageStats) {
	// Split with path.Dir rather than by counting components, so a file with no directory at all
	// still lands in a package: path.Dir gives it ".", which is the row it renders as. Reading the
	// package off the second-to-last component instead left such a file hanging under the tree
	// root, which nothing draws — `prettycov -new=.` printed an empty report and exited 0.
	//
	// path.Dir cleans on the way, which the walk below relies on: splitting a path is not the same
	// as walking one, and "m//a/b.go" would otherwise give an empty component and read "m//a".
	dir := n.directory(path.Dir(file))
	dir.isPkg = true

	leaf := dir.child(path.Base(file))
	// Accumulated, not assigned: a profile may name the same file more than once, which is how a
	// run with -coverpkg across several packages reports one of them.
	leaf.Coverage.Covered += stats.Covered
	leaf.Coverage.Uncovered += stats.Uncovered
	leaf.isFile = true
}

// directory returns the node at dir, creating the nodes along the way.
func (n *PathTree) directory(dir string) *PathTree {
	node := n
	for part := range strings.SplitSeq(dir, "/") {
		node = node.child(part)
	}

	return node
}

// child returns the node under n called name, creating it if this is the first time it is named.
func (n *PathTree) child(name string) *PathTree {
	if existing, ok := n.Children[name]; ok {
		return existing
	}

	if n.Children == nil {
		n.Children = map[string]*PathTree{}
	}

	created := &PathTree{}
	n.Children[name] = created

	return created
}

// IsFile reports whether the node is one of the profile's files and nothing else. Children holds
// files as well as directories, so anything walking the tree to enumerate packages has to ask.
//
// A name that is both — "m/a.go" beside "m/a.go/b.go", which no filesystem allows but a merge of
// two profiles can produce — reports false, because a caller skipping it would drop the packages
// underneath while still counting their statements in every ancestor. The report answers it the
// same way, by drawing such a node as the directory it also is.
//
// Nil is not a file: Get returns nil for a path the profile does not hold, and indexing Children
// with a name it does not have gives the same, so both ways of reaching a node can produce one.
func (n *PathTree) IsFile() bool { return n != nil && n.isFile && len(n.Children) == 0 }

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
