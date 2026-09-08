package prettycov_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

func TestParseDepth(t *testing.T) {
	t.Parallel()

	tests := map[string]prettycov.Depth{
		"0":                    0,
		"1":                    1,
		"9":                    9,
		"max":                  prettycov.DepthAll,
		"18446744073709551615": prettycov.DepthAll,
		// Decimal, not the base 0 the flag package used to read: this one meant eight.
		"010": 10,
	}

	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			got, err := prettycov.ParseDepth(in)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

// A number too large to be a depth is told apart from a typo, because it says what to type: that
// number was reaching for the whole tree, and "max" is how to ask.
func TestParseDepthRejections(t *testing.T) {
	t.Parallel()

	tests := map[string]error{
		"deep": prettycov.ErrBadDepth,
		"":     prettycov.ErrBadDepth,
		"-1":   prettycov.ErrBadDepth,
		"1.5":  prettycov.ErrBadDepth,
		"MAX":  prettycov.ErrBadDepth,
		// Taken as hex and as a digit separator by the flag package's base 0.
		"0x3":                  prettycov.ErrBadDepth,
		"1_0":                  prettycov.ErrBadDepth,
		"99999999999999999999": prettycov.ErrDepthTooLarge,
	}

	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			_, err := prettycov.ParseDepth(in)

			require.ErrorIs(t, err, want)
		})
	}
}
