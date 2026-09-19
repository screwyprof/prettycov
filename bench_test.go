package prettycov_test

import (
	"fmt"
	"io"
	"regexp"
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

// Grafting every file onto the tree and rolling the totals back up.
func BenchmarkProcess(b *testing.B) {
	files := syntheticProfile(b)

	b.ReportAllocs()

	for b.Loop() {
		_ = prettycov.Process(files)
	}
}

// The one benchmark reading bytes off disk rather than starting from a parsed slice.
func BenchmarkParseProfile(b *testing.B) {
	path := writeSyntheticProfile(b)

	b.ReportAllocs()

	for b.Loop() {
		if _, err := prettycov.ParseProfile(path); err != nil {
			b.Fatal(err)
		}
	}
}

// Get is called once per invocation, so this exists to hold a claim rather than to chase a cost:
// the walk allocates nothing, for a hit, a file hit and a miss alike.
//
// The miss case costs more than the hits and is meant to. A key the tree does not hold is the one
// that reaches underRoot, which descends the collapsed root trying each prefix before giving up.
// measured at 90ns before that fallback existed and 218ns after (benchstat, p=0.000, n=10). Kept:
// the one call this makes per run sits beside ParseProfile at 5.7ms, so 130ns buys `total
// pkg/logger` resolving without --old and --new for 0.003% of a run.
func BenchmarkGet(b *testing.B) {
	tree := prettycov.Process(syntheticProfile(b))

	for _, bc := range []struct {
		name string
		key  string
	}{
		{name: "package", key: "github.com/acme/monorepo/unit3/pkg/logger"},
		{name: "file", key: "github.com/acme/monorepo/unit3/pkg/logger/logger.go"},
		{name: "miss", key: "github.com/acme/monorepo/unit3/pkg/nope"},
	} {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				_ = tree.Get(bc.key)
			}
		})
	}
}

// The default invocation: one level, no files. Cheap, and the one every run pays.
func BenchmarkDisplayTreeDefault(b *testing.B) {
	benchDisplayTree(b, prettycov.Options{Depth: 1})
}

// The deepest the printer goes, which is where a row's cost is multiplied by every file.
func BenchmarkDisplayTreeMaxFiles(b *testing.B) {
	benchDisplayTree(b, prettycov.Options{Depth: prettycov.DepthAll, Files: true})
}

// --hide-covered adds the allCovered re-walk, which is O(n·depth) along the surviving path.
func BenchmarkDisplayTreeHideCovered(b *testing.B) {
	bar := prettycov.MustThreshold(100.0)

	benchDisplayTree(b, prettycov.Options{Depth: prettycov.DepthAll, Files: true, HideCovered: &bar})
}

// coveredProfile is the same fixture with all but one file in two hundred fully covered. The
// profile itself is under-covered, so BenchmarkDisplayTreeHideCovered's allCovered refuses at the
// first node and measures none of the walk below it — which is the walk the flag is made of.
//
// Blocks are cloned rather than rewritten: syntheticProfile's copies share one backing array.
func coveredProfile(tb testing.TB) []prettycov.FileCoverage {
	tb.Helper()

	files := syntheticProfile(tb)

	for i, f := range files {
		// One file in two hundred keeps its misses, so there is still something to draw.
		if i%200 == 0 {
			continue
		}

		blocks := make([]prettycov.Block, len(f.Blocks))
		for j, block := range f.Blocks {
			block.Coverage = prettycov.CoverageStats{Covered: block.Coverage.Total()}
			blocks[j] = block
		}

		f.Blocks, f.Coverage = blocks, prettycov.CoverageStats{Covered: f.Coverage.Total()}
		files[i] = f
	}

	return files
}

// --hide-covered over a tree it descends rather than refusing at the top row, which is the shape
// the flag exists for and the one a repository running this gate on itself has.
func BenchmarkDisplayTreeHideCoveredDense(b *testing.B) {
	bar := prettycov.MustThreshold(100.0)
	tree := prettycov.Process(coveredProfile(b))
	opts := prettycov.Options{Depth: prettycov.DepthAll, Files: true, HideCovered: &bar}

	b.ReportAllocs()

	for b.Loop() {
		// io.Discard never fails, so there is no error here to be interested in.
		_, _ = prettycov.DisplayTree(io.Discard, tree, opts)
	}
}

// Rows without the writer, so the traversal is measured rather than the formatting.
func BenchmarkRows(b *testing.B) {
	tree := prettycov.Process(syntheticProfile(b))
	opts := prettycov.Options{Depth: prettycov.DepthAll, Files: true}

	b.ReportAllocs()

	for b.Loop() {
		_ = prettycov.Rows(tree, opts)
	}
}

// The same traversal as the tree benchmarks, plus folding every unrun block in the profile.
func BenchmarkMisses(b *testing.B) {
	tree := prettycov.Process(syntheticProfile(b))
	opts := missOpts(prettycov.DepthAll)

	b.ReportAllocs()

	for b.Loop() {
		_ = prettycov.Misses(tree, opts)
	}
}

// The same again, plus formatting and writing every position.
func BenchmarkDisplayMisses(b *testing.B) {
	tree := prettycov.Process(syntheticProfile(b))
	opts := missOpts(prettycov.DepthAll)

	b.ReportAllocs()

	for b.Loop() {
		// io.Discard never fails, so there is no error here to be interested in.
		_, _ = prettycov.DisplayMisses(io.Discard, tree, opts)
	}
}

// Two patterns, one matching whole files and one matching block coordinates, because Exclude asks
// every pattern about both spellings of every block and the coordinate path is the hot one.
func BenchmarkExclude(b *testing.B) {
	files := syntheticProfile(b)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`/sub7/`),
		regexp.MustCompile(`file1\d\d\.go:3`),
	}

	b.ReportAllocs()

	for b.Loop() {
		_, _ = prettycov.Exclude(files, patterns)
	}
}
