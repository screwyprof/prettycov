package discover_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov/internal/discover"
)

// TestScanRealRepos triangulates discovery against two independent counts on checkouts that are
// not in this repository: a filesystem walk for directories holding Go source, and the naive
// `go list ./...` every recipe starts from. Set PRETTYCOV_REAL to a colon-separated list of paths.
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

			packages, tested, inWorkspace := 0, 0, 0

			for _, m := range repo.Modules {
				if m.InWorkspace {
					inWorkspace++
				}

				packages += len(m.Packages)

				for _, pkg := range m.Packages {
					if pkg.HasTests {
						tested++
					}
				}
			}

			t.Logf("%-20s modules=%-3d in_workspace=%-3d packages=%-4d tested=%-4d | "+
				"dirs_with_go=%-4d go_list_dotdotdot=%-4d",
				filepath.Base(root), len(repo.Modules), inWorkspace, packages, tested,
				dirsWithGo(t, root), countLines(goList(t, root, "./...")))
		})
	}
}

// dirsWithGo counts directories holding at least one non-test .go file, skipping what the go tool
// skips. It is an independent estimate of the package set: no go tool answers it.
func dirsWithGo(t *testing.T, root string) int {
	t.Helper()

	dirs := map[string]bool{}

	require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "vendor" || name == "testdata" ||
				strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}

			return nil
		}

		if strings.HasSuffix(d.Name(), ".go") {
			dirs[filepath.Dir(path)] = true
		}

		return nil
	}))

	return len(dirs)
}
