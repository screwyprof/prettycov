package prettycov_test

import (
	"math"
	"testing"

	"github.com/screwyprof/prettycov"
)

// Every statement in a subtree must be counted exactly once when rolling up into a parent.
// The shapes below are the ones that get this wrong; see the table for what each one probes.
func TestProcessCountsEachStatementOnce(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		files []prettycov.FileCoverage
		want  map[string]float64
	}{
		// A directory that is BOTH a package and a parent of packages. This is the shape that
		// breaks in practice: scraper/ in the corpus profile has service.go and subscriber.go
		// alongside a store/ subpackage, and reports 87.03% where the truth is 90.00%.
		"dir is both package and parent": {
			files: []prettycov.FileCoverage{
				file("m/scraper/service.go", 8, 2),
				file("m/scraper/store/store.go", 5, 5),
			},
			want: map[string]float64{
				"m/scraper":       65.00, // (8+5) / (10+10)
				"m/scraper/store": 50.00,
			},
		},

		// Sibling subtrees whose child counts differ. Each subtree's totals get scaled by its
		// own number of children, so the parent's weighted average skews.
		"siblings with differing child counts": {
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

		// Guard against over-correcting: the simple cases must keep working.
		"single package under root": {
			files: []prettycov.FileCoverage{
				file("m/pkg/a/a.go", 3, 1),
			},
			want: map[string]float64{
				"m":       75.00,
				"m/pkg":   75.00,
				"m/pkg/a": 75.00,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tree := prettycov.Process(tc.files, "", "")

			for path, want := range tc.want {
				node := tree.Get(path)
				if node == nil {
					t.Fatalf("path %q missing from tree", path)
				}

				if got := node.Coverage.Ratio; !closeEnough(got, want) {
					t.Errorf("%s: got %.2f%%, want %.2f%%", path, got, want)
				}
			}
		})
	}
}

func file(name string, covered, uncovered int) prettycov.FileCoverage {
	return prettycov.FileCoverage{
		File:     name,
		Coverage: prettycov.CoverageStats{Covered: covered, Uncovered: uncovered},
	}
}

func closeEnough(got, want float64) bool {
	return math.Abs(got-want) < 0.005
}
