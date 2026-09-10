package prettycov

import (
	"errors"
	"math"
	"strconv"
)

// Depth is how many levels of a tree to show below its top row, the way `tree -L` counts them.
// DepthAll is all of them.
type Depth uint

// DepthAll shows every level.
const DepthAll Depth = math.MaxUint

// depthMax is how DepthAll is spelled, in and out and in the errors, so those cannot drift apart.
const depthMax = "max"

var (
	// ErrBadDepth is a depth that is neither a number of levels nor "max".
	ErrBadDepth = errors.New(`want a number of levels, or "` + depthMax + `"`)
	// ErrDepthTooLarge is a number too large to be a depth. Separate from ErrBadDepth because it
	// says what to type: a number that big was reaching for the whole tree.
	ErrDepthTooLarge = errors.New(`too many levels; use "` + depthMax + `" for the whole tree`)
)

// String writes DepthAll back as "max", the way ParseDepth reads it, rather than as the sentinel.
func (d Depth) String() string {
	if d == DepthAll {
		return depthMax
	}

	return strconv.FormatUint(uint64(d), 10)
}

// ParseDepth reads a level count or "max".
func ParseDepth(s string) (Depth, error) {
	if s == depthMax {
		return DepthAll, nil
	}

	// Read at 64 bits and clamped, not read at Depth's own width: at its own width a 32-bit build
	// would refuse a number a 64-bit build accepts, and the same command should not depend on the
	// architecture. Clamping shows the same tree either way and cannot truncate.
	levels, err := strconv.ParseUint(s, 10, 64)

	switch {
	case errors.Is(err, strconv.ErrRange):
		//nolint:wrapcheck // a sentinel of this package's own, returned for errors.Is.
		return 0, ErrDepthTooLarge
	case err != nil:
		//nolint:wrapcheck // see above.
		return 0, ErrBadDepth
	}

	return Depth(min(levels, uint64(DepthAll))), nil
}
