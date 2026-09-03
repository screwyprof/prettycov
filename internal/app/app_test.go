package app_test

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov/internal/app"
)

// Literal, not app's own constants: a script sees these numbers, so renumbering one has to fail.
const (
	codeOK     = 0
	codeBelow  = 1
	codeFailed = 2
)

// 6 of 10 statements covered, so the report reads 60.00 and a -fail-under above that fails.
// Copied in cmd/prettycov/testdata/sixty-percent.out too: go:embed cannot reach out of its own package. Change both.
//
//go:embed testdata/sixty-percent.out
var profile string

func TestRunRendersTheReport(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, profile)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"-profile", path, "-color", "never"}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stdout.String(), "60.00")
	assert.Empty(t, stderr.String())
}

// The profile may be positional, so `prettycov coverage.out` works.
func TestRunAcceptsAPositionalProfile(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{writeProfile(t, profile), "-color", "never"}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stdout.String(), "60.00")
}

// With no profile named at all, read ./coverage.out: that is what `go test -coverprofile` is
// conventionally pointed at, so running prettycov bare in a repo should just work.
//
//nolint:paralleltest // t.Chdir cannot be combined with t.Parallel.
func TestRunDefaultsToCoverageOutInTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "coverage.out"), []byte(profile), 0o600))

	t.Chdir(dir)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	// No arguments at all, which is the truest form of this: a bare `prettycov`. Colour is off
	// anyway, since a buffer is not a terminal.
	code := app.Run(nil, stdout, stderr)

	assert.Equal(t, codeOK, code, stderr.String())
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
	code := app.Run([]string{"sub/cov.out", "-color", "never"}, stdout, stderr)

	assert.Equal(t, codeOK, code, stderr.String())
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
			args: []string{"-fail-under", "80"}, wantCode: codeBelow,
			wantErr: "total coverage 60.00% is below 80.00%",
		},
		{
			name: "at the threshold passes", profile: profile,
			args: []string{"-fail-under", "60"}, wantCode: codeOK,
		},
		{
			name: "unset means no gate", profile: profile,
			args: []string{}, wantCode: codeOK,
		},
		{
			// Silently passing here would make the gate useless on an empty or mis-pointed profile.
			name: "nothing to cover cannot clear a threshold", profile: "mode: atomic\nm/d/doc.go:1.1,2.2 0 0\n",
			args: []string{"-fail-under", "1"}, wantCode: codeBelow,
			wantErr: "no statements to cover",
		},
		{
			// Zero is a real threshold: it asks only that the profile hold some statements. It
			// must not silently mean "no gate", which is what a zero default would make it.
			name: "zero still requires something to measure", profile: "mode: atomic\nm/d/doc.go:1.1,2.2 0 0\n",
			args: []string{"-fail-under", "0"}, wantCode: codeBelow,
			wantErr: "no statements to cover",
		},
		{
			name: "zero passes when there is anything at all", profile: profile,
			args: []string{"-fail-under", "0"}, wantCode: codeOK,
		},
		{
			name: "not a number", profile: profile,
			args: []string{"-fail-under", "abc"}, wantCode: codeFailed,
			wantErr: "want a percentage",
		},
		{
			// The dangerous one: `total < NaN` is false, so this used to clear the gate at any
			// coverage and print nothing. A CI config templating a bad value gets a diagnostic.
			name: "NaN", profile: profile,
			args: []string{"-fail-under", "nan"}, wantCode: codeFailed,
			wantErr: "want a percentage",
		},
		{
			name: "negative", profile: profile,
			args: []string{"-fail-under", "-5"}, wantCode: codeFailed,
			wantErr: "want a percentage",
		},
		{
			// Unreachable rather than merely strict: nothing can cover 150% of its statements.
			name: "above 100", profile: profile,
			args: []string{"-fail-under", "150"}, wantCode: codeFailed,
			wantErr: "want a percentage",
		},
		{
			// Counts this large only come from a hand-written profile, and summing them wraps.
			// Rejecting the profile beats reporting a percentage derived from the wreckage.
			name: "counts that overflow past zero",
			profile: "mode: set\nm/a.go:1.1,2.2 4611686018427387904 1\n" +
				"m/b.go:3.1,4.2 9223372036854775807 0\nm/c.go:5.1,6.2 9223372036854775807 0\n",
			args: []string{"-fail-under", "80"}, wantCode: codeFailed,
			wantErr: "statement counts overflow",
		},
		{
			// The one that mattered: wrapping all the way round to a small positive total left a
			// plausible-looking 33.33% that cleared the gate and exited 0. Guarding only against
			// a negative total missed it, because this total is not negative.
			name: "counts that wrap back to a plausible total",
			profile: "mode: set\nm/a.go:1.1,2.2 5 1\n" +
				"m/b.go:3.1,4.2 9223372036854775807 0\nm/c.go:5.1,6.2 9223372036854775807 0\n" +
				"m/d.go:7.1,8.2 12 0\n",
			args: []string{"-fail-under", "30"}, wantCode: codeFailed,
			wantErr: "statement counts overflow",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			args := append([]string{writeProfile(t, tc.profile), "-color", "never"}, tc.args...)

			assert.Equal(t, tc.wantCode, app.Run(args, stdout, stderr))

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
		{name: "always", value: "always", wantCode: codeOK, wantColor: true},
		{name: "never", value: "never", wantCode: codeOK, wantColor: false},
		{name: "auto into a buffer stays clean", value: "auto", wantCode: codeOK, wantColor: false},
		{name: "rejected", value: "sometimes", wantCode: codeFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run([]string{writeProfile(t, profile), "-color", tc.value}, stdout, stderr)

			require.Equal(t, tc.wantCode, code)

			if tc.wantCode != codeOK {
				assert.Contains(t, stderr.String(), `invalid value "sometimes" for flag -color`)

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

// Naming the profile twice is a mistake, not a request to merge them — whichever way it is named.
func TestRunRejectsTwoProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    func(a, b string) []string
		wantErr string
	}{
		{
			name:    "two positionals",
			args:    func(a, b string) []string { return []string{a, b} },
			wantErr: "at most one profile path",
		},
		{
			name:    "positional and -profile",
			args:    func(a, b string) []string { return []string{a, "-profile", b} },
			wantErr: "profile given twice",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(tc.args(writeProfile(t, profile), writeProfile(t, profile)), stdout, stderr)

			assert.Equal(t, codeFailed, code)
			assert.Contains(t, stderr.String(), tc.wantErr)
			assert.Empty(t, stdout.String(), "nothing rendered when the input is ambiguous")
		})
	}
}

// Someone running this for the first time in a repo with no profile is one command away, so say
// which rather than leaving them a bare file-not-found.
func TestRunReportsAMissingProfileWithTheCommandThatMakesOne(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{filepath.Join(t.TempDir(), "absent.out")}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), "cannot read coverage profile")
	assert.Contains(t, stderr.String(), "go test -coverprofile=")
	assert.Empty(t, stdout.String())
}

// A malformed profile is not a missing one, so it must not suggest re-running the tests.
func TestRunReportsAMalformedProfileWithoutTheHint(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{writeProfile(t, "not a profile\n")}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), "invalid coverage profile")
	assert.NotContains(t, stderr.String(), "go test -coverprofile=")
}

// Help that was asked for is output, not a diagnostic, so it belongs on stdout — otherwise
// `prettycov help | less` shows nothing. Usage printed because of a mistake stays on stderr.
func TestRunPrintsRequestedHelpOnStdout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "subcommand", args: []string{"help"}},
		{name: "flag", args: []string{"-help"}},
		{name: "shorthand", args: []string{"-h"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			assert.Equal(t, codeOK, app.Run(tc.args, stdout, stderr))
			assert.Contains(t, stdout.String(), "Prettycov:")
			assert.Empty(t, stderr.String())

			// Every flag has to be listed, or the help is worse than none.
			for _, flag := range []string{"-depth", "-old", "-new", "-color", "-fail-under", "-profile"} {
				assert.Contains(t, stdout.String(), flag)
			}
		})
	}
}

// Everything after a bare -- is a path, verbatim. Parsing once per positional consumed the --
// with the first Parse, so anything after the first argument went back to being read as a flag:
// `prettycov -- a.out -depth=2` quietly set the depth instead of complaining about two paths.
func TestRunTreatsArgsAfterDoubleDashAsPaths(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"--", writeProfile(t, profile), "-depth=2"}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), "at most one profile path")
}

// A profile whose name starts with a dash is nameable after --, and stays a path.
func TestRunReadsADashedProfileAfterDoubleDash(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "-dashed.out")
	require.NoError(t, os.WriteFile(path, []byte(profile), 0o600))

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"-color", "never", "--", path}, stdout, stderr)

	assert.Equal(t, codeOK, code, stderr.String())
	assert.Contains(t, stdout.String(), "60.00")
}

// One typo used to print the message, then the whole usage, then the message again: 33 lines.
func TestRunKeepsABadFlagShort(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"-nope"}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), "not defined: -nope")
	assert.Contains(t, stderr.String(), `run "prettycov -help" for usage`)
	assert.NotContains(t, stderr.String(), "Prettycov:", "the usage text belongs behind -help")
	assert.LessOrEqual(t, strings.Count(stderr.String(), "\n"), 3)
}

// `version` as a bare word is recognised before the flag package sees it, which would take it for
// a profile path; -version is the ordinary flag. What the version actually says is the binary
// tests' business — it depends on how the binary was linked, and nix stamps it during checkPhase.
func TestRunAcceptsBothVersionSpellings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "subcommand", args: []string{"version"}},
		{name: "flag", args: []string{"-version"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			assert.Equal(t, codeOK, app.Run(tc.args, stdout, stderr))
			assert.NotEmpty(t, strings.TrimSpace(stdout.String()), "never a bare newline")
			assert.Empty(t, stderr.String())
		})
	}
}

func writeProfile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "coverage.out")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}
