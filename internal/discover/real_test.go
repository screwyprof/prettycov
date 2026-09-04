package discover_test

import (
	"errors"
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov/internal/discover"
)

// TestScanRealRepos triangulates discovery on checkouts that are not in this repository. Set
// PRETTYCOV_REAL to a colon-separated list of paths, each optionally suffixed #tag,tag.
//
// The corpus states what discovery should report on a shape someone wrote down. This states what
// it must never do on shapes nobody anticipated, and every rule here was written after a real
// checkout broke it.
func TestScanRealRepos(t *testing.T) {
	t.Parallel()

	paths := os.Getenv("PRETTYCOV_REAL")
	if paths == "" {
		t.Skip("set PRETTYCOV_REAL=/path/one:/path/two, optionally /path#tag,tag")
	}

	for entry := range strings.SplitSeq(paths, ":") {
		root, tags := parseEntry(entry)

		t.Run(filepath.Base(root), func(t *testing.T) {
			t.Parallel()

			repo, err := discover.Scan(t.Context(), root, discover.Config{Tags: tags})
			require.NoError(t, err)

			sum := summarise(repo)
			constrained, lost := classify(t, root, buildContext(tags), accountedDirs(t, repo))

			report(t, root, sum, constrained, countLines(goList(t, root, "./...")))

			assertScanInvariants(t, repo)
			assertNothingLost(t, root, lost)
		})
	}
}

// assertNothingLost is the rule the whole harness exists for. Anything holding Go source is a
// package discovery found, a directory the constraints exclude, or a bug.
func assertNothingLost(t *testing.T, root string, lost []string) {
	t.Helper()

	assert.Empty(t, relativeTo(root, lost), "directories holding Go source that are neither a "+
		"discovered package nor excluded by build constraints")
}

// report prints what the scan found. It is the output of a run, not an assertion: the numbers are
// only meaningful next to the repository they came from.
func report(t *testing.T, root string, sum totals, constrained []string, dotdotdot int) {
	t.Helper()

	t.Logf("%-20s modules=%-3d in_workspace=%-3d packages=%-4d tested=%-4d | "+
		"constrained_out=%-3d go_list_dotdotdot=%-4d",
		filepath.Base(root), sum.modules, sum.inWorkspace, sum.packages, sum.tested,
		len(constrained), dotdotdot)

	for _, dir := range relativeTo(root, constrained) {
		t.Logf("  constrained out: %s", dir)
	}

	for _, unreadable := range sum.unreadable {
		t.Logf("  unreadable: %s", unreadable)
	}
}

// parseEntry splits a PRETTYCOV_REAL entry into its path and build tags.
func parseEntry(entry string) (root string, tags []string) {
	root, list, _ := strings.Cut(entry, "#")
	if list == "" {
		return root, nil
	}

	return root, strings.Split(list, ",")
}

// buildContext is the context go/build would use for a run under these tags.
func buildContext(tags []string) *build.Context {
	ctx := build.Default
	ctx.BuildTags = tags

	return &ctx
}

// totals is what a scan amounts to.
type totals struct {
	modules     int
	packages    int
	tested      int
	inWorkspace int
	unreadable  []string
}

// summarise counts a scan. It asserts nothing, so the numbers can be reported on a run that fails.
func summarise(repo discover.Repo) totals {
	sum := totals{modules: len(repo.Modules)}

	for _, module := range repo.Modules {
		if module.InWorkspace {
			sum.inWorkspace++
		}
	}

	for _, module := range repo.Broken() {
		sum.unreadable = append(sum.unreadable, module.Dir+": "+module.Err.Error())
	}

	for pkg := range repo.Packages() {
		sum.packages++

		if pkg.HasTests {
			sum.tested++
		}
	}

	return sum
}

// accountedDirs is every directory the scan explains, so classify can look only at the rest.
//
// A module that could not be read contributes its whole subtree: nothing below it can be listed,
// which is not the same as missing. bubbletea ships a tutorials/go.mod that needs go mod tidy.
func accountedDirs(t *testing.T, repo discover.Repo) map[string]bool {
	t.Helper()

	accounted := map[string]bool{}

	for _, module := range repo.Broken() {
		markTree(t, accounted, module.Dir)
	}

	for pkg := range repo.Packages() {
		accounted[pkg.Dir] = true
	}

	return accounted
}

// classify splits the directories holding Go source that discovery did not report into the ones
// go/build excludes by constraints and the ones nothing accounts for.
//
// go/build is the authority here rather than a file count: NoGoError means "no buildable Go source
// files", and IgnoredGoFiles then names what the constraints hid.
func classify(t *testing.T, root string, ctx *build.Context, accounted map[string]bool) (constrained, lost []string) {
	t.Helper()

	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if path != root && skipTestDir(entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		dir := filepath.Dir(path)
		if !strings.HasSuffix(entry.Name(), ".go") || accounted[dir] {
			return nil
		}

		accounted[dir] = true

		if constrainedOut(ctx, dir) {
			constrained = append(constrained, dir)
		} else {
			lost = append(lost, dir)
		}

		return nil
	}))

	return constrained, lost
}

// constrainedOut reports whether every Go file in dir is excluded by build constraints.
func constrainedOut(ctx *build.Context, dir string) bool {
	pkg, err := ctx.ImportDir(dir, 0)
	if _, ok := errors.AsType[*build.NoGoError](err); !ok {
		return false
	}

	return len(pkg.IgnoredGoFiles) > 0
}

// markTree records every directory under root as accounted for.
func markTree(t *testing.T, accounted map[string]bool, root string) {
	t.Helper()

	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			accounted[path] = true
		}

		return err
	}))
}

// relativeTo trims the checkout's own path, which is noise repeated on every line.
func relativeTo(root string, dirs []string) []string {
	trimmed := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		trimmed = append(trimmed, strings.TrimPrefix(dir, root+string(filepath.Separator)))
	}

	return trimmed
}
