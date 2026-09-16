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
				require.NotNil(t, m.Tree)
				assert.Equal(t, prettycov.NotEmpty, m.Empty)
				assert.False(t, m.RootMissed)
				assert.Equal(t, 10, m.Tree.Coverage.Total())
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
				assert.Nil(t, m.Tree)
				assert.Equal(t, prettycov.NoStatements, m.Empty)
				assert.False(t, m.RootMissed, "the profile is what is empty, not the root")
			},
		},
		"patterns that take everything": {
			profile: twoRoots,
			req: func(p string) prettycov.Request {
				return prettycov.Request{Profile: p, Exclude: excluding(t, `\.go$`)}
			},
			want: func(t *testing.T, m prettycov.Measurement) {
				t.Helper()
				assert.Nil(t, m.Tree)
				assert.Equal(t, prettycov.ExcludedAway, m.Empty)
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
				require.NotNil(t, m.Tree)
				assert.False(t, m.RootMissed)
				assert.NotNil(t, m.Tree.Get("x"), "the label is the new root")
			},
		},
		"a rename that matches nothing": {
			profile: twoRoots,
			req: func(p string) prettycov.Request {
				return prettycov.Request{Profile: p, Rename: prettycov.Rename{From: "nowhere", To: "x"}}
			},
			want: func(t *testing.T, m prettycov.Measurement) {
				t.Helper()
				assert.True(t, m.RootMissed, "only the matching can catch a root that moved")
				assert.Nil(t, m.Tree)
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
				assert.False(t, m.RootMissed, "the pattern took them, the root is not at fault")
				require.NotNil(t, m.Tree, "and what is outside the root still measures")
				assert.Nil(t, m.Tree.Get("x"))
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
