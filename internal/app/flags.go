package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/screwyprof/prettycov"
)

// defaultProfile is what `go test -coverprofile=...` is conventionally pointed at, so running
// prettycov with no arguments in a repo that just ran its tests does the obvious thing.
const defaultProfile = "coverage.out"

// defaultDepth shows the top row plus one level. Measured across 16 real repositories it is the
// only fixed value that stays on a screen everywhere: the worst case is hugo at 37 rows, where
// depth 2 gives 152 and gitea 196.
const defaultDepth prettycov.Depth = 1

var (
	errTooManyProfiles = errors.New("want at most one profile path")
	errTwoProfiles     = errors.New("profile given twice")
	errBadFailUnder    = errors.New("want a percentage from 0 to 100")
	errHalfARename     = errors.New("-old and -new rename a root package together; one alone does nothing")
	errRootNamesNoPkg  = errors.New("-old names no package")
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
	Depth       prettycov.Depth
	Color       colorMode
	Exclude     []*regexp.Regexp
	FailUnder   *float64
	Counts      bool
	Files       bool
	Total       bool
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
	// Set here rather than by the flag package, which carries no default through Func. colorAuto
	// is the zero value, so -color needs no such line.
	cfg.Depth = defaultDepth

	set.Func("depth",
		// Rendered by Depth, so the help cannot advertise a word ParseDepth does not read.
		fmt.Sprintf("`levels` below the top row, like tree -L, or %q (default %s)",
			prettycov.DepthAll, defaultDepth),
		func(s string) error {
			var err error

			cfg.Depth, err = prettycov.ParseDepth(s)

			//nolint:wrapcheck // ParseDepth's errors are already phrased for the flag package,
			// which prefixes the flag name and the offending value.
			return err
		})
	// Kept as a mode, not resolved: "auto" is not an answer until the destination is known, and
	// it is not known here. showReport settles it against stdout, where it is.
	set.Func("color", "when to colour: \"auto\" (default), \"never\" or \"always\"", func(s string) (err error) {
		//nolint:wrapcheck // parseColorMode's error is already phrased for the flag package.
		cfg.Color, err = parseColorMode(s)

		return err
	})
	// Repeatable: assigning instead of appending would silently apply only the last pattern.
	// Compiled here so a bad one is a flag error rather than a panic later.
	const excludeUsage = "omit files whose path, or blocks whose file:line:col, match this `regexp`; repeatable"

	set.Func("exclude", excludeUsage, func(s string) error {
		re, err := prettycov.ParseExclude(s)
		if err != nil {
			//nolint:wrapcheck // the flag package already prefixes the flag name and the value.
			return err
		}

		cfg.Exclude = append(cfg.Exclude, re)

		return nil
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
	set.BoolVar(&cfg.Counts, "counts", false, "show uncovered/total statements after each percentage")
	set.BoolVar(&cfg.Files, "files", false, "show the profile's files, not only its packages")
	set.BoolVar(&cfg.Total, "total", false, "print only the total percentage, for scripts")
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

	// Refused rather than ignored, for the reason ParseExclude refuses the empty pattern: a flag
	// that silently does nothing is a mistake nobody is told about. `-new=.` alone looks like it
	// shortens every label and does not, and an unset `-old=$(MODULE)` leaves the report full of
	// paths its author thought were gone.
	// Two ways to ask for a rename and not get one, and they are different mistakes, so they get
	// different sentences. Telling someone "one alone does nothing" when they passed both sends
	// them to supply a flag they already supplied.
	//
	// A root of nothing but separators names no package: Shorten drops the trailing one and then
	// matches a prefix that is not there, so -old=/ and -old=// leave the report exactly as it
	// was. `-old=$(MODULE)/` with MODULE unset spells the first of those.
	//
	// Only the old root is trimmed, matching Shorten, which trims that one and uses the new one
	// raw as the replacement. -new=/ is a working target — it renders the tree under the
	// filesystem root — so trimming both here would refuse a rename that works.
	//
	// Either message quotes what was typed rather than what is left of it, since that is what the
	// reader has to find on their own command line.
	switch oldRoot := strings.Trim(cfg.CurrentRoot, "/"); {
	case oldRoot == "" && cfg.CurrentRoot != "":
		return cfg, fmt.Errorf("%w: got -old=%s", errRootNamesNoPkg, cfg.CurrentRoot)
	case (oldRoot == "") != (cfg.NewRoot == ""):
		return cfg, fmt.Errorf("%w: got %s", errHalfARename, given(cfg.CurrentRoot, cfg.NewRoot))
	}

	return cfg, nil
}

// given names whichever half was passed, so the message points at the flag that is there rather
// than the one that is not.
func given(oldRoot, newRoot string) string {
	if oldRoot != "" {
		return "-old=" + oldRoot
	}

	return "-new=" + newRoot
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
