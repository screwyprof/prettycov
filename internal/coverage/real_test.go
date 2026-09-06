package coverage_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov/internal/coverage"
	"github.com/screwyprof/prettycov/internal/discover"
)

// TestMeasureRealRepos checks the plumbing on checkouts outside this repository: every module that
// has packages and could be read must come back with a profile. Set PRETTYCOV_REAL to a
// colon-separated list; PRETTYCOV_ARGS is forwarded to go test (use -run=XXXNOMATCH to compile and
// measure everything without waiting for the suites to run).
func TestMeasureRealRepos(t *testing.T) {
	t.Parallel()

	paths := os.Getenv("PRETTYCOV_REAL")
	if paths == "" {
		t.Skip("set PRETTYCOV_REAL")
	}

	var args []string
	if a := os.Getenv("PRETTYCOV_ARGS"); a != "" {
		args = strings.Fields(a)
	}

	for root := range strings.SplitSeq(paths, ":") {
		t.Run(filepath.Base(root), func(t *testing.T) {
			t.Parallel()

			repo, err := discover.Scan(t.Context(), root, discover.Config{})
			require.NoError(t, err)

			result, err := coverage.Measure(t.Context(), repo, coverage.Config{
				Args: args, Dir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard,
			})
			require.NoError(t, err)

			report(t, root, repo, result)
			assertNothingSilentlyUnmeasured(t, root, result)
		})
	}
}

// report prints what the pass amounted to. The numbers only mean anything next to the repository
// they came from, so they are logged rather than asserted.
func report(t *testing.T, root string, repo discover.Repo, result coverage.Result) {
	t.Helper()

	measured, empty, failed, outside := 0, 0, 0, 0

	for _, run := range result.Runs {
		switch {
		case run.Profile != "":
			measured++
		case run.Err != nil:
			failed++

			t.Logf("  no data: %s: %s", rel(root, run.Module.Dir), tail(run.Err))
		default:
			empty++
		}

		if repo.Workspace != "" && !run.Module.InWorkspace {
			outside++
		}
	}

	t.Logf("%-20s modules=%-3d measured=%-3d no_packages=%-2d no_data=%-2d outside_workspace=%d",
		filepath.Base(root), len(result.Runs), measured, empty, failed, outside)
}

// assertNothingSilentlyUnmeasured is the rule this harness exists for: a module discovery could
// read and that holds packages either produced a profile or said why not. Silence would mean we
// reached it in discovery and then lost it.
func assertNothingSilentlyUnmeasured(t *testing.T, root string, result coverage.Result) {
	t.Helper()

	for _, run := range result.Runs {
		if run.Module.Err != nil || len(run.Module.Packages) == 0 {
			continue
		}

		assert.False(t, run.Profile == "" && run.Err == nil,
			"%s produced neither a profile nor a reason", rel(root, run.Module.Dir))
	}
}

func rel(root, dir string) string {
	if r, err := filepath.Rel(root, dir); err == nil {
		return r
	}

	return dir
}

// tail is the last line of go's own complaint, which is the part that names the cause.
func tail(err error) string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")

	return strings.TrimSpace(lines[len(lines)-1])
}
