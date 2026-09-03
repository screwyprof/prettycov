package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/screwyprof/prettycov"
)

// defaultProfile is what `go test -coverprofile=...` is conventionally pointed at, so running
// prettycov with no arguments in a repo that just ran its tests does the obvious thing.
const defaultProfile = "coverage.out"

var (
	errBadColor        = errors.New(`invalid -color, want "auto", "never" or "always"`)
	errTooManyProfiles = errors.New("want at most one profile path")
)

// parseInterspersed lets flags appear on either side of the profile path. The flag package stops
// at the first non-flag argument, so `prettycov cov.out -depth=2` would otherwise parse no flags
// at all and silently ignore the depth.
func parseInterspersed(set *flag.FlagSet, args []string) ([]string, error) {
	var positional []string

	for {
		if err := set.Parse(args); err != nil {
			return nil, fmt.Errorf("cannot parse flags: %w", err)
		}

		if set.NArg() == 0 {
			return positional, nil
		}

		positional = append(positional, set.Arg(0))
		args = set.Args()[1:]
	}
}

const usageMessage = "" +
	`Prettycov:
Given a coverage profile produced by 'go test'.
	go test -coverprofile=coverage.out ./...
Show the top level packages, reading ./coverage.out:
	prettycov
Read a profile elsewhere:
	prettycov path/to/coverage.out
Show another level down:
	prettycov -depth=2
Replace a long root package path:
	prettycov -old=gitlab.com/Company/Department/product/unicorn -new=unicorn
Fail when total coverage is below a threshold, for CI:
	prettycov -fail-under=80
`

type config struct {
	Profile     string
	CurrentRoot string
	NewRoot     string
	Depth       uint
	Color       prettycov.ColorMode
	FailUnder   float64
	Help        bool
	Version     bool
}

// newFlagSet wires the flags onto cfg. Shared by parsing and by printing usage, so the two cannot
// describe different flags.
func newFlagSet(out io.Writer, cfg *config, color *string) *flag.FlagSet {
	set := flag.NewFlagSet("prettycov", flag.ContinueOnError)
	set.SetOutput(out)

	set.StringVar(&cfg.Profile, "profile", "", "coverage profile path")
	set.StringVar(&cfg.CurrentRoot, "old", "", "old project's root package")
	set.StringVar(&cfg.NewRoot, "new", "", "new project's root package")
	set.UintVar(&cfg.Depth, "depth", 1, "levels to show below the top row, like tree -L")
	set.StringVar(color, "color", "auto", `when to colour: "auto", "never" or "always"`)
	set.Float64Var(&cfg.FailUnder, "fail-under", 0, "exit 1 when total coverage is below this percentage")
	set.BoolVar(&cfg.Help, "help", false, "show help")
	set.BoolVar(&cfg.Version, "version", false, "show version")

	set.Usage = func() { printUsage(out) }

	return set
}

// printUsage writes the help text and the flag defaults.
func printUsage(w io.Writer) {
	var (
		cfg   config
		color string
	)

	_, _ = fmt.Fprint(w, usageMessage)
	_, _ = fmt.Fprintln(w, "\nFlags:")

	newFlagSet(w, &cfg, &color).PrintDefaults()
}

// parseFlags reads args, which excludes the program name. The profile may be given as -profile or
// as the sole positional argument, and defaults to ./coverage.out.
func parseFlags(args []string, stderr io.Writer) (config, error) {
	var (
		cfg   config
		color string
	)

	set := newFlagSet(stderr, &cfg, &color)

	positional, err := parseInterspersed(set, args)
	if err != nil {
		return cfg, err
	}

	if len(positional) > 1 {
		return cfg, fmt.Errorf("%w, got %d", errTooManyProfiles, len(positional))
	}

	mode, err := parseColor(color)
	if err != nil {
		return cfg, err
	}

	cfg.Color = mode

	if cfg.Profile == "" && len(positional) > 0 {
		cfg.Profile = positional[0]
	}

	if cfg.Profile == "" {
		cfg.Profile = defaultProfile
	}

	return cfg, nil
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
		return prettycov.ColorAuto, fmt.Errorf("%w, got %q", errBadColor, name)
	}
}
