package prettycov_test

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

func TestPathTreeGetReturnsNilForAPathThatIsNotThere(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{file("m/pkg/a.go", 1, 1)})

	assert.Nil(t, tree.Get("m/absent"))
	assert.NotNil(t, tree.Get("m/pkg"), "and finds one that is")

	// A miss is chainable, so walking down a path one component at a time does not have to check
	// after every step. 0.8.0's Get returned the file itself and IsFile answered for a nil node;
	// with files behind a map, this is what is left to be nil-safe.
	assert.Nil(t, tree.Get("m/absent").Get("deeper"), "a miss is still a tree to ask")
	assert.Nil(t, (*prettycov.PathTree)(nil).Get("m"))
}

// Files and directories are separate maps, so a caller enumerating packages walks Children and is
// never handed a file by accident. Get answers for directories; a file is reached through Files.
func TestPathTreeKeepsFilesAndDirectoriesApart(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/x/own.go", 1, 1),
		file("m/x/sub/s.go", 1, 1),
	})

	pkg := tree.Get("m/x")
	require.NotNil(t, pkg)

	assert.Equal(t, []string{"sub"}, slices.Sorted(maps.Keys(pkg.Children)), "directories only")
	assert.Equal(t, []string{"own.go"}, slices.Sorted(maps.Keys(pkg.Files)), "and the files it holds")

	// Two maps, so a name belonging to both stays two nodes — but Get reaches through to the file,
	// since the last segment of a path a reader typed off a row is the row they were looking at.
	own := tree.Get("m/x/own.go")
	require.NotNil(t, own, "the last segment may name a file")
	assert.Same(t, pkg.Files["own.go"], own, "and it is the file, not something rebuilt")
	assert.Empty(t, own.Children, "a file holds nothing")
}

// A file wins the last segment, which only matters for a profile no filesystem could have produced:
// one directory cannot hold a file and a directory of one name. cmd/cover cannot write it, so the
// rule is here to be predictable rather than to arbitrate a real case — and a path ending in .go is
// a file to whoever typed it.
func TestPathTreeGetPrefersAFileOnTheLastSegment(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/a.go", 1, 9),      // the file
		file("m/a.go/b.go", 9, 1), // a directory of the same name
	})

	got := tree.Get("m/a.go")
	require.NotNil(t, got)

	pct, ok := got.Coverage.Percentage()
	require.True(t, ok)
	assert.InDelta(t, 10.00, pct.Float(), ratioTolerance, "the file, not the directory's 90.00")

	// The directory is still there, and still reachable through what it holds.
	assert.NotNil(t, tree.Get("m/a.go/b.go"), "the directory is not shadowed, only its own name is")
}

// A key read off a row resolves, and the report draws two labels the tree does not hold under that
// name: path.Clean drops a "." component, so a file the profile gave no directory of its own merges
// into a row spelled as just the file; and the filesystem root has no name of its own, so it draws
// as "/". Both are the renderer's substitutions, and Get undoes them — otherwise -total=main.go is
// refused for a row the tool printed one line above.
func TestPathTreeGetTakesTheSpellingTheReportDraws(t *testing.T) {
	t.Parallel()

	bare := prettycov.Process([]prettycov.FileCoverage{
		file("main.go", 3, 1), // no directory at all: lands under "."
		file("pkg/a.go", 2, 0),
	})

	// "./" alone is not a path to anything: stripping it would leave the empty key, which names the
	// root and would hand back the whole tree for what reads as a typo.
	assert.Nil(t, bare.Get("./"), `"./" names nothing`)

	for _, key := range []string{"main.go", "./main.go", "."} {
		node := bare.Get(key)
		require.NotNilf(t, node, "Get(%q)", key)

		pct, ok := node.Coverage.Percentage()
		require.True(t, ok)
		assert.InDeltaf(t, 75.00, pct.Float(), ratioTolerance, "Get(%q)", key)
	}

	rooted := prettycov.Process([]prettycov.FileCoverage{
		file("/a.go", 3, 1),
		file("/b.go", 0, 1),
	})

	slash := rooted.Get("/")
	require.NotNil(t, slash, `the row drawn as "/"`)

	pct, ok := slash.Coverage.Percentage()
	require.True(t, ok)
	assert.InDelta(t, 60.00, pct.Float(), ratioTolerance)

	assert.NotNil(t, rooted.Get("/a.go"), "and a file under it")
}

// A package named as strconv.ParseBool reads it — t, f, true, 1 and their spellings, every one a
// legal Go directory name — cannot be asked for by name, because -total settles the value before
// the tree is consulted. "./t" is the escape, and it is the only one: the flag cannot tell them
// apart, so the library has to offer a spelling the flag never claims.
func TestPathTreeGetTakesADotSlashEscape(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("t/a.go", 2, 0),
		file("f/b.go", 0, 2),
	})

	for key, want := range map[string]float64{"t": 100, "./t": 100, "f": 0, "./f": 0} {
		node := tree.Get(key)
		require.NotNilf(t, node, "Get(%q)", key)

		pct, ok := node.Coverage.Percentage()
		require.True(t, ok)
		assert.InDeltaf(t, want, pct.Float(), ratioTolerance, "Get(%q)", key)
	}

	// The prefix is stripped, not resolved against a directory called ".": this tree has none.
	assert.Nil(t, tree.Get("./nope"))
}

// A segment repeated further down must not resolve early. Get walks with Cut and only asks Files
// where there is no separator left, so "a/x/a" is the file two levels down; comparing each segment
// against a precomputed last one would match the first "a" and hand back a file from the top.
func TestPathTreeGetDoesNotResolveARepeatedSegmentEarly(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("a/x/a", 1, 9),         // a file named "a", inside a directory also named "a"
		file("a/x/a/deep.go", 9, 1), // and a directory of that name beside it
	})

	deep := tree.Get("a/x/a")
	require.NotNil(t, deep)

	pct, ok := deep.Coverage.Percentage()
	require.True(t, ok)
	assert.InDelta(t, 10.00, pct.Float(), ratioTolerance, "the file two levels down, not the root")

	assert.NotNil(t, tree.Get("a/x/a/deep.go"), "and the walk still passes through the directory")
}

// Nothing at all is nil rather than a zero node, so a caller can tell "no such path" from "nothing
// covered" — the two print very differently and only one is a mistake.
func TestPathTreeGetMissesAreNil(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{file("m/x/own.go", 1, 1)})

	// "" is in the list because prefixing under the collapsed root nearly broke it: an empty key
	// joins to the root itself, so Get("") handed back the top node where every other spelling of
	// "nothing" is nil.
	for _, key := range []string{"", "nope", "m/nope", "m/x/own.go/deeper", "m/x/own.go/"} {
		assert.Nil(t, tree.Get(key), "Get(%q)", key)
	}
}

// Prefixing under the collapsed root must not let a key climb out of it. Building each candidate as
// a path meant path.Clean, which folds "..", so `total ..` resolved to an ancestor and printed its
// percentage with exit 0 — the one failure the "a path the profile does not hold is exit 2" rule
// exists to prevent, since a CI gate would grade a different node instead of failing.
func TestPathTreeGetRefusesToClimbOutOfTheRoot(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/x/y/p/a.go", 1, 0),
		file("m/x/y/q/b.go", 0, 1),
	})

	require.NotNil(t, tree.Get("p"), "the run is collapsed, so a bare row label resolves")

	for _, key := range []string{"..", "p/..", "x/../p", "../q"} {
		assert.Nil(t, tree.Get(key), "Get(%q) names no node", key)
	}
}

// An absolute profile collapses the same way, and the prefixing has to reach it. Rebuilding the
// path to probe with lost this: an absolute path splits to a leading empty component, path.Join
// drops it, and "/abs/x/y/p" was probed as "abs/x/y/p" — so no absolute tree ever resolved a row
// label, while the identical relative profile did.
func TestPathTreeGetResolvesUnderAnAbsoluteRoot(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("/abs/x/y/p/a.go", 1, 0),
		file("/abs/x/y/q/b.go", 0, 1),
	})

	assert.Same(t, tree.Get("/abs/x/y/p"), tree.Get("p"), "the label the report draws")
	assert.Same(t, tree.Get("/abs/x/y/q/b.go"), tree.Get("q/b.go"))
	assert.Nil(t, tree.Get("nowhere"))
}

// A segment of the collapsed root that is also a package inside it resolves to the package, which
// is the row the report drew. Probing on the way down the run answered from above that row: "y" is
// the last segment of "github.com/x/y" and a package beside "z", and the root won — so
// `total y --fail-under=80` graded the whole tree at 90 and passed where the package it names is 0.
func TestPathTreeGetPrefersTheDeepestRootPrefix(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("github.com/x/y/y/a.go", 0, 1),
		file("github.com/x/y/z/b.go", 1, 0),
	})

	assert.Same(t, tree.Get("github.com/x/y/y"), tree.Get("y"), "the row the report draws")
	assert.Same(t, tree.Get("github.com/x/y/z"), tree.Get("z"), "and its sibling, as before")
}

// Get documents that a miss can be chained, and these are what a caller reaches for next. All three
// panicked on the node Get had just handed back, so the one line the doc invites —
// tree.Get("pkg").Uncovered() — was the one that crashed.
//
// A nil node answers as a node holding nothing does, which is what the tree already says about an
// empty one: no uncovered statements, no percentage to report, and not at any bar — including 0,
// since nothing to cover is not "at least anything".
func TestPathTreeMethodsAnswerForAMissedNode(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/a.go", 1, 1),
	})

	missed := tree.Get("nope")
	require.Nil(t, missed, "the case Get promises is chainable")

	assert.Equal(t, 0, missed.Uncovered())

	pct, ok := missed.Percentage()
	assert.False(t, ok, "no statements, so no share to report")
	assert.Equal(t, prettycov.Percentage{}, pct)

	assert.False(t, missed.AtLeast(prettycov.MustThreshold(0)), "not at any bar, 0 included")
	assert.False(t, missed.AtLeast(prettycov.MustThreshold(100)))

	// And the node that is there answers from its counts, which is the contrast that gives the
	// nil answers their meaning.
	held := tree.Get("m/a.go")
	require.NotNil(t, held)

	assert.Equal(t, 1, held.Uncovered())

	pct, ok = held.Percentage()
	require.True(t, ok)
	assert.InDelta(t, 50.0, pct.Float(), ratioTolerance)

	assert.True(t, held.AtLeast(prettycov.MustThreshold(50)))
	assert.False(t, held.AtLeast(prettycov.MustThreshold(51)))
}

// Get follows the collapsed root by descending single-child directories, and Children is exported,
// so a tree assembled by hand can point back at itself. Bounded rather than trusted: the answer is
// a miss, and the point is that there is one.
func TestPathTreeGetSurvivesACycle(t *testing.T) {
	t.Parallel()

	loop := &prettycov.PathTree{}
	loop.Children = map[string]*prettycov.PathTree{"m": loop}

	done := make(chan *prettycov.PathTree, 1)
	go func() { done <- loop.Get("nope") }()

	select {
	case got := <-done:
		assert.Nil(t, got)
	case <-time.After(5 * time.Second):
		t.Fatal("Get did not return: the descent past the collapsed root is unbounded")
	}
}

// A name that is both is two nodes, one in each map, and neither has to answer for the other. That
// is what makes every row the sum of what is drawn beneath it with no exception.
func TestPathTreeSplitsANameThatIsBothAFileAndADirectory(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/a.go", 5, 0),
		file("m/a.go/b.go", 0, 7),
	})

	m := tree.Get("m")
	require.NotNil(t, m)

	assert.Equal(t, 5, m.Files["a.go"].Coverage.Total(), "the file")
	assert.Equal(t, 7, m.Children["a.go"].Coverage.Total(), "the directory of the same name")
	assert.Equal(t, 12, m.Coverage.Total(), "and m is exactly the two of them")
}

// Get resolves a path under the root the report collapsed away, which is the third substitution the
// renderer makes and the last one Get undoes. A row carries its own segment, so "pkg/logger" is what
// a reader copies off a report and the tree holds it under "github.com/x/y".
func TestGetResolvesAPathTheRootWasCollapsedFrom(t *testing.T) {
	t.Parallel()

	profile := writeProfile(t, "mode: set\ngithub.com/x/y/pkg/logger/a.go:1.1,2.2 1 1\n")

	got, err := prettycov.Measure(prettycov.Request{Profile: profile})
	require.NoError(t, err)

	tree, ok := got.Tree()
	require.True(t, ok)

	full := tree.Get("github.com/x/y/pkg/logger")
	require.NotNil(t, full, "the spelling the profile holds")

	// The same node, not merely a node: the literal spelling is tried first, so prefixing can only
	// add answers and never change one.
	assert.Same(t, full, tree.Get("pkg/logger"))
	assert.Same(t, full.Files["a.go"], tree.Get("pkg/logger/a.go"))

	assert.Nil(t, tree.Get("nowhere"))
}

// The prefixing stops where the run does. A directory holding a file as well as a single
// subdirectory ends it, because past there the path is a choice rather than the root.
func TestGetStopsPrefixingWhereTheRunBranches(t *testing.T) {
	t.Parallel()

	// m holds one subdirectory and a file of its own, so the run ends at m.
	profile := writeProfile(t, "mode: set\nm/own.go:1.1,2.2 1 1\nm/deep/a.go:1.1,2.2 1 1\n")

	got, err := prettycov.Measure(prettycov.Request{Profile: profile})
	require.NoError(t, err)

	tree, ok := got.Tree()
	require.True(t, ok)

	assert.Same(t, tree.Get("m/deep"), tree.Get("deep"), "m is the root the report collapsed away")

	assert.Nil(t, tree.Get("deep/a.go/nope"),
		"and the run does not continue past a directory holding files")
}
