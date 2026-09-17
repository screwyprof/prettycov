// Package cli is the command line: the commands, the flags they take, and every sentence prettycov
// says that is not the report itself.
//
// The router and its handlers, in the shape an HTTP service uses. A command reads its flags, asks
// the domain to measure, and turns what came back into output and a status; it decides no coverage
// question of its own. Everything user-facing lives here — the domain returns facts and outcomes,
// never words — which is why this is the one place a flag is named in a sentence.
//
// The composition root is internal/app: it builds the parser and binds these handlers to their
// dependencies. A depguard rule keeps that direction, so nothing here imports it.
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

// Measured are the flags that decide what is in the answer, embedded by the three commands that read
// a profile. Not at the root: kong would make them global, so `version --help` would list --exclude.
//
//nolint:lll // a struct tag is one unit; splitting it hides the declaration.
type Measured struct {
	Profile   string               `help:"Coverage profile to read. Default ${profile}."                                        default:"${profile}" placeholder:"PATH"`
	Old       string               `help:"Root package path to shorten. Needs --new."                                                                placeholder:"PATH"`
	New       string               `help:"What to shorten it to; --new=. strips it. Needs --old."                                                    placeholder:"PATH"`
	Exclude   []string             `help:"Omit files whose path, or blocks whose file:line:col, match this regexp. Repeatable."                      placeholder:"REGEXP" sep:"none"`
	FailUnder *prettycov.Threshold `help:"Exit 1 when coverage is below this percentage."                                                            placeholder:"PCT"`
}

// Validate is the only place the rename is judged. Kong's `and:"rename"` asked the wrong question —
// satisfied once both flags appear, whatever they hold, so `--old=$(MODULE) --new=.` with MODULE
// unset passed and renamed nothing.
//
// Order matters: `--old=/` with no --new answers both, and naming the root is the more useful
// sentence.
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

// given names the half that was supplied, which is the flag to pair rather than the one to fix.
// Exactly one is non-empty: Half is what got us here.
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

// What the program calls itself. Here rather than the composition root: the description names a
// command, and commands are this package's.
const (
	Name        = "prettycov"
	Description = "Given a coverage profile produced by 'go test', draw the packages and what they cover.\n\n" +
		"\tgo test -covermode=atomic -coverprofile=coverage.out ./...\n\tprettycov report"
)

// options is how a row is drawn. Colour is not among them: only report draws anything a palette
// reaches, so it carries the flag and resolves it against the destination — a question argv is too
// early to ask.
func (d drawn) options() prettycov.Options {
	return prettycov.Options{Depth: d.Depth, HideCovered: d.HideCovered}
}

// CLI is the whole command line. Every command is named, so `prettycov` alone prints help — an
// implicit one would need its flags at the root, where help does not list them and a bare word is
// ambiguous between a command and a file.
//
//nolint:lll // a struct tag is one unit.
type CLI struct {
	// Two spellings of one request: Go will not let both fields be Version, so the command names
	// itself in a tag.
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
// gate comes back too, since the same bar grades what was measured and refuses what was not.
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

	opts := c.options()
	opts.Files, opts.Counts, opts.Color = c.Files, c.Counts, c.Color.palette(s.Out)

	shown, err := prettycov.DisplayTree(s.Out, tree, opts)
	if err != nil {
		return cannotWrite(err, *s)
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

	shown, err := prettycov.DisplayMisses(s.Out, tree, c.options())
	if err != nil {
		return cannotWrite(err, *s)
	}

	// Two messages where the tree has one: a shallow tree carries its subtree's count on every row
	// and reads as a summary, where a short list reads as a clean bill.
	switch {
	case shown == 0:
		c.sayNothingShown(tree, *s)
	case shown < tree.Uncovered():
		_, _ = fmt.Fprintf(s.Err, "%s lists %d of %s\n",
			c.filters(), shown, plural(tree.Uncovered(), "uncovered statement"))
	}

	return g.grade(tree, *s)
}

// Run refuses an empty path before reading anything: `total "$PKG"` with PKG unset would otherwise
// grade the whole tree and pass a gate the package would have failed.
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

// request is what the domain measures. Only the patterns are settled here — every other flag is
// already its parsed type, read at the boundary.
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
// NoOptDefVal, so Decode takes a value only when "=" supplied one — the shape kong's own boolMapper
// uses. `--hide-covered 90` is the bare form and a stray argument, as under cobra too.
//
// Named rather than registered for *float64, which --fail-under also is and has no bare form.
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
