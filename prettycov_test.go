package prettycov_test

import (
	"path/filepath"
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

			tree := prettycov.Process(tc.files, "", "")

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

func TestPathTreeGetReturnsNilForAPathThatIsNotThere(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{file("m/pkg/a.go", 1, 1)}, "", "")

	assert.Nil(t, tree.Get("m/absent"))
	assert.NotNil(t, tree.Get("m/pkg"), "and finds one that is")
}

func TestProcessShortensTheRootPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    string
		old     string
		replace string
		want    string
	}{
		{
			name: "replaces a leading root", file: "github.com/o/repo/pkg/a.go",
			old: "github.com/o/repo", replace: "repo", want: "repo/pkg",
		},
		{
			// "api" appears inside "rapid" first. Replacing the first match anywhere turned
			// github.com/rapid/api into github.com/rcored/api.
			name: "only a leading one", file: "github.com/rapid/api/svc/a.go",
			old: "api", replace: "core", want: "github.com/rapid/api/svc",
		},
		{
			// The separator is implied, so writing it out changes nothing.
			name: "a trailing slash on the old root is the same root", file: "github.com/o/repo/pkg/a.go",
			old: "github.com/o/repo/", replace: "repo", want: "repo/pkg",
		},
		{
			// A prefix is not a root: "github.com/foo" starts "github.com/foobar" too, and cutting
			// it there left the unrelated package as "xbar/svc".
			name: "and only a whole path segment", file: "github.com/foobar/svc/a.go",
			old: "github.com/foo", replace: "x", want: "github.com/foobar/svc",
		},
		{
			// An empty old root matches at position 0, so this used to prepend rather than replace.
			name: "no old root means no rewrite", file: "github.com/o/repo/pkg/a.go",
			old: "", replace: "repo", want: "github.com/o/repo/pkg",
		},
		{
			name: "no new root means no rewrite", file: "github.com/o/repo/pkg/a.go",
			old: "github.com/o/repo", replace: "", want: "github.com/o/repo/pkg",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := prettycov.Process([]prettycov.FileCoverage{file(tc.file, 1, 1)}, tc.old, tc.replace)

			assert.NotNil(t, tree.Get(tc.want), "want a node at %q", tc.want)
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

			tree := prettycov.Process(tc.files, "zz", tc.newRoot)

			assert.Equal(t, tc.want, prettycov.Rows(tree, prettycov.Options{})[0].Label)
		})
	}
}

// A file with no directory component belongs to ".", which is a row like any other. Reading the
// package off the second-to-last path component instead left such a file hanging under the tree
// root, which nothing draws: the statements stayed in the total and appeared beside no row, and a
// profile of nothing but bare filenames printed an empty report and exited 0.
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

			tree := prettycov.Process(tc.files, "foo", tc.newRoot)
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

// Children holds files as well as directories, so a caller enumerating packages needs to be able
// to tell them apart. Nothing unexported can answer that from outside the package.
func TestPathTreeReportsWhichNodesAreFiles(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{
		file("m/x/own.go", 1, 1),
		file("m/x/sub/s.go", 1, 1),
	}, "", "")

	pkg := tree.Get("m/x")
	require.NotNil(t, pkg)

	assert.False(t, pkg.IsFile(), "a directory")
	assert.False(t, pkg.Children["sub"].IsFile(), "a directory holding a file")
	assert.True(t, pkg.Children["own.go"].IsFile(), "a file")

	// Both ways of reaching a node hand back nil for one the profile does not hold, and IsFile is
	// how the CHANGELOG tells a caller to read a tree, so it answers rather than panicking.
	assert.False(t, tree.Get("m/x/nope").IsFile(), "a path the profile does not hold")
	assert.False(t, pkg.Children["nope.go"].IsFile(), "a name Children does not have")
}

// Process must not write through the slice it is handed.
func TestProcessDoesNotModifyItsInput(t *testing.T) {
	t.Parallel()

	files := []prettycov.FileCoverage{file("example.com/m/pkg/a.go", 1, 1)}
	before := files[0].File

	prettycov.Process(files, "example.com/m", "m")

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
		if len(node.Children) == 0 {
			return node.Coverage.Total()
		}

		sum := 0
		for name, child := range node.Children {
			sum += walk(path+"/"+name, child)
		}

		assert.Equal(t, sum, node.Coverage.Total(),
			"%s does not equal the sum of its children", path)

		return sum
	}

	tree := prettycov.Process(files, "", "")

	assert.Equal(t, 568, walk("", tree), "the profile's own total")
}

func file(name string, covered, uncovered int) prettycov.FileCoverage {
	return prettycov.FileCoverage{
		File:     name,
		Coverage: prettycov.CoverageStats{Covered: covered, Uncovered: uncovered},
	}
}
