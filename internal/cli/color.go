package cli

import (
	"encoding"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/screwyprof/prettycov"
)

var errBadColor = errors.New(`want "auto", "never" or "always"`)

// colorMode is what --color said. auto needs the destination to mean anything; never and always are
// for a caller that knows better than the heuristic.
type colorMode int

const (
	// colorAuto colours only when writing to a terminal that has not asked otherwise.
	colorAuto colorMode = iota
	// colorNever never colours, whatever it is writing to.
	colorNever
	// colorAlways colours even into a pipe, for a caller that will render the escapes itself.
	colorAlways
)

// See prettycov.Depth's assertion for why: nothing calls this by name, so without it a rename
// compiles and --color=never stops being a spelling kong knows.
var _ encoding.TextUnmarshaler = (*colorMode)(nil)

// UnmarshalText parses a mode, so a colorMode exists only because a valid spelling was given —
// kong finds it by interface, leaving no window where an unchecked string is lying around.
func (m *colorMode) UnmarshalText(text []byte) error {
	switch string(text) {
	case "never":
		*m = colorNever
	case "always":
		*m = colorAlways
	case "auto":
		*m = colorAuto
	default:
		return fmt.Errorf("%w: %q", errBadColor, text)
	}

	return nil
}

// isTerminal is a variable so the tests can answer for a terminal without opening a pty.
//
//nolint:gochecknoglobals // a seam, swapped by the tests in this package.
var isTerminal = term.IsTerminal

// palette settles the mode against w, which is what colorAuto was waiting for. At the point of
// writing, since that is where the destination is known.
//
// NO_COLOR counts however it is set, including empty (https://no-color.org).
func (m colorMode) palette(w io.Writer) prettycov.Palette {
	switch m {
	case colorAlways:
		return prettycov.ANSI
	case colorNever:
		return prettycov.Plain
	case colorAuto:
	}

	if _, set := os.LookupEnv("NO_COLOR"); set {
		return prettycov.Plain
	}

	if os.Getenv("TERM") == "dumb" {
		return prettycov.Plain
	}

	// term.IsTerminal asks the descriptor itself, where stat'ing for a character device only
	// guesses: /dev/null and /dev/urandom are character devices too, and would have been coloured.
	file, ok := w.(*os.File)
	if !ok || !isTerminal(int(file.Fd())) {
		return prettycov.Plain
	}

	return prettycov.ANSI
}
