package app

import (
	"errors"
	"io"
	"os"

	"github.com/screwyprof/prettycov"
)

var errBadColor = errors.New(`want "auto", "never" or "always"`)

// colorMode is what -color said, which is not yet what the report will do: two of the three are
// answers and one is a question. Flags read what was typed; "auto" is a valid thing to have typed.
type colorMode int

const (
	// colorAuto colours only when writing to a terminal that has not asked otherwise.
	colorAuto colorMode = iota
	// colorNever never colours, whatever it is writing to.
	colorNever
	// colorAlways colours even into a pipe, for a caller that will render the escapes itself.
	colorAlways
)

func parseColorMode(s string) (colorMode, error) {
	switch s {
	case "auto":
		return colorAuto, nil
	case "never":
		return colorNever, nil
	case "always":
		return colorAlways, nil
	default:
		//nolint:wrapcheck // the flag package already prefixes the flag name and the value.
		return colorAuto, errBadColor
	}
}

// palette settles the mode against w, which is what colorAuto was waiting for. Asked at the point
// of writing, because that is where the destination is known — parsing argv is too early, and no
// other flag needs to know where output goes.
//
// NO_COLOR counts however it is set, including empty, per the convention at https://no-color.org.
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

	file, ok := w.(*os.File)
	if !ok {
		return prettycov.Plain
	}

	if info, err := file.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return prettycov.Plain
	}

	return prettycov.ANSI
}
