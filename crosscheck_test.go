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

					// Every level, because depth decides which rows exist and the guards that went
					// wrong were the ones deciding whether a node is drawn at all.
					for _, depth := range []prettycov.Depth{0, 1, 2, 3, prettycov.DepthAll} {
						assertRowsMatchTheProfile(t, files,
							prettycov.Options{Depth: depth, Files: withFiles, Counts: true})
					}

					assertRowsHoldEveryStatement(t, files, withFiles)
				})
			}
		})
	}
}

// assertRowsMatchTheProfile checks each row against totals derived from the parsed files rather
// than from the tree, and checks that what is printed carries that row's own numbers — so a row
// cannot be right while the line describing it is wrong.
func assertRowsMatchTheProfile(t *testing.T, files []prettycov.FileCoverage, opts prettycov.Options) {
	t.Helper()

	tree := prettycov.Process(files, "", "")
	rows := prettycov.Rows(tree, opts)
	totals := nodeTotals(files)

	var buf strings.Builder

	prettycov.DisplayTree(&buf, tree, opts)

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	require.Lenf(t, lines, len(rows), "one line per row, depth=%v files=%v", opts.Depth, opts.Files)

	for i, r := range rowInfos(rows) {
		want, ok := totals[r.path]
		require.Truef(t, ok, "row %q is not a path the profile names", r.path)
		assert.Equalf(t, want, rows[i].Coverage, "row %q at depth %v", r.path, opts.Depth)

		if r.total > 0 {
			assert.Containsf(t, lines[i], fmt.Sprintf("%d/%d uncovered", want.Uncovered, r.total),
				"the printed line for %q", r.path)
		}
	}
}

// assertRowsHoldEveryStatement is the reconciliation, at full depth: take what the rows beneath a
// row show away from it, and what is left must be exactly the files that row is the last one to
// account for. Zero for a directory once -files draws them; the directory's own files without it,
// which is the `du` behaviour the README documents.
func assertRowsHoldEveryStatement(t *testing.T, files []prettycov.FileCoverage, withFiles bool) {
	t.Helper()

	tree := prettycov.Process(files, "", "")
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

	drawn := make(map[string]bool, len(infos))
	for _, r := range infos {
		drawn[r.path] = true
	}

	kept := keptBack(files, drawn)

	for i, r := range infos {
		assert.Equalf(t, kept[r.path], r.total-below[i],
			"row %q reports %d statements and the rows below it show %d", r.path, r.total, below[i])
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

// rowInfos reconstructs the path each row stands for. The prefix is one leading space plus two
// runes per level, and a collapsed run's label already carries its slashes, so joining the labels
// down the stack rebuilds the path the profile used.
func rowInfos(rows []prettycov.Row) []row {
	stack := []string{}
	infos := make([]row, len(rows))

	for i, r := range rows {
		level := (len([]rune(r.Prefix)) - 1) / 2
		stack = append(stack[:level], r.Label)

		infos[i] = row{
			// Cleaned, because a file with no directory of its own sits under a "." row and would
			// otherwise join to "./a.go", which is not what the profile called it.
			path:  path.Clean(strings.Join(stack, "/")),
			level: level,
			total: r.Coverage.Covered + r.Coverage.Uncovered,
		}
	}

	return infos
}

// nodeTotals charges every file to itself and to each directory above it, from the parsed files
// rather than from anything the tree did. A path that is both a file and a directory collects
// both, which is the one row such a name gets.
func nodeTotals(files []prettycov.FileCoverage) map[string]prettycov.CoverageStats {
	totals := map[string]prettycov.CoverageStats{}

	add := func(key string, c prettycov.CoverageStats) {
		stat := totals[key]
		stat.Covered += c.Covered
		stat.Uncovered += c.Uncovered
		totals[key] = stat
	}

	for _, f := range files {
		add(f.File, f.Coverage)

		for dir := path.Dir(f.File); ; dir = path.Dir(dir) {
			add(dir, f.Coverage)

			if !strings.Contains(dir, "/") {
				break
			}
		}
	}

	return totals
}

// keptBack is what each row has to account for by itself: the files no row below it shows. Walking
// up from the file and stopping at the first drawn path finds the deepest row covering it — its own
// row where it has one, the single row standing for a name it shares with a directory, or the
// closest drawn directory above. Walking beats scanning the rows for the deepest match, which is
// quadratic in the size of the profile.
func keptBack(files []prettycov.FileCoverage, drawn map[string]bool) map[string]int {
	kept := map[string]int{}

	for _, f := range files {
		n := f.Coverage.Covered + f.Coverage.Uncovered

		if drawn[f.File] {
			kept[f.File] += n

			continue
		}

		// A collapsed run leaves the levels between undrawn, so this keeps walking rather than
		// giving up at the first miss.
		for dir := path.Dir(f.File); ; dir = path.Dir(dir) {
			if drawn[dir] {
				kept[dir] += n

				break
			}

			if !strings.Contains(dir, "/") {
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
