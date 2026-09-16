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
	if n == nil {
		return nil
	}

	// "./x" is x. That is how a package whose name strconv.ParseBool reads — t, f, true, false, 1,
	// 0 and their spellings, all of them legal directory names — can be named at all: -total settles
	// the value before this ever sees it, so -total=t is the bare flag and -total=./t is the
	// package. Nothing else needs the prefix, and stripping it changes no other answer, since "./x"
	// and "x" name one node.
	if rest, found := strings.CutPrefix(key, "./"); found && rest != "" {
		key = rest
	}

	if node := n.walk(key); node != nil {
		return node
	}

	// A key read off a row may be spelled as the report draws it rather than as the tree holds it.
	// The renderer makes exactly two substitutions, and these undo them.
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
		return dot.walk(key)
	}

	return nil
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

// UnderRoot reports want with the module root in front of it, when that spelling is a path the tree
// holds.
//
// A row carries its own segment, so "pkg/logger" read off a report is the obvious thing to type and
// the wrong one: the tree holds it under the root the report collapsed away. This returns the
// spelling that is there — "github.com/x/y/pkg/logger" — so a caller can offer it rather than only
// refusing.
//
// It prefixes and nothing else. want is never returned unchanged, so a path the tree already holds
// reports false: there is nothing to suggest about a path that works. Callers reach this after Get
// has missed, which is the only time the question means anything.
//
// The root is a run of single-child directories rather than one node, so this descends the run the
// way collapse does, and joins the way the renderer joins — an empty first name is the filesystem
// root, where the separator is the whole name, which is why the first segment is taken as it is and
// the rest are joined.
//
// Only ever names a path the tree holds: every candidate is checked with Get, so a caller can print
// what this returns without checking again.
func (n *PathTree) UnderRoot(want string) (string, bool) {
	root := ""

	for node := n; node != nil && len(node.Children) == 1 && len(node.Files) == 0; {
		for name, child := range node.Children {
			if root == "" {
				root, node = name, child

				continue
			}

			root, node = join(root, name), child
		}

		if full := join(root, want); n.Get(full) != nil {
			return full, true
		}
	}

	return "", false
}

// Uncovered is how many statements this node and everything beneath it leave uncovered.
//
// A method rather than a caller reading Coverage.Uncovered: a node knows its own counts, and every
// reader that reached through the field had to know that a node keeps CoverageStats and what is in
// them. Coverage stays exported for a caller assembling a tree of its own.
func (n *PathTree) Uncovered() int { return n.Coverage.Uncovered }

// Percentage is the share of this node's statements that are covered, and whether there were any to
// cover. False is not 0% — there is nothing to report.
func (n *PathTree) Percentage() (Percentage, bool) { return n.Coverage.Percentage() }

// AtLeast reports whether this node is covered to the bar, which is not always what comparing the
// ratio would say — see CoverageStats.AtLeast for why 100 is asked of the counts.
func (n *PathTree) AtLeast(bar Threshold) bool { return n.Coverage.AtLeast(bar) }
