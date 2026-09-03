package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"

	"github.com/screwyprof/prettycov"
)

// defaultProfile is what `go test -coverprofile=...` is conventionally pointed at, so running
// prettycov with no arguments in a repo that just ran its tests does the obvious thing.
const defaultProfile = "coverage.out"

var (
	errBadColor        = errors.New(`want "auto", "never" or "always"`)
	errTooManyProfiles = errors.New("want at most one profile path")
	errTwoProfiles     = errors.New("profile given twice")
	errBadFailUnder    = errors.New("want a percentage from 0 to 100")
)

// parseInterspersed lets flags appear on either side of the profile path. The flag package stops
// at the first non-flag argument, so `prettycov cov.out -depth=2` would otherwise parse no flags
// at all and silently ignore the depth.
func parseInterspersed(set *flag.FlagSet, args []string) ([]string, error) {
	// Everything after a bare -- is a path, verbatim, so a profile whose name starts with a dash
	// can still be named. Split it off first: the flag package honours -- only within a single
	// Parse, and the loop below calls Parse once per positional.
	var verbatim []string

	if end := slices.Index(args, "--"); end >= 0 {
		verbatim = args[end+1:]
		args = args[:end]
	}

	var positional []string

	for {
		if err := set.Parse(args); err != nil {
			return nil, fmt.Errorf("cannot parse flags: %w", err)
		}

		if set.NArg() == 0 {
			return append(positional, verbatim...), nil
		}

		positional = append(positional, set.Arg(0))
		args = set.Args()[1:]
	}
}

type config struct {
	Profile     string
	CurrentRoot string
	NewRoot     string
	Depth       uint
	Color       prettycov.ColorMode
	FailUnder   *float64
	Help        bool
	Version     bool
}

// newFlagSet wires every flag onto cfg, so a parsed set is a finished config with nothing left to
// convert. Shared by parsing and by printing usage, so the two cannot describe different flags. It
// takes no writer: the set is silenced below, and printUsage points it at its own destination.
func newFlagSet(cfg *config) *flag.FlagSet {
	set := flag.NewFlagSet("prettycov", flag.ContinueOnError)

	set.StringVar(&cfg.Profile, "profile", "", "coverage profile path")
	set.StringVar(&cfg.CurrentRoot, "old", "", "old project's root package")
	set.StringVar(&cfg.NewRoot, "new", "", "new project's root package")
	set.UintVar(&cfg.Depth, "depth", 1, "levels to show below the top row, like tree -L")
	// Parsed here rather than handed back as a string for the caller to convert: ColorAuto is the
	// zero value, so leaving the flag out lands on the default without stating it twice.
	set.Func("color", "when to colour: \"auto\" (default), \"never\" or \"always\"", func(s string) error {
		mode, err := parseColor(s)
		cfg.Color = mode

		//nolint:wrapcheck // parseColor's error is already phrased for the flag package.
		return err
	})
	// A pointer, not a float with a default: zero is a legitimate threshold — it asks only that
	// the profile hold some statements — so the value cannot say whether the flag was given.
	set.Func("fail-under", "exit 1 when total coverage is below this `percentage`", func(s string) error {
		// ParseFloat alone would take "nan", and `total < NaN` is false, so the gate would pass
		// at any coverage and say nothing. Infinities and out-of-range values are the same kind
		// of mistake: a threshold that cannot mean what it says.
		pct, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(pct) || pct < 0 || pct > 100 {
			// The flag package prefixes this with the flag name and the offending value, so
			// wrapping would print that value twice.
			//nolint:wrapcheck // see above.
			return errBadFailUnder
		}

		cfg.FailUnder = &pct

		return nil
	})
	set.BoolVar(&cfg.Help, "help", false, "show help")
	set.BoolVar(&cfg.Help, "h", false, "show help (shorthand)")
	set.BoolVar(&cfg.Version, "version", false, "show version")

	// Registering -h ourselves stops the flag package special-casing it, so every help path is
	// the same path. With that, nothing here needs the package's own reporting: a mistyped flag
	// used to print the message, then the whole usage, then the message again, 33 lines for one
	// typo. run says what was wrong and where to look.
	set.SetOutput(io.Discard)
	set.Usage = func() {}

	return set
}

// subcommand matches a bare `help` or `version`, which have to be recognised before the flag
// package sees them: it would take either for a profile path.
func subcommand(args []string) (config, bool) {
	if len(args) == 0 {
		return config{}, false
	}

	switch args[0] {
	case "help":
		return config{Help: true}, true
	case "version":
		return config{Version: true}, true
	}

	return config{}, false
}

// parseFlags reads args, which excludes the program name. The profile may be given as -profile or
// as the sole positional argument, and defaults to ./coverage.out.
func parseFlags(args []string) (config, error) {
	if cfg, ok := subcommand(args); ok {
		return cfg, nil
	}

	var cfg config

	set := newFlagSet(&cfg)

	positional, err := parseInterspersed(set, args)
	if err != nil {
		return cfg, err
	}

	if cfg.Profile, err = profilePath(cfg.Profile, positional); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// profilePath settles which profile to read. Naming it both ways is a mistake rather than a
// preference, so it is reported instead of resolved.
func profilePath(flagged string, positional []string) (string, error) {
	if len(positional) > 1 {
		return "", fmt.Errorf("%w, got %d", errTooManyProfiles, len(positional))
	}

	if flagged != "" && len(positional) > 0 {
		return "", fmt.Errorf("%w: -profile %q and %q", errTwoProfiles, flagged, positional[0])
	}

	switch {
	case flagged != "":
		return flagged, nil
	case len(positional) > 0:
		return positional[0], nil
	default:
		return defaultProfile, nil
	}
}

func parseColor(name string) (prettycov.ColorMode, error) {
	switch name {
	case "auto":
		return prettycov.ColorAuto, nil
	case "never":
		return prettycov.ColorNever, nil
	case "always":
		return prettycov.ColorAlways, nil
	default:
		// Terse: the flag package prefixes the flag name and the offending value.
		//nolint:wrapcheck // see above.
		return prettycov.ColorAuto, errBadColor
	}
}
