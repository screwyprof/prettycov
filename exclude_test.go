package prettycov_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// Exclusion is what lets a project stop counting code it never intended to test, which is most of
// the distance between a hand-written `go list | grep -v` recipe and this tool's number.
func TestExcludeDropsMatchingPackages(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{
		file("example.com/p/cmd/web/main.go", 0, 0),
		file("example.com/p/cmd/scraper/main.go", 0, 0),
		file("example.com/p/web/handler/handler.go", 0, 0),
		file("example.com/p/web/config/config.go", 0, 0),
		file("example.com/p/web/api/api.pb.go", 0, 0),
	}

	tests := []struct {
		name string
		re   string
		want []string
	}{
		// Unanchored, so one short pattern reaches every command in the tree.
		{name: "a directory anywhere", re: "cmd/", want: []string{
			"example.com/p/web/handler/handler.go", "example.com/p/web/config/config.go",
			"example.com/p/web/api/api.pb.go",
		}},
		{name: "one package by path", re: "web/config", want: []string{
			"example.com/p/cmd/web/main.go", "example.com/p/cmd/scraper/main.go",
			"example.com/p/web/handler/handler.go", "example.com/p/web/api/api.pb.go",
		}},
		// File names are matchable, which is what real ignore lists are written against: etcd's
		// codecov.yml drops **/*.pb.go, and no pattern over package paths could say that.
		{name: "a file suffix anywhere", re: `\.pb\.go$`, want: []string{
			"example.com/p/cmd/web/main.go", "example.com/p/cmd/scraper/main.go",
			"example.com/p/web/handler/handler.go", "example.com/p/web/config/config.go",
		}},
		{name: "everything", re: "example.com", want: []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, _ := prettycov.Exclude(items, []*regexp.Regexp{regexp.MustCompile(tc.re)})

			files := make([]string, 0, len(got))
			for _, item := range got {
				files = append(files, item.File)
			}

			assert.Equal(t, tc.want, files)
		})
	}
}

// No pattern is the common case, and it must not copy or reorder anything.
func TestExcludeWithoutAPatternKeepsEverything(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{file("example.com/p/a.go", 0, 0), {File: "example.com/p/b.go"}}

	kept, dropped := prettycov.Exclude(items, nil)

	assert.Equal(t, items, kept)
	assert.Empty(t, dropped)
}

// What each pattern removed is the whole mitigation for unanchored matching: "cmd/" also taking
// pkg/subcmd is invisible in a total and obvious beside the pattern that did it.
func TestExcludeReportsWhatEachPatternRemoved(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{
		file("ex.com/p/cmd/web/main.go", 1, 9),
		file("ex.com/p/pkg/subcmd/run.go", 4, 0),
		file("ex.com/p/api/api.pb.go", 0, 7),
		file("ex.com/p/web/handler.go", 5, 5),
	}

	kept, dropped := prettycov.Exclude(items, []*regexp.Regexp{
		regexp.MustCompile("cmd/"),
		regexp.MustCompile(`\.pb\.go$`),
		regexp.MustCompile("nothing-is-here"),
	})

	assert.Len(t, kept, 1, "only the handler survives")

	assert.Equal(t, []prettycov.Exclusion{
		// Two files, because unanchored "cmd/" took pkg/subcmd as well. Saying so is the point.
		{Pattern: "cmd/", Files: 2, Statements: 14},
		{Pattern: `\.pb\.go$`, Files: 1, Statements: 7},
		{Pattern: "nothing-is-here", Files: 0, Statements: 0},
	}, dropped)
}

// A file two patterns both match is charged once, so the reported statements add up to what left.
// The loser is still credited with the match: a pattern that only ever meets files an earlier one
// already took is working, and reporting it as matching nothing sends someone to fix what is right.
func TestExcludeChargesAnOverlappingFileOnce(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{
		file("ex.com/p/cmd/gen.pb.go", 2, 3),
	}

	_, dropped := prettycov.Exclude(items, []*regexp.Regexp{
		regexp.MustCompile("cmd/"),
		regexp.MustCompile(`\.pb\.go$`),
	})

	assert.Equal(t, []prettycov.Exclusion{
		{Pattern: "cmd/", Files: 1, Statements: 5},
		{Pattern: `\.pb\.go$`, OverlappedFiles: 1},
	}, dropped)
}

// Overlapping and matching nothing are different states, and only the second is a typo.
func TestExcludeSeparatesOverlapFromNoMatch(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{file("ex.com/p/cmd/gen.pb.go", 2, 3)}

	_, dropped := prettycov.Exclude(items, []*regexp.Regexp{
		regexp.MustCompile("cmd/"),
		regexp.MustCompile(`\.pb\.go$`),
		regexp.MustCompile("typo"),
	})

	assert.Equal(t, 1, dropped[1].Overlapped(), "matched, but an earlier pattern was charged")
	assert.Equal(t, 0, dropped[2].Overlapped(), "never matched at all")
}

// The empty pattern matches every file, so honouring it empties the report and, with no threshold
// to fail, says so with a zero exit. Refused here rather than in whatever reads a flag, so a
// caller driving the library directly is guarded too.
func TestParseExclude(t *testing.T) {
	t.Parallel()

	t.Run("compiles a pattern", func(t *testing.T) {
		t.Parallel()

		re, err := prettycov.ParseExclude(`\.pb\.go$`)

		require.NoError(t, err)
		assert.True(t, re.MatchString("api/v1/api.pb.go"))
		assert.False(t, re.MatchString("api/v1/api.go"))
	})

	t.Run("refuses the empty one", func(t *testing.T) {
		t.Parallel()

		re, err := prettycov.ParseExclude("")

		require.ErrorIs(t, err, prettycov.ErrEmptyExclude)
		assert.Nil(t, re)
	})

	t.Run("reports one that does not compile", func(t *testing.T) {
		t.Parallel()

		_, err := prettycov.ParseExclude("(")

		require.Error(t, err)
		assert.NotErrorIs(t, err, prettycov.ErrEmptyExclude)
	})
}

// The coordinate is the block's start, which cmd/cover opens just after the brace: `if !ok {` on
// line 32 owns the `return` on line 33.
func TestExcludeTakesBlocksByCoordinate(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{withBlocks("m/a.go",
		block(31, 2, 2, 0), // covered
		block(32, 9, 0, 1), // the miss
		block(36, 2, 1, 0), // covered
	)}

	kept, dropped := prettycov.Exclude(items, patterns(t, `a\.go:32`))

	require.Len(t, kept, 1)
	assert.Equal(t, prettycov.CoverageStats{Covered: 3}, kept[0].Coverage,
		"the file stays, one statement lighter, and the total is the sum of what is left")
	assert.Len(t, kept[0].Blocks, 2, "the two covered blocks survive")

	assert.Equal(t, 1, dropped[0].Blocks)
	assert.Equal(t, 0, dropped[0].Files, "a coordinate never takes the file")
	assert.Equal(t, 1, dropped[0].Statements)
}

// Matching is unanchored against "path:line:col", so the column is optional — it is there to tell
// apart two blocks opening on one line.
func TestExcludeCoordinateMatchesWithOrWithoutAColumn(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{withBlocks("m/a.go", block(32, 9, 0, 1), block(40, 2, 1, 0))}

	for _, pattern := range []string{`a\.go:32`, `a\.go:32:9`, `a\.go:32:`} {
		kept, dropped := prettycov.Exclude(items, patterns(t, pattern))

		require.Len(t, kept, 1, pattern)
		assert.Equalf(t, 1, dropped[0].Blocks, "pattern %q", pattern)
		assert.Equalf(t, prettycov.CoverageStats{Covered: 1}, kept[0].Coverage, "pattern %q", pattern)
	}
}

// cmd/cover's blocks touch — 31.2,32.9 ends where 32.9,34.3 begins — and only starts are matched,
// so naming line 32 cannot take the covered block that merely reaches it.
func TestExcludeCoordinateTakesOnlyTheBlockThatStartsThere(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{withBlocks("m/a.go",
		block(31, 2, 2, 0), // spans lines 31-32, starts at 31
		block(32, 9, 0, 1), // starts at 32
	)}

	kept, _ := prettycov.Exclude(items, patterns(t, `a\.go:32`))

	require.Len(t, kept, 1)
	assert.Equal(t, prettycov.CoverageStats{Covered: 2}, kept[0].Coverage,
		"the block starting at 31 is untouched, however far it reaches")
}

// A file with every block taken goes too, rather than drawing as an "n/a" row with nothing in it.
func TestExcludeDropsAFileWhoseEveryBlockGoes(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{
		withBlocks("m/a.go", block(10, 2, 1, 0), block(11, 2, 0, 1)),
		withBlocks("m/b.go", block(10, 2, 1, 0)),
	}

	kept, dropped := prettycov.Exclude(items, patterns(t, `a\.go:1[01]`))

	require.Len(t, kept, 1)
	assert.Equal(t, "m/b.go", kept[0].File)
	assert.Equal(t, 2, dropped[0].Blocks)
	assert.Equal(t, 0, dropped[0].Files, "the file went a block at a time, so it is not charged as one")
}

// Or the report would say "12 blocks" where a package went.
func TestExcludePrefersThePathOverTheBlocksInside(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{withBlocks("m/logger/a.go", block(10, 2, 1, 0), block(11, 2, 0, 1))}

	kept, dropped := prettycov.Exclude(items, patterns(t, `/logger/`))

	assert.Empty(t, kept)
	assert.Equal(t, 1, dropped[0].Files)
	assert.Equal(t, 0, dropped[0].Blocks, "charged as the file it is, not as its parts")
	assert.Equal(t, 2, dropped[0].Statements)
}

// Blocks are optional, and a file without them must not crash or quietly vanish.
func TestExcludeLeavesAFileWithNoBlocksAlone(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{file("m/a.go", 3, 1)}

	kept, dropped := prettycov.Exclude(items, patterns(t, `a\.go:32`))

	require.Len(t, kept, 1)
	assert.Equal(t, prettycov.CoverageStats{Covered: 3, Uncovered: 1}, kept[0].Coverage)
	assert.Equal(t, 0, dropped[0].Blocks)

	// The path still reaches it, which is every pattern written before this existed.
	kept, dropped = prettycov.Exclude(items, patterns(t, `a\.go$`))
	assert.Empty(t, kept)
	assert.Equal(t, 1, dropped[0].Files)
}

// cmd/cover emits them, and naming one is a match even though the total does not move.
func TestExcludeCountsABlockThatDeclaresNoStatements(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{withBlocks("m/a.go", block(10, 2, 0, 0), block(20, 2, 2, 0))}

	kept, dropped := prettycov.Exclude(items, patterns(t, `a\.go:10`))

	require.Len(t, kept, 1)
	assert.Equal(t, 1, dropped[0].Blocks)
	assert.Equal(t, 0, dropped[0].Statements, "nothing left the denominator")
	assert.Equal(t, prettycov.CoverageStats{Covered: 2}, kept[0].Coverage)
}

// As for files: the second pattern works, the first was simply charged first.
func TestExcludeChargesAnOverlappingBlockOnce(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{withBlocks("m/a.go", block(32, 9, 0, 1), block(40, 2, 1, 0))}

	_, dropped := prettycov.Exclude(items, patterns(t, `a\.go:32`, `\.go:32:9`))

	assert.Equal(t, 1, dropped[0].Blocks, "first match is charged")
	assert.Equal(t, 0, dropped[1].Blocks)
	assert.Equal(t, 1, dropped[1].OverlappedBlocks, "and the second is not a typo")
	assert.Equal(t, 1, dropped[1].Overlapped())
}

// A path holds no colon, so a coordinate can never take a whole file by accident.
func TestExcludeKeepsPathsAndCoordinatesApart(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{withBlocks("m/a.go", block(32, 9, 0, 1))}

	kept, dropped := prettycov.Exclude(items, patterns(t, `m/a\.go:99`))

	require.Len(t, kept, 1, "a coordinate that matches no block leaves the file whole")
	assert.Equal(t, 0, dropped[0].Files)
	assert.Equal(t, 0, dropped[0].Blocks)
}

// patterns compiles through the same door the flag uses.
func patterns(t *testing.T, exprs ...string) []*regexp.Regexp {
	t.Helper()

	compiled := make([]*regexp.Regexp, 0, len(exprs))

	for _, expr := range exprs {
		re, err := prettycov.ParseExclude(expr)
		require.NoError(t, err)

		compiled = append(compiled, re)
	}

	return compiled
}

func block(line, col, covered, uncovered int) prettycov.Block {
	return prettycov.Block{
		Line:     line,
		Col:      col,
		Coverage: prettycov.CoverageStats{Covered: covered, Uncovered: uncovered},
	}
}

// withBlocks is a file whose Coverage is the sum of its blocks, as ParseProfile builds it.
func withBlocks(name string, blocks ...prettycov.Block) prettycov.FileCoverage {
	item := prettycov.FileCoverage{File: name, Blocks: blocks}
	for _, b := range blocks {
		item.Coverage.Add(b.Coverage)
	}

	return item
}

// A pattern naming a block inside a file another pattern took whole has not failed. Reporting it as
// a typo invites deleting it, and the day the path pattern narrows the block is back in the
// denominator with nobody the wiser.
func TestExcludeCreditsABlockInsideAFileTakenWhole(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{withBlocks("m/a.go", block(31, 2, 2, 0), block(32, 9, 0, 1))}

	_, dropped := prettycov.Exclude(items, patterns(t, `a\.go:32`, `a\.go$`))

	assert.Equal(t, 0, dropped[0].Blocks, "the file went whole, so it is charged to the path")
	assert.Equal(t, 1, dropped[0].OverlappedBlocks, "but the coordinate did match")
	assert.Equal(t, 1, dropped[1].Files)

	// Unanchored, so it matches the coordinates as well as the path — a path is a prefix of every
	// one of them. It still may not be credited twice for the same match.
	_, unanchored := prettycov.Exclude(items, patterns(t, `m/a\.go`, `a\.go:32`))

	assert.Equal(t, 1, unanchored[0].Files)
	assert.Equal(t, 0, unanchored[0].OverlappedBlocks, "the pattern that took the path is not charged twice")
	assert.Equal(t, 1, unanchored[1].OverlappedBlocks)
}

// Unanchored, the line is a prefix. Anchored, it is the line — which needs the position matched
// without its column too, or "$" could never follow a line number.
func TestExcludeAnchorsOnTheLine(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{withBlocks("m/a.go",
		block(3, 2, 0, 1), block(30, 2, 0, 1), block(300, 2, 0, 1), block(50, 2, 5, 0),
	)}

	_, loose := prettycov.Exclude(items, patterns(t, `a\.go:3`))
	assert.Equal(t, 3, loose[0].Blocks, "3, 30 and 300")

	_, exact := prettycov.Exclude(items, patterns(t, `a\.go:3$`))
	assert.Equal(t, 1, exact[0].Blocks, "line 3 alone")

	_, withCol := prettycov.Exclude(items, patterns(t, `a\.go:3:2$`))
	assert.Equal(t, 1, withCol[0].Blocks, "and the column still anchors")
}

// cmd/cover emits blocks declaring no statements, so "every block went" is the wrong test for an
// empty file: one of those left behind kept it alive as a row reading "n/a".
func TestExcludeDropsAFileLeftWithNoStatements(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{
		withBlocks("m/a.go", block(10, 2, 0, 0), block(32, 9, 0, 1)),
		withBlocks("m/b.go", block(1, 1, 4, 0)),
	}

	kept, dropped := prettycov.Exclude(items, patterns(t, `a\.go:32`))

	require.Len(t, kept, 1)
	assert.Equal(t, "m/b.go", kept[0].File, "a.go held nothing but an empty block, so it went too")
	assert.Equal(t, 1, dropped[0].Blocks)
}
