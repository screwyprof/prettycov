package prettycov_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// update rewrites the golden files instead of comparing against them. Read the diff before
// committing: this test's whole value is that an unintended change has to be looked at.
//
//nolint:gochecknoglobals // a test flag has nowhere else to live.
var update = flag.Bool("update", false, "rewrite the golden files")

// Every other test here asks a question about the report — is it sorted, does it collapse, does it
// colour. None of them would notice the whole thing changing shape, and TestDisplayTreeIsDeterministic
// compares renders only to each other, so it passes just as happily if every row is wrong.
//
// This is the check that refactoring the printer keeps needing: render a real profile and compare
// it to what it rendered before.
func TestDisplayTreeMatchesGolden(t *testing.T) {
	t.Parallel()

	files, err := prettycov.ParseProfile(filepath.Join("testdata", "delegator.coverage.out"))
	require.NoError(t, err)

	tree := prettycov.Process(files, "github.com/screwyprof/delegator", "delegator")

	for _, depth := range []uint{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("depth-%d", depth), func(t *testing.T) {
			t.Parallel()

			// Colour on, so the escapes are part of what is pinned. A golden file that stops at
			// the text would miss a grade landing in the wrong band.
			assertGolden(t, fmt.Sprintf("delegator-depth-%d.golden", depth),
				renderWith(t, tree, depth, prettycov.ColorAlways))
		})
	}
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))

		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file; run: go test . -update")

	assert.Equal(t, string(want), got, "run `go test . -update` and read the diff")
}
