package discover_test

import (
	"os/exec"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/cover"
	"golang.org/x/tools/txtar"

	"github.com/screwyprof/prettycov/internal/discover"
)

// state is what a package amounts to once discovery and the profile are read together. Neither
// alone can say: a profile does not record what was meant to be in it, and the source does not
// know what ran.
type state string

const (
	stateCovered  state = "tested, covered"
	stateZero     state = "tested, nothing of its own covered"
	stateLost     state = "tested, absent from the profile"
	stateUntested state = "untested, honest zero"
)

// TestPackageStates pins the distinction the whole tool rests on: 0.00 and silence are different
// answers, and only one of them means the package was measured.
//
// stateLost is the one that costs something. go writes nothing at all for a package whose test
// dies, so the total is computed over a smaller denominator — on this tree, 33.3% whether the
// lost package would have been 100.0% or 0.0%. Same number, opposite truths.
func TestPackageStates(t *testing.T) {
	t.Parallel()

	root := extractArchive(t, "states.txtar")

	profile := runCoverage(t, root)
	repo, err := discover.Scan(t.Context(), root, discover.Config{})
	require.NoError(t, err)

	assert.Equal(t, map[string]state{
		"states.test/covered": stateCovered,
		"states.test/partial": stateZero,
		"states.test/panics":  stateLost,
		"states.test/helper":  stateUntested,
		"states.test/notest":  stateUntested,
	}, packageStates(t, repo, profile))
}

// runCoverage runs the tests under coverage and parses the profile. It ignores the exit status on
// purpose: a suite with a failing package is the case being measured, and it is also what every
// `|| true` in the wild is there to swallow.
func runCoverage(t *testing.T, dir string) []*cover.Profile {
	t.Helper()

	out := filepath.Join(t.TempDir(), "cover.out")

	cmd := exec.CommandContext(t.Context(), "go", "test", "-covermode=count", "-coverprofile="+out, "./...")
	cmd.Dir = dir

	_ = cmd.Run()

	profile, err := cover.ParseProfiles(out)
	require.NoError(t, err, "the run produced no readable profile")

	return profile
}

// packageStates reads each discovered package's state out of the profile. Profile file names are
// import paths, so the package is their directory.
func packageStates(t *testing.T, repo discover.Repo, profile []*cover.Profile) map[string]state {
	t.Helper()

	covered := map[string]int{}
	present := map[string]bool{}

	for _, file := range profile {
		pkg := path.Dir(file.FileName)
		present[pkg] = true

		for _, block := range file.Blocks {
			if block.Count > 0 {
				covered[pkg] += block.NumStmt
			}
		}
	}

	states := map[string]state{}
	for pkg := range repo.Packages() {
		states[pkg.ImportPath] = stateOf(pkg.HasTests, covered[pkg.ImportPath], present[pkg.ImportPath])
	}

	return states
}

// stateOf names the combination. Absence is not zero, which is the whole point.
func stateOf(hasTests bool, covered int, inProfile bool) state {
	switch {
	case hasTests && !inProfile:
		return stateLost
	case hasTests && covered > 0:
		return stateCovered
	case hasTests:
		return stateZero
	default:
		return stateUntested
	}
}

// extractArchive reads a testdata archive and writes it to a temporary directory.
func extractArchive(t *testing.T, name string) string {
	t.Helper()

	ar, err := txtar.ParseFile(filepath.Join("testdata", name))
	require.NoError(t, err)

	return extract(t, ar)
}
