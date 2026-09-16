package prettycov_test

import (
	"regexp"
	"testing"

	"github.com/screwyprof/prettycov"
)

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
