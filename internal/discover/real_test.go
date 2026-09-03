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

			repo, err := discover.Scan(t.Context(), root, tags...)
			require.NoError(t, err)

			sum := summarise(t, repo)
			constrained, lost := classify(t, root, buildContext(tags), sum.accounted)

			t.Logf("%-20s modules=%-3d in_workspace=%-3d packages=%-4d tested=%-4d | "+
				"constrained_out=%-3d go_list_dotdotdot=%-4d",
				filepath.Base(root), len(repo.Modules), sum.inWorkspace, sum.packages, sum.tested,
				len(constrained), countLines(goList(t, root, "./...")))

			for _, dir := range constrained {
				t.Logf("  constrained out: %s", strings.TrimPrefix(dir, root+string(filepath.Separator)))
			}

			assert.Empty(t, lost, "directories holding Go source that are neither a discovered "+
				"package nor excluded by build constraints")
		})
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

// totals is what a scan amounts to, plus the directories it accounts for.
type totals struct {
	packages    int
	tested      int
	inWorkspace int

	// accounted holds every directory the scan explains, so classify can look only at the rest.
	accounted map[string]bool
}

// summarise counts a scan and checks the invariants that hold on every repository.
func summarise(t *testing.T, repo discover.Repo) totals {
	t.Helper()

	sum := totals{accounted: map[string]bool{}}
	owner := map[string]string{}

	assert.Empty(t, repo.Unreadable, "directories the walk could not descend into")

	for _, module := range repo.Modules {
		if module.InWorkspace {
			sum.inWorkspace++
		}

		if module.Err != nil {
			// Nothing beneath an unreadable module can be accounted for, so it is not missing
			// either. bubbletea ships a tutorials/go.mod that needs go mod tidy.
			t.Logf("  unreadable: %s: %v", module.Dir, module.Err)
			markTree(t, sum.accounted, module.Dir)

			continue
		}

		sum.packages += len(module.Packages)

		for _, pkg := range module.Packages {
			sum.accounted[pkg.Dir] = true

			// A package with no directory is a go list row misread; the same import path twice
			// means a module boundary was crossed. Both double-count in a merged profile.
			assert.NotEmpty(t, pkg.Dir, "%s has no directory", pkg.ImportPath)
			assert.NotContains(t, owner, pkg.ImportPath, "also in %s", owner[pkg.ImportPath])

			owner[pkg.ImportPath] = module.Path

			if pkg.HasTests {
				sum.tested++
			}
		}
	}

	return sum
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
