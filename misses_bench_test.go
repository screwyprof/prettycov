package prettycov_test

import (
	"io"
	"testing"

	"github.com/screwyprof/prettycov"
)

// The same traversal as the tree benchmarks, plus folding every unrun block in the profile.
func BenchmarkMisses(b *testing.B) {
	tree := prettycov.Process(syntheticProfile(b))
	opts := missOpts(prettycov.DepthAll)

	b.ReportAllocs()

	for b.Loop() {
		_ = prettycov.Misses(tree, opts)
	}
}

func BenchmarkDisplayMisses(b *testing.B) {
	tree := prettycov.Process(syntheticProfile(b))
	opts := missOpts(prettycov.DepthAll)

	b.ReportAllocs()

	for b.Loop() {
		// io.Discard never fails, so there is no error here to be interested in.
		_, _ = prettycov.DisplayMisses(io.Discard, tree, opts)
	}
}
