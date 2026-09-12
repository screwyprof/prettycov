package app_test

import (
	"bytes"
	_ "embed"
	"io"
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
			// The boundary itself is allowed: "everything must be covered" is a real thing to ask.
			// Nothing pinned this, so tightening the check to >= would have gone unnoticed.
			name: "exactly 100 is a threshold, not an error", profile: profile,
			args: []string{"-fail-under", "100"}, wantCode: codeBelow,
			wantErr: "is below 100.00%",
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

func TestRunCountsFlag(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	code := app.Run([]string{"-profile", writeProfile(t, profile), "-color", "never", "-counts"}, stdout, io.Discard)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stdout.String(), "60.00  4/10 uncovered\n")
}

// -files draws the profile's files as well as its packages, one level below the package holding
// them. The fixture is m/a/a.go and m/b/b.go, so each package gains its file as a leaf.
func TestRunFilesFlag(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, profile)

	stdout := &bytes.Buffer{}
	code := app.Run([]string{"-profile", path, "-color", "never", "-files", "-depth", "max"}, stdout, io.Discard)

	require.Equal(t, codeOK, code)
	assert.Contains(t, stdout.String(), "a.go - 100.00")
	assert.Contains(t, stdout.String(), "b.go - 0.00")

	// Without it, the same tree stops at the packages.
	stdout.Reset()
	code = app.Run([]string{"-profile", path, "-color", "never", "-depth", "max"}, stdout, io.Discard)

	require.Equal(t, codeOK, code)
	assert.NotContains(t, stdout.String(), ".go")
}

func writeProfile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "coverage.out")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// -exclude changes the denominator, so it changes the number the gate reads. The fixture is 6 of
// 10 statements; dropping the uncovered package leaves a full house.
func TestRunExcludesPackages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantCode int
		want     string
		wantErr  string
	}{
		{
			name: "without it, the uncovered package counts",
			args: []string{"-fail-under", "100"}, wantCode: codeBelow, want: "60.00",
		},
		{
			name: "excluded, the same profile clears the gate",
			args: []string{"-exclude", "uncovered", "-fail-under", "100"}, wantCode: codeOK, want: "100.00",
		},
		{
			// A pattern that excludes the whole profile leaves nothing to average, which
			// checkThreshold already refuses rather than passing silently.
			// The gate says what it wanted; it cannot say what emptied the report, so the reason
			// is the same sentence either way. Being sent to check `go test -coverprofile` for a
			// report your own pattern emptied is what naming -exclude here prevents, and the
			// gated path used to do exactly that.
			name: "excluding everything cannot pass a gate",
			args: []string{"-exclude", "example.com", "-fail-under", "0"}, wantCode: codeBelow,
			wantErr: "-exclude left nothing to report, wanted at least 0.00%",
		},
		{
			// Without a gate nothing refuses it downstream, and an empty report exiting 0 is
			// the green no-op the empty-pattern guard exists to stop, by another spelling.
			name: "excluding everything without a gate is an error, not an empty report",
			args: []string{"-exclude", "example.com"}, wantCode: codeFailed,
			wantErr: "-exclude left nothing to report",
		},
		{
			name: "a pattern that does not compile is a flag error",
			args: []string{"-exclude", "("}, wantCode: codeFailed,
		},
		{
			// An unset make variable reaches the flag as "", which matches every file. Honouring
			// it would empty the report and exit 0, turning a coverage step into a green no-op.
			name: "the empty pattern is refused, not honoured",
			args: []string{"-exclude", ""}, wantCode: codeFailed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeProfile(t, "mode: set\n"+
				"example.com/p/covered/a.go:1.1,2.2 6 1\n"+
				"example.com/p/uncovered/b.go:1.1,2.2 4 0\n")

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(append(tc.args, "-profile", path, "-color", "never"), stdout, stderr)

			assert.Equal(t, tc.wantCode, code)

			if tc.want != "" {
				assert.Contains(t, stdout.String(), tc.want)
			}

			if tc.wantErr != "" {
				assert.Contains(t, stderr.String(), tc.wantErr)
			}
		})
	}
}

// The flag is repeatable and every pattern is kept. Assigning instead of appending made -exclude
// look like it worked and silently applied only the last one.
func TestRunAppliesEveryExcludePattern(t *testing.T) {
	t.Parallel()

	patterned := "mode: set\n" +
		"ex.com/p/cmd/web/main.go:1.1,2.2 10 0\n" +
		"ex.com/p/testutil/t.go:1.1,2.2 10 1\n" +
		"ex.com/p/web/handler.go:1.1,2.2 10 1\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-exclude", "cmd/", "-exclude", "testutil", "-exclude", "absent",
		"-profile", writeProfile(t, patterned), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stdout.String(), "100.00", "both excluded, only the covered handler is left")

	// Every pattern is accounted for, including the one that took nothing.
	assert.Contains(t, stderr.String(), `-exclude "cmd/" left out 10 statements in 1 file`)
	assert.Contains(t, stderr.String(), `-exclude "testutil" left out 10 statements in 1 file`)
	assert.Contains(t, stderr.String(), `-exclude "absent" matched nothing`)
}

// A pattern beaten to a file by an earlier one is not a typo, and must not be reported as one:
// the fix a reader would make is to delete a pattern that is doing its job.
func TestRunTellsOverlapApartFromNoMatch(t *testing.T) {
	t.Parallel()

	overlapping := "mode: set\n" +
		"ex.com/p/cmd/gen.pb.go:1.1,2.2 10 1\n" +
		"ex.com/p/web/handler.go:1.1,2.2 10 1\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-exclude", "cmd/", "-exclude", `\.pb\.go$`, "-exclude", "absent",
		"-profile", writeProfile(t, overlapping), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stderr.String(), `-exclude "cmd/" left out 10 statements in 1 file`)
	assert.Contains(t, stderr.String(), `took nothing out, 1 file already excluded`)
	assert.Contains(t, stderr.String(), `-exclude "absent" matched nothing`)
}

// A pattern that matched nothing is said and not refused, unlike a root that did. prettycov has no
// history, so it cannot tell a pattern that rotted from one written to be conditional — and "drop
// this if it is here" is a reasonable thing to write. A defensive `-exclude=\.pb\.go$`, or one
// config shared across repositories, is right to match nothing where nothing is generated. A root
// has no such case: it either names this profile's packages or the labels are wrong.
func TestRunSaysWhenAnExcludePatternMatchedNothing(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-exclude", "absent", "-depth", "0",
		"-profile", writeProfile(t, profile), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code, "a filter that had nothing to drop did its job")
	assert.Contains(t, stderr.String(), `-exclude "absent" matched nothing`)
	assert.Equal(t, " m - 60.00\n", stdout.String(), "and the report is drawn")
}

// A stale pattern beside a wrong root: the root decides the exit, the pattern is still reported.
func TestRunRefusesOnTheRootNotThePattern(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-exclude", "absent", "-old", "example.com/WRONG", "-new", "w",
		"-profile", writeProfile(t, profile), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), `-exclude "absent" matched nothing`)
	assert.Contains(t, stderr.String(), `-old "example.com/WRONG" matched nothing`)
	assert.Empty(t, stdout.String())
}

// A coordinate takes one block, and the report says "block" so a reader is not told a file went.
func TestRunExcludesOneBlockByCoordinate(t *testing.T) {
	t.Parallel()

	twoBlocks := "mode: set\n" +
		"ex.com/p/app/version.go:31.2,32.9 2 1\n" +
		"ex.com/p/app/version.go:32.9,34.3 1 0\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-exclude", `version\.go:32`, "-total",
		"-profile", writeProfile(t, twoBlocks), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stderr.String(), `-exclude "version\\.go:32" left out 1 statement in 1 block`)
	assert.Equal(t, "100.00\n", stdout.String(), "the uncovered block left the denominator with it")
}

// One pattern can reach both: `a\.go$` takes the file, `b\.go:32` takes a block of another, and
// neither count may be dropped from the message.
func TestRunReportsFilesAndBlocksTakenByOnePattern(t *testing.T) {
	t.Parallel()

	both := "mode: set\n" +
		"ex.com/p/app/a.go:1.1,2.2 4 1\n" +
		"ex.com/p/app/b.go:31.2,32.9 2 1\n" +
		"ex.com/p/app/b.go:32.9,34.3 1 0\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-exclude", `(a\.go$|b\.go:32:)`, "-total",
		"-profile", writeProfile(t, both), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stderr.String(), "left out 5 statements in 1 file and 1 block")
	assert.Equal(t, "100.00\n", stdout.String())
}

// A pattern can take something and still be covering for an earlier one. Saying only what it took
// reads as barely earning its keep, and deleting it hands back the files the earlier pattern is
// holding — the trap the overlap count exists to prevent, sprung on a pattern that did take
// something.
func TestRunReportsOverlapAlongsideWhatAPatternTook(t *testing.T) {
	t.Parallel()

	overlapping := "mode: set\n" +
		"ex.com/p/cmd/a.go:1.1,2.2 3 1\n" +
		"ex.com/p/cmd/b.go:1.1,2.2 4 1\n" +
		"ex.com/p/web/c.go:31.2,32.9 2 1\n" +
		"ex.com/p/web/c.go:32.9,34.3 2 0\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-exclude", "cmd/", "-exclude", `(cmd/|c\.go:32:)`, "-total",
		"-profile", writeProfile(t, overlapping), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stderr.String(),
		"left out 2 statements in 1 block, and 2 files already excluded")

	// And the pattern that overlapped nothing says nothing about it. The trailing newline is the
	// assertion: without it, Contains passes just as well when the clause is always appended.
	assert.Contains(t, stderr.String(), `-exclude "cmd/" left out 7 statements in 2 files`+"\n")

	assert.Equal(t, "100.00\n", stdout.String())
}

// A pattern that matched a file declaring no statements emptied nothing, so the reader is sent to
// the profile rather than to a pattern that is not the reason — the report was empty before it ran.
func TestRunBlamesTheProfileWhenAPatternTookNoStatements(t *testing.T) {
	t.Parallel()

	noStatements := "mode: set\nexample.com/p/doc.go:1.1,2.2 0 0\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-exclude", `doc\.go`, "-profile", writeProfile(t, noStatements), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), "no statements to cover")
	assert.NotContains(t, stderr.String(), "-exclude left nothing to report")
}

// A profile with nothing in it has nothing for a flag to match, so judging one against it reports
// every pattern and every root as stale — naming a good `-old=$(MODULE)` as the fault when the
// profile is what is empty, and exiting 2 where the gate says 1.
func TestRunDoesNotBlameFlagsForAnEmptyProfile(t *testing.T) {
	t.Parallel()

	// A file declaring no statements, not a bare header: the profile has to hold something for the
	// question "does anything here have statements" to be asked of it at all. With no files the
	// answer is no whichever way the test is written, and a guard that accepted a file of nothing
	// would go unnoticed.
	empty := "mode: set\nexample.com/p/doc.go:1.1,2.2 0 0\n"

	tests := map[string][]string{
		"a root the profile does not hold": {"-old", "example.com/WRONG", "-new", "w"},
		"a pattern that matches nothing":   {"-exclude", `pb\.go`},
		"no flags at all":                  nil,
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(append(args,
				"-fail-under", "80", "-profile", writeProfile(t, empty), "-color", "never",
			), stdout, stderr)

			assert.Equal(t, codeBelow, code, "an empty profile is a failed gate, not a bad invocation")
			assert.Equal(t, "no statements to cover, wanted at least 80.00%\n", stderr.String())
			assert.Empty(t, stdout.String())
		})
	}
}

// -old and -new are one rename between them. Alone, either silently did nothing: `-new=.` looks
// like it shortens every label, and an unset `-old=$(MODULE)` leaves the report full of paths its
// author believed were gone.
func TestRunRefusesHalfARename(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args []string
		want string
	}{
		// The whole message, not its shared prefix: naming which half was given is the only thing
		// `given` does, and a prefix assertion holds just as well when it names the wrong one.
		"old without new": {
			args: []string{"-old", "example.com/p"},
			want: `one alone does nothing: got -old="example.com/p"`,
		},
		"new without old": {
			args: []string{"-new", "p"},
			want: `one alone does nothing: got -new="p"`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(append(tc.args, "-profile", writeProfile(t, profile)), stdout, stderr)

			assert.Equal(t, codeFailed, code)
			assert.Empty(t, stdout.String(), "nothing on stdout for an argument error")
			assert.Contains(t, stderr.String(), tc.want)
		})
	}
}

// A different mistake, so a different sentence. -old=/ names no package, and both flags may well
// have been given — telling the reader "one alone does nothing" sends them to supply a flag they
// already supplied. `-old=$(MODULE)/` with MODULE unset spells this, and with both unset it is
// -old=/ with no -new at all.
func TestRunRefusesARootThatNamesNoPackage(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"one separator, with a target":    {"-old", "/", "-new", "x"},
		"several separators":              {"-old", "//", "-new", "x"},
		"one separator, without a target": {"-old", "/"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(append(args, "-profile", writeProfile(t, profile)), stdout, stderr)

			assert.Equal(t, codeFailed, code)
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), `-old names no package: got -old="`+args[1]+`"`)
		})
	}
}

// A rename that lands says nothing at all, and the label it lands on is the new root verbatim.
//
// "/" is one of those: only the old root is trimmed before the guard asks whether one was given,
// the new one being the replacement, used raw. It renders the tree under the filesystem root. A
// guard made symmetrical would refuse it, and until now nothing would have noticed.
func TestRunIsSilentWhenARootMatched(t *testing.T) {
	t.Parallel()

	tests := map[string]struct{ oldRoot, newRoot, want string }{
		"a package name":      {oldRoot: "m", newRoot: "renamed", want: "renamed"},
		"the filesystem root": {oldRoot: "m", newRoot: "/", want: "/"},
		// Both halves of the trimming meet here: the guard lets a root through on what is left
		// after every separator goes, so Shorten has to trim them all too or a root that is in
		// the profile silently matches nothing and gets reported as a root that is not.
		"a root written with a separator too many": {oldRoot: "m//", newRoot: "renamed", want: "renamed"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run([]string{
				"-old", tc.oldRoot, "-new", tc.newRoot, "-depth", "0",
				"-profile", writeProfile(t, profile), "-color", "never",
			}, stdout, stderr)

			assert.Equal(t, codeOK, code)
			assert.Equal(t, " "+tc.want+" - 60.00\n", stdout.String())
			assert.Empty(t, stderr.String())
		})
	}
}

// A root that names no package in the profile rewrites nothing, which looks exactly like asking
// for no rename at all. parseFlags catches a root that is empty; only the matching can catch one
// that is merely wrong, so it is refused a step later rather than differently.
func TestRunRefusesARootThatMatchedNothing(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-old", "example.com/WRONG", "-new", "w", "-depth", "0",
		"-profile", writeProfile(t, profile), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), `-old "example.com/WRONG" matched nothing`)
	assert.Empty(t, stdout.String(), "and no report goes out under the labels it was not given")
}

// Shorten runs on what -exclude left, so a pattern that took every file under a perfectly good
// root would report the root as wrong — sending someone to fix a flag that is already right. The
// question is asked of the whole profile instead.
func TestRunDoesNotBlameTheRootForWhatExcludeTook(t *testing.T) {
	t.Parallel()

	// A file outside the root, so taking everything under it still leaves a report standing. With a
	// profile entirely under m/ the run ends in "-exclude left nothing to report", and the silence
	// below holds for that reason rather than the one being tested — which is what it did.
	twoRoots := "mode: set\nm/a.go:1.1,2.2 5 1\nother/b.go:1.1,2.2 5 1\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-old", "m", "-new", "x", "-exclude", "^m/", "-depth", "0",
		"-profile", writeProfile(t, twoRoots), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code, "the root is fine, so the report is drawn")
	assert.Equal(t, " other - 100.00\n", stdout.String())
	assert.NotContains(t, stderr.String(), "matched nothing, so no label was shortened")
	assert.Contains(t, stderr.String(), `-exclude "^m/" left out`, "and -exclude still says what it took")
}

func TestRunPrintsOnlyTheTotal(t *testing.T) {
	t.Parallel()

	sixtyOfTen := "mode: set\n" +
		"example.com/p/covered/a.go:1.1,2.2 6 1\n" +
		"example.com/p/uncovered/b.go:1.1,2.2 4 0\n"

	tests := []struct {
		name     string
		args     []string
		wantCode int
		want     string
	}{
		{name: "the number alone, no label, no tree", args: nil, wantCode: codeOK, want: "60.00\n"},
		{
			// The same denominator the tree would use, so a summary cannot disagree with the report.
			name: "it follows -exclude", args: []string{"-exclude", "uncovered"},
			wantCode: codeOK, want: "100.00\n",
		},
		{
			name: "it still gates", args: []string{"-fail-under", "90"},
			wantCode: codeBelow, want: "60.00\n",
		},
		{
			// A suffix here would break every `$(shell prettycov -total)` there is.
			name: "it ignores -counts", args: []string{"-counts"},
			wantCode: codeOK, want: "60.00\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			// Appending onto a fresh literal, not onto tc.args: the table's slices are shared
			// across parallel subtests.
			args := append([]string{"-total", "-profile", writeProfile(t, sixtyOfTen), "-color", "never"},
				tc.args...)

			assert.Equal(t, tc.wantCode, app.Run(args, stdout, stderr))
			assert.Equal(t, tc.want, stdout.String())
		})
	}
}

// A `go test -coverprofile` that matched no packages produces this, and an empty report exiting 0
// would call it green. -fail-under grades it rather than reporting a tool that could not run.
func TestRunOnAProfileWithNothingToCover(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, "mode: set\nm/doc.go:1.1,2.2 0 0\n")

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{
			name:     "-total is refused outright",
			args:     []string{"-total"},
			wantCode: codeFailed,
			wantErr:  "no statements to cover",
		},
		{name: "the tree is refused too", wantCode: codeFailed, wantErr: "no statements to cover"},
		{
			name: "-fail-under reports it instead", args: []string{"-total", "-fail-under", "50"},
			wantCode: codeBelow, wantErr: "no statements to cover, wanted at least 50.00%",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := append([]string{"-profile", path, "-color", "never"}, tc.args...)

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			assert.Equal(t, tc.wantCode, app.Run(args, stdout, stderr))
			assert.Empty(t, stdout.String(), "nothing a script could mistake for a number")
			assert.Contains(t, stderr.String(), tc.wantErr)
		})
	}
}

// Settled by whether -exclude took the statements out, not by whether any file came through it.
func TestRunNamesExcludeAsTheReasonTheReportIsEmpty(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, "mode: set\n"+
		"m/a.go:1.1,2.2 10 1\n"+
		"m/doc.go:1.1,2.2 0 0\n")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	args := []string{"-exclude", `a\.go`, "-profile", path, "-color", "never"}

	assert.Equal(t, codeFailed, app.Run(args, stdout, stderr))
	assert.Contains(t, stderr.String(), "-exclude left nothing to report")
	assert.Empty(t, stdout.String())
}

// Rows walks the root's children, so when a profile spans two top-level paths the root itself is
// never drawn. -total reports that root, so its number appears in no row.
func TestTotalOverAProfileWithNoSingleRoot(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, "mode: set\n"+
		"example.com/p/a.go:1.1,2.2 2 1\n"+
		"other.com/q/b.go:1.1,2.2 1 0\n")

	total, tree := &bytes.Buffer{}, &bytes.Buffer{}
	require.Equal(t, codeOK, app.Run([]string{"-total", "-profile", path, "-color", "never"}, total, io.Discard))
	require.Equal(t, codeOK, app.Run([]string{"-profile", path, "-color", "never"}, tree, io.Discard))

	assert.Equal(t, "66.67\n", total.String(), "the union of both roots")
	assert.NotContains(t, tree.String(), "66.67", "which no row carries")
	assert.Contains(t, tree.String(), "example.com/p - 100.00")
	assert.Contains(t, tree.String(), "other.com/q - 0.00")
}

// The tree renders the same ratio, so the two must not round differently.
func TestTotalMatchesTheTreesOwnRendering(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, "mode: set\n"+
		"example.com/p/a.go:1.1,2.2 2 1\n"+
		"example.com/p/b.go:1.1,2.2 1 0\n")

	total, tree := &bytes.Buffer{}, &bytes.Buffer{}
	require.Equal(t, codeOK, app.Run([]string{"-total", "-profile", path, "-color", "never"}, total, io.Discard))
	require.Equal(t, codeOK, app.Run([]string{"-depth", "0", "-profile", path, "-color", "never"}, tree, io.Discard))

	assert.Equal(t, "66.67\n", total.String())
	assert.Contains(t, tree.String(), "66.67", "the tree reports the same figure")
}

// Setting -depth past the bottom of the tree is how the README told people to see all of it, and
// guessing that number is wrong both ways: too small truncates without saying so, too large is
// harmless but arbitrary. "max" is the only value that is right without knowing the answer first.
func TestRunAcceptsMaxDepth(t *testing.T) {
	t.Parallel()

	fourLevels := "mode: set\n" +
		"ex.com/p/a.go:1.1,2.2 1 1\n" +
		"ex.com/p/one/b.go:1.1,2.2 1 1\n" +
		"ex.com/p/one/two/c.go:1.1,2.2 1 1\n" +
		"ex.com/p/one/two/three/d.go:1.1,2.2 1 0\n"

	tests := []struct {
		name     string
		args     []string
		wantRows int
	}{
		{name: "the default is one level below the top row", wantRows: 2},
		{name: "max reaches the bottom", args: []string{"-depth", "max"}, wantRows: 4},
		{name: "a number past the bottom reaches it too", args: []string{"-depth", "9"}, wantRows: 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := append([]string{"-profile", writeProfile(t, fourLevels), "-color", "never"}, tc.args...)

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			require.Equal(t, codeOK, app.Run(args, stdout, stderr))
			assert.Len(t, strings.Split(strings.TrimSpace(stdout.String()), "\n"), tc.wantRows)
		})
	}
}

// One case per error, not the whole matrix: which strings ParseDepth refuses is its own business
// and TestParseDepthRejections owns it. What only this layer can show is that the refusal reaches
// stderr and exits 2 rather than rendering a silently different depth.
func TestRunRejectsABadDepth(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"deep":                 `want a number of levels, or "max"`,
		"99999999999999999999": `too many levels; use "max" for the whole tree`,
	}

	for arg, want := range tests {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run([]string{"-depth", arg, "-profile", "x.out"}, stdout, stderr)

			assert.Equal(t, codeFailed, code)
			assert.Contains(t, stderr.String(), want)
		})
	}
}

// clearColorEnv puts the environment in the state where only the destination decides. Its callers
// cannot be parallel: t.Setenv panics if the test or any parent has called t.Parallel.
func clearColorEnv(t *testing.T) {
	t.Helper()

	// Registers the restore, then clears it: NO_COLOR set to anything, empty included, means no.
	t.Setenv("NO_COLOR", "")
	require.NoError(t, os.Unsetenv("NO_COLOR"))
	t.Setenv("TERM", "xterm")
}

// A regular file is not a terminal, and a closed one answers no rather than panicking — its
// descriptor is -1 by then. Both are branches a bytes.Buffer never reaches, since it is not an
// *os.File at all. The closed one also pins the exit code: the printer discards write errors, so a
// report nobody could read is still exit 0. Deliberate for now, and this is where it is decided.
//
// The environment is cleared first, and nothing here is parallel because of it: palette asks about
// NO_COLOR and TERM before it looks at the descriptor, so a runner with NO_COLOR exported would
// pass these at the first guard and never test what they are named for.
//
//nolint:paralleltest // t.Setenv, through clearColorEnv, panics under t.Parallel.
func TestRunAutoColorAgainstRealFiles(t *testing.T) {
	clearColorEnv(t)

	path := writeProfile(t, profile)

	t.Run("a regular file gets no colour", func(t *testing.T) {
		out, err := os.Create(filepath.Join(t.TempDir(), "report.txt"))
		require.NoError(t, err)

		require.Equal(t, codeOK, app.Run([]string{path}, out, io.Discard))
		require.NoError(t, out.Close())

		written, err := os.ReadFile(out.Name())
		require.NoError(t, err)

		assert.NotEmpty(t, written)
		assert.NotContains(t, string(written), "\x1b[")
	})

	t.Run("a closed file is not a panic", func(t *testing.T) {
		closed, err := os.CreateTemp(t.TempDir(), "closed")
		require.NoError(t, err)
		require.NoError(t, closed.Close())

		assert.Equal(t, codeOK, app.Run([]string{path}, closed, io.Discard))
	})
}

// -hide-covered shapes the report and never the measurement: -total and -fail-under read the same
// with it as without, which is what separates it from -exclude.
func TestRunHideCovered(t *testing.T) {
	t.Parallel()

	// done/ is finished, work/ is not, and work/deep sits below a parent that is itself above 90.
	shaped := "mode: set\n" +
		"m/done/a.go:1.1,2.2 4 1\n" +
		"m/work/c.go:1.1,2.2 9 1\n" +
		"m/work/deep/d.go:1.1,2.2 1 0\n"

	tests := map[string]struct {
		args []string
		want string
	}{
		"unset":     {args: nil, want: " m - 92.86\n ├ done - 100.00\n └ work - 90.00\n   └ deep - 0.00\n"},
		"bare":      {args: []string{"-hide-covered"}, want: " m - 92.86\n └ work - 90.00\n   └ deep - 0.00\n"},
		"threshold": {args: []string{"-hide-covered=90"}, want: " m - 92.86\n └ work - 90.00\n   └ deep - 0.00\n"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := writeProfile(t, shaped)

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(append(tc.args, "-depth", "max", "-profile", path, "-color", "never"), stdout, stderr)

			assert.Equal(t, codeOK, code)
			assert.Equal(t, tc.want, stdout.String())
			assert.Empty(t, stderr.String(), "a display flag says nothing, as -depth does not")

			// The measurement is untouched whatever was drawn.
			total, gate := &bytes.Buffer{}, &bytes.Buffer{}
			require.Equal(t, codeOK, app.Run(append(tc.args,
				"-total", "-profile", path, "-color", "never"), total, gate))
			assert.Equal(t, "92.86\n", total.String())
		})
	}
}

// The threshold can take the whole report. The exit code is unchanged, but a command that prints
// nothing reads as one that failed, so it says which flag emptied it.
func TestRunSaysWhenHideCoveredTookEverything(t *testing.T) {
	t.Parallel()

	// Every package above the bar, or there is something left to draw and the report is not empty.
	allAbove := "mode: set\nm/a/a.go:1.1,2.2 9 1\nm/b/b.go:1.1,2.2 1 1\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"-hide-covered=40", "-profile", writeProfile(t, allAbove), "-color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "-hide-covered=40 hid the whole report; nothing is below it")
}

func TestRunRefusesABadHideCovered(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{
		"over a hundred": "101", "negative": "-5", "not a number": "abc", "NaN": "nan",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run([]string{
				"-hide-covered=" + value, "-profile", writeProfile(t, profile), "-color", "never",
			}, stdout, stderr)

			assert.Equal(t, codeFailed, code)
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "want a percentage from 0 to 100")
		})
	}
}

// The ends of the range are thresholds, not errors: 100 is the bare form written out, and 0 asks
// for everything with a percentage to go. Nothing pinned either, so tightening a bound to < or >
// would have gone unnoticed.
func TestRunAcceptsTheEndsOfTheHideCoveredRange(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"0", "100"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run([]string{
				"-hide-covered=" + value, "-profile", writeProfile(t, profile), "-color", "never",
			}, stdout, stderr)

			assert.Equal(t, codeOK, code)
			assert.NotContains(t, stderr.String(), "want a percentage")
		})
	}
}
