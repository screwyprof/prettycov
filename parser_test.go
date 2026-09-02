package prettycov_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// Malformed profiles must be reported as ErrInvalidProfile. Deliberately no assertion on the
// wording: the cover package builds its errors with %v, so nothing of its text is contractual and
// it changes whenever x/tools does.
func TestParseProfileRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		profile string
	}{
		{name: "not a profile at all", profile: "this is not a coverage profile\n"},
		{name: "block line missing its count", profile: "mode: atomic\nnot-a-block\n"},
		{name: "negative statement count", profile: "mode: atomic\na/b.go:1.1,2.2 -3 1\n"},
		{name: "inconsistent statement count for one block", profile: "mode: atomic\n" +
			"a/b.go:1.1,2.2 3 1\na/b.go:1.1,2.2 4 1\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := prettycov.ParseProfile(writeProfile(t, tc.profile))

			require.ErrorIs(t, err, prettycov.ErrInvalidProfile)
		})
	}
}

// A profile that cannot be opened keeps its cause, so callers can tell "no such file" from "not a
// profile" without reading either message. fs.ErrNotExist rather than the OS's text, which differs
// between platforms.
func TestParseProfileReportsUnreadableFile(t *testing.T) {
	t.Parallel()

	_, err := prettycov.ParseProfile(filepath.Join(t.TempDir(), "absent.out"))

	require.ErrorIs(t, err, fs.ErrNotExist)
	require.NotErrorIs(t, err, prettycov.ErrInvalidProfile, "an unreadable file is not an invalid one")
}

// A profile carrying only a mode line is well formed and simply has nothing in it. Parsing must
// succeed; it is the caller's business that the result is empty.
func TestParseProfileAcceptsProfileWithNoBlocks(t *testing.T) {
	t.Parallel()

	items, err := prettycov.ParseProfile(writeProfile(t, "mode: atomic\n"))

	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestParseProfileSumsStatementsPerFile(t *testing.T) {
	t.Parallel()

	profile := "mode: atomic\n" +
		"m/a.go:1.1,2.2 3 1\n" + // covered
		"m/a.go:4.1,5.2 2 0\n" + // not covered
		"m/b.go:1.1,2.2 4 9\n" // covered

	items, err := prettycov.ParseProfile(writeProfile(t, profile))
	require.NoError(t, err)

	byFile := map[string]prettycov.CoverageStats{}
	for _, item := range items {
		byFile[item.File] = item.Coverage
	}

	assert.Equal(t, prettycov.CoverageStats{Covered: 3, Uncovered: 2}, byFile["m/a.go"])
	assert.Equal(t, prettycov.CoverageStats{Covered: 4, Uncovered: 0}, byFile["m/b.go"])
}

// `go test -coverpkg` emits the same block once per test binary that loaded the package. Those
// repeats describe one block, so they must be merged, not added up: a block is covered when any
// run hit it. Getting this wrong inflates every total.
func TestParseProfileMergesRepeatedBlocks(t *testing.T) {
	t.Parallel()

	profile := "mode: atomic\n" +
		"m/a.go:1.1,2.2 3 0\n" +
		"m/a.go:1.1,2.2 3 0\n" +
		"m/a.go:1.1,2.2 3 5\n" // one run hit it, so the block counts as covered

	items, err := prettycov.ParseProfile(writeProfile(t, profile))
	require.NoError(t, err)

	require.Len(t, items, 1)
	assert.Equal(t, prettycov.CoverageStats{Covered: 3, Uncovered: 0}, items[0].Coverage)
}

// A package with no statements must still produce a comparable result. Storing a derived ratio
// put a NaN in the struct, and NaN does not equal itself, so two identical parses compared
// unequal — breaking any caller that compares stats, not just the display.
func TestCoverageStatsAreComparable(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, "mode: atomic\nm/doc.go:1.1,2.2 0 0\n")

	first, err := prettycov.ParseProfile(path)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := prettycov.ParseProfile(path)
	require.NoError(t, err)

	assert.Equal(t, first, second, "two identical parses must compare equal")
}

func writeProfile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "coverage.out")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}
