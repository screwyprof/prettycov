package prettycov_test

import (
	"testing"

	"github.com/screwyprof/prettycov"
)

// The default invocation: one level, no files. Cheap, and the one every run pays.
func BenchmarkDisplayTreeDefault(b *testing.B) {
	benchDisplayTree(b, prettycov.Options{Depth: 1})
}

// The deepest the printer goes, which is where a row's cost is multiplied by every file.
func BenchmarkDisplayTreeMaxFiles(b *testing.B) {
	benchDisplayTree(b, prettycov.Options{Depth: prettycov.DepthAll, Files: true})
}

// -hide-covered adds the allCovered re-walk, which is O(n·depth) along the surviving path.
func BenchmarkDisplayTreeHideCovered(b *testing.B) {
	bar := prettycov.MustThreshold(100.0)

	benchDisplayTree(b, prettycov.Options{Depth: prettycov.DepthAll, Files: true, HideCovered: &bar})
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
