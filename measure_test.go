package prettycov_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
)

// twoRoots holds a file under m/ and one outside it, so a pattern can take everything under the
// root and still leave a measurement standing. With one root, every case below would end in
// ExcludedAway and prove nothing about the root.
const twoRoots = "mode: set\nm/a.go:1.1,2.2 5 1\nother/b.go:1.1,2.2 5 0\n"

func excluding(tb testing.TB, patterns ...string) []*regexp.Regexp {
	tb.Helper()

	compiled := make([]*regexp.Regexp, 0, len(patterns))

	for _, p := range patterns {
		re, err := prettycov.ParseExclude(p)
		require.NoError(tb, err)

		compiled = append(compiled, re)
	}

	return compiled
}

// Measure states facts. Every case here reads a field rather than a sentence, which is the whole
// reason it is separable from the command line that phrases them.
func TestMeasureReportsWhatTheProfileAndTheFlagsLeft(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		profile string
		req     func(path string) prettycov.Request
		want    func(t *testing.T, m prettycov.Measurement)
	}{
		"a profile with statements yields a tree": {
			profile: twoRoots,
			req:     func(p string) prettycov.Request { return prettycov.Request{Profile: p} },
			want: func(t *testing.T, m prettycov.Measurement) {
				t.Helper()

				tree, ok := m.Tree()
				require.True(t, ok)
				assert.Equal(t, prettycov.Measured, m.Outcome())
				assert.Equal(t, 10, tree.Coverage.Total())
			},
		},
		// Asked before any flag is judged, so a good rename is not blamed for an empty profile.
		"a profile declaring no statements": {
			profile: "mode: set\nm/doc.go:1.1,2.2 0 0\n",
			req: func(p string) prettycov.Request {
				return prettycov.Request{Profile: p, Rename: prettycov.Rename{From: "nowhere", To: "x"}}
			},
			want: func(t *testing.T, m prettycov.Measurement) {
				t.Helper()

				_, ok := m.Tree()
				assert.False(t, ok)
				assert.Equal(t, prettycov.NoStatements, m.Outcome(), "the profile is what is empty, not the root")
			},
		},
		"patterns that take everything": {
			profile: twoRoots,
			req: func(p string) prettycov.Request {
				return prettycov.Request{Profile: p, Exclude: excluding(t, `\.go$`)}
			},
			want: func(t *testing.T, m prettycov.Measurement) {
				t.Helper()

				_, ok := m.Tree()
				assert.False(t, ok)
				assert.Equal(t, prettycov.ExcludedAway, m.Outcome())
				assert.Len(t, m.Exclusions, 1, "and the accounting survives, so a caller can say what took it")
			},
		},
		"a rename that matches": {
			profile: twoRoots,
			req: func(p string) prettycov.Request {
				return prettycov.Request{Profile: p, Rename: prettycov.Rename{From: "m", To: "x"}}
			},
			want: func(t *testing.T, m prettycov.Measurement) {
				t.Helper()

				tree, ok := m.Tree()
				require.True(t, ok)
				assert.NotNil(t, tree.Get("x"), "the label is the new root")
			},
		},
		"a rename that matches nothing": {
			profile: twoRoots,
			req: func(p string) prettycov.Request {
				return prettycov.Request{Profile: p, Rename: prettycov.Rename{From: "nowhere", To: "x"}}
			},
			want: func(t *testing.T, m prettycov.Measurement) {
				t.Helper()
				assert.Equal(t, prettycov.RootMissed, m.Outcome(), "only the matching can catch a root that moved")

				_, ok := m.Tree()
				assert.False(t, ok)
			},
		},
		// The order that makes this right: the root is matched against the whole profile, not
		// against what the patterns left. Otherwise a pattern that took every file under a perfectly
		// good root reports the root as wrong, sending someone to fix a flag that is already fine.
		"a rename whose files a pattern took": {
			profile: twoRoots,
			req: func(p string) prettycov.Request {
				return prettycov.Request{
					Profile: p,
					Rename:  prettycov.Rename{From: "m", To: "x"},
					Exclude: excluding(t, `^m/`),
				}
			},
			want: func(t *testing.T, m prettycov.Measurement) {
				t.Helper()

				tree, ok := m.Tree()
				require.True(t, ok, "the pattern took them, the root is not at fault")
				assert.Nil(t, tree.Get("x"))
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := prettycov.Measure(tc.req(writeProfile(t, tc.profile)))
			require.NoError(t, err)
			tc.want(t, got)
		})
	}
}

// The error is for a profile that cannot be read. Everything else is a field, because a caller may
// want to report it and carry on.
func TestMeasureReturnsAnErrorOnlyForAnUnreadableProfile(t *testing.T) {
	t.Parallel()

	_, err := prettycov.Measure(prettycov.Request{Profile: "no/such/profile.out"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot read coverage profile")
}

// Wanted is what separates "no rename asked for" from "asked for and did not happen". Only the
// source decides: --new alone is refused by whatever parses the flags, never reaching here.
func TestRenameWantedReadsTheSource(t *testing.T) {
	t.Parallel()

	assert.True(t, prettycov.Rename{From: "m", To: "x"}.Wanted())
	assert.False(t, prettycov.Rename{}.Wanted())
	assert.False(t, prettycov.Rename{To: "x"}.Wanted())
}

// Get resolves a path under the root the report collapsed away, which is the third substitution the
// renderer makes and the last one Get undoes. A row carries its own segment, so "pkg/logger" is what
// a reader copies off a report and the tree holds it under "github.com/x/y".
func TestGetResolvesAPathTheRootWasCollapsedFrom(t *testing.T) {
	t.Parallel()

	profile := writeProfile(t, "mode: set\ngithub.com/x/y/pkg/logger/a.go:1.1,2.2 1 1\n")

	got, err := prettycov.Measure(prettycov.Request{Profile: profile})
	require.NoError(t, err)

	tree, ok := got.Tree()
	require.True(t, ok)

	full := tree.Get("github.com/x/y/pkg/logger")
	require.NotNil(t, full, "the spelling the profile holds")

	// The same node, not merely a node: the literal spelling is tried first, so prefixing can only
	// add answers and never change one.
	assert.Same(t, full, tree.Get("pkg/logger"))
	assert.Same(t, full.Files["a.go"], tree.Get("pkg/logger/a.go"))

	assert.Nil(t, tree.Get("nowhere"))
}

// NamesNoPackage is Shorten's own rule, asked where a caller can reach it: Shorten trims every
// trailing separator before matching, so a source that trims away renames nothing and says nothing.
func TestRenameNamesNoPackage(t *testing.T) {
	t.Parallel()

	for _, from := range []string{"/", "//", "///"} {
		assert.True(t, prettycov.Rename{From: from, To: "x"}.NamesNoPackage(), "%q", from)
	}

	for _, from := range []string{"", "m", "example.com/m", "m/"} {
		assert.False(t, prettycov.Rename{From: from, To: "x"}.NamesNoPackage(), "%q", from)
	}
}

// Half asks about the values, which is the whole point: a presence check is satisfied by
// `--old=$(MODULE) --new=.` with MODULE unset, and that renames nothing.
//
// Neither side given is not half a rename — it is no rename, which is every run that does not ask
// for one.
func TestRenameHalfReadsBothValues(t *testing.T) {
	t.Parallel()

	assert.True(t, prettycov.Rename{From: "m"}.Half(), "a source with no target")
	assert.True(t, prettycov.Rename{To: "x"}.Half(), "a target with no source")

	assert.False(t, prettycov.Rename{}.Half(), "neither is no rename, not half of one")
	assert.False(t, prettycov.Rename{From: "m", To: "x"}.Half())
	assert.False(t, prettycov.Rename{From: "m", To: "/"}.Half(), "the filesystem root is a target")
}

// The prefixing stops where the run does. A directory holding a file as well as a single
// subdirectory ends it, because past there the path is a choice rather than the root.
func TestGetStopsPrefixingWhereTheRunBranches(t *testing.T) {
	t.Parallel()

	// m holds one subdirectory and a file of its own, so the run ends at m.
	profile := writeProfile(t, "mode: set\nm/own.go:1.1,2.2 1 1\nm/deep/a.go:1.1,2.2 1 1\n")

	got, err := prettycov.Measure(prettycov.Request{Profile: profile})
	require.NoError(t, err)

	tree, ok := got.Tree()
	require.True(t, ok)

	assert.Same(t, tree.Get("m/deep"), tree.Get("deep"), "m is the root the report collapsed away")

	assert.Nil(t, tree.Get("deep/a.go/nope"),
		"and the run does not continue past a directory holding files")
}

// Depth and Threshold read themselves, which is what lets a flag hold the parsed value.
func TestParsedTypesReadText(t *testing.T) {
	t.Parallel()

	var d prettycov.Depth

	require.NoError(t, d.UnmarshalText([]byte("max")))
	assert.Equal(t, prettycov.DepthAll, d)
	require.Error(t, d.UnmarshalText([]byte("abc")))

	assert.Panics(t, func() { prettycov.MustThreshold(150) }, "a constant out of range is a broken build")
	assert.NotPanics(t, func() { prettycov.MustThreshold(100) })
}
