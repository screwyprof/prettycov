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
			modules, err := discover.Modules(t.Context(), dir)
			require.NoError(t, err)

			packages, tested := 0, 0

			for _, m := range modules {
				packages += len(m.Packages)

				for _, pkg := range m.Packages {
					if pkg.HasTests {
						tested++
					}
				}
			}

			assert.Len(t, modules, want["discovered_modules"], "modules discovered")
			assert.Equal(t, want["discovered_packages"], packages, "packages discovered")
			assert.Equal(t, want["packages_with_tests"], tested, "packages carrying tests")
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
			name := d.Name()
			if name == "vendor" || name == "testdata" || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
				if path != dir {
					return filepath.SkipDir
				}
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
