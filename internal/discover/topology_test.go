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

			repo, err := discover.Scan(t.Context(), dir, discover.Config{})
			require.NoError(t, err)

			got := observe(t, dir, repo)

			assertScanInvariants(t, repo)
			assert.Equal(t, expectations(t, ar.Comment, got), got)
		})
	}
}

// extract writes the archive to a temporary directory and returns it.
func extract(t *testing.T, ar *txtar.Archive) string {
	t.Helper()

	fsys, err := txtar.FS(ar)
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, fsys))

	return dir
}

// observe measures a tree every way the corpus records.
func observe(t *testing.T, dir string, repo discover.Repo) map[string]int {
	t.Helper()

	packages, tested, inWorkspace := 0, 0, 0

	for _, pkg := range repo.Packages() {
		packages++

		if pkg.HasTests {
			tested++
		}
	}

	for _, module := range repo.Modules {
		if module.InWorkspace {
			inWorkspace++
		}
	}

	return map[string]int{
		"modules_on_disk":      countGoMod(t, dir),
		"go_list_m":            countLines(goList(t, dir, "-m")),
		"go_list_dotdotdot":    countLines(goList(t, dir, "./...")),
		"discovered_modules":   len(repo.Modules),
		"discovered_packages":  packages,
		"packages_with_tests":  tested,
		"has_workspace":        boolToInt(repo.Workspace != ""),
		"modules_in_workspace": inWorkspace,
		"modules_with_errors":  len(repo.Broken()),
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// expectations reads the "key: number" lines from an archive's comment. The prose around them is
// for the reader; only these lines are asserted.
//
// Every key observe measures has to be stated, including the zeros — a fixture that says nothing
// about unreadable modules is claiming there are none, and that is worth writing down.
func expectations(t *testing.T, comment []byte, measured map[string]int) map[string]int {
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

		key = strings.TrimSpace(key)
		require.Contains(t, measured, key, "fixture states a key nothing measures")

		want[key] = n
	}

	require.Len(t, want, len(measured), "fixture must state every measured key, zeros included")

	return want
}

// assertScanInvariants checks what has to hold of any repository at all.
func assertScanInvariants(t *testing.T, repo discover.Repo) {
	t.Helper()

	assert.Empty(t, repo.Unreadable, "directories the walk could not descend into")

	seen := map[string]bool{}

	for _, pkg := range repo.Packages() {
		// A package with no directory is a go list row misread; the same import path twice means
		// a module boundary was crossed. Both double-count in a merged profile.
		assert.NotEmpty(t, pkg.Dir, "%s has no directory", pkg.ImportPath)
		assert.NotContains(t, seen, pkg.ImportPath, "%s is claimed by two modules", pkg.ImportPath)

		seen[pkg.ImportPath] = true
	}
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
