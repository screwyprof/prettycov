package prettycov_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// A bar outside 0..100 cannot be built, so AtLeast has no such case to answer: the range is the
// type's rather than a rule its callers are trusted to keep. NaN is the one worth naming — every
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

// OptionalThreshold is what a field needs to speak the comma-ok shape the rest of this package
// returns — a *Threshold cannot, so every holder had to be told in prose that nil meant "no bar".
//
// The zero value is none, so a struct holding one is usable without a constructor.
func TestOptionalThresholdUnwrap(t *testing.T) {
	t.Parallel()

	var zero prettycov.OptionalThreshold

	_, ok := zero.Unwrap()
	assert.False(t, ok, "the zero value is none")

	_, ok = prettycov.NoneThreshold().Unwrap()
	assert.False(t, ok)

	// Zero is a real bar, which is the whole reason this type exists: a Threshold of 0 and no
	// Threshold at all are different, and a plain Threshold cannot tell them apart.
	bar, ok := prettycov.SomeThreshold(prettycov.MustThreshold(0)).Unwrap()
	require.True(t, ok)
	assert.InDelta(t, 0.0, bar.Float(), ratioTolerance)

	bar, ok = prettycov.SomeThreshold(prettycov.MustThreshold(90)).Unwrap()
	require.True(t, ok)
	assert.InDelta(t, 90.0, bar.Float(), ratioTolerance)
}

// UnmarshalText is how a flag holding one of these stays none until the flag is given, and it
// refuses what Threshold refuses rather than accepting a bar that cannot mean what it says.
func TestOptionalThresholdReadsText(t *testing.T) {
	t.Parallel()

	var opt prettycov.OptionalThreshold

	require.NoError(t, opt.UnmarshalText([]byte("90")))

	bar, ok := opt.Unwrap()
	require.True(t, ok)
	assert.InDelta(t, 90.0, bar.Float(), ratioTolerance)

	for _, text := range []string{"abc", "-1", "101", "nan"} {
		var bad prettycov.OptionalThreshold

		require.ErrorIs(t, bad.UnmarshalText([]byte(text)), prettycov.ErrBadThreshold, "%q", text)

		_, ok := bad.Unwrap()
		assert.False(t, ok, "%q leaves it none", text)
	}
}
