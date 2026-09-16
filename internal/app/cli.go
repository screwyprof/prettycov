package app

import (
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/screwyprof/prettycov"
)

// defaultProfile is what `go test -coverprofile=...` is conventionally pointed at, so running
// prettycov with no arguments in a repo that just ran its tests does the obvious thing.
const defaultProfile = "coverage.out"

// defaultDepth shows the top row plus one level. Measured across 16 real repositories it is the only
// fixed value that stays on a screen everywhere: hugo is 37 rows at depth 1, 152 at depth 2.
const defaultDepth = "1"

var errBadPercentage = errors.New("want a percentage from 0 to 100")

var errEmptyExclude = errors.New("want a pattern")

var errRootNamesNoPkg = errors.New("--old names no package")

// measured are the flags that decide what is in the answer. Embedded in every command, because every
// command measures; a command that only draws differently does not repeat them.
//
//nolint:lll // a struct tag is one unit; splitting it hides the declaration.
type measured struct {
	Profile   string   `help:"Coverage profile to read."                                                            default:"${profile}" placeholder:"PATH"`
	Old       string   `help:"Root package path to shorten."                                                                             placeholder:"PATH"   and:"rename"`
	New       string   `help:"What to shorten it to; --new=. strips it."                                                                 placeholder:"PATH"   and:"rename"`
	Exclude   []string `help:"Omit files whose path, or blocks whose file:line:col, match this regexp. Repeatable."                      placeholder:"REGEXP"              sep:"none"`
	FailUnder *float64 `help:"Exit 1 when coverage is below this percentage."                                                            placeholder:"PCT"`
	Color     string   `help:"When to colour: auto, never or always."                                               default:"auto"                                                    enum:"auto,never,always"`
}

// Validate is kong's per-struct hook. A root of only separators names no package — `--old=$(MODULE)/`
// with MODULE unset — which the `and:"rename"` tag cannot say, because it is about a value rather
// than about the pair.
func (m *measured) Validate() error {
	if m.FailUnder != nil && badPercentage(*m.FailUnder) {
		return fmt.Errorf("--fail-under: %w", errBadPercentage)
	}

	// Kong's slice flag takes "" without complaint, where the flag package handed it to
	// ParseExclude and got a refusal. The rule is the same either way: a pattern that matches
	// everything is never what was meant.
	for _, pattern := range m.Exclude {
		if pattern == "" {
			return fmt.Errorf("--exclude: %w", errEmptyExclude)
		}
	}

	if m.Old != "" && strings.TrimRight(m.Old, "/") == "" {
		return fmt.Errorf("%w: got --old=%q", errRootNamesNoPkg, m.Old)
	}

	return nil
}

// drawn are the flags that shape a report rather than decide what is in it. Embedded by the two
// commands that draw rows; total does not embed it, so those flags do not exist for total at all.
//
//nolint:lll // a struct tag is one unit.
type drawn struct {
	Depth       string   `help:"Levels below the top row, like tree -L, or \"max\"."             default:"${depth}" placeholder:"LEVELS"`
	HideCovered *float64 `help:"Leave out subtrees at this percentage or above; bare means 100."                    placeholder:"PCT"    type:"hidecovered"`
}

// CLI is the whole command line. Every command is named: there is no default, so `prettycov` alone
// prints help rather than drawing.
//
// That is what makes the four peers. An implicit command has to be reachable without naming it,
// which means its flags live at the root — where the help does not list them and a bare word is
// ambiguous between a command and a file. Naming it costs one word and deletes all of that.
//
//nolint:lll // a struct tag is one unit.
type CLI struct {
	// Global, so they may be written on either side of the command name. Embedded in each command
	// instead, they bound only after it, and `--profile X total` silently read the default profile.
	measured `embed:""`

	Version2 kong.VersionFlag `name:"version" short:"v" help:"Print the version and exit."`

	Report  reportCmd  `cmd:"" help:"Draw the packages and what they cover, one row each."`
	Misses  missesCmd  `cmd:"" help:"Print where the uncovered statements are, as file:line:col."`
	Total   totalCmd   `cmd:"" help:"Print only the coverage percentage, for a Makefile or a badge."`
	Version versionCmd `cmd:"" help:"Print the version and exit."`
	// Kong has --help but no help command; cobra generates one. Four lines to match.
	Help helpCmd `cmd:"" help:"Print help for a command."`
}

//nolint:lll // a struct tag is one unit.
type reportCmd struct {
	drawn `embed:""`

	Files  bool `help:"Draw the profile's files, not only its packages."`
	Counts bool `help:"Show uncovered/total statements after each percentage."`
}

//nolint:lll // a struct tag is one unit.
type missesCmd struct {
	drawn `embed:""`
}

//nolint:lll // a struct tag is one unit.
type totalCmd struct {
	Node string `arg:"" optional:"" help:"Package or file, spelled as the report prints it." placeholder:"PATH"`
}

type versionCmd struct{}

type helpCmd struct {
	Command []string `arg:"" optional:"" help:"Command to print help for."`
}

func (c *helpCmd) Run(k *kong.Context) error {
	ctx, err := kong.Trace(k.Kong, c.Command)
	if err != nil {
		//nolint:wrapcheck // kong names the command it could not find.
		return err
	}

	//nolint:wrapcheck // kong writes the help itself.
	return ctx.PrintUsage(false)
}

// streams is what the commands write to, bound by Run so that nothing reaches os.Stdout directly.
type streams struct {
	out, err io.Writer
}

func (c *reportCmd) Run(s *streams, m *measured) error {
	cfg, err := m.settle()
	if err != nil {
		return err
	}

	if err := c.shape(&cfg); err != nil {
		return err
	}

	cfg.Files, cfg.Counts = c.Files, c.Counts

	return status(showReport(cfg, s.out, s.err))
}

func (c *missesCmd) Run(s *streams, m *measured) error {
	cfg, err := m.settle()
	if err != nil {
		return err
	}

	if err := c.shape(&cfg); err != nil {
		return err
	}

	cfg.Misses = true

	return status(showReport(cfg, s.out, s.err))
}

func (c *totalCmd) Run(s *streams, m *measured) error {
	cfg, err := m.settle()
	if err != nil {
		return err
	}

	cfg.Total = &c.Node

	return status(showReport(cfg, s.out, s.err))
}

func (c *versionCmd) Run(s *streams) error {
	_, _ = fmt.Fprintln(s.out, buildVersion())

	return nil
}

// config is what the report needs, whichever command asked for it. The kong structs above are the
// command line; this is the answer they agree on.
type config struct {
	Profile     string
	CurrentRoot string
	NewRoot     string
	Depth       prettycov.Depth
	Color       colorMode
	Exclude     []*regexp.Regexp
	FailUnder   *float64
	HideCovered *float64
	Counts      bool
	Files       bool
	Misses      bool
	Total       *string
}

// settle turns the measured flags into the half of a config every command shares.
func (m measured) settle() (config, error) {
	cfg := config{Profile: m.Profile, CurrentRoot: m.Old, NewRoot: m.New, FailUnder: m.FailUnder}

	for _, pattern := range m.Exclude {
		re, err := prettycov.ParseExclude(pattern)
		if err != nil {
			return config{}, fmt.Errorf("--exclude: %w", err)
		}

		cfg.Exclude = append(cfg.Exclude, re)
	}

	mode, err := parseColorMode(m.Color)
	if err != nil {
		return config{}, fmt.Errorf("--color: %w", err)
	}

	cfg.Color = mode

	return cfg, nil
}

// shape adds the flags that decide how a row looks.
func (d drawn) shape(cfg *config) error {
	depth, err := prettycov.ParseDepth(d.Depth)
	if err != nil {
		return fmt.Errorf("--depth: %w", err)
	}

	cfg.Depth = depth
	cfg.HideCovered = d.HideCovered

	return nil
}

// optionalPercentage is --hide-covered, the one flag whose value may be left off. Kong has no
// NoOptDefVal, so it is a mapper: IsBool stops the scanner consuming the next argument, and Decode
// takes the value only when "=" supplied one. It is the same shape kong's own boolMapper uses.
//
// Named rather than registered for *float64, which --fail-under also is and which has no bare form.
type optionalPercentage struct{}

// IsBool reports that no following argument belongs to this flag.
func (optionalPercentage) IsBool() bool { return true }

func (optionalPercentage) Decode(ctx *kong.DecodeContext, target reflect.Value) error {
	pct := bareHideCovered

	if ctx.Scan.Peek().Type == kong.FlagValueToken {
		parsed, err := strconv.ParseFloat(fmt.Sprint(ctx.Scan.Pop().Value), 64)
		if err != nil || badPercentage(parsed) {
			//nolint:wrapcheck // a sentinel of this package's own.
			return errBadPercentage
		}

		pct = parsed
	}

	target.Set(reflect.ValueOf(&pct))

	return nil
}

// badPercentage is the one rule both thresholds obey. NaN needs naming: every comparison against it
// is false, so a gate would pass at any coverage and say nothing about it — --fail-under=nan printed
// "is below NaN%" and exited 1 on a report that was fine.
func badPercentage(pct float64) bool {
	return math.IsNaN(pct) || pct < 0 || pct > 100
}

// bareHideCovered is what --hide-covered means with nothing after it: hide what is fully covered,
// where absence means "nothing to do here".
const bareHideCovered = 100.0
