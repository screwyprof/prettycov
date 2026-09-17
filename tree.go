package prettycov

import (
	"path"
	"slices"
	"strings"
)

// PathTree is a directory and what the profile says about it: the files it holds, the directories
// below it, and the statements of everything under both once Process has rolled them up.
//
// Two maps rather than one, because a name can be a file and a directory at once. A profile
// naming "m/a.go" and "m/a.go/b.go" describes both, which no filesystem allows but merging two
// profiles can produce. One namespace made that a single node standing for two things, and every
// question about it had to be answered with a flag: whether it counted as a package, whether it
// counted as a file, whether it could be folded away. Here it is simply two nodes.
type PathTree struct {
	Coverage CoverageStats
	// Blocks is where a file's statements are, in the order the profile listed them, and is set on
	// the nodes in Files and nowhere else, since a directory holds no statements of its own. Optional in
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
// Only the file carries the statements. Putting them on the directory as well, which is what
// totalling per directory before building the tree amounts to, makes rollUp count every statement
// twice, once on the directory and once beneath it.
func (n *PathTree) add(file string, stats CoverageStats, blocks []Block, nodes *arena) {
	// Split with path.Dir rather than by counting components, so a file with no directory at all
	// still lands somewhere: path.Dir gives it ".", which is the row it renders as. Reading the
	// directory off the second-to-last component instead left such a file hanging under the tree
	// root, which nothing draws: `prettycov report --new=.` printed an empty report and exited 0.
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
	// ParseProfile cannot deliver that, since x/tools keys profiles by filename and merges their
	// blocks, so this is for a caller handing Process a slice of its own.
	leaf.Coverage = leaf.Coverage.Plus(stats)
	// Kept because Misses reads positions the counts cannot say. Shared with the caller's slice
	// rather than copied: the parser hands out one capped window per file, so appending to a leaf
	// can never reach into the next file's blocks, and nothing here reorders or trims them. merge
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

// arena hands out nodes a chunk at a time, so a hundred thousand of them cost a few hundred
// allocations rather than one each. Indexed, not appended: append would copy every node into a new
// array that nothing reads, since the tree already points into the old one. 36.9MB against 22.7MB.
type arena struct {
	chunk []PathTree
	used  int
}

// chunkNodes is 28KB at PathTree's size, and also the floor: a one-file profile pays a whole chunk.
// Measured across 64..32768: time is flat for a large profile, bytes scale with the chunk for a
// small one.
const chunkNodes = 512

func (a *arena) next() *PathTree {
	if a.used == len(a.chunk) {
		a.chunk, a.used = make([]PathTree, chunkNodes), 0
	}

	node := &a.chunk[a.used]
	a.used++

	return node
}

// Get returns the node at key, or nil if the tree has no such path, including when there is no
// tree, so that a miss can be chained: Get("a").Get("b") is nil where it used to panic.
//
// A file wins the last segment: a path ending in one is what a reader types off a row, and only a
// file can be there. Files stays a separate map so a name belonging to both keeps two nodes, and
// being a field, it is not nil-safe the way this is.
func (n *PathTree) Get(key string) *PathTree {
	// The empty key names nothing. An absolute profile holds the filesystem root as Children[""],
	// which is what walk("") reads: the whole tree, for a key naming no path. Here, because this is
	// the one point all three resolution paths pass through.
	if n == nil || key == "" {
		return nil
	}

	// "./x" is x. "." is a real directory here, where a bare file lands, so without the strip
	// "./t" would resolve under it.
	if rest, found := strings.CutPrefix(key, "./"); found && rest != "" {
		key = rest
	}

	if node := n.walk(key); node != nil {
		return node
	}

	// A key read off a row is spelled as the report draws it. The two fallbacks below undo a renderer
	// substitution each; underRoot, last, accepts a path relative to the collapsed root.
	//
	// The filesystem root has no name of its own and draws as "/".
	if key == "/" {
		return n.Children[""]
	}

	// A bare file lands under ".", and merges with it into a row drawn as just the file, so
	// "main.go" is a label with no matching path.
	if dot := n.Children["."]; dot != nil {
		if node := dot.walk(key); node != nil {
			return node
		}
	}

	return n.underRoot(key, maxRootDepth)
}

// onlyChild is the single directory below this node when that is all there is. One definition
// because collapse folds such a run into a row and Get puts it back in front of a path read off
// that row, and a second copy would let `total <label>` grade a node the report never drew.
func (n *PathTree) onlyChild() (string, *PathTree, bool) {
	if len(n.Files) != 0 || len(n.Children) != 1 {
		return "", nil, false
	}

	for name, child := range n.Children {
		return name, child, true
	}

	// Unreachable: the guard above leaves exactly one child. Required, since Go cannot see that.
	return "", nil, false
}

// maxRootDepth bounds how far the collapsed root is followed. The deepest run measured across the
// reference checkouts is seven, so this stops a cycle without reaching any real tree.
const maxRootDepth = 64

// underRoot resolves key under the run of single-child directories the report collapsed into its
// top row: "pkg/logger" read off a report, where the tree holds "github.com/x/y/pkg/logger".
// Literal spellings are tried first, so this only adds answers.
//
// Descended first and probed from the deepest node back up, since the whole run is the one row the
// report drew. Probing downwards let `total y` match the "y" inside "github.com/x/y" before the
// package "y" beside it, grading the whole tree and passing where that package failed.
func (n *PathTree) underRoot(key string, depth int) *PathTree {
	// No empty-key guard: Get is the only caller, refuses one, and the recursion passes key through.
	if depth == 0 {
		return nil
	}

	_, child, ok := n.onlyChild()
	if !ok {
		return nil
	}

	// Deepest first, so a shorter prefix of the run cannot answer instead.
	if found := child.underRoot(key, depth-1); found != nil {
		return found
	}

	// walk, not Get, which is what calls this: probing through Get would recurse without bound.
	return child.walk(key)
}

// walk resolves key against this node: directories all the way but the last segment, where a file
// wins. Cut rather than SplitSeq, which cannot say where the last segment is, and comparing against
// a precomputed one would probe Files at the first "a" of "a/x/a".
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

// stats is this node's rolled-up counts, and all the accessors below read.
//
// Answers for a nil node, since Get promises a miss can be chained: a nil node holds nothing, so no
// percentage and not at any bar, so a mistyped path fails a gate rather than passing it. One guard
// rather than one per accessor, so a fourth cannot be added without it.
func (n *PathTree) stats() CoverageStats {
	if n == nil {
		return CoverageStats{}
	}

	return n.Coverage
}

// Uncovered is how many statements this node and everything beneath it leave uncovered. Coverage
// stays exported for a caller building its own tree, but nothing here reaches through it.
func (n *PathTree) Uncovered() int { return n.stats().Uncovered }

// Percentage is the share of this node's statements that are covered, and whether there were any to
// cover. False is not 0%; there is nothing to report.
func (n *PathTree) Percentage() (Percentage, bool) { return n.stats().Percentage() }

// AtLeast reports whether this node is covered to the bar, which is not always what comparing the
// ratio would say. See CoverageStats.AtLeast for why 100 is asked of the counts.
func (n *PathTree) AtLeast(bar Threshold) bool { return n.stats().AtLeast(bar) }
