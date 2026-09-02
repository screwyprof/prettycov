package prettycov_test

import (
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// Every profile in testdata is checked against totals derived straight from the file text, for
// every directory in it — not just the root. Adding a profile therefore needs no hand-computed
// expectations: drop the file in testdata and it is covered from then on.
func TestProcessMatchesProfileTotals(t *testing.T) {
	t.Parallel()

	profiles, err := filepath.Glob(filepath.Join("testdata", "*.out"))
	require.NoError(t, err)
	require.NotEmpty(t, profiles, "no profiles found in testdata")

	for _, profile := range profiles {
		t.Run(filepath.Base(profile), func(t *testing.T) {
			t.Parallel()

			files, err := prettycov.ParseProfile(profile)
			require.NoError(t, err)

			tree := prettycov.Process(files, "", "")

			for dir, want := range oracleTotals(t, profile) {
				node := tree.Get(dir)
				require.NotNilf(t, node, "%q is in the profile but missing from the tree", dir)

				// Counts only. The ratio is derived from these by CoverageStats.Ratio, so it
				// cannot disagree with them — which it could when it was a stored field, and did.
				assert.Equalf(t, want.Covered, node.Coverage.Covered, "covered statements at %q", dir)
				assert.Equalf(t, want.Uncovered, node.Coverage.Uncovered, "uncovered statements at %q", dir)
			}
		})
	}
}

// oracleTotals derives, for every directory in a profile, the statements beneath it. It reads the
// profile text itself rather than going through cover.ParseProfiles or anything in this package,
// so it is an independent check rather than a restatement of the code under test.
//
// Duplicate blocks are merged the way -coverpkg output requires: one block counts once, and is
// covered if any run hit it. Summing counts and OR-ing them agree on that, so this holds for
// every mode the go tool emits.
func oracleTotals(t *testing.T, profile string) map[string]prettycov.CoverageStats {
	t.Helper()

	raw, err := os.ReadFile(profile)
	require.NoError(t, err)

	statements := map[string]int{}
	hits := map[string]int{}

	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}

		fields := strings.Fields(line)
		require.Lenf(t, fields, 3, "malformed block line: %q", line)

		numStmt, err := strconv.Atoi(fields[1])
		require.NoErrorf(t, err, "block %q", line)

		count, err := strconv.Atoi(fields[2])
		require.NoErrorf(t, err, "block %q", line)

		statements[fields[0]] = numStmt
		hits[fields[0]] += count
	}

	totals := map[string]prettycov.CoverageStats{}

	for block, numStmt := range statements {
		covered := hits[block] > 0

		// Charge the block to its own package and to every directory above it.
		for dir := path.Dir(blockFile(t, block)); dir != "." && dir != "/"; dir = path.Dir(dir) {
			stat := totals[dir]

			if covered {
				stat.Covered += numStmt
			} else {
				stat.Uncovered += numStmt
			}

			totals[dir] = stat
		}
	}

	return totals
}

// blockFile strips the position suffix, turning "a/b.go:1.1,2.2" into "a/b.go".
func blockFile(t *testing.T, block string) string {
	t.Helper()

	idx := strings.LastIndex(block, ":")
	require.Positivef(t, idx, "block %q has no position suffix", block)

	return block[:idx]
}
