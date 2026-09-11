package prettycov_test

import (
	"io"
	"testing"

	"github.com/screwyprof/prettycov"
)

func BenchmarkFullRun(b *testing.B) {
	b.ReportAllocs()

	for range b.N {
		files, err := prettycov.ParseProfile("/tmp/big.out")
		if err != nil {
			b.Fatal(err)
		}

		tree := prettycov.Process(files, "", "")
		prettycov.DisplayTree(io.Discard, tree, prettycov.Options{Depth: prettycov.DepthAll, Files: true})
	}
}
