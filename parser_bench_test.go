package prettycov_test

import (
	"testing"

	"github.com/screwyprof/prettycov"
)

func BenchmarkParseProfile(b *testing.B) {
	path := writeSyntheticProfile(b)

	b.ReportAllocs()

	for b.Loop() {
		if _, err := prettycov.ParseProfile(path); err != nil {
			b.Fatal(err)
		}
	}
}
