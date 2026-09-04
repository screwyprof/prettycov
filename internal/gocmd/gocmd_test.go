package gocmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov/internal/gocmd"
)

// ParsePackages is tested on strings rather than through a `go list` run, because the rows that
// matter most are the ones go does not currently produce: this is the seam where a change to the
// go tool's output would arrive, and it has to fail loudly when it does.
func TestParsePackages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want []gocmd.Package
	}{
		{
			name: "internal tests count",
			out:  "m/a\t/src/a\t2\t0\n",
			want: []gocmd.Package{{ImportPath: "m/a", Dir: "/src/a", HasTests: true}},
		},
		{
			name: "external tests count too",
			out:  "m/a\t/src/a\t0\t1\n",
			want: []gocmd.Package{{ImportPath: "m/a", Dir: "/src/a", HasTests: true}},
		},
		{
			name: "no tests either way",
			out:  "m/a\t/src/a\t0\t0\n",
			want: []gocmd.Package{{ImportPath: "m/a", Dir: "/src/a", HasTests: false}},
		},
		{
			name: "rows keep the order go listed them in",
			out:  "m/b\t/src/b\t0\t0\nm/a\t/src/a\t1\t0\n",
			want: []gocmd.Package{
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
			want: []gocmd.Package{{ImportPath: "m/a", Dir: "/src/a", HasTests: false}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := gocmd.ParsePackages(tt.out)

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
		{name: "one good row does not excuse a bad one", out: "m/a\t/src/a\t0\t0\nm/b\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := gocmd.ParsePackages(tt.out)

			require.ErrorIs(t, err, gocmd.ErrBadOutput)
			assert.Contains(t, err.Error(), "unexpected go list output", "the error says what it read")
		})
	}
}

// TestPackagesReadsAModule runs the command for real, which is the only way to find out that
// Format still names fields go list has.
func TestPackagesReadsAModule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, dir, "go.mod", "module gocmd.test\n\ngo 1.27\n")
	write(t, dir, "a/a.go", "package a\n")
	write(t, dir, "b/b.go", "package b\n")
	write(t, dir, "b/b_test.go", "package b\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n")

	got, err := gocmd.Packages(t.Context(), dir, nil)

	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"gocmd.test/a": false, "gocmd.test/b": true}, hasTests(got))
}

// TestPackagesReportsGoTheError keeps go's own diagnosis rather than "exit status 1".
func TestPackagesReportsGoTheError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, dir, "go.mod", "this is not a go.mod\n")

	_, err := gocmd.Packages(t.Context(), dir, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown directive", "go's reason reaches the caller")
}

func hasTests(packages []gocmd.Package) map[string]bool {
	tests := map[string]bool{}
	for _, pkg := range packages {
		tests[pkg.ImportPath] = pkg.HasTests
	}

	return tests
}

func write(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// TestWorkspace finds the go.work above dir, because go searches parents for it and a caller
// standing in a subdirectory of a workspace is still in one.
func TestWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	write(t, root, "go.work", "go 1.27\n\nuse ./svc\n")
	write(t, root, "svc/go.mod", "module ws.test/svc\n\ngo 1.27\n")
	write(t, root, "svc/svc.go", "package svc\n")

	got, err := gocmd.Workspace(t.Context(), filepath.Join(root, "svc"))

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "go.work"), got)
}

// TestWorkspaceReadsOffAsNone decodes the go command's own convention: GOWORK reads back the
// literal "off" when a caller has disabled workspace mode, and that means there is no workspace,
// not one whose path happens to be "off".
func TestWorkspaceReadsOffAsNone(t *testing.T) {
	t.Setenv("GOWORK", "off")

	root := t.TempDir()

	write(t, root, "go.work", "go 1.27\n\nuse ./svc\n")
	write(t, root, "svc/go.mod", "module ws.test/svc\n\ngo 1.27\n")

	got, err := gocmd.Workspace(t.Context(), root)

	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestWorkspaceReportsNoneOutsideOne is the ordinary case for a single-module repository.
func TestWorkspaceReportsNoneOutsideOne(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, root, "go.mod", "module lone.test\n\ngo 1.27\n")

	got, err := gocmd.Workspace(t.Context(), root)

	require.NoError(t, err)
	assert.Empty(t, got)
}
