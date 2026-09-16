package prettycov

import (
	"encoding"
	"errors"
	"math"
	"strconv"
)

// ErrBadThreshold is a bar that cannot mean what it says.
var ErrBadThreshold = errors.New("want a percentage from 0 to 100")

// A Threshold is a coverage bar. Parsed once, where the value arrives, so AtLeast cannot be handed
// 150 or NaN: the range used to be a rule the caller was trusted to keep and this type's doc was
// where that trust was written down.
//
// NaN is the one worth naming. Every comparison against it is false, so a gate would pass at any
// coverage and a filter would hide nothing, each saying nothing about it.
type Threshold struct{ value float64 }

// NewThreshold is a bar a caller computed. The zero Threshold is 0%, which is a real bar — use the
// pointer, or a bool beside it, to say that none was asked for.
func NewThreshold(pct float64) (Threshold, error) {
	if math.IsNaN(pct) || pct < 0 || pct > 100 {
		//nolint:wrapcheck // a sentinel of this package's own.
		return Threshold{}, ErrBadThreshold
	}

	return Threshold{value: pct}, nil
}

// See Depth's assertion for why. OptionalPercentage.Decode happens to call this by name today,
// which is the only reason a rename here would not compile anyway.
var _ encoding.TextUnmarshaler = (*Threshold)(nil)

// UnmarshalText reads a bar, so flag and config libraries produce the parsed type rather than a
// float a caller has to remember to range-check.
func (t *Threshold) UnmarshalText(text []byte) error {
	pct, err := strconv.ParseFloat(string(text), 64)
	if err != nil {
		//nolint:wrapcheck // a sentinel of this package's own.
		return ErrBadThreshold
	}

	parsed, err := NewThreshold(pct)
	if err != nil {
		return err
	}

	*t = parsed

	return nil
}

// Float is the bar as a number, for a caller rendering it.
func (t Threshold) Float() float64 { return t.value }

// MustThreshold is NewThreshold for a constant a caller knows is in range. Panics otherwise, which
// is a broken build rather than a runtime condition.
func MustThreshold(pct float64) Threshold {
	bar, err := NewThreshold(pct)
	if err != nil {
		panic(err)
	}

	return bar
}
