package cli

import (
	"io"
	"os"

	"golang.org/x/term"

	"github.com/screwyprof/prettycov"
)

// colorMode is what -color said. auto needs the destination to mean anything; never and always
// exist because a caller sometimes knows better than the heuristic, which is why every tool that
// colours output offers the same three.
type colorMode int

const (
	// colorAuto colours only when writing to a terminal that has not asked otherwise.
	colorAuto colorMode = iota
	// colorNever never colours, whatever it is writing to.
	colorNever
	// colorAlways colours even into a pipe, for a caller that will render the escapes itself.
	colorAlways
)

// colorOf reads a mode kong has already checked against the enum in the flag's tag. Total, with no
// error to return: anything outside the enum is refused before this runs, so a second check here
// would be a branch no invocation can take.
func colorOf(s string) colorMode {
	switch s {
	case "never":
		return colorNever
	case "always":
		return colorAlways
	default:
		return colorAuto
	}
}

// isTerminal is a variable so the tests can answer for a terminal without opening a pty.
//
//nolint:gochecknoglobals // a seam, swapped by the tests in this package.
var isTerminal = term.IsTerminal

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

	// term.IsTerminal asks the descriptor itself, where stat'ing for a character device only
	// guesses: /dev/null and /dev/urandom are character devices too, and would have been coloured.
	file, ok := w.(*os.File)
	if !ok || !isTerminal(int(file.Fd())) {
		return prettycov.Plain
	}

	return prettycov.ANSI
}
