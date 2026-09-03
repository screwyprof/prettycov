package discover_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"

	"github.com/screwyprof/prettycov/internal/discover"
)

// Every recipe for building a coverage profile begins by deciding which packages exist, and every
// one of them gets it wrong on some layout — silently, because the go tool reports a smaller set
// without complaint and the run exits 0.
//
// Each archive in testdata/topologies is a repository shape, and its comment states what the go
// tool reports for it. Nothing here asserts that the tool is wrong: it records what it does, so a
// change in behaviour shows up as a failing test rather than as a number nobody questions.
func TestTopologies(t *testing.T) {
	t.Parallel()

	archives, err := filepath.Glob(filepath.Join("testdata", "topologies", "*.txtar"))
	require.NoError(t, err)
	require.NotEmpty(t, archives, "no topology fixtures found")

	for _, archive := range archives {
		t.Run(strings.TrimSuffix(filepath.Base(archive), ".txtar"), func(t *testing.T) {
			t.Parallel()

			ar, err := txtar.ParseFile(archive)
			require.NoError(t, err)

			dir := extract(t, ar)
			want := expectations(t, ar.Comment)

			assert.Equal(t, want["modules_on_disk"], countGoMod(t, dir),
				"go.mod files on disk — what a filesystem walk finds")
			assert.Equal(t, want["go_list_m"], countLines(goList(t, dir, "-m")),
				"go list -m — what the module graph reports")
			assert.Equal(t, want["go_list_dotdotdot"], countLines(goList(t, dir, "./...")),
				"go list ./... — what the default package pattern reaches")

			// And what discovery makes of the same tree. Where these disagree with the two
			// numbers above is precisely where a recipe built on go list under-measures.
			repo, err := discover.Scan(t.Context(), dir)
			require.NoError(t, err)

			packages, tested, inWorkspace, broken := 0, 0, 0, 0

			for _, m := range repo.Modules {
				if m.InWorkspace {
					inWorkspace++
				}

				if m.Err != nil {
					broken++
				}

				packages += len(m.Packages)

				for _, pkg := range m.Packages {
					if pkg.HasTests {
						tested++
					}
				}
			}

			assert.Len(t, repo.Modules, want["discovered_modules"], "modules discovered")
			assert.Equal(t, want["discovered_packages"], packages, "packages discovered")
			assert.Equal(t, want["packages_with_tests"], tested, "packages carrying tests")

			// A module can be absent from a workspace, or there can be no workspace to be absent
			// from. Only the first is a question about the module.
			assert.Equal(t, want["has_workspace"] == 1, repo.Workspace != "", "go.work found")
			assert.Equal(t, want["modules_in_workspace"], inWorkspace, "modules the workspace lists")

			// Absent from a fixture's comment this is zero, which is the assertion that matters
			// everywhere else: a healthy tree must not quietly record a module it failed to read.
			assert.Equal(t, want["modules_with_errors"], broken, "modules that could not be read")
		})
	}
}

// TestScanUsesBuildTags scans the build-tags fixture again, this time with the tag its acceptance
// suite is behind. The counts in that fixture are what discovery reports without it: one package,
// with the whole suite absent and nothing saying so.
func TestScanUsesBuildTags(t *testing.T) {
	t.Parallel()

	ar, err := txtar.ParseFile(filepath.Join("testdata", "topologies", "build-tags.txtar"))
	require.NoError(t, err)

	repo, err := discover.Scan(t.Context(), extract(t, ar), "acceptance")
	require.NoError(t, err)

	require.Len(t, repo.Modules, 1)

	paths := make([]string, 0, len(repo.Modules[0].Packages))

	for _, pkg := range repo.Modules[0].Packages {
		require.True(t, pkg.HasTests, "%s", pkg.ImportPath)

		paths = append(paths, pkg.ImportPath)
	}

	assert.ElementsMatch(t, []string{"tags.test", "tags.test/api"}, paths)
}

// TestScanFindsParentWorkspace scans below the directory holding go.work. The go tool searches
// parents for it, so a services/ subdirectory of a workspace is still in one, and discovery has to
// agree or every module there looks deliberately excluded.
func TestScanFindsParentWorkspace(t *testing.T) {
	t.Parallel()

	ar, err := txtar.ParseFile(filepath.Join("testdata", "topologies", "parent-workspace.txtar"))
	require.NoError(t, err)

	root := extract(t, ar)

	repo, err := discover.Scan(t.Context(), filepath.Join(root, "services"))
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(root, "go.work"), repo.Workspace)

	require.Len(t, repo.Modules, 1)
	assert.True(t, repo.Modules[0].InWorkspace, "the workspace above lists this module")
}

// TestScanRecordsUnreadableDir keeps a directory it cannot open from hiding the rest of the tree.
// A root-owned build artefact or a cache is enough to cause this, and reporting nothing at all
// would be a worse answer than reporting what is readable and saying where it stopped.
//
// No txtar fixture can express a mode, so the tree is built here.
func TestScanRecordsUnreadableDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	locked := filepath.Join(root, "locked")

	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module locked.test\n\ngo 1.27\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "f.go"), []byte("package locked\n"), 0o600))
	require.NoError(t, os.Mkdir(locked, 0o000))

	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("running as a user that can read mode 000")
	}

	repo, err := discover.Scan(t.Context(), root)
	require.NoError(t, err)

	assert.Equal(t, []string{locked}, repo.Unreadable)

	require.Len(t, repo.Modules, 1)
	require.NoError(t, repo.Modules[0].Err)
	assert.Equal(t, "locked.test", repo.Modules[0].Path)
}

// extract writes the archive to a temporary directory and returns it.
func extract(t *testing.T, ar *txtar.Archive) string {
	t.Helper()

	dir := t.TempDir()

	for _, f := range ar.Files {
		path := filepath.Join(dir, filepath.FromSlash(f.Name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, f.Data, 0o600))
	}

	return dir
}

// expectations reads the "key: number" lines from an archive's comment. The prose around them is
// for the reader; only these lines are asserted.
func expectations(t *testing.T, comment []byte) map[string]int {
	t.Helper()

	want := map[string]int{}

	for line := range strings.SplitSeq(string(comment), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}

		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			continue
		}

		want[strings.TrimSpace(key)] = n
	}

	require.NotEmpty(t, want, "fixture states no expectations")

	return want
}

// skipTestDir mirrors the package's own skipDir, which these tests cannot reach from outside. Its
// job is to be an independent statement of the rule rather than a call to the code under test.
func skipTestDir(name string) bool {
	return name == "vendor" || name == "testdata" ||
		strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".")
}

// countGoMod walks for go.mod the way discovery must: vendor holds whole modules that are not
// yours, and the go tool ignores testdata and underscore-prefixed directories.
func countGoMod(t *testing.T, dir string) int {
	t.Helper()

	count := 0

	require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path != dir && skipTestDir(d.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if d.Name() == "go.mod" {
			count++
		}

		return nil
	}))

	return count
}

// goList runs `go list` in dir and returns its stdout. A pattern that resolves to nothing is an
// answer, not a failure, so the error is deliberately ignored.
func goList(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", append([]string{"list"}, args...)...)
	cmd.Dir = dir

	out, _ := cmd.Output()

	return string(out)
}

func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	return strings.Count(s, "\n") + 1
}
