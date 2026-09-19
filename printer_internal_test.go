package prettycov

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prepare yields rather than returning a slice, so it has to honour a consumer that stops early:
// range-over-func panics if the body is left and the function yields again. The unwinding is what
// is easy to get wrong: stopping inside a grandchild has to stop every ancestor's loop too, not
// just the one that yielded, and neither Rows nor Misses breaks, so nothing else exercises it.
func TestPrepareStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	// Two top-level packages, each with two files, so a stop inside the first has siblings left at
	// both levels to run on.
	tree := Process([]FileCoverage{
		{File: "m/a/one.go", Coverage: CoverageStats{Covered: 1}},
		{File: "m/a/two.go", Coverage: CoverageStats{Covered: 1}},
		{File: "m/b/one.go", Coverage: CoverageStats{Covered: 1}},
		{File: "m/b/two.go", Coverage: CoverageStats{Covered: 1}},
	})

	// Files is not in this: prepare reads Depth and HideCovered from the options, and takes files
	// from shape. Setting it here would suggest a coupling the commit exists to remove.
	opts := Options{Depth: DepthAll}

	var all []string
	for d := range prepare(tree, opts, shape{files: true}) {
		all = append(all, d.Label)
	}

	require.Greater(t, len(all), 3, "fixture needs rows at more than one level")

	for stop := 1; stop <= len(all); stop++ {
		var got []string

		for d := range prepare(tree, opts, shape{files: true}) {
			got = append(got, d.Label)

			if len(got) == stop {
				break
			}
		}

		assert.Equal(t, all[:stop], got, "stopping after %d rows", stop)
	}
}

// onlyChildren is an iterator, so it has to honour a consumer that stops: range-over-func panics if
// the body is left and the function yields again. Neither collapse nor underRoot breaks, so nothing
// else reaches the early exit.
func TestOnlyChildrenStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	// A run of three, so a stop after the first leaves two it must not yield.
	tree := Process([]FileCoverage{{File: "a/b/c/d.go", Coverage: CoverageStats{Covered: 1}}})

	var got []string

	for name := range tree.onlyChildren() {
		got = append(got, name)

		break
	}

	assert.Equal(t, []string{"a"}, got)
}
