package app

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/screwyprof/prettycov"
)

// defaultProfile is what `go test -coverprofile=...` is conventionally pointed at, so running
// prettycov with no arguments in a repo that just ran its tests does the obvious thing.
const defaultProfile = "coverage.out"

// defaultDepth shows the top row plus one level. Measured across 16 real repositories it is the only
// fixed value that stays on a screen everywhere: hugo is 37 rows at depth 1, 152 at depth 2.
const defaultDepth = "1"

var errRootNamesNoPkg = errors.New("--old names no package")

// measured are the flags that decide what is in the answer. Embedded in every command, because every
// command measures; a command that only draws differently does not repeat them.
//
//nolint:lll // a struct tag is one unit; splitting it hides the declaration.
type measured struct {
	Profile   string   `help:"Coverage profile to read."                                                            default:"${profile}" placeholder:"PATH"`
	Old       string   `help:"Root package path to shorten."                                                                             placeholder:"PATH"   and:"rename"`
	New       string   `help:"What to shorten it to; --new=. strips it."                                                                 placeholder:"PATH"   and:"rename"`
	Exclude   []string `help:"Omit files whose path, or blocks whose file:line:col, match this regexp. Repeatable."                      placeholder:"REGEXP"`
	FailUnder *float64 `help:"Exit 1 when coverage is below this percentage."                                                            placeholder:"PCT"`
	Color     string   `help:"When to colour: auto, never or always."                                               default:"auto"                                         enum:"auto,never,always"`
}

// Validate is kong's per-struct hook. A root of only separators names no package — `--old=$(MODULE)/`
// with MODULE unset — which the `and:"rename"` tag cannot say, because it is about a value rather
// than about the pair.
func (m *measured) Validate() error {
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
	Depth       string   `help:"Levels below the top row, like tree -L, or \"max\"." default:"${depth}" placeholder:"LEVELS"`
	HideCovered *float64 `help:"Leave out subtrees at this percentage or above."                        placeholder:"PCT"`
}

// CLI is the whole command line. The default command draws the tree, which is what a bare
// `prettycov` has always done; "withargs" lets it keep taking a profile as a positional.
//
//nolint:lll // a struct tag is one unit.
type CLI struct {
	// Global, so they may be written on either side of the command name. Embedded in each command
	// instead, they bound only after it, and `--profile X total` silently read the default profile.
	measured `embed:""`

	Tree    treeCmd    `cmd:"" name:"tree" default:"withargs" help:"Draw the packages and what they cover."`
	Misses  missesCmd  `cmd:""                                help:"Print where the uncovered statements are, as file:line:col."`
	Total   totalCmd   `cmd:""                                help:"Print only the coverage percentage, for a Makefile or a badge."`
	Version versionCmd `cmd:""                                help:"Print the version and exit."`
}

//nolint:lll // a struct tag is one unit.
type treeCmd struct {
	drawn `embed:""`

	Files  bool   `help:"Draw the profile's files, not only its packages."`
	Counts bool   `help:"Show uncovered/total statements after each percentage."`
	Path   string `help:"Coverage profile to read."                              arg:"" optional:"" placeholder:"PROFILE"`
}

//nolint:lll // a struct tag is one unit.
type missesCmd struct {
	drawn `embed:""`

	Path string `arg:"" optional:"" help:"Coverage profile to read." placeholder:"PROFILE"`
}

//nolint:lll // a struct tag is one unit.
type totalCmd struct {
	Node string `arg:"" optional:"" help:"Package or file, spelled as the report prints it." placeholder:"PATH"`
}

type versionCmd struct{}

// streams is what the commands write to, bound by Run so that nothing reaches os.Stdout directly.
type streams struct {
	out, err io.Writer
}

func (c *treeCmd) Run(s *streams, m *measured) error {
	cfg, err := m.settle(c.Path)
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
	cfg, err := m.settle(c.Path)
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
	cfg, err := m.settle("")
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

// settle turns the measured flags into the half of a config every command shares. The positional is
// a profile for the commands that draw one; total passes "" because its own is a node.
func (m measured) settle(positional string) (config, error) {
	cfg := config{Profile: m.Profile, CurrentRoot: m.Old, NewRoot: m.New, FailUnder: m.FailUnder}

	if positional != "" {
		cfg.Profile = positional
	}

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
