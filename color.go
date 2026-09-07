package prettycov

// Colour thresholds, in percent. Cosmetic: they grade a row at a glance and are deliberately not
// tied to any pass/fail decision.
const (
	poor = 50.0
	good = 80.0
)

// Base ANSI colours only. The 256-colour and truecolor ranges name an exact shade and so override
// whatever the user's terminal theme chose; these four are remapped by it, which is the point.
const (
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	reset  = "\x1b[0m"
)

// Palette is how a percentage is written. Plain is the zero value, so a caller that says nothing
// about colour gets none.
//
// An enum and not a bool: the choice belongs to this package because it owns what a row looks
// like, and a second palette — 256-colour, or none of the escapes at all — would be a value here
// rather than a second parameter everywhere.
type Palette int

const (
	// Plain writes the percentage and nothing else.
	Plain Palette = iota
	// ANSI grades the percentage red, yellow or green.
	ANSI
)

func grade(pct float64) string {
	switch {
	case pct < poor:
		return red
	case pct < good:
		return yellow
	default:
		return green
	}
}
