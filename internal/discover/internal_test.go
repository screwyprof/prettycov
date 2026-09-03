package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Everything else in this package reaches its subject through a `go list` subprocess, which makes
// those tests slow, dependent on a toolchain, and unable to state a case go itself will not
// produce. This is the part with a decision in it, so it is tested on its own.
func TestParsePackages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want []Package
	}{
		{
			name: "internal tests count",
			out:  "m/a\t/src/a\t2\t0\n",
			want: []Package{{ImportPath: "m/a", Dir: "/src/a", HasTests: true}},
		},
		{
			name: "external tests count too",
			out:  "m/a\t/src/a\t0\t1\n",
			want: []Package{{ImportPath: "m/a", Dir: "/src/a", HasTests: true}},
		},
		{
			name: "no tests either way",
			out:  "m/a\t/src/a\t0\t0\n",
			want: []Package{{ImportPath: "m/a", Dir: "/src/a", HasTests: false}},
		},
		{
			name: "several rows keep their order",
			out:  "m/b\t/src/b\t0\t0\nm/a\t/src/a\t1\t0\n",
			want: []Package{
				{ImportPath: "m/b", Dir: "/src/b", HasTests: false},
				{ImportPath: "m/a", Dir: "/src/a", HasTests: true},
			},
		},
		{
			// A module holding no packages: go warns on stderr and lists nothing, which is an
			// answer rather than a failure.
			name: "no rows at all",
			out:  "",
			want: nil,
		},
		{
			name: "blank lines are not rows",
			out:  "\n\nm/a\t/src/a\t0\t0\n\n",
			want: []Package{{ImportPath: "m/a", Dir: "/src/a", HasTests: false}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parsePackages(tt.out)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// A row this package cannot read has to stop it. Guessing a zero would report the package as
// untested, and "untested" against "measured and covered nothing" is the distinction the whole
// tool turns on — so the one wrong answer available here is the one that looks plausible.
func TestParsePackagesRejectsMalformedRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
	}{
		{name: "too few columns", out: "m/a\t/src/a\t0\n"},
		{name: "too many columns", out: "m/a\t/src/a\t0\t0\textra\n"},
		{name: "no columns at all", out: "m/a\n"},
		{name: "internal count is not a number", out: "m/a\t/src/a\tmany\t0\n"},
		{name: "external count is not a number", out: "m/a\t/src/a\t0\t?\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parsePackages(tt.out)

			require.ErrorIs(t, err, errBadListOutput)
		})
	}
}

// TestSkipDir states the rule in one place. vendor holds whole modules that are not yours, and the
// go tool itself ignores the rest, so a module parked there is deliberately outside the build.
func TestSkipDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{name: "vendored dependencies", dir: "vendor", want: true},
		{name: "test fixtures", dir: "testdata", want: true},
		{name: "underscore prefix", dir: "_reference", want: true},
		{name: "dot prefix", dir: ".git", want: true},
		{name: "ordinary package", dir: "internal", want: false},
		{name: "merely contains vendor", dir: "vendoring", want: false},
		{name: "merely contains testdata", dir: "testdata2", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, skipDir(tt.dir))
		})
	}
}
