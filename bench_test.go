package prettycov_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// benchFiles is how many files the synthetic profile names. Small enough that `go test -bench=.`
// is not a coffee break, large enough that growth shows: doubling a slice by 96 bytes a row rather
// than 56 is invisible at a hundred rows. Edit it to measure the shape of a monorepo. The
// regression these exist to catch was found at 30000, where the tree printer had gone from 11.8MB
// to 23.3MB and 46k allocations to 123k.
const benchFiles = 2000

// benchUnit is the profile every benchmark is built from, and it is a real one.
const benchUnit = "testdata/delegator-go126.out"

// benchUnitRoot is the module path benchUnit names, replaced so the copies do not collide.
const benchUnitRoot = "github.com/screwyprof/delegator/"

// The execution count a run block carries. Any positive number reads the same to the parser, which
// asks whether it is zero and nothing else.
const someRuns = 7

// The column a block closes at, which the profile carries and Block does not keep.
const closeColumn = 3

// syntheticProfile is benchUnit repeated under distinct roots until it names benchFiles files.
//
// Nothing about the shape is invented, which is the point: the directory spread (14 directories for
// 19 files, ten of them holding one), the block layout, the run counts and which files are finished
// are all a Go repository's. Generating it took a dozen constants that had to be argued for, and two
// of them were wrong. Every package held exactly one file at 2000 and exactly four at 30000, so
// collapse either fired on everything or on nothing, and neither is what the printer meets.
//
// The copies share each file's Blocks rather than cloning them. Nothing here mutates a block, and
// merge clones before it sorts.
func syntheticProfile(tb testing.TB) []prettycov.FileCoverage {
	tb.Helper()

	unit, err := prettycov.ParseProfile(benchUnit)
	require.NoError(tb, err)
	require.NotEmpty(tb, unit)

	out := make([]prettycov.FileCoverage, 0, benchFiles)

	for copies := 0; len(out) < benchFiles; copies++ {
		for _, file := range unit {
			if len(out) == benchFiles {
				break
			}

			file.File = fmt.Sprintf("github.com/acme/monorepo/unit%d/%s",
				copies, strings.TrimPrefix(file.File, benchUnitRoot))

			out = append(out, file)
		}
	}

	return out
}

// writeSyntheticProfile renders the same fixture as a profile on disk, for the one benchmark that
// needs bytes rather than a parsed slice.
func writeSyntheticProfile(tb testing.TB) string {
	tb.Helper()

	var b strings.Builder

	b.WriteString("mode: atomic\n")

	for _, file := range syntheticProfile(tb) {
		for _, block := range file.Blocks {
			count := 0
			if block.Coverage.Covered > 0 {
				count = someRuns
			}

			// `file:startLine.startCol,endLine.endCol statements count`, which is the line cmd/cover
			// writes and x/tools parses.
			fmt.Fprintf(&b, "%s:%d.%d,%d.%d %d %d\n",
				file.File, block.Line, block.Col, block.EndLine, closeColumn,
				block.Coverage.Total(), count)
		}
	}

	return writeProfile(tb, b.String())
}
