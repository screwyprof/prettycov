package prettycov_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// A bar outside 0..100 cannot be built, so AtLeast has no such case to answer: the range is the
// type's rather than a rule its callers are trusted to keep. NaN is the one worth naming. Every
// comparison against it is false, so a gate would pass at any coverage and say nothing about it.
func TestThresholdRefusesABarThatCannotMeanWhatItSays(t *testing.T) {
	t.Parallel()

	for _, pct := range []float64{150, -5, math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := prettycov.NewThreshold(pct)
		require.ErrorIs(t, err, prettycov.ErrBadThreshold, "%v", pct)
	}

	for _, pct := range []float64{0, 50, 100} {
		bar, err := prettycov.NewThreshold(pct)
		require.NoError(t, err)
		assert.InDelta(t, pct, bar.Float(), 0)
	}
}

// UnmarshalText is how a flag or a config file produces one, so a word that is not a number and a
// number out of range read the same way: neither can mean what it says.
func TestThresholdReadsText(t *testing.T) {
	t.Parallel()

	var bar prettycov.Threshold

	require.NoError(t, bar.UnmarshalText([]byte("90")))
	assert.InDelta(t, 90.0, bar.Float(), 0)

	for _, text := range []string{"abc", "150", "-5", "nan", ""} {
		require.ErrorIs(t, bar.UnmarshalText([]byte(text)), prettycov.ErrBadThreshold, "%q", text)
	}
}
