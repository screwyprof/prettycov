package prettycov_test

import (
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

				pct, ok := node.Coverage.Ratio()
				require.Truef(t, ok, "no statements at %q", path)
				assert.InDeltaf(t, want, pct, ratioTolerance, "coverage at %q", path)
			}
		})
	}
}

func TestCoverageStatsRatio(t *testing.T) {
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pct, ok := tc.stats.Ratio()

			assert.Equal(t, tc.wantOK, ok)
			assert.InDelta(t, tc.wantPct, pct, ratioTolerance)
		})
	}
}

func TestPathTreeGetReturnsNilForAPathThatIsNotThere(t *testing.T) {
	t.Parallel()

	tree := prettycov.Process([]prettycov.FileCoverage{file("m/pkg/a.go", 1, 1)}, "", "")

	assert.Nil(t, tree.Get("m/absent"))
	assert.NotNil(t, tree.Get("m/pkg"), "and finds one that is")
}

// Process must not write through the slice it is handed.
func TestProcessDoesNotModifyItsInput(t *testing.T) {
	t.Parallel()

	files := []prettycov.FileCoverage{file("example.com/m/pkg/a.go", 1, 1)}
	before := files[0].File

	prettycov.Process(files, "example.com/m", "m")

	assert.Equal(t, before, files[0].File, "Process rewrote the caller's slice")
}

func file(name string, covered, uncovered int) prettycov.FileCoverage {
	return prettycov.FileCoverage{
		File:     name,
		Coverage: prettycov.CoverageStats{Covered: covered, Uncovered: uncovered},
	}
}
