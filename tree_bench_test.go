package prettycov_test

import (
	"testing"

	"github.com/screwyprof/prettycov"
)

// Get is called once per invocation, so this exists to hold a claim rather than to chase a cost:
// the walk allocates nothing, for a hit, a file hit and a miss alike.
//
// The miss case costs more than the hits and is meant to. A key the tree does not hold is the one
// that reaches underRoot, which descends the collapsed root trying each prefix before giving up —
// measured at 90ns before that fallback existed and 240ns after (benchstat, p=0.000, n=10). Kept:
// the one call this makes per run sits beside ParseProfile at 5.7ms, so 150ns buys `total
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
