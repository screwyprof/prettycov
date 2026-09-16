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

var (
	errRootNamesNoPkg = errors.New("--old names no package")
	errHalfARename    = errors.New("--old and --new rename a root package together; one alone does nothing")
	errEmptyTotalPath = errors.New("want a path, or total on its own for the whole tree")
)

// Measured are the flags that decide what is in the answer. Embedded by the three commands that
// read a profile, and by nothing else: version answers without one, so it offers none of these.
//
// Not at the root. Kong would make them global, which reads as convenience and is really a claim
// that every command takes them — `version --help` then lists --exclude. They were at the root
// because a per-command flag written before the command name used to be read as the default
// command's positional argument, silently; with every command named there is no default to absorb
// it, and kong answers "unknown flag" instead.
//
//nolint:lll // a struct tag is one unit; splitting it hides the declaration.
type Measured struct {
	Profile   string               `help:"Coverage profile to read. Default ${profile}."                                        default:"${profile}" placeholder:"PATH"`
	Old       string               `help:"Root package path to shorten. Needs --new."                                                                placeholder:"PATH"`
	New       string               `help:"What to shorten it to; --new=. strips it. Needs --old."                                                    placeholder:"PATH"`
	Exclude   []string             `help:"Omit files whose path, or blocks whose file:line:col, match this regexp. Repeatable."                      placeholder:"REGEXP" sep:"none"`
	FailUnder *prettycov.Threshold `help:"Exit 1 when coverage is below this percentage."                                                            placeholder:"PCT"`
}

// Validate is kong's per-struct hook, and the only place the rename is judged. Kong's `and:"rename"`
// group was here and asked the wrong question: it is satisfied once both flags appear, whatever they
// hold, so `--old=$(MODULE) --new=.` with MODULE unset passed it and then renamed nothing, silently
// — the one failure the guard exists to prevent.
//
// Order is load-bearing. `--old=/` with no --new answers both, and naming the root is the more
// useful sentence: supplying --new would not help.
func (m *Measured) Validate() error {
	rename := prettycov.Rename{From: m.Old, To: m.New}

	if rename.NamesNoPackage() {
		return fmt.Errorf("%w: got --old=%q", errRootNamesNoPkg, m.Old)
	}

	if rename.Half() {
		return fmt.Errorf("%w: got %s", errHalfARename, given(m.Old, m.New))
	}

	return nil
}

// given names the half that was supplied, which is the flag the reader has to pair rather than the
// one to fix. Exactly one is non-empty here: Half is what got us in.
func given(oldRoot, newRoot string) string {
	if oldRoot != "" {
		return fmt.Sprintf("--old=%q", oldRoot)
	}

	return fmt.Sprintf("--new=%q", newRoot)
}

// drawn are the flags that shape a drawing rather than decide what is in it. Embedded by the two
// commands that draw, so they do not exist for total at all.
//
//nolint:lll // a struct tag is one unit.
type drawn struct {
	Depth       prettycov.Depth      `help:"Levels below the top row, like tree -L, or \"max\". Default ${depth}." default:"${depth}" placeholder:"LEVELS"`
	HideCovered *prettycov.Threshold `help:"Leave out subtrees at this percentage or above; bare means 100."                          placeholder:"PCT"    type:"hidecovered"`
}

// Name and Description are what the program calls itself. Here rather than in the composition root
// because the description names a command, and a command is this package's.
const (
	Name        = "prettycov"
	Description = "Given a coverage profile produced by 'go test', draw the packages and what they cover.\n\n" +
		"\tgo test -covermode=atomic -coverprofile=coverage.out ./...\n\tprettycov report"
)

// options is how a row is drawn, for the two commands that draw one. The palette is resolved here
// because where the output goes is a question argv is too early to ask.
// Colour is left at the zero Palette, which is Plain. Only report draws anything a palette reaches,
// so only report carries the flag and sets it.
func (d drawn) options(_ io.Writer) prettycov.Options {
	return prettycov.Options{Depth: d.Depth, HideCovered: d.HideCovered}
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
	// Two spellings of one request. The flag takes the field name and the command says its own in a
	// tag: Go will not let both be Version, and naming one of them around that is a workaround
	// wearing a name.
	Version kong.VersionFlag `short:"v" help:"Print the version and exit."`

	Report       reportCmd  `cmd:"" help:"Draw the packages and what they cover, one row each."`
	Misses       missesCmd  `cmd:"" help:"Print where the uncovered statements are, as file:line:col."`
	Total        totalCmd   `cmd:"" help:"Print only the coverage percentage, for a Makefile or a badge."`
	PrintVersion versionCmd `cmd:"" help:"Print the version and exit."                                    name:"version"`
}

type reportCmd struct {
	Measured `embed:""`
	drawn    `embed:""`

	Files  bool      `help:"Draw the profile's files, not only its packages."`
	Counts bool      `help:"Show uncovered/total statements after each percentage."`
	Color  colorMode `help:"When to colour: auto, never or always."                 default:"auto"`
}

type missesCmd struct {
	Measured `embed:""`
	drawn    `embed:""`
}

// Node is a pointer so that no argument and an empty one are different things: `total` is the whole
// tree, `total ""` is a mistake. A plain string spells both "".
type totalCmd struct {
	Measured `embed:""`

	Node *string `arg:"" optional:"" help:"Package or file, spelled as the report prints it." placeholder:"PATH"`
}

type versionCmd struct{}

// Streams is where a handler writes, bound by the composition root so nothing reaches os.Stdout
// directly.
type Streams struct {
	Out, Err io.Writer
}

// measure is every command's first move: settle the flags, then read the profile through them. The
// gate comes back with the tree because the same bar grades what was measured and refuses what was
// not — treeOf already needs it to tell "empty report" from "empty report under --fail-under".
func (m *Measured) measure(s Streams) (*prettycov.PathTree, gate, error) {
	req, err := m.request()
	if err != nil {
		return nil, gate{}, err
	}

	g := gate{m.FailUnder}

	tree, err := treeOf(req, g, s)

	return tree, g, err
}

func (c *reportCmd) Run(s *Streams) error {
	tree, g, err := c.measure(*s)
	if err != nil {
		return err
	}

	opts := c.options(s.Out)
	opts.Files, opts.Counts, opts.Color = c.Files, c.Counts, c.Color.palette(s.Out)

	// S2: inlined, because render had one caller and its three-argument shape was the interface
	// that used to need it.
	shown, err := prettycov.DisplayTree(s.Out, tree, opts)
	if err != nil {
		return wroteNothing(err, *s)
	}

	if shown == 0 {
		c.sayNothingShown(tree, *s)
	}

	return g.grade(tree, *s)
}

func (c *missesCmd) Run(s *Streams) error {
	tree, g, err := c.measure(*s)
	if err != nil {
		return err
	}

	shown, err := prettycov.DisplayMisses(s.Out, tree, c.options(s.Out))
	if err != nil {
		return wroteNothing(err, *s)
	}

	// Two messages where the tree has one: only a list can stop short of what is behind it. A tree
	// carries its subtree's count on every row, so a shallow one is a summary rather than a
	// fragment, where a short list reads as a clean bill.
	switch {
	case shown == 0:
		c.sayNothingShown(tree, *s)
	case shown < tree.Uncovered():
		_, _ = fmt.Fprintf(s.Err, "%s lists %d of %s\n",
			c.filters(), shown, plural(tree.Uncovered(), "uncovered statement"))
	}

	return g.grade(tree, *s)
}

// Run refuses an empty path before reading anything. `total "$PKG"` with PKG unset would otherwise
// grade the whole tree, and a tree passing a gate the package would have failed is the one way this
// command can be silently wrong in CI.
func (c *totalCmd) Run(s *Streams) error {
	want := ""
	if c.Node != nil {
		if want = *c.Node; want == "" {
			//nolint:wrapcheck // a sentinel of this package's own.
			return errEmptyTotalPath
		}
	}

	tree, g, err := c.measure(*s)
	if err != nil {
		return err
	}

	return total(g, tree, want, *s)
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

// OptionalPercentage is --hide-covered, the one flag whose value may be left off. Kong has no
// NoOptDefVal, so it is a mapper: Decode takes a value only when "=" supplied one, which is the
// same shape kong's own boolMapper uses. `--hide-covered 90` is not the bare form with a number
// after it, it is the bare form and a stray argument — as it is under cobra's NoOptDefVal too.
//
// Named rather than registered for *float64, which --fail-under also is and which has no bare form.
type OptionalPercentage struct{}

func (OptionalPercentage) Decode(ctx *kong.DecodeContext, target reflect.Value) error {
	// 100 is what --hide-covered means with nothing after it: hide what is fully covered, where
	// absence means "nothing to do here".
	bar := prettycov.MustThreshold(100)

	if ctx.Scan.Peek().Type == kong.FlagValueToken {
		if err := bar.UnmarshalText(fmt.Append(nil, ctx.Scan.Pop().Value)); err != nil {
			//nolint:wrapcheck // Threshold's error is already phrased for a flag.
			return err
		}
	}

	target.Set(reflect.ValueOf(&bar))

	return nil
}
