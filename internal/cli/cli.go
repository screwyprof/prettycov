package cli

import (
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/screwyprof/prettycov"
)

// An ExitCode is what the process exits with. Named, so an ordinary int cannot be one.
//
// Below is distinct from Failed so a CI step can tell "coverage dropped" from "prettycov could not
// run".
type ExitCode int

const (
	ExitOK ExitCode = iota
	ExitBelow
	ExitFailed
)

// exitError carries a status out through the error a handler returns, which is kong's only channel.
// A failed --fail-under gate is not a usage mistake: reported as one it would be printed and
// counted as exit 2.
//
// Error is never called — ExitCodeOf reads the code and the composition root prints nothing — but
// the method is forced, because a status has to be an error to travel this way.
type exitError struct{ code ExitCode }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// ExitCodeOf reports the status an error carries, and whether it carries one. False means a plain
// error the composition root still has to print.
func ExitCodeOf(err error) (ExitCode, bool) {
	if exit, ok := errors.AsType[exitError](err); ok {
		return exit.code, true
	}

	return ExitFailed, false
}

// DefaultProfile is what `go test -coverprofile=...` is conventionally pointed at, so running
// prettycov with no arguments in a repo that just ran its tests does the obvious thing.
const DefaultProfile = "coverage.out"

// DefaultDepth shows the top row plus one level. Measured across 16 real repositories it is the only
// fixed value that stays on a screen everywhere: hugo is 37 rows at depth 1, 152 at depth 2.
const DefaultDepth = "1"

var errBadPercentage = errors.New("want a percentage from 0 to 100")

var errEmptyExclude = errors.New("want a pattern")

var errRootNamesNoPkg = errors.New("--old names no package")

// measured are the flags that decide what is in the answer. Embedded in every command, because every
// command measures; a command that only draws differently does not repeat them.
//
//nolint:lll // a struct tag is one unit; splitting it hides the declaration.
type Measured struct {
	Profile   string    `help:"Coverage profile to read."                                                            default:"${profile}" placeholder:"PATH"`
	Old       string    `help:"Root package path to shorten."                                                                             placeholder:"PATH"   and:"rename"`
	New       string    `help:"What to shorten it to; --new=. strips it."                                                                 placeholder:"PATH"   and:"rename"`
	Exclude   []string  `help:"Omit files whose path, or blocks whose file:line:col, match this regexp. Repeatable."                      placeholder:"REGEXP"              sep:"none"`
	FailUnder *float64  `help:"Exit 1 when coverage is below this percentage."                                                            placeholder:"PCT"`
	Color     colorMode `help:"When to colour: auto, never or always."                                               default:"auto"`
}

// Validate is kong's per-struct hook. A root of only separators names no package — `--old=$(MODULE)/`
// with MODULE unset — which the `and:"rename"` tag cannot say, because it is about a value rather
// than about the pair.
func (m *Measured) Validate() error {
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
	Depth       prettycov.Depth `help:"Levels below the top row, like tree -L, or \"max\"."             default:"${depth}" placeholder:"LEVELS"`
	HideCovered *float64        `help:"Leave out subtrees at this percentage or above; bare means 100."                    placeholder:"PCT"    type:"hidecovered"`
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
	Measured `embed:""`

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

// A Version is the string the binary reports. It is bound by the composition root, which is what
// knows how this build was stamped; nothing here reads a linker variable or the build info.
type Version string

type versionCmd struct{}

type helpCmd struct {
	Command []string `arg:"" optional:"" help:"Command to print help for."`
}

// Run resolves the named command and prints its usage.
//
// Trace always returns a nil error — it puts the failure in Context.Error (kong context.go, the
// last line of Trace). Reading the return instead meant `prettycov help nope` printed the root's
// help as though nope were fine.
func (c *helpCmd) Run(k *kong.Context) error {
	ctx, _ := kong.Trace(k.Kong, c.Command)
	if ctx.Error != nil {
		//nolint:wrapcheck // kong names the command it could not find.
		return ctx.Error
	}

	//nolint:wrapcheck // kong writes the help itself.
	return ctx.PrintUsage(false)
}

func (c *reportCmd) render(cfg config, tree *prettycov.PathTree, s Streams) error {
	if shown := prettycov.DisplayTree(s.Out, tree, cfg.options(s.Out)); shown == 0 {
		sayNothingShown(cfg, tree, s)
	}

	return gate{cfg.FailUnder}.grade(tree, s)
}

// render has a second message the tree has none of: only a list can stop short of what is behind
// it. A tree carries its subtree's count on every row, so a shallow one is a summary rather than a
// fragment, where a short list reads as a clean bill — pipe eight of thirty-four into `vim -q -`,
// fix them, and the quickfix says there is nothing left.
func (c *missesCmd) render(cfg config, tree *prettycov.PathTree, s Streams) error {
	shown := prettycov.DisplayMisses(s.Out, tree, cfg.options(s.Out))

	switch {
	case shown == 0:
		sayNothingShown(cfg, tree, s)
	case shown < tree.Uncovered():
		_, _ = fmt.Fprintf(s.Err, "%s lists %d of %s\n",
			cfg.outputFilters(), shown, plural(tree.Uncovered(), "uncovered statement"))
	}

	return gate{cfg.FailUnder}.grade(tree, s)
}

// Streams is where a handler writes, bound by the composition root so nothing reaches os.Stdout
// directly.
type Streams struct {
	Out, Err io.Writer
}

func (c *reportCmd) Run(s *Streams, m *Measured) error {
	cfg, err := c.settled(m)
	if err != nil {
		return err
	}

	cfg.Files, cfg.Counts = c.Files, c.Counts

	tree, err := treeOf(cfg.Request, gate{cfg.FailUnder}, *s)
	if err != nil {
		return err
	}

	return c.render(cfg, tree, *s)
}

func (c *missesCmd) Run(s *Streams, m *Measured) error {
	cfg, err := c.settled(m)
	if err != nil {
		return err
	}

	tree, err := treeOf(cfg.Request, gate{cfg.FailUnder}, *s)
	if err != nil {
		return err
	}

	return c.render(cfg, tree, *s)
}

func (c *totalCmd) Run(s *Streams, m *Measured) error {
	cfg, err := m.settle()
	if err != nil {
		return err
	}

	tree, err := treeOf(cfg.Request, gate{cfg.FailUnder}, *s)
	if err != nil {
		return err
	}

	return total(gate{cfg.FailUnder}, tree, c.Node, *s)
}

func (c *versionCmd) Run(s *Streams, v Version) error {
	_, _ = fmt.Fprintln(s.Out, string(v))

	return nil
}

// config is what the report needs, whichever command asked for it. The kong structs above are the
// command line; this is the answer they agree on.
type config struct {
	// Request is what the domain measures: the profile, the rename, the patterns. Embedded rather
	// than copied field by field, so Measure takes it directly and the two cannot drift.
	prettycov.Request

	Depth       prettycov.Depth
	Color       colorMode
	FailUnder   *float64
	HideCovered *float64
	Counts      bool
	Files       bool
}

// settle turns the measured flags into the half of a config every command shares.
func (m Measured) settle() (config, error) {
	cfg := config{
		Request:   prettycov.Request{Profile: m.Profile, Rename: prettycov.Rename{From: m.Old, To: m.New}},
		FailUnder: m.FailUnder,
	}

	for _, pattern := range m.Exclude {
		re, err := prettycov.ParseExclude(pattern)
		if err != nil {
			return config{}, fmt.Errorf("--exclude: %w", err)
		}

		cfg.Exclude = append(cfg.Exclude, re)
	}

	cfg.Color = m.Color

	return cfg, nil
}

// settled is the shared half of what the two drawing commands need: the measured flags plus the
// ones that shape a row.
func (d drawn) settled(m *Measured) (config, error) {
	cfg, err := m.settle()
	if err != nil {
		return config{}, err
	}

	d.shape(&cfg)

	return cfg, nil
}

// options is what the printers in package prettycov take, settled against the destination: --color
// resolves against where the output goes, which parsing argv is too early to ask.
func (c config) options(out io.Writer) prettycov.Options {
	return prettycov.Options{
		Depth:       c.Depth,
		Color:       c.Color.palette(out),
		Counts:      c.Counts,
		Files:       c.Files,
		HideCovered: c.HideCovered,
	}
}

// shape adds the flags that decide how a row looks. Nothing to parse and nothing to fail: both
// arrived parsed, because both types read themselves.
func (d drawn) shape(cfg *config) {
	cfg.Depth = d.Depth
	cfg.HideCovered = d.HideCovered
}

// OptionalPercentage is --hide-covered, the one flag whose value may be left off. Kong has no
// NoOptDefVal, so it is a mapper: Decode takes a value only when "=" supplied one, which is the
// same shape kong's own boolMapper uses. `--hide-covered 90` is not the bare form with a number
// after it, it is the bare form and a stray argument — as it is under cobra's NoOptDefVal too.
//
// Named rather than registered for *float64, which --fail-under also is and which has no bare form.
type OptionalPercentage struct{}

func (OptionalPercentage) Decode(ctx *kong.DecodeContext, target reflect.Value) error {
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
