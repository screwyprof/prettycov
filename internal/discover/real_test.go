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
// Every directory holding Go source must either be a package discovery found, or be one go/build
// says is excluded by constraints. Anything else is a package we lost, which is how the build-tag
// defect surfaced: delegator's whole acceptance suite was missing and no count said so.
func TestScanRealRepos(t *testing.T) {
	t.Parallel()

	paths := os.Getenv("PRETTYCOV_REAL")
	if paths == "" {
		t.Skip("set PRETTYCOV_REAL=/path/one:/path/two, optionally /path#tag,tag")
	}

	for entry := range strings.SplitSeq(paths, ":") {
		root, tagList, _ := strings.Cut(entry, "#")

		t.Run(filepath.Base(root), func(t *testing.T) {
			t.Parallel()

			var tags []string
			if tagList != "" {
				tags = strings.Split(tagList, ",")
			}

			repo, err := discover.Scan(t.Context(), root, tags...)
			require.NoError(t, err)

			found := map[string]bool{}
			packages, tested, inWorkspace := 0, 0, 0

			for _, m := range repo.Modules {
				if m.InWorkspace {
					inWorkspace++
				}

				if m.Err != nil {
					// Nothing beneath an unreadable module can be accounted for, so it is not
					// missing either. bubbletea ships a tutorials/go.mod that needs go mod tidy.
					t.Logf("  unreadable: %s: %v", m.Dir, m.Err)
					markTree(t, found, m.Dir)
				}

				packages += len(m.Packages)

				for _, pkg := range m.Packages {
					found[pkg.Dir] = true

					if pkg.HasTests {
						tested++
					}
				}
			}

			ctx := build.Default
			ctx.BuildTags = tags

			constrained, lost := classify(t, root, &ctx, found)

			t.Logf("%-20s modules=%-3d in_workspace=%-3d packages=%-4d tested=%-4d | "+
				"constrained_out=%-3d go_list_dotdotdot=%-4d",
				filepath.Base(root), len(repo.Modules), inWorkspace, packages, tested,
				len(constrained), countLines(goList(t, root, "./...")))

			for _, dir := range constrained {
				t.Logf("  constrained out: %s", strings.TrimPrefix(dir, root+"/"))
			}

			assert.Empty(t, lost, "directories holding Go source that are neither a discovered "+
				"package nor excluded by build constraints")
		})
	}
}

// classify splits the directories holding Go source that discovery did not report into the ones
// go/build excludes by constraints and the ones nothing accounts for.
//
// go/build is the authority here rather than a file count: NoGoError means "no buildable Go source
// files", and IgnoredGoFiles then names what the constraints hid.
func classify(t *testing.T, root string, ctx *build.Context, found map[string]bool) (constrained, lost []string) {
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
		if !strings.HasSuffix(entry.Name(), ".go") || found[dir] {
			return nil
		}

		found[dir] = true

		if constrainedOut(ctx, dir) {
			constrained = append(constrained, dir)
		} else {
			lost = append(lost, dir)
		}

		return nil
	}))

	return constrained, lost
}

// markTree records every directory under root as accounted for.
func markTree(t *testing.T, found map[string]bool, root string) {
	t.Helper()

	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			found[path] = true
		}

		return err
	}))
}

// constrainedOut reports whether every Go file in dir is excluded by build constraints.
func constrainedOut(ctx *build.Context, dir string) bool {
	pkg, err := ctx.ImportDir(dir, 0)
	if _, ok := errors.AsType[*build.NoGoError](err); !ok {
		return false
	}

	return len(pkg.IgnoredGoFiles) > 0
}
