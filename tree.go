package prettycov

import (
	"path"
	"slices"
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
	// Blocks is where a file's statements are, in the order the profile listed them, and is set on
	// the nodes in Files and nowhere else — a directory holds no statements of its own. Optional in
	// the same sense FileCoverage.Blocks is: a caller who built its own has none to give.
	Blocks []Block
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
func (n *PathTree) add(file string, stats CoverageStats, blocks []Block, nodes *arena) {
	// Split with path.Dir rather than by counting components, so a file with no directory at all
	// still lands somewhere: path.Dir gives it ".", which is the row it renders as. Reading the
	// directory off the second-to-last component instead left such a file hanging under the tree
	// root, which nothing draws — `prettycov report --new=.` printed an empty report and exited 0.
	//
	// path.Dir cleans on the way, which the walk below relies on: splitting a path is not the same
	// as walking one, and "m//a/b.go" would otherwise give an empty component and read "m//a".
	// The trailing separator goes first, because splitting a string that is only the separator
	// yields two empty components where the filesystem root is one node: "/a.go" gave the root a
	// child of the same nameless kind, and both drew as a blank label. Every other directory
	// path.Dir returns has no trailing slash, so this touches nothing else.
	dir := n
	for part := range strings.SplitSeq(strings.TrimSuffix(path.Dir(file), "/"), "/") {
		dir = nodes.child(&dir.Children, part)
	}

	leaf := nodes.child(&dir.Files, path.Base(file))
	// Accumulated, not assigned, so a file named twice adds up rather than keeping the last one.
	// ParseProfile cannot deliver that — x/tools keys profiles by filename and merges their blocks
	// — so this is for a caller handing Process a slice of its own.
	leaf.Coverage = leaf.Coverage.Plus(stats)
	// Kept because Misses reads positions the counts cannot say. Shared with the caller's slice
	// rather than copied: the parser hands out one capped window per file, so appending to a leaf
	// can never reach into the next file's blocks, and nothing here reorders or trims them — merge
	// sorts a copy when it has to. Copying instead held a second image of every block in the
	// profile alongside the first, 13MB of a 30,000-file one.
	//
	// The append is for the same file named twice, which ParseProfile cannot deliver but a caller
	// assembling its own can; cap == len makes that one copy rather than write into the window.
	if leaf.Blocks == nil {
		leaf.Blocks = blocks

		return
	}

	// Clipped first, so appending allocates rather than writing into whatever the caller's slice
	// shares its array with. The parser's windows are already capped and this is a no-op for them;
	// a caller slabbing its own blocks and naming one file twice would otherwise have the second
	// add overwrite the blocks of the file after it.
	leaf.Blocks = append(slices.Clip(leaf.Blocks), blocks...)
}

// child returns the node called name in the given map, creating both if this is the first time it
// is named. The map is taken by pointer so the nil one a fresh node starts with can be filled in.
func (a *arena) child(nodes *map[string]*PathTree, name string) *PathTree {
	if existing, ok := (*nodes)[name]; ok {
		return existing
	}

	if *nodes == nil {
		*nodes = map[string]*PathTree{}
	}

	created := a.next()
	(*nodes)[name] = created

	return created
}

// arena hands out nodes a chunk at a time, so a tree of a hundred thousand of them costs a few
// hundred allocations rather than one each.
//
// Indexed rather than appended, so a full chunk can only be replaced and never grown. Growing is
// not unsafe — the tree holds pointers into the old array, which stays alive and correct — it is
// waste: append copies every node into the new array, nothing reads the copies, and the originals
// keep the old array anyway. Measured at 36.9MB against 22.7MB on a 30,000-file profile, before
// Blocks was added to the node.
type arena struct {
	chunk []PathTree
	used  int
}

// chunkNodes is 28KB at PathTree's current size, which Blocks took from 32 bytes to 56. That is
// also the floor: the first node allocates a whole chunk, so a one-file profile pays the 28KB where
// it used to pay one node. Measured across 64..32768, time is flat for a large profile and bytes
// scale with the chunk for a small one, so this trades a fixed chunk against an allocation per
// node.
const chunkNodes = 512

func (a *arena) next() *PathTree {
	if a.used == len(a.chunk) {
		a.chunk, a.used = make([]PathTree, chunkNodes), 0
	}

	node := &a.chunk[a.used]
	a.used++

	return node
}

// Get returns the node at key, or nil if the tree has no such path — including when there is no
// tree, so that a miss can be chained: Get("a").Get("b") is nil where it used to panic.
//
// The last segment may name a file, and a file wins: a path ending in one is what a reader types
// off a row, and only a file can be there. A directory of the same name in the same parent cannot
// also exist — no filesystem holds two entries under one name — so a profile that claims both was
// not written by cmd/cover, and the tree draws both either way.
//
// Files are still a map of their own rather than more Children, so that a name belonging to both
// stays two nodes. That map is a field rather than a method, so nothing can make it nil-safe the
// way this is: a caller reading one still has to check what Get handed back.
func (n *PathTree) Get(key string) *PathTree {
	// The empty key names nothing, and has to say so here rather than be left to walk. An absolute
	// profile splits to a leading empty component, so the filesystem root is held as Children[""] —
	// which is exactly the entry walk("") reads, handing back the whole tree for a key naming no
	// path. Here rather than in any one branch below: this is the single point all three resolution
	// paths pass through, so one guard covers walk, the "." retry and underRoot alike.
	if n == nil || key == "" {
		return nil
	}

	// "./x" is x: the tree holds one node under either spelling, and "." is a directory of its own
	// — where a file the profile gave no directory lands — so without the strip "./t" would resolve
	// under it rather than at the top.
	if rest, found := strings.CutPrefix(key, "./"); found && rest != "" {
		key = rest
	}

	if node := n.walk(key); node != nil {
		return node
	}

	// A key read off a row may be spelled as the report draws it rather than as the tree holds it.
	// The two below undo a renderer substitution each; underRoot, last, is not one of those — it
	// accepts a path relative to the collapsed root, which is a convenience for the one prefix
	// nobody wants to retype rather than the inverse of anything.
	//
	// The filesystem root has no name of its own — an absolute path splits to a leading empty
	// component — and draws as "/".
	if key == "/" {
		return n.Children[""]
	}

	// A file the profile gave no directory lands under ".", which path.Dir returns for a bare name.
	// That row draws as "." on its own, and merged with its file as just the file — path.Clean
	// drops the component — so "main.go" is a label with no matching path.
	if dot := n.Children["."]; dot != nil {
		if node := dot.walk(key); node != nil {
			return node
		}
	}

	return n.underRoot(key, maxRootDepth)
}

// onlyChild is the one directory below this node when that is all there is: no files of its own,
// and exactly one child. It is the step a run of pass-through directories is made of.
//
// One definition because two callers must agree on it. collapse folds such a run into a single row,
// and Get puts that run back in front of a path read off the row — so a second copy of this
// predicate would let `total <label>` grade a node the report never drew, which is the class of bug
// the renderers already share prepare to avoid.
func (n *PathTree) onlyChild() (string, *PathTree, bool) {
	if len(n.Files) != 0 || len(n.Children) != 1 {
		return "", nil, false
	}

	for name, child := range n.Children {
		return name, child, true
	}

	return "", nil, false
}

// maxRootDepth bounds how far the collapsed root is followed. A module path is three or four
// segments and the deepest run measured across the reference checkouts is seven, so this stops a
// cycle without reaching any real tree.
const maxRootDepth = 64

// underRoot resolves key under the run of single-child directories the report collapses into its
// top row, and is Get's last fallback.
//
// The third renderer substitution, undone here with the other two: a row carries its own segment,
// so "pkg/logger" read off a report is what a reader types and the tree holds it under
// "github.com/x/y". Literal spellings are tried first, above, so this can only add answers.
//
// The run is descended first and probed from its deepest node back up, because the whole run is
// what the report drew as its top row — the same condition collapse stops on. Probing on the way
// down let a key that matches a segment inside the run answer from above the row the report drew:
// with "github.com/x/y/y" and "github.com/x/y/z" in the profile, `total y` reached the "y" of the
// root before the "y" beside "z", so --fail-under graded the whole tree and passed where the
// package it names failed. Shallower prefixes are still tried, after the full one, so this still
// only adds answers.
//
// walk rather than Get, which is what calls this: probing through Get would recurse without bound.
// The descent is bounded for the same reason — Children is exported, so a caller assembling a tree
// by hand can make a cycle, and Get has to answer rather than hang.
func (n *PathTree) underRoot(key string, depth int) *PathTree {
	// No empty-key guard here: Get is the only caller and refuses one before this is reached, and
	// the recursion below passes key through unchanged. A second copy would be a condition nothing
	// can make true — which is how the first one read once Get grew its own.
	if depth == 0 {
		return nil
	}

	_, child, ok := n.onlyChild()
	if !ok {
		return nil
	}

	// Deepest first: the whole collapsed run is what the report drew as its top row, so a shorter
	// prefix of it must not answer instead. Recursing gives that order, and the descent path, for
	// free — collecting the run into a slice to walk it backwards said the same thing with a slice.
	if found := child.underRoot(key, depth-1); found != nil {
		return found
	}

	// walk rather than Get, which is what calls this: probing through Get would recurse without
	// bound. depth is bounded for the same reason — Children is exported, so a caller assembling a
	// tree by hand can make a cycle, and Get has to answer rather than hang.
	return child.walk(key)
}

// walk resolves key against this node, directories all the way but for the last segment, where a
// file wins.
//
// Cut rather than Split, which allocates a slice to walk once, and rather than the SplitSeq this
// replaced, which cannot say where the last segment is. Not a speedup: SplitSeq allocated nothing
// either, and probing Files on the last segment costs what the new answer is worth — BenchmarkGet
// puts it at 3% over the three shapes, on a call made once per invocation.
//
// Comparing against a precomputed last segment instead would be wrong: "a/x/a" would probe Files at
// the first "a" and hand back a file two levels early.
func (n *PathTree) walk(key string) *PathTree {
	node := n

	for {
		part, rest, more := strings.Cut(key, "/")
		if !more {
			if file, ok := node.Files[part]; ok {
				return file
			}

			return node.Children[part]
		}

		if node = node.Children[part]; node == nil {
			return nil
		}

		key = rest
	}
}

// stats is this node's rolled-up counts, and the whole of what the three accessors below read.
//
// Answers for a nil node, because Get promises a miss can be chained and these are what a caller
// reaches for next: tree.Get("pkg").Uncovered() is the obvious line to write, and it panicked. A
// nil node answers as a node holding nothing does — no statements, so no percentage, and not at
// any bar, which is the safe direction: a mistyped path fails a gate rather than passing it.
//
// One guard rather than one per accessor, so a fourth cannot be added without it.
func (n *PathTree) stats() CoverageStats {
	if n == nil {
		return CoverageStats{}
	}

	return n.Coverage
}

// Uncovered is how many statements this node and everything beneath it leave uncovered.
//
// A method rather than a caller reading Coverage.Uncovered: a node knows its own counts. Coverage
// stays exported for a caller assembling a tree of its own, but nothing in this module reaches
// through it — there is one way to ask.
func (n *PathTree) Uncovered() int { return n.stats().Uncovered }

// Percentage is the share of this node's statements that are covered, and whether there were any to
// cover. False is not 0% — there is nothing to report.
func (n *PathTree) Percentage() (Percentage, bool) { return n.stats().Percentage() }

// AtLeast reports whether this node is covered to the bar, which is not always what comparing the
// ratio would say — see CoverageStats.AtLeast for why 100 is asked of the counts.
func (n *PathTree) AtLeast(bar Threshold) bool { return n.stats().AtLeast(bar) }
