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

			repo, err := discover.Scan(t.Context(), dir)
			require.NoError(t, err)

			assertScanInvariants(t, repo)
			assert.Equal(t, want, observe(t, dir, repo))
		})
	}
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

// measured are the keys every fixture is judged on. A fixture states the ones that are not zero;
// the rest still hold, and most of them are only interesting when they are zero — a healthy tree
// must record no unreadable module, and a tree with no go.work must claim no members.
var measured = []string{ //nolint:gochecknoglobals // the corpus schema, shared by two functions.
	"modules_on_disk",      // what a filesystem walk finds
	"go_list_m",            // what the module graph reports
	"go_list_dotdotdot",    // what the default package pattern reaches
	"discovered_modules",   // and what discovery makes of the same tree; where these disagree
	"discovered_packages",  // with the two above is where a recipe built on go list under-measures
	"packages_with_tests",  //
	"has_workspace",        // 1 or 0: a go.work governs this tree, or none does
	"modules_in_workspace", // meaningless unless has_workspace is 1
	"modules_with_errors",  // modules found on disk that could not be read
}

// observe measures a tree every way the corpus records.
func observe(t *testing.T, dir string, repo discover.Repo) map[string]int {
	t.Helper()

	got := map[string]int{
		"modules_on_disk":     countGoMod(t, dir),
		"go_list_m":           countLines(goList(t, dir, "-m")),
		"go_list_dotdotdot":   countLines(goList(t, dir, "./...")),
		"discovered_modules":  len(repo.Modules),
		"discovered_packages": 0,
	}

	if repo.Workspace != "" {
		got["has_workspace"] = 1
	}

	got["modules_with_errors"] = len(repo.Broken())

	for _, module := range repo.Modules {
		if module.InWorkspace {
			got["modules_in_workspace"]++
		}
	}

	for pkg := range repo.Packages() {
		got["discovered_packages"]++

		if pkg.HasTests {
			got["packages_with_tests"]++
		}
	}

	return fill(got)
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

		require.Contains(t, measured, strings.TrimSpace(key), "fixture states an unmeasured key")

		want[strings.TrimSpace(key)] = n
	}

	require.NotEmpty(t, want, "fixture states no expectations")

	return fill(want)
}

// fill defaults every measured key that is absent to zero, so the two maps compare as wholes and
// a missing line reads as a claim rather than as silence.
func fill(counts map[string]int) map[string]int {
	for _, key := range measured {
		if _, ok := counts[key]; !ok {
			counts[key] = 0
		}
	}

	return counts
}

// assertScanInvariants checks what has to hold of any repository at all.
func assertScanInvariants(t *testing.T, repo discover.Repo) {
	t.Helper()

	assert.Empty(t, repo.Unreadable, "directories the walk could not descend into")

	seen := map[string]bool{}

	for pkg := range repo.Packages() {
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
