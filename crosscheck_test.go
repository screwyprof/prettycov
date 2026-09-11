package prettycov_test

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// A rendered report is a claim about arithmetic: each row covers the rows drawn beneath it plus
// whatever it holds that they do not show. oracle_test.go checks the tree's totals, and every bug
// this file was written for left those totals right and lost a row — a subtree hidden by the wrong
// guard, a node folded into its child's label — so a parent went on counting statements that
// appeared beside nothing.
//
// Reading the rows back as a flat table is what makes that visible: a table has no indentation to
// hide behind, so every row names its own path and the numbers have to reconcile.
func TestRowsReconcileAgainstTheProfile(t *testing.T) {
	t.Parallel()

	for name, files := range crosscheckProfiles(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, withFiles := range []bool{false, true} {
				t.Run(fmt.Sprintf("files=%v", withFiles), func(t *testing.T) {
					t.Parallel()

					// Built once: neither the tree nor the totals depend on the depth being drawn.
					tree := prettycov.Process(files)
					totals := nodeTotals(files)

					// Every level, because depth decides which rows exist and the guards that went
					// wrong were the ones deciding whether a node is drawn at all.
					for _, depth := range []prettycov.Depth{0, 1, 2, 3, prettycov.DepthAll} {
						assertRowsMatchTheProfile(t, tree, totals,
							prettycov.Options{Depth: depth, Files: withFiles, Counts: true})
					}

					assertRowsHoldEveryStatement(t, tree, files, withFiles)
				})
			}
		})
	}
}

// assertRowsMatchTheProfile checks each row against totals derived from the parsed files rather
// than from the tree, and checks that what is printed carries that row's own numbers — so a row
// cannot be right while the line describing it is wrong.
func assertRowsMatchTheProfile(
	t *testing.T, tree *prettycov.PathTree, totals map[string][]prettycov.CoverageStats, opts prettycov.Options,
) {
	t.Helper()

	rows := prettycov.Rows(tree, opts)

	lines := strings.Split(strings.TrimSuffix(renderOpts(t, tree, opts), "\n"), "\n")
	require.Lenf(t, lines, len(rows), "one line per row, depth=%v files=%v", opts.Depth, opts.Files)

	for i, r := range rowInfos(rows) {
		// Candidates, not one total: a name can be a file and a directory at once, and then the
		// path names two nodes with different numbers. Every other path names exactly one, so this
		// is the same assertion there.
		want, ok := totals[r.path]
		require.Truef(t, ok, "row %q is not a path the profile names", r.path)
		assert.Containsf(t, want, rows[i].Coverage, "row %q at depth %v", r.path, opts.Depth)

		if r.total > 0 {
			assert.Containsf(t, lines[i], fmt.Sprintf("%d/%d uncovered", rows[i].Coverage.Uncovered, r.total),
				"the printed line for %q", r.path)
		}
	}
}

// assertRowsHoldEveryStatement is the reconciliation, at full depth: take what the rows beneath a
// row show away from it, and what is left must be exactly the files that row is the last one to
// account for. Zero for a directory once -files draws them; the directory's own files without it,
// which is the `du` behaviour the README documents.
func assertRowsHoldEveryStatement(
	t *testing.T, tree *prettycov.PathTree, files []prettycov.FileCoverage, withFiles bool,
) {
	t.Helper()

	rows := prettycov.Rows(tree, prettycov.Options{Depth: prettycov.DepthAll, Files: withFiles})
	infos := rowInfos(rows)

	// Parents come from the drawn nesting rather than from the path strings: a top-level "m" is a
	// sibling of the "." holding the files with no directory, not a child of it, and only the
	// indentation says so.
	below := make([]int, len(infos))
	ancestry := []int{}

	for i, r := range infos {
		if r.level > 0 {
			below[ancestry[r.level-1]] += r.total
		}

		ancestry = append(ancestry[:r.level], i)
	}

	// Summed per path rather than per row, because a name that is both a file and a directory is
	// drawn twice and the files charged to it are charged to the path, not to one of the two.
	// Per path also lets one row of a pair borrow from the other — swap them and the sum still
	// balances — so each row is checked to hold back at least nothing, which borrowing is not.
	drawn := make(map[string]bool, len(infos))
	held := map[string]int{}

	for i, r := range infos {
		keep := r.total - below[i]

		assert.GreaterOrEqualf(t, keep, 0,
			"row %q reports %d statements and the rows below it show %d", r.path, r.total, below[i])

		drawn[r.path] = true
		held[r.path] += keep
	}

	kept := keptBack(files, drawn, withFiles)

	for path, n := range held {
		assert.Equalf(t, kept[path], n, "the rows for %q keep back %d statements", path, n)
	}

	// Arithmetic alone is too weak: a row that quietly keeps back a file no deeper row shows still
	// balances, which is exactly what a folded-away node looks like. So say which rows must exist.
	// At full depth that is every file when -files is on, and every directory holding one when it
	// is off — anything less and a statement is drawn beside a row that does not name it.
	for _, f := range files {
		want := path.Dir(f.File)
		if withFiles {
			want = f.File
		}

		assert.Truef(t, drawn[want], "%q is in the profile and no row stands for %q", f.File, want)
	}
}

// row is one line of a report reduced to what the reconciliation needs: where it sits, what it
// stands for, and how much it claims.
type row struct {
	path  string
	level int
	total int
}

// rowInfos reconstructs the path each row stands for. A collapsed run's label already carries its
// slashes, so joining the labels down the stack rebuilds the path the profile used. Row.Level says
// how deep to join from — read rather than measured off the box-drawing prefix, which would make
// every level here depend on the glyphs staying two runes wide.
func rowInfos(rows []prettycov.Row) []row {
	stack := []string{}
	infos := make([]row, len(rows))

	for i, r := range rows {
		stack = append(stack[:r.Level], r.Label)

		infos[i] = row{
			// Cleaned, because a file with no directory of its own sits under a "." row and would
			// otherwise join to "./a.go", which is not what the profile called it.
			path:  path.Clean(strings.Join(stack, "/")),
			level: r.Level,
			total: r.Coverage.Total(),
		}
	}

	return infos
}

// nodeTotals charges every file to itself and to each directory above it, from the parsed files
// rather than from anything the tree did, and reports the candidates at each path. A name that is
// both a file and a directory has two, and the row drawn for either is one of them.
func nodeTotals(files []prettycov.FileCoverage) map[string][]prettycov.CoverageStats {
	asFile := map[string]prettycov.CoverageStats{}
	asDir := map[string]prettycov.CoverageStats{}

	add := func(into map[string]prettycov.CoverageStats, key string, c prettycov.CoverageStats) {
		stat := into[key]
		stat.Add(c)
		into[key] = stat
	}

	for _, f := range files {
		add(asFile, f.File, f.Coverage)

		for _, dir := range dirsOf(f.File) {
			add(asDir, dir, f.Coverage)
		}
	}

	totals := map[string][]prettycov.CoverageStats{}

	for _, in := range []map[string]prettycov.CoverageStats{asFile, asDir} {
		for path, stat := range in {
			totals[path] = append(totals[path], stat)
		}
	}

	return totals
}

// dirsOf is the directories a file is counted in, nearest first, ending at "." for a file with no
// directory of its own. The stop is on "/" as well as on a name with no separator in it, because
// path.Dir("/") is "/": a walk that only looks for a separator never ends on an absolute path, and
// a profile holding one would hang the suite rather than fail it.
func dirsOf(file string) []string {
	var dirs []string

	for dir := path.Dir(file); ; dir = path.Dir(dir) {
		dirs = append(dirs, dir)

		if dir == "/" || !strings.Contains(dir, "/") {
			return dirs
		}
	}
}

// keptBack is what each row has to account for by itself: the files no row below it shows. Walking
// up from the file and stopping at the first drawn path finds the deepest row covering it — its own
// row where it has one, the single row standing for a name it shares with a directory, or the
// closest drawn directory above. Walking beats scanning the rows for the deepest match, which is
// quadratic in the size of the profile.
func keptBack(files []prettycov.FileCoverage, drawn map[string]bool, withFiles bool) map[string]int {
	kept := map[string]int{}

	for _, f := range files {
		// The file's own row first, then the directories above it. A collapsed run leaves the
		// levels between undrawn, so this keeps walking rather than giving up at the first miss.
		//
		// Only when files are drawn does a row at the file's own path stand for the file: with
		// them hidden, a row there is a directory that happens to share the name, and the file is
		// accounted for by the closest directory above it.
		candidates := dirsOf(f.File)
		if withFiles {
			candidates = append([]string{f.File}, candidates...)
		}

		for _, candidate := range candidates {
			if drawn[candidate] {
				kept[candidate] += f.Coverage.Total()

				break
			}
		}
	}

	return kept
}

// crosscheckProfiles is every profile in testdata, plus the shapes it has none of. The first three
// are impossible from one `go test` run — no filesystem lets a file and a directory share a name —
// but a merge of two profiles or an -old/-new rewrite produces them, and each one hid a lost row.
func crosscheckProfiles(t *testing.T) map[string][]prettycov.FileCoverage {
	t.Helper()

	cases := map[string][]prettycov.FileCoverage{
		"name is a file and a directory": {
			file("m/a.go", 5, 0),
			file("m/a.go/b.go", 0, 7),
		},
		"name is a file and a directory, deeper": {
			file("m/a.go", 5, 0),
			file("m/a.go/sub/b.go", 0, 7),
		},
		"file with a subtree beside a sibling": {
			file("m/a.go", 5, 0),
			file("m/a.go/sub/b.go", 0, 7),
			file("m/other/c.go", 3, 3),
		},
		"no directory component at all": {
			file("a.go", 3, 1),
			file("b.go", 2, 0),
		},
		"a bare file beside a package": {
			file("printer.go", 3, 1),
			file("internal/app/a.go", 2, 1),
		},
		// A CI checkout can put absolute paths in a profile, and walking up one ends at "/" rather
		// than at a bare name. The walk has to stop there itself: path.Dir("/") is "/".
		"absolute paths": {
			file("/home/ci/repo/pkg/a.go", 3, 1),
			file("/home/ci/repo/main.go", 2, 0),
		},
		// Two files under the directory, so it does not merge into one of them and the file and
		// the directory are drawn at the same path — the shape the per-path reconciliation exists
		// for, and the only one where a row could borrow its sibling's number.
		"a file and a directory of one name, both drawn": {
			file("m/a.go", 5, 0),
			file("m/a.go/b.go", 0, 4),
			file("m/a.go/c.go", 0, 3),
		},
		// The "." holding a bare file merges to that file's name, which a sibling directory can
		// already have — two rows of one map tied on the label, where every other tie is between
		// the two maps.
		"a bare file taking a sibling's name": {
			file("a.go", 3, 0),
			file("a.go/b.go", 0, 4),
			file("a.go/c.go", 0, 3),
		},
		// And the root can hold a file directly, which is the one node with no name of its own.
		// `-new=/` reaches this from an ordinary profile.
		"files at the filesystem root": {
			file("/a.go", 3, 1),
			file("/b/c.go", 2, 0),
		},
	}

	profiles, err := filepath.Glob(filepath.Join("testdata", "*.out"))
	require.NoError(t, err)
	require.NotEmpty(t, profiles)

	for _, profile := range profiles {
		files, err := prettycov.ParseProfile(profile)
		require.NoError(t, err)

		cases[filepath.Base(profile)] = files
	}

	return cases
}
