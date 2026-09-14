package prettycov_test

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/screwyprof/prettycov"
)

// benchFiles is how many files the synthetic profile names. Small enough that `go test -bench=.`
// is not a coffee break, large enough that growth shows: doubling a slice by 96 bytes a row rather
// than 56 is invisible at a hundred rows. Edit it to measure the shape of a monorepo — the
// regression these exist to catch was found at 30000, where the tree printer had gone from 11.8MB
// to 23.3MB and 46k allocations to 123k.
const benchFiles = 2000

// benchSeed is fixed, so two runs of benchstat compare the same tree rather than two samples of a
// distribution.
const benchSeed = 1

// The shape is taken from testdata/delegator-go126.out rather than invented: 19 files, 288 blocks,
// and 9 of the 19 with nothing left to cover.
const (
	blocksPerFile = 15

	// wellCovered is the share of files with little or nothing left in them. Two populations, not
	// one: a uniform sprinkle of misses would leave -hide-covered nothing to prune and merge
	// nothing adjacent to fold, which is most of what these benchmarks are here to measure.
	wellCovered = 47

	// How often a block ran, within a file of each population. Not 100 and 0: a well-covered file
	// still has the odd untaken error branch, which is what leaves merge single statements with
	// nothing adjacent to fold, and a poor one still has its happy path.
	runsWhenWellCovered = 92
	runsOtherwise       = 15

	// A block holds one to four statements and spans two to four lines, which is a branch body.
	// Blocks abut, so merge has runs to fold where the misses fall together.
	statementsPerBlock = 4
	linesPerBlock      = 3

	// The column a gofmt'd statement opens at, one tab in, and the one its block closes at.
	openColumn  = 2
	closeColumn = 3

	// The execution count a run block carries. Any positive number reads the same to the parser,
	// which asks whether it is zero and nothing else.
	ranOnce = 7
)

// Fan-out per level. Coprime with each other and with the number of areas, so the directories do
// not line up into a lattice and the collapse of single-child runs gets something to do.
const (
	groupsPerArea = 97
	subsPerGroup  = 13
)

// syntheticProfile shapes a tree the way a Go repository does: a module path nobody would type, a
// handful of top-level areas, packages nested under them, and files drawn from the two populations
// above.
func syntheticProfile(tb testing.TB, files int) []prettycov.FileCoverage {
	tb.Helper()

	// The top-level directories a repository of this size tends to have.
	areas := []string{"pkg", "internal", "cmd", "web", "scraper", "store"}

	rng := rand.New(rand.NewSource(benchSeed))

	out := make([]prettycov.FileCoverage, 0, files)

	for i := range files {
		name := fmt.Sprintf("github.com/acme/monorepo/%s/group%d/sub%d/file%d.go",
			areas[i%len(areas)], i%groupsPerArea, i%subsPerGroup, i)

		runs := runsOtherwise
		if rng.Intn(100) < wellCovered {
			runs = runsWhenWellCovered
		}

		var (
			stats  prettycov.CoverageStats
			blocks []prettycov.Block
			line   = 1
		)

		for range blocksPerFile {
			statements := 1 + rng.Intn(statementsPerBlock)
			end := line + 1 + rng.Intn(linesPerBlock)

			if rng.Intn(100) < runs {
				blocks = append(blocks, covered(line, openColumn, end, statements))
				stats.Add(prettycov.CoverageStats{Covered: statements})
			} else {
				blocks = append(blocks, uncovered(line, openColumn, end, statements))
				stats.Add(prettycov.CoverageStats{Uncovered: statements})
			}

			line = end + 1
		}

		out = append(out, prettycov.FileCoverage{File: name, Coverage: stats, Blocks: blocks})
	}

	return out
}

// writeSyntheticProfile renders the same fixture as a profile on disk, for the one benchmark that
// needs bytes rather than a parsed slice.
func writeSyntheticProfile(tb testing.TB, files int) string {
	tb.Helper()

	path := filepath.Join(tb.TempDir(), "coverage.out")

	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	w := bufio.NewWriterSize(f, 1<<20)

	if _, err := fmt.Fprintln(w, "mode: atomic"); err != nil {
		tb.Fatal(err)
	}

	for _, file := range syntheticProfile(tb, files) {
		for _, block := range file.Blocks {
			count := 0
			if block.Coverage.Covered > 0 {
				count = ranOnce
			}

			// `file:startLine.startCol,endLine.endCol statements count`, which is the line cmd/cover
			// writes and x/tools parses.
			if _, err := fmt.Fprintf(w, "%s:%d.%d,%d.%d %d %d\n",
				file.File, block.Line, block.Col, block.EndLine, closeColumn,
				block.Coverage.Total(), count); err != nil {
				tb.Fatal(err)
			}
		}
	}

	if err := w.Flush(); err != nil {
		tb.Fatal(err)
	}

	return path
}

// benchTree is the shape every tree benchmark shares: build once, render many times, so what is
// measured is the printer rather than Process.
func benchTree(b *testing.B, opts prettycov.Options) {
	b.Helper()

	tree := prettycov.Process(syntheticProfile(b, benchFiles))

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		prettycov.DisplayTree(io.Discard, tree, opts)
	}
}
