package prettycov_test

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// ratioTolerance is half a display digit: the report prints two decimals, so anything closer
// than this is the same number as far as a reader is concerned.
const ratioTolerance = 0.005

// rollUpCase describes one tree shape and the percentage every named node must report. A slice
// rather than a map keyed by name: map iteration is randomised, so a map would run these in a
// different order every time and report failures in a different order too.
type rollUpCase struct {
	name  string
	files []prettycov.FileCoverage
	want  map[string]float64
}

// Every statement in a subtree must be counted exactly once when rolling up into a parent.
// The shapes below are the ones that got this wrong; see each case for what it probes.
func TestProcessCountsEachStatementOnce(t *testing.T) {
	t.Parallel()

	tests := []rollUpCase{
		// A directory that is BOTH a package and a parent of packages. This is the shape that
		// breaks in practice: scraper/ in the delegator profile has service.go and subscriber.go
		// beside a store/ subpackage, and reported 87.03% where the truth is 90.00%.
		{
			name: "dir is both package and parent",
			files: []prettycov.FileCoverage{
				file("m/scraper/service.go", 8, 2),
				file("m/scraper/store/store.go", 5, 5),
			},
			want: map[string]float64{
				"m/scraper":       65.00, // (8+5) / (10+10)
				"m/scraper/store": 50.00,
			},
		},

		// Sibling subtrees whose child counts differ. Each subtree's totals were scaled by its
		// own number of children, skewing the parent's weighted average.
		{
			name: "siblings with differing child counts",
			files: []prettycov.FileCoverage{
				file("m/x/a/f.go", 2, 0),
				file("m/y/a/f.go", 0, 2),
				file("m/y/b/f.go", 0, 2),
				file("m/y/c/f.go", 0, 2),
			},
			want: map[string]float64{
				"m":   25.00, // 2 covered of 8
				"m/x": 100.00,
				"m/y": 0.00,
			},
		},

		// Guards against over-correcting: the simple cases must keep working.
		{
			name: "single package under root",
			files: []prettycov.FileCoverage{
				file("m/pkg/a/a.go", 3, 1),
			},
			want: map[string]float64{
				"m":       75.00,
				"m/pkg":   75.00,
				"m/pkg/a": 75.00,
			},
		},

		// A package whose files all sit at the same level, with no children at all.
		{
			name: "flat package",
			files: []prettycov.FileCoverage{
				file("m/a.go", 1, 3),
				file("m/b.go", 1, 1),
			},
			want: map[string]float64{
				"m": 33.33, // 2 covered of 6
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := prettycov.Process(tc.files)

			for path, want := range tc.want {
				node := tree.Get(path)
				require.NotNilf(t, node, "path %q missing from tree", path)

				pct, ok := node.Coverage.Percentage()
				require.Truef(t, ok, "no statements at %q", path)
				assert.InDeltaf(t, want, pct.Float(), ratioTolerance, "coverage at %q", path)
			}
		})
	}
}

func TestCoverageStatsPercentage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stats   prettycov.CoverageStats
		wantPct float64
		wantOK  bool
	}{
		{name: "all covered", stats: prettycov.CoverageStats{Covered: 4}, wantPct: 100, wantOK: true},
		{name: "none covered", stats: prettycov.CoverageStats{Uncovered: 4}, wantPct: 0, wantOK: true},
		{name: "partly covered", stats: prettycov.CoverageStats{Covered: 1, Uncovered: 3}, wantPct: 25, wantOK: true},
		// Not 0%: there is nothing to cover, so there is no percentage to report.
		{name: "no statements", stats: prettycov.CoverageStats{}, wantPct: 0, wantOK: false},
		// Statement counts are non-negative and covered is at most the total. Each of these says
		// the sum overflowed, which a profile declaring blocks of billions of statements can do.
		{name: "total wrapped negative", stats: prettycov.CoverageStats{Covered: 1, Uncovered: -3}, wantOK: false},
		{name: "more covered than total", stats: prettycov.CoverageStats{Covered: 100, Uncovered: -2}, wantOK: false},
		{name: "covered wrapped negative", stats: prettycov.CoverageStats{Covered: -1, Uncovered: 8}, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pct, ok := tc.stats.Percentage()

			assert.Equal(t, tc.wantOK, ok)
			assert.InDelta(t, tc.wantPct, pct.Float(), ratioTolerance)
		})
	}
}

// AtLeast is the gate -fail-under and -hide-covered are graded by, and at 100 it is not the
// comparison the ratio would make. Exported, so a caller can hand it any number the CLI's own
// [0, 100] clamp would have refused.
func TestCoverageStatsAtLeast(t *testing.T) {
	t.Parallel()

	// One statement short of complete, and large enough that the miss falls below the mantissa: the
	// ratio is exactly 100.0 in float64, which is why the question at 100 is about the counts.
	const huge = 1 << 56

	tests := []struct {
		name  string
		stats prettycov.CoverageStats
		pct   float64
		want  bool
	}{
		{name: "above the bar", stats: prettycov.CoverageStats{Covered: 9, Uncovered: 1}, pct: 80, want: true},
		{name: "exactly at it", stats: prettycov.CoverageStats{Covered: 8, Uncovered: 2}, pct: 80, want: true},
		{name: "below it", stats: prettycov.CoverageStats{Covered: 7, Uncovered: 3}, pct: 80, want: false},
		{name: "complete at 100", stats: prettycov.CoverageStats{Covered: 4}, pct: 100, want: true},
		{
			name:  "one short of complete, rounding to 100",
			stats: prettycov.CoverageStats{Covered: huge - 1, Uncovered: 1},
			pct:   100,
			want:  false,
		},
		// Nothing to cover has no share to compare, so it is not at any bar — including 0, which
		// would otherwise make every empty package pass every gate.
		{name: "no statements", stats: prettycov.CoverageStats{}, pct: 0, want: false},
		// And nothing reaches more than all of it. A caller passing a computed threshold, or one it
		// meant as a fraction, gets a refusal rather than a gate that reads 100 as "at least 150".
		{name: "past 100", stats: prettycov.CoverageStats{Covered: 10}, pct: 150, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.stats.AtLeast(tc.pct))
		})
	}
}

// Both sides move together, or a percentage ends up drawn from counts that were never summed the
// same way.
func TestCoverageStatsAdd(t *testing.T) {
	t.Parallel()

	stats := prettycov.CoverageStats{Covered: 3, Uncovered: 1}
	stats = stats.Plus(prettycov.CoverageStats{Covered: 4, Uncovered: 2})

	assert.Equal(t, prettycov.CoverageStats{Covered: 7, Uncovered: 3}, stats)

	stats = stats.Plus(prettycov.CoverageStats{})
	assert.Equal(t, prettycov.CoverageStats{Covered: 7, Uncovered: 3}, stats, "adding nothing changes nothing")
}

// The count is the whole reason Shorten is its own step: a root that matches nothing rewrites
// nothing, and that is indistinguishable from no rename being asked for unless the count says
// otherwise. Which paths match is the table below; only more than one file can show the counting,
// and only a rewrite can show that the caller's slice survives it.
func TestShortenCountsEveryFileItRenamed(t *testing.T) {
	t.Parallel()

	files := []prettycov.FileCoverage{
		file("example.com/m/a.go", 1, 0),
		file("example.com/m/b.go", 1, 0),
		file("other.com/c.go", 1, 0),
	}

	shortened, renamed := prettycov.Shorten(files, "example.com/m", "m")

	assert.Equal(t, 2, renamed, "two of the three")
	assert.Equal(t, "m/a.go", shortened[0].File)
	assert.Equal(t, "m/b.go", shortened[1].File)
	assert.Equal(t, "other.com/c.go", shortened[2].File, "and the third is left alone")

	assert.Equal(t, "example.com/m/a.go", files[0].File, "the input is not modified")
}

// HasRoot answers with a boolean what Shorten answers with a count, so the two have to agree on
// what a root names — every case here is one Shorten is asserted on above, asked the other way.
func TestHasRootMatchesTheSameRootsShortenRenames(t *testing.T) {
	t.Parallel()

	files := []prettycov.FileCoverage{
		file("github.com/foobar/svc/a.go", 1, 0),
		file("github.com/o/repo/pkg/b.go", 1, 0),
	}

	tests := map[string]struct {
		root string
		want bool
	}{
		"a root the profile holds":          {root: "github.com/o/repo", want: true},
		"however many trailing separators":  {root: "github.com/o/repo//", want: true},
		"a root it does not":                {root: "github.com/WRONG", want: false},
		"not a bare prefix":                 {root: "github.com/foo", want: false},
		"not a component out of the middle": {root: "repo", want: false},
		"no root names nothing":             {root: "", want: false},
		"nor one of only separators":        {root: "//", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, prettycov.HasRoot(files, tc.root))

			// The agreement itself, not just the two answers: whatever Shorten would rewrite is
			// what HasRoot has to find, or the CLI reports a root as absent while renaming by it.
			_, renamed := prettycov.Shorten(files, tc.root, "x")
			assert.Equal(t, tc.want, renamed > 0, "Shorten disagrees")
		})
	}
}

// Not parallel: AllocsPerRun counts allocations process-wide and panics if asked to do it beside
// another test.
//
//nolint:paralleltest // see above.
func TestHasRootAllocatesNothing(t *testing.T) {
	files := []prettycov.FileCoverage{file("m/a.go", 1, 0), file("m/b.go", 1, 0)}

	// One allocation would be the copy Shorten makes, which is the whole reason this exists.
	assert.Zero(t, testing.AllocsPerRun(100, func() {
		_ = prettycov.HasRoot(files, "m")
	}), "HasRoot allocates")
}

// A caller assembling its own FileCoverage can name one file twice, which ParseProfile cannot: it
// keys profiles by filename and merges them first. Both the counts and the positions have to add up
// rather than the second replacing the first, or a tree built by hand loses half a file.
func TestProcessAddsUpAFileNamedTwice(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		{
			File:     "m/a.go",
			Coverage: prettycov.CoverageStats{Covered: 1, Uncovered: 1},
			Blocks: []prettycov.Block{
				{Line: 3, Col: 2, EndLine: 4, Coverage: prettycov.CoverageStats{Covered: 1}},
				{Line: 9, Col: 2, EndLine: 10, Coverage: prettycov.CoverageStats{Uncovered: 1}},
			},
		},
		{
			File:     "m/a.go",
			Coverage: prettycov.CoverageStats{Uncovered: 2},
			Blocks: []prettycov.Block{
				{Line: 40, Col: 2, EndLine: 41, Coverage: prettycov.CoverageStats{Uncovered: 2}},
			},
		},
	})

	node := tree.Get("m")
	require.NotNil(t, node)
	assert.Equal(t, prettycov.CoverageStats{Covered: 1, Uncovered: 3}, node.Files["a.go"].Coverage)

	// And the second file's blocks are appended to the first's rather than replacing them, in the
	// order they arrived. Asserted on the leaf, not through a report: this is what add does, and a
	// report would only show it once merging and the depth have had their say.
	assert.Equal(t, []prettycov.Block{
		{Line: 3, Col: 2, EndLine: 4, Coverage: prettycov.CoverageStats{Covered: 1}},
		{Line: 9, Col: 2, EndLine: 10, Coverage: prettycov.CoverageStats{Uncovered: 1}},
		{Line: 40, Col: 2, EndLine: 41, Coverage: prettycov.CoverageStats{Uncovered: 2}},
	}, node.Files["a.go"].Blocks)
}

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

// Which paths a root names, and the count that follows from it — a row that rewrites nothing is a
// row that counts nothing, and stating both together is what stops the two drifting apart.
//
// Asserted on the path Shorten returns rather than on a node in the tree built from it: renaming
// is no longer part of building the tree, and asking Process where a label ended up tested this
// through an indirection that could only mislead whoever a failure here sends looking.
func TestShortenReplacesOnlyALeadingRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		file        string
		old         string
		replace     string
		want        string
		wantRenamed int
	}{
		{
			name: "replaces a leading root", file: "github.com/o/repo/pkg/a.go",
			old: "github.com/o/repo", replace: "repo", want: "repo/pkg/a.go", wantRenamed: 1,
		},
		{
			// "api" appears inside "rapid" first. Replacing the first match anywhere turned
			// github.com/rapid/api into github.com/rcored/api.
			name: "only a leading one", file: "github.com/rapid/api/svc/a.go",
			old: "api", replace: "core", want: "github.com/rapid/api/svc/a.go",
		},
		{
			// The separator is implied, so writing it out changes nothing.
			name: "a trailing slash on the old root is the same root", file: "github.com/o/repo/pkg/a.go",
			old: "github.com/o/repo/", replace: "repo", want: "repo/pkg/a.go", wantRenamed: 1,
		},
		{
			// `-old=$(MODULE)/` with MODULE already ending in one. Trimming a single separator left
			// "github.com/o/repo/" to be matched against a path carrying one separator there, so a
			// root that is in the profile matched nothing and the CLI called it a root that is not.
			name: "however many of them there are", file: "github.com/o/repo/pkg/a.go",
			old: "github.com/o/repo//", replace: "repo", want: "repo/pkg/a.go", wantRenamed: 1,
		},
		{
			// And only the trailing ones: a leading separator is where an absolute path begins, so
			// trimming it would look for a root the profile does not contain.
			name: "a leading separator is part of the root", file: "/home/x/pkg/a.go",
			old: "/home/x", replace: "x", want: "x/pkg/a.go", wantRenamed: 1,
		},
		{
			// A prefix is not a root: "github.com/foo" starts "github.com/foobar" too, and cutting
			// it there left the unrelated package as "xbar/svc".
			name: "and only a whole path segment", file: "github.com/foobar/svc/a.go",
			old: "github.com/foo", replace: "x", want: "github.com/foobar/svc/a.go",
		},
		{
			// An empty old root matches at position 0, so this used to prepend rather than replace.
			name: "no old root means no rewrite", file: "github.com/o/repo/pkg/a.go",
			old: "", replace: "repo", want: "github.com/o/repo/pkg/a.go",
		},
		{
			name: "no new root means no rewrite", file: "github.com/o/repo/pkg/a.go",
			old: "github.com/o/repo", replace: "", want: "github.com/o/repo/pkg/a.go",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			shortened, renamed := prettycov.Shorten([]prettycov.FileCoverage{file(tc.file, 1, 1)}, tc.old, tc.replace)

			assert.Equal(t, tc.want, shortened[0].File)
			assert.Equal(t, tc.wantRenamed, renamed)
		})
	}
}

// Splitting a path is not the same as walking one. Totalling per directory used to go through
// path.Dir, which cleans on the way, so a doubled separator never reached a label; building the
// tree from the file path directly has to clean it itself. -new with a trailing slash is how a
// caller produces one without meaning to.
func TestProcessCleansPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   []prettycov.FileCoverage
		newRoot string
		want    string
	}{
		{
			name:  "doubled separator in the profile",
			files: []prettycov.FileCoverage{file("m//a/x.go", 1, 1)},
			want:  "m/a",
		},
		{
			name:    "trailing slash on -new",
			files:   []prettycov.FileCoverage{file("zz/a/x.go", 1, 1), file("zz/b/y.go", 1, 1)},
			newRoot: "dg/",
			want:    "dg",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			shortened, _ := prettycov.Shorten(tc.files, "zz", tc.newRoot)
			tree := prettycov.Process(shortened)

			assert.Equal(t, tc.want, prettycov.Rows(tree, prettycov.Options{})[0].Label)
		})
	}
}

// A file with no directory component belongs to ".", which is a row like any other. Reading the
// package off the second-to-last path component instead left such a file hanging under the tree
// root, which nothing draws: the statements stayed in the total and appeared beside no row, and a
// profile of nothing but bare filenames printed an empty report and exited 0.
// "./x.go" and "x.go" are one file, because they are one path — path.Dir cleans a leading "." away
// as redundant. Worth pinning: making -new=. draw a single "." root means giving that prefix a
// meaning of its own, and then a profile naming both spellings of one package splits into two rows
// carrying the same label, with the package's statements divided between them.
func TestProcessReadsADotPrefixAsTheSamePath(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("sub/x.go", 4, 0),
		file("./sub/y.go", 0, 3),
	})

	rows := prettycov.Rows(tree, prettycov.Options{Depth: prettycov.DepthAll})

	require.Len(t, rows, 1, "one package, however its files were spelled")
	assert.Equal(t, "sub", rows[0].Label)
	assert.Equal(t, 7, rows[0].Coverage.Total(), "holding every statement of both")
}

// -new=. strips the root rather than renaming it to a node called ".", because that is what the
// path means: everything below the old root moves up, and a module whose top level holds more than
// one entry is drawn as more than one row. Identical to the profile it would have been written as.
func TestShortenToDotStripsTheRoot(t *testing.T) {
	t.Parallel()

	shortened, renamed := prettycov.Shorten(
		[]prettycov.FileCoverage{file("m/a.go", 5, 1), file("m/sub/b.go", 0, 4)}, "m", ".")
	require.Equal(t, 2, renamed)

	stripped := prettycov.Rows(prettycov.Process(shortened), prettycov.Options{Depth: prettycov.DepthAll})
	native := prettycov.Rows(prettycov.Process([]prettycov.FileCoverage{
		file("a.go", 5, 1), file("sub/b.go", 0, 4),
	}), prettycov.Options{Depth: prettycov.DepthAll})

	assert.Equal(t, native, stripped)
}

func TestProcessGivesFilesWithNoDirectoryAPackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   []prettycov.FileCoverage
		newRoot string
		want    []string
	}{
		{
			name:  "every file is bare",
			files: []prettycov.FileCoverage{file("a.go", 3, 1), file("b.go", 2, 0)},
			want:  []string{"."},
		},
		{
			// -new=. is a natural way to strip a module prefix, and it is how a real profile ends
			// up with a file at the top and packages beneath it.
			name:    "a bare file beside a package",
			files:   []prettycov.FileCoverage{file("foo/printer.go", 3, 1), file("foo/internal/app/a.go", 2, 1)},
			newRoot: ".",
			want:    []string{".", "internal/app"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			shortened, _ := prettycov.Shorten(tc.files, "foo", tc.newRoot)
			tree := prettycov.Process(shortened)
			rows := prettycov.Rows(tree, prettycov.Options{})

			labels := make([]string, 0, len(rows))

			total := 0

			for _, row := range rows {
				labels = append(labels, row.Label)
				total += row.Coverage.Total()
			}

			assert.Equal(t, tc.want, labels)
			assert.Equal(t, tree.Coverage.Total(), total,
				"every statement in the profile is drawn beside some row")
		})
	}
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

	for _, key := range []string{"", "nope", "m/nope", "m/x/own.go/deeper", "m/x/own.go/"} {
		assert.Nil(t, tree.Get(key), "Get(%q)", key)
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

// Process must not write through the slice it is handed, which it documents and a caller reusing
// the parse for a second report relies on. Handed the slice directly: with Shorten in between this
// asserted on a slice Process never saw, so it held even when Process rewrote every path it got.
func TestProcessDoesNotModifyItsInput(t *testing.T) {
	t.Parallel()

	files := []prettycov.FileCoverage{file("example.com/m/pkg/a.go", 1, 1)}
	before := files[0].File

	prettycov.Process(files)

	assert.Equal(t, before, files[0].File, "Process rewrote the caller's slice")
}

// Every statement in the profile hangs off exactly one leaf, so a node with children holds no
// statements of its own and its total is precisely their sum. That is what makes a rendered report
// addable, and it is the reason the profile's files are nodes rather than one row standing in for
// them: a directory that both held statements and had children would leave a share on the parent
// that no row below it accounts for.
func TestProcessMakesEveryParentTheSumOfItsChildren(t *testing.T) {
	t.Parallel()

	files, err := prettycov.ParseProfile(filepath.Join("testdata", "delegator.coverage.out"))
	require.NoError(t, err)

	var walk func(path string, node *prettycov.PathTree) int

	walk = func(path string, node *prettycov.PathTree) int {
		if len(node.Children) == 0 && len(node.Files) == 0 {
			return node.Coverage.Total()
		}

		sum := 0

		for _, below := range []map[string]*prettycov.PathTree{node.Files, node.Children} {
			for name, child := range below {
				sum += walk(path+"/"+name, child)
			}
		}

		assert.Equal(t, sum, node.Coverage.Total(),
			"%s does not equal the sum of its children", path)

		return sum
	}

	tree := prettycov.Process(files)

	assert.Equal(t, 568, walk("", tree), "the profile's own total")
}

func file(name string, covered, uncovered int) prettycov.FileCoverage {
	return prettycov.FileCoverage{
		File:     name,
		Coverage: prettycov.CoverageStats{Covered: covered, Uncovered: uncovered},
	}
}

func BenchmarkProcess(b *testing.B) {
	files := syntheticProfile(b)

	b.ReportAllocs()

	for b.Loop() {
		_ = prettycov.Process(files)
	}
}

// Get is called once per invocation, so this exists to hold a claim rather than to chase a cost:
// the walk allocates nothing, for a hit, a file hit and a miss alike.
func BenchmarkGet(b *testing.B) {
	tree := prettycov.Process(syntheticProfile(b))

	for _, bc := range []struct {
		name string
		key  string
	}{
		{name: "package", key: "github.com/acme/monorepo/unit3/pkg/logger"},
		{name: "file", key: "github.com/acme/monorepo/unit3/pkg/logger/logger.go"},
		{name: "miss", key: "github.com/acme/monorepo/unit3/pkg/nope"},
	} {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				_ = tree.Get(bc.key)
			}
		})
	}
}
