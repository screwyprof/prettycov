package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const profile = "mode: atomic\n" +
	"m/a/a.go:1.1,2.2 6 1\n" + // 6 covered
	"m/b/b.go:1.1,2.2 4 0\n" // 4 not covered -> 60% overall

func TestRunRendersTheReport(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, profile)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := run([]string{"-profile", path, "-color", "never"}, stdout, stderr)

	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout.String(), "60.00")
	assert.Empty(t, stderr.String())
}

// The profile may be positional, so `prettycov coverage.out` works.
func TestRunAcceptsAPositionalProfile(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := run([]string{writeProfile(t, profile), "-color", "never"}, stdout, stderr)

	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout.String(), "60.00")
}

// With no profile named at all, read ./coverage.out: that is what `go test -coverprofile` is
// conventionally pointed at, so running prettycov bare in a repo should just work.
//
//nolint:paralleltest // t.Chdir cannot be combined with t.Parallel.
func TestRunDefaultsToCoverageOutInTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, defaultProfile), []byte(profile), 0o600))

	t.Chdir(dir)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := run([]string{"-color", "never"}, stdout, stderr)

	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout.String(), "60.00")
}

// A relative path with a directory component used to fail: showReport chdir'd to the profile's
// directory and then opened the path it was given, which no longer resolved from there.
//
//nolint:paralleltest // t.Chdir cannot be combined with t.Parallel.
func TestRunReadsARelativePathWithADirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "cov.out"), []byte(profile), 0o600))

	t.Chdir(dir)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := run([]string{"sub/cov.out", "-color", "never"}, stdout, stderr)

	assert.Equal(t, exitOK, code, stderr.String())
	assert.Contains(t, stdout.String(), "60.00")
}

func TestRunFailUnder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		profile  string
		wantCode int
		wantErr  string
	}{
		{
			name: "below the threshold", profile: profile,
			args: []string{"-fail-under", "80"}, wantCode: exitBelow,
			wantErr: "total coverage 60.00% is below 80.00%",
		},
		{
			name: "at the threshold passes", profile: profile,
			args: []string{"-fail-under", "60"}, wantCode: exitOK,
		},
		{
			name: "unset means no gate", profile: profile,
			args: []string{}, wantCode: exitOK,
		},
		{
			// Silently passing here would make the gate useless on an empty or mis-pointed profile.
			name: "nothing to cover cannot clear a threshold", profile: "mode: atomic\nm/d/doc.go:1.1,2.2 0 0\n",
			args: []string{"-fail-under", "1"}, wantCode: exitBelow,
			wantErr: "no statements to cover",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			args := append([]string{writeProfile(t, tc.profile), "-color", "never"}, tc.args...)

			assert.Equal(t, tc.wantCode, run(args, stdout, stderr))

			if tc.wantErr != "" {
				assert.Contains(t, stderr.String(), tc.wantErr)
			}
		})
	}
}

func TestRunColorFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantCode  int
		wantColor bool
	}{
		{name: "always", value: "always", wantCode: exitOK, wantColor: true},
		{name: "never", value: "never", wantCode: exitOK, wantColor: false},
		{name: "auto into a buffer stays clean", value: "auto", wantCode: exitOK, wantColor: false},
		{name: "rejected", value: "sometimes", wantCode: exitFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := run([]string{writeProfile(t, profile), "-color", tc.value}, stdout, stderr)

			require.Equal(t, tc.wantCode, code)

			if tc.wantCode != exitOK {
				assert.Contains(t, stderr.String(), "invalid -color")

				return
			}

			if tc.wantColor {
				assert.Contains(t, stdout.String(), "\x1b[")
			} else {
				assert.NotContains(t, stdout.String(), "\x1b[")
			}
		})
	}
}

// Two paths is a mistake, not a request to merge them.
func TestRunRejectsMoreThanOneProfile(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := run([]string{writeProfile(t, profile), writeProfile(t, profile)}, stdout, stderr)

	assert.Equal(t, exitFailed, code)
	assert.Contains(t, stderr.String(), "at most one profile path")
}

func TestRunReportsAMissingProfile(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := run([]string{filepath.Join(t.TempDir(), "absent.out")}, stdout, stderr)

	assert.Equal(t, exitFailed, code)
	assert.Contains(t, stderr.String(), "cannot read coverage profile")
	assert.Empty(t, stdout.String())
}

func TestRunHelpAndVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantOut  string
		wantErr  string
		wantCode int
	}{
		{name: "help subcommand", args: []string{"help"}, wantErr: "Prettycov:", wantCode: exitOK},
		{name: "help flag", args: []string{"-help"}, wantErr: "Prettycov:", wantCode: exitOK},
		{name: "version subcommand", args: []string{"version"}, wantOut: "\n", wantCode: exitOK},
		{name: "version flag", args: []string{"-version"}, wantOut: "\n", wantCode: exitOK},
		{name: "unknown flag", args: []string{"-nope"}, wantErr: "flag provided but not defined", wantCode: exitFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			assert.Equal(t, tc.wantCode, run(tc.args, stdout, stderr))

			if tc.wantOut != "" {
				assert.Contains(t, stdout.String(), tc.wantOut)
			}

			if tc.wantErr != "" {
				assert.Contains(t, stderr.String(), tc.wantErr)
			}
		})
	}
}

func writeProfile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "coverage.out")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}
