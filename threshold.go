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

// The interface kong looks for, asserted because it looks by reflection. OptionalPercentage.Decode
// happens to call this by name today, which is the only reason a rename does not compile.
var (
	_ encoding.TextUnmarshaler = (*Threshold)(nil)
	_ encoding.TextUnmarshaler = (*OptionalThreshold)(nil)
)

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

// An OptionalThreshold is a bar that may not have been asked for. The zero value is none, so a
// struct holding one needs no constructor to be usable.
//
// A type rather than *Threshold, because the pointer carried the distinction in a comment: a
// Threshold's zero value is 0%, which is a real bar, so nil was the only way to say "no bar" and
// every reader had to be told that. Here the type says it, and a caller cannot dereference the
// absent case by reaching past a check made four lines earlier.
type OptionalThreshold struct{ bar *Threshold }

// SomeThreshold is a bar that was asked for, including 0.
func SomeThreshold(bar Threshold) OptionalThreshold { return OptionalThreshold{bar: &bar} }

// NoneThreshold is no bar at all, which is also the zero value.
func NoneThreshold() OptionalThreshold { return OptionalThreshold{} }

// Unwrap is the bar and whether there is one, which is the comma-ok shape every other optional in
// this package uses — see CoverageStats.Percentage and Measurement.Tree.
func (o OptionalThreshold) Unwrap() (Threshold, bool) {
	if o.bar == nil {
		return Threshold{}, false
	}

	return *o.bar, true
}

// UnmarshalText reads a bar, so a flag holding one of these is none until the flag is given. Kong
// finds this by reflection and calls it only when the flag appears, which is exactly the semantics
// the pointer used to carry.
func (o *OptionalThreshold) UnmarshalText(text []byte) error {
	var bar Threshold
	if err := bar.UnmarshalText(text); err != nil {
		return err
	}

	*o = SomeThreshold(bar)

	return nil
}
