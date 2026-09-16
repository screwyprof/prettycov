package cli

import (
	"errors"
	"fmt"
	"io"
	"reflect"

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

var errRootNamesNoPkg = errors.New("--old names no package")

// Measured are the flags that decide what is in the answer. Embedded in every command, because every
// command measures; a command that only draws differently does not repeat them.
//
//nolint:lll // a struct tag is one unit; splitting it hides the declaration.
type Measured struct {
	Profile   string               `help:"Coverage profile to read."                                                            default:"${profile}" placeholder:"PATH"`
	Old       string               `help:"Root package path to shorten."                                                                             placeholder:"PATH"   and:"rename"`
	New       string               `help:"What to shorten it to; --new=. strips it."                                                                 placeholder:"PATH"   and:"rename"`
	Exclude   []string             `help:"Omit files whose path, or blocks whose file:line:col, match this regexp. Repeatable."                      placeholder:"REGEXP"              sep:"none"`
	FailUnder *prettycov.Threshold `help:"Exit 1 when coverage is below this percentage."                                                            placeholder:"PCT"`
	Color     colorMode            `help:"When to colour: auto, never or always."                                               default:"auto"`
}

// Validate is kong's per-struct hook. A root of only separators names no package — `--old=$(MODULE)/`
// with MODULE unset — which the `and:"rename"` tag cannot say, because it is about a value rather
// than about the pair.
func (m *Measured) Validate() error {
	if (prettycov.Rename{From: m.Old, To: m.New}).NamesNoPackage() {
		return fmt.Errorf("%w: got --old=%q", errRootNamesNoPkg, m.Old)
	}

	return nil
}

// drawn are the flags that shape a report rather than decide what is in it. Embedded by the two
// commands that draw rows; total does not embed it, so those flags do not exist for total at all.
//
//nolint:lll // a struct tag is one unit.
type drawn struct {
	Depth       prettycov.Depth      `help:"Levels below the top row, like tree -L, or \"max\"."             default:"${depth}" placeholder:"LEVELS"`
	HideCovered *prettycov.Threshold `help:"Leave out subtrees at this percentage or above; bare means 100."                    placeholder:"PCT"    type:"hidecovered"`
}

// Name and Description are what the program calls itself. Here rather than in the composition root
// because the description names a command, and a command is this package's.
const (
	Name        = "prettycov"
	Description = "Given a coverage profile produced by 'go test', draw the packages and what they cover.\n\n" +
		"\tgo test -covermode=atomic -coverprofile=coverage.out ./...\n\tprettycov report"
)

// Args settles what was typed. No command at all is someone finding out what this does, not a
// mistake: kong would answer "expected one of ...", where the help says that and more.
//
// Here rather than in the composition root because it is a routing rule, and the reason for it is
// the paragraph on CLI below.
func Args(args []string) []string {
	if len(args) == 0 {
		return []string{"--help"}
	}

	return args
}

// options is how a row is drawn, for the two commands that draw one. The colour comes from
// Measured because it is a terminal policy every command takes, and is resolved here because where
// the output goes is a question argv is too early to ask.
func (d drawn) options(mode colorMode, out io.Writer) prettycov.Options {
	return prettycov.Options{Depth: d.Depth, HideCovered: d.HideCovered, Color: mode.palette(out)}
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

	//nolint:nilaway // Trace's last line is `return c, nil`: the context is never nil.
	if ctx.Error != nil {
		//nolint:wrapcheck // kong names the command it could not find.
		return ctx.Error
	}

	//nolint:wrapcheck // kong writes the help itself.
	return ctx.PrintUsage(false)
}

// Streams is where a handler writes, bound by the composition root so nothing reaches os.Stdout
// directly.
type Streams struct {
	Out, Err io.Writer
}

func (c *reportCmd) Run(s *Streams, m *Measured) error {
	req, err := m.request()
	if err != nil {
		return err
	}

	g := m.gate()

	tree, err := treeOf(req, g, *s)
	if err != nil {
		return err
	}

	opts := c.options(m.Color, s.Out)
	opts.Files, opts.Counts = c.Files, c.Counts

	// S2: inlined, because render had one caller and its three-argument shape was the interface
	// that used to need it.
	if shown := prettycov.DisplayTree(s.Out, tree, opts); shown == 0 {
		sayNothingShown(c.drawn, tree, *s)
	}

	return g.grade(tree, *s)
}

func (c *missesCmd) Run(s *Streams, m *Measured) error {
	req, err := m.request()
	if err != nil {
		return err
	}

	g := m.gate()

	tree, err := treeOf(req, g, *s)
	if err != nil {
		return err
	}

	shown := prettycov.DisplayMisses(s.Out, tree, c.options(m.Color, s.Out))

	// Two messages where the tree has one: only a list can stop short of what is behind it. A tree
	// carries its subtree's count on every row, so a shallow one is a summary rather than a
	// fragment, where a short list reads as a clean bill.
	switch {
	case shown == 0:
		sayNothingShown(c.drawn, tree, *s)
	case shown < tree.Uncovered():
		_, _ = fmt.Fprintf(s.Err, "%s lists %d of %s\n",
			c.filters(), shown, plural(tree.Uncovered(), "uncovered statement"))
	}

	return g.grade(tree, *s)
}

func (c *totalCmd) Run(s *Streams, m *Measured) error {
	req, err := m.request()
	if err != nil {
		return err
	}

	g := m.gate()

	tree, err := treeOf(req, g, *s)
	if err != nil {
		return err
	}

	return total(g, tree, c.Node, *s)
}

func (c *versionCmd) Run(s *Streams, vars kong.Vars) error {
	_, _ = fmt.Fprintln(s.Out, vars["version"])

	return nil
}

// request is what the domain measures. The only thing settled here is the patterns: everything else
// is already the parsed type, because every flag reads itself at the boundary.
func (m *Measured) request() (prettycov.Request, error) {
	req := prettycov.Request{Profile: m.Profile, Rename: prettycov.Rename{From: m.Old, To: m.New}}

	for _, pattern := range m.Exclude {
		re, err := prettycov.ParseExclude(pattern)
		if err != nil {
			return prettycov.Request{}, fmt.Errorf("--exclude: %w", err)
		}

		req.Exclude = append(req.Exclude, re)
	}

	return req, nil
}

// gate is --fail-under, read where it is declared.
func (m *Measured) gate() gate { return gate{m.FailUnder} }

// OptionalPercentage is --hide-covered, the one flag whose value may be left off. Kong has no
// NoOptDefVal, so it is a mapper: Decode takes a value only when "=" supplied one, which is the
// same shape kong's own boolMapper uses. `--hide-covered 90` is not the bare form with a number
// after it, it is the bare form and a stray argument — as it is under cobra's NoOptDefVal too.
//
// Named rather than registered for *float64, which --fail-under also is and which has no bare form.
type OptionalPercentage struct{}

func (OptionalPercentage) Decode(ctx *kong.DecodeContext, target reflect.Value) error {
	bar := prettycov.MustThreshold(bareHideCovered)

	if ctx.Scan.Peek().Type == kong.FlagValueToken {
		if err := bar.UnmarshalText(fmt.Append(nil, ctx.Scan.Pop().Value)); err != nil {
			//nolint:wrapcheck // Threshold's error is already phrased for a flag.
			return err
		}
	}

	target.Set(reflect.ValueOf(&bar))

	return nil
}

// bareHideCovered is what --hide-covered means with nothing after it: hide what is fully covered,
// where absence means "nothing to do here".
const bareHideCovered = 100.0
