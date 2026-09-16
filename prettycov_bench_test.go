package prettycov_test

import (
	"testing"

	"github.com/screwyprof/prettycov"
)

func BenchmarkProcess(b *testing.B) {
	files := syntheticProfile(b)

	b.ReportAllocs()

	for b.Loop() {
		_ = prettycov.Process(files)
	}
}
