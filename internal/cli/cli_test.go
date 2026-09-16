package cli_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov/internal/app"
	"github.com/screwyprof/prettycov/internal/cli"
)

// Literal, not app's own constants: a script sees these numbers, so renumbering one has to fail.
const (
	codeOK     = 0
	codeBelow  = 1
	codeFailed = 2
)

// 6 of 10 statements covered, so the report reads 60.00 and a --fail-under above that fails. In the
// repo's testdata because cmd/prettycov's integration test needs the same bytes, and go:embed cannot
// reach out of its own directory — which is what kept two copies of this in step by hand.
//
//nolint:gochecknoglobals // one read for every test in the file, as the embed it replaces was.
var profile = string(fixture("sixty-percent.out"))

// fixture reads a shared profile. Panics: a missing one is a broken checkout, not a test failure.
func fixture(name string) []byte {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		panic(err)
	}

	return data
}

func TestRunRendersTheReport(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, profile)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"report", "--profile", path, "--color", "never"}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stdout.String(), "60.00")
	assert.Empty(t, stderr.String())
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

	// The command and nothing else: `prettycov report` in a repo that has just been tested. Colour
	// is off anyway, since a buffer is not a terminal.
	code := app.Run([]string{"report"}, stdout, stderr)

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
	code := app.Run([]string{"report", "--profile", "sub/cov.out", "--color", "never"}, stdout, stderr)

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
			args: []string{"report", "--fail-under", "80"}, wantCode: codeBelow,
			wantErr: "total coverage 60.00% is below 80.00%",
		},
		{
			name: "at the threshold passes", profile: profile,
			args: []string{"report", "--fail-under", "60"}, wantCode: codeOK,
		},
		{
			name: "unset means no gate", profile: profile,
			args: []string{"report"}, wantCode: codeOK,
		},
		{
			// Silently passing here would make the gate useless on an empty or mis-pointed profile.
			name: "nothing to cover cannot clear a threshold", profile: "mode: atomic\nm/d/doc.go:1.1,2.2 0 0\n",
			args: []string{"report", "--fail-under", "1"}, wantCode: codeBelow,
			wantErr: "no statements to cover",
		},
		{
			// Zero is a real threshold: it asks only that the profile hold some statements. It
			// must not silently mean "no gate", which is what a zero default would make it.
			name: "zero still requires something to measure", profile: "mode: atomic\nm/d/doc.go:1.1,2.2 0 0\n",
			args: []string{"report", "--fail-under", "0"}, wantCode: codeBelow,
			wantErr: "no statements to cover",
		},
		{
			name: "zero passes when there is anything at all", profile: profile,
			args: []string{"report", "--fail-under", "0"}, wantCode: codeOK,
		},
		{
			name: "not a number", profile: profile,
			args: []string{"report", "--fail-under", "abc"}, wantCode: codeFailed,
			// Kong reads the type before this package grades the value, so a word that is not a
			// number is its sentence, not ours.
			wantErr: "want a percentage from 0 to 100",
		},
		{
			// The dangerous one: `total < NaN` is false, so this used to clear the gate at any
			// coverage and print nothing. A CI config templating a bad value gets a diagnostic.
			name: "NaN", profile: profile,
			args: []string{"report", "--fail-under", "nan"}, wantCode: codeFailed,
			wantErr: "want a percentage",
		},
		{
			name: "negative", profile: profile,
			args: []string{"report", "--fail-under=-5"}, wantCode: codeFailed,
			wantErr: "want a percentage",
		},
		{
			// Unreachable rather than merely strict: nothing can cover 150% of its statements.
			name: "above 100", profile: profile,
			args: []string{"report", "--fail-under", "150"}, wantCode: codeFailed,
			wantErr: "want a percentage",
		},
		{
			// The boundary itself is allowed: "everything must be covered" is a real thing to ask.
			// Nothing pinned this, so tightening the check to >= would have gone unnoticed.
			name: "exactly 100 is a threshold, not an error", profile: profile,
			args: []string{"report", "--fail-under", "100"}, wantCode: codeBelow,
			wantErr: "is below 100.00%",
		},
		{
			// Counts this large only come from a hand-written profile, and summing them wraps.
			// Rejecting the profile beats reporting a percentage derived from the wreckage.
			name: "counts that overflow past zero",
			profile: "mode: set\nm/a.go:1.1,2.2 4611686018427387904 1\n" +
				"m/b.go:3.1,4.2 9223372036854775807 0\nm/c.go:5.1,6.2 9223372036854775807 0\n",
			args: []string{"report", "--fail-under", "80"}, wantCode: codeFailed,
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
			args: []string{"report", "--fail-under", "30"}, wantCode: codeFailed,
			wantErr: "statement counts overflow",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			args := slices.Concat(tc.args, []string{"--profile", writeProfile(t, tc.profile), "--color", "never"})

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
			code := app.Run(
				[]string{"report", "--profile", writeProfile(t, profile), "--color", tc.value},
				stdout,
				stderr,
			)

			require.Equal(t, tc.wantCode, code)

			if tc.wantCode != codeOK {
				assert.Contains(t, stderr.String(), `--color: want "auto", "never" or "always"`)

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

// Someone running this for the first time in a repo with no profile is one command away, so say
// which rather than leaving them a bare file-not-found.
func TestRunReportsAMissingProfileWithTheCommandThatMakesOne(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"report", "--profile", filepath.Join(t.TempDir(), "absent.out")}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), "cannot read coverage profile")
	assert.Contains(t, stderr.String(), "go test -coverprofile=")
	assert.Empty(t, stdout.String())
}

// A malformed profile is not a missing one, so it must not suggest re-running the tests.
func TestRunReportsAMalformedProfileWithoutTheHint(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"report", "--profile", writeProfile(t, "not a profile\n")}, stdout, stderr)

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
		{name: "flag", args: []string{"--help"}},
		{name: "shorthand", args: []string{"-h"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			assert.Equal(t, codeOK, app.Run(tc.args, stdout, stderr))
			assert.Contains(t, stdout.String(), "Usage: prettycov <command>")
			assert.Empty(t, stderr.String())

			// The root lists what the root takes, which is every command and nothing else. A flag
			// belongs to the command that reads it, so listing --profile here would be a claim
			// that `prettycov --profile x` means something.
			for _, command := range []string{"report", "misses", "total", "version"} {
				assert.Contains(t, stdout.String(), command)
			}

			for _, flag := range []string{"--old", "--new", "--color", "--fail-under", "--profile"} {
				assert.NotContains(t, stdout.String(), flag)
			}
		})
	}
}

// One typo used to print the message, then the whole usage, then the message again: 33 lines.
func TestRunKeepsABadFlagShort(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"report", "-nope"}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), "unknown flag")
	assert.Contains(t, stderr.String(), `run "prettycov --help" for usage`)
	assert.NotContains(t, stderr.String(), "Prettycov:", "the usage text belongs behind --help")
	assert.LessOrEqual(t, strings.Count(stderr.String(), "\n"), 3)
}

// `version` as a bare word is recognised before the flag package sees it, which would take it for
// a profile path; --version is the ordinary flag. What the version actually says is the binary
// tests' business — it depends on how the binary was linked, and nix stamps it during checkPhase.
func TestRunAcceptsBothVersionSpellings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "subcommand", args: []string{"version"}},
		{name: "flag", args: []string{"report", "--version"}},
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
	code := app.Run(
		[]string{"report", "--profile", writeProfile(t, profile), "--color", "never", "--counts"},
		stdout,
		io.Discard,
	)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stdout.String(), "60.00  4/10 uncovered\n")
}

// --files draws the profile's files as well as its packages, one level below the package holding
// them. The fixture is m/a/a.go and m/b/b.go, so each package gains its file as a leaf.
func TestRunFilesFlag(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, profile)

	stdout := &bytes.Buffer{}
	code := app.Run(
		[]string{"report", "--profile", path, "--color", "never", "--files", "--depth", "max"},
		stdout,
		io.Discard,
	)

	require.Equal(t, codeOK, code)
	assert.Contains(t, stdout.String(), "a.go - 100.00")
	assert.Contains(t, stdout.String(), "b.go - 0.00")

	// Without it, the same tree stops at the packages.
	stdout.Reset()
	code = app.Run([]string{"report", "--profile", path, "--color", "never", "--depth", "max"}, stdout, io.Discard)

	require.Equal(t, codeOK, code)
	assert.NotContains(t, stdout.String(), ".go")
}

func writeProfile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "coverage.out")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// --exclude changes the denominator, so it changes the number the gate reads. The fixture is 6 of
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
			args: []string{"report", "--fail-under", "100"}, wantCode: codeBelow, want: "60.00",
		},
		{
			name: "excluded, the same profile clears the gate",
			args: []string{"report", "--exclude", "uncovered", "--fail-under", "100"}, wantCode: codeOK, want: "100.00",
		},
		{
			// A pattern that excludes the whole profile leaves nothing to average, which
			// checkThreshold already refuses rather than passing silently.
			// The gate says what it wanted; it cannot say what emptied the report, so the reason
			// is the same sentence either way. Being sent to check `go test -coverprofile` for a
			// report your own pattern emptied is what naming --exclude here prevents, and the
			// gated path used to do exactly that.
			name: "excluding everything cannot pass a gate",
			args: []string{"report", "--exclude", "example.com", "--fail-under", "0"}, wantCode: codeBelow,
			wantErr: "--exclude left nothing to report, wanted at least 0.00%",
		},
		{
			// Without a gate nothing refuses it downstream, and an empty report exiting 0 is
			// the green no-op the empty-pattern guard exists to stop, by another spelling.
			name: "excluding everything without a gate is an error, not an empty report",
			args: []string{"report", "--exclude", "example.com"}, wantCode: codeFailed,
			wantErr: "--exclude left nothing to report",
		},
		{
			name: "a pattern that does not compile is a flag error",
			args: []string{"report", "--exclude", "("}, wantCode: codeFailed,
		},
		{
			// An unset make variable reaches the flag as "", which matches every file. Honouring
			// it would empty the report and exit 0, turning a coverage step into a green no-op.
			name: "the empty pattern is refused, not honoured",
			args: []string{"report", "--exclude", ""}, wantCode: codeFailed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeProfile(t, "mode: set\n"+
				"example.com/p/covered/a.go:1.1,2.2 6 1\n"+
				"example.com/p/uncovered/b.go:1.1,2.2 4 0\n")

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(append(tc.args, "--profile", path, "--color", "never"), stdout, stderr)

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

// The flag is repeatable and every pattern is kept. Assigning instead of appending made --exclude
// look like it worked and silently applied only the last one.
func TestRunAppliesEveryExcludePattern(t *testing.T) {
	t.Parallel()

	patterned := "mode: set\n" +
		"ex.com/p/cmd/web/main.go:1.1,2.2 10 0\n" +
		"ex.com/p/testutil/t.go:1.1,2.2 10 1\n" +
		"ex.com/p/web/handler.go:1.1,2.2 10 1\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"report", "--exclude", "cmd/", "--exclude", "testutil", "--exclude", "absent",
		"--profile", writeProfile(t, patterned), "--color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stdout.String(), "100.00", "both excluded, only the covered handler is left")

	// Every pattern is accounted for, including the one that took nothing.
	assert.Contains(t, stderr.String(), `--exclude "cmd/" left out 10 statements in 1 file`)
	assert.Contains(t, stderr.String(), `--exclude "testutil" left out 10 statements in 1 file`)
	assert.Contains(t, stderr.String(), `--exclude "absent" matched nothing`)
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
		"report", "--exclude", "cmd/", "--exclude", `\.pb\.go$`, "--exclude", "absent",
		"--profile", writeProfile(t, overlapping), "--color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stderr.String(), `--exclude "cmd/" left out 10 statements in 1 file`)
	assert.Contains(t, stderr.String(), `took nothing out, 1 file already excluded`)
	assert.Contains(t, stderr.String(), `--exclude "absent" matched nothing`)
}

// A pattern that matched nothing is said and not refused, unlike a root that did. prettycov has no
// history, so it cannot tell a pattern that rotted from one written to be conditional — and "drop
// this if it is here" is a reasonable thing to write. A defensive `--exclude=\.pb\.go$`, or one
// config shared across repositories, is right to match nothing where nothing is generated. A root
// has no such case: it either names this profile's packages or the labels are wrong.
func TestRunSaysWhenAnExcludePatternMatchedNothing(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"report", "--exclude", "absent", "--depth", "0",
		"--profile", writeProfile(t, profile), "--color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code, "a filter that had nothing to drop did its job")
	assert.Contains(t, stderr.String(), `--exclude "absent" matched nothing`)
	assert.Equal(t, " m - 60.00\n", stdout.String(), "and the report is drawn")
}

// A stale pattern beside a wrong root: the root decides the exit, the pattern is still reported.
func TestRunRefusesOnTheRootNotThePattern(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"report", "--exclude", "absent", "--old", "example.com/WRONG", "--new", "w",
		"--profile", writeProfile(t, profile), "--color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), `--exclude "absent" matched nothing`)
	assert.Contains(t, stderr.String(), `--old "example.com/WRONG" matched nothing`)
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
		"total", "--exclude", `version\.go:32`,
		"--profile", writeProfile(t, twoBlocks),
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stderr.String(), `--exclude "version\\.go:32" left out 1 statement in 1 block`)
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
		"total", "--exclude", `(a\.go$|b\.go:32:)`,
		"--profile", writeProfile(t, both),
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
		"total", "--exclude", "cmd/", "--exclude", `(cmd/|c\.go:32:)`,
		"--profile", writeProfile(t, overlapping),
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Contains(t, stderr.String(),
		"left out 2 statements in 1 block, and 2 files already excluded")

	// And the pattern that overlapped nothing says nothing about it. The trailing newline is the
	// assertion: without it, Contains passes just as well when the clause is always appended.
	assert.Contains(t, stderr.String(), `--exclude "cmd/" left out 7 statements in 2 files`+"\n")

	assert.Equal(t, "100.00\n", stdout.String())
}

// A pattern that matched a file declaring no statements emptied nothing, so the reader is sent to
// the profile rather than to a pattern that is not the reason — the report was empty before it ran.
func TestRunBlamesTheProfileWhenAPatternTookNoStatements(t *testing.T) {
	t.Parallel()

	noStatements := "mode: set\nexample.com/p/doc.go:1.1,2.2 0 0\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run(
		[]string{"report", "--exclude", `doc\.go`, "--profile", writeProfile(t, noStatements), "--color", "never"},
		stdout,
		stderr,
	)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), "no statements to cover")
	assert.NotContains(t, stderr.String(), "--exclude left nothing to report")
}

// A profile with nothing in it has nothing for a flag to match, so judging one against it reports
// every pattern and every root as stale — naming a good `--old=$(MODULE)` as the fault when the
// profile is what is empty, and exiting 2 where the gate says 1.
func TestRunDoesNotBlameFlagsForAnEmptyProfile(t *testing.T) {
	t.Parallel()

	// A file declaring no statements, not a bare header: the profile has to hold something for the
	// question "does anything here have statements" to be asked of it at all. With no files the
	// answer is no whichever way the test is written, and a guard that accepted a file of nothing
	// would go unnoticed.
	empty := "mode: set\nexample.com/p/doc.go:1.1,2.2 0 0\n"

	tests := map[string][]string{
		"a root the profile does not hold": {"report", "--old", "example.com/WRONG", "--new", "w"},
		"a pattern that matches nothing":   {"report", "--exclude", `pb\.go`},
		"no flags at all":                  {"report"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(slices.Concat(args, []string{
				"--fail-under", "80", "--profile", writeProfile(t, empty), "--color", "never",
			}), stdout, stderr)

			assert.Equal(t, codeBelow, code, "an empty profile is a failed gate, not a bad invocation")
			assert.Equal(t, "no statements to cover, wanted at least 80.00%\n", stderr.String())
			assert.Empty(t, stdout.String())
		})
	}
}

// --old and --new are one rename between them. Alone, either silently did nothing: `--new=.` looks
// like it shortens every label, and an unset `--old=$(MODULE)` leaves the report full of paths its
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
			args: []string{"report", "--old", "example.com/p"},
			want: `one alone does nothing: got --old="example.com/p"`,
		},
		"new without old": {
			args: []string{"report", "--new", "p"},
			want: `one alone does nothing: got --new="p"`,
		},
		// Both flags given, one of them empty, which is what a Makefile writing --old=$(MODULE)
		// spells when MODULE is unset. Kong's `and:"rename"` group passed these — it is satisfied
		// once both flags appear, whatever they hold — and the report came back unrenamed, exit 0,
		// with nothing on stderr. The rule is about the values, so it cannot live in a tag.
		"old empty, new given": {
			args: []string{"report", "--old", "", "--new", "."},
			want: `one alone does nothing: got --new="."`,
		},
		"old given, new empty": {
			args: []string{"report", "--old", "example.com/p", "--new", ""},
			want: `one alone does nothing: got --old="example.com/p"`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(append(tc.args, "--profile", writeProfile(t, profile)), stdout, stderr)

			assert.Equal(t, codeFailed, code)
			assert.Empty(t, stdout.String(), "nothing on stdout for an argument error")
			assert.Contains(t, stderr.String(), tc.want)
		})
	}
}

// A different mistake, so a different sentence. --old=/ names no package, and both flags may well
// have been given — telling the reader "--old and --new must be used together" sends them to supply a flag they
// already supplied. `--old=$(MODULE)/` with MODULE unset spells this, and with both unset it is
// --old=/ with no --new at all.
func TestRunRefusesARootThatNamesNoPackage(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"one separator, with a target":    {"report", "--old", "/", "--new", "x"},
		"several separators":              {"report", "--old", "//", "--new", "x"},
		"one separator, without a target": {"report", "--old", "/"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(slices.Concat(args, []string{"--profile", writeProfile(t, profile)}), stdout, stderr)

			assert.Equal(t, codeFailed, code)
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), `--old names no package: got --old="`+args[2]+`"`)
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
				"report", "--old", tc.oldRoot, "--new", tc.newRoot, "--depth", "0",
				"--profile", writeProfile(t, profile), "--color", "never",
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
		"report", "--old", "example.com/WRONG", "--new", "w", "--depth", "0",
		"--profile", writeProfile(t, profile), "--color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeFailed, code)
	assert.Contains(t, stderr.String(), `--old "example.com/WRONG" matched nothing`)
	assert.Empty(t, stdout.String(), "and no report goes out under the labels it was not given")
}

// Shorten runs on what --exclude left, so a pattern that took every file under a perfectly good
// root would report the root as wrong — sending someone to fix a flag that is already right. The
// question is asked of the whole profile instead.
func TestRunDoesNotBlameTheRootForWhatExcludeTook(t *testing.T) {
	t.Parallel()

	// A file outside the root, so taking everything under it still leaves a report standing. With a
	// profile entirely under m/ the run ends in "--exclude left nothing to report", and the silence
	// below holds for that reason rather than the one being tested — which is what it did.
	twoRoots := "mode: set\nm/a.go:1.1,2.2 5 1\nother/b.go:1.1,2.2 5 1\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"report", "--old", "m", "--new", "x", "--exclude", "^m/", "--depth", "0",
		"--profile", writeProfile(t, twoRoots), "--color", "never",
	}, stdout, stderr)

	assert.Equal(t, codeOK, code, "the root is fine, so the report is drawn")
	assert.Equal(t, " other - 100.00\n", stdout.String())
	assert.NotContains(t, stderr.String(), "matched nothing, so no label was shortened")
	assert.Contains(t, stderr.String(), `--exclude "^m/" left out`, "and --exclude still says what it took")
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
			name: "it follows --exclude", args: []string{"--exclude", "uncovered"},
			wantCode: codeOK, want: "100.00\n",
		},
		{
			name: "it still gates", args: []string{"--fail-under", "90"},
			wantCode: codeBelow, want: "60.00\n",
		},
		{
			// A suffix here would break every `$(shell prettycov total)` there is, so the flag that
			// would add one is not registered on this command at all.
			name: "it does not take --counts", args: []string{"--counts"},
			wantCode: codeFailed, want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			// Appending onto a fresh literal, not onto tc.args: the table's slices are shared
			// across parallel subtests.
			args := append([]string{"total", "--profile", writeProfile(t, sixtyOfTen)},
				tc.args...)

			assert.Equal(t, tc.wantCode, app.Run(args, stdout, stderr))
			assert.Equal(t, tc.want, stdout.String())
		})
	}
}

// totalShaped is a tree whose every node reports a different number, so a lookup that returned the
// wrong one cannot pass. pkg holds a file of its own beside a subpackage, so it is not its child;
// web holds two files, so it is not either of them — which is what makes the file cases prove that
// the last segment resolves to a file and not to the directory above it.
//
//	m                          74.00
//	m/pkg                      73.33   m/pkg/util.go               40.00
//	m/pkg/logger               90.00   m/pkg/logger/logger.go      80.00
//	                                   m/pkg/logger/middleware.go 100.00
//	m/web                      75.00   m/web/handler.go            50.00
//	                                   m/web/router.go            100.00
const totalShaped = "mode: set\n" +
	"m/pkg/util.go:1.1,2.2 4 1\n" +
	"m/pkg/util.go:3.1,4.2 6 0\n" +
	"m/pkg/logger/logger.go:1.1,2.2 8 1\n" +
	"m/pkg/logger/logger.go:3.1,4.2 2 0\n" +
	"m/pkg/logger/middleware.go:1.1,2.2 10 1\n" +
	"m/web/handler.go:1.1,2.2 5 1\n" +
	"m/web/handler.go:3.1,4.2 5 0\n" +
	"m/web/router.go:1.1,2.2 10 1\n"

// `total <path>` reports one node of the tree, which is the number the report already draws and
// nothing could hand back. The paths carry the module root because this fixture does not rename
// one; after --old and --new they would be spelled the way the report prints them, which is the point
// of looking up the built tree rather than matching the profile.
func TestRunTotalOfOnePath(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args []string
		want string
	}{
		"bare is the whole tree": {args: []string{"total"}, want: "74.00\n"},
		"the root by name":       {args: []string{"total", "m"}, want: "74.00\n"},
		// Not 90.00: pkg holds util.go as well as the logger package.
		"a package":     {args: []string{"total", "m/pkg"}, want: "73.33\n"},
		"a deeper one":  {args: []string{"total", "m/pkg/logger"}, want: "90.00\n"},
		"its own file":  {args: []string{"total", "m/pkg/util.go"}, want: "40.00\n"},
		"a file":        {args: []string{"total", "m/pkg/logger/logger.go"}, want: "80.00\n"},
		"a second file": {args: []string{"total", "m/pkg/logger/middleware.go"}, want: "100.00\n"},
		// 75.00 is web's; 50.00 is the file's. The last segment has to resolve to the file.
		"a package of two files":  {args: []string{"total", "m/web"}, want: "75.00\n"},
		"and one of them by name": {args: []string{"total", "m/web/handler.go"}, want: "50.00\n"},
		"and the other":           {args: []string{"total", "m/web/router.go"}, want: "100.00\n"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			args := slices.Concat(tc.args, []string{"--profile", writeProfile(t, totalShaped)})

			assert.Equal(t, codeOK, app.Run(args, stdout, stderr), stderr.String())
			assert.Equal(t, tc.want, stdout.String())
			assert.Empty(t, stderr.String())
		})
	}
}

// The gate grades the node that was printed. Two lookups would be two chances to disagree, and this
// tool has already shipped a --fail-under that passed a run whose own report read 99.99.
func TestRunTotalOfOnePathIsWhatFailUnderGrades(t *testing.T) {
	t.Parallel()

	// The tree is at 74.00 and web/handler.go at 50.00, so a threshold between them says which.
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"total", "m/web/handler.go", "--fail-under", "70",
		"--profile", writeProfile(t, totalShaped),
	}, stdout, stderr)

	assert.Equal(t, codeBelow, code, "the file is 50.00, under the bar; the tree at 74.00 is not")
	assert.Equal(t, "50.00\n", stdout.String(), "and the number printed is the one graded")
	assert.Contains(t, stderr.String(), "total coverage 50.00% is below 70.00%")
}

// A path the profile does not hold is an argument mistake, not a coverage of zero: printing 0.00
// into `COVERAGE := $(shell ...)` would read as a real and terrible number. Which paths miss is
// TestPathTreeGetMissesAreNil's; this is the exit code and the message.
func TestRunTotalRefusesAPathTheTreeDoesNotHold(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run(
		[]string{"total", "m/nope", "--profile", writeProfile(t, totalShaped)},
		stdout,
		stderr,
	)

	assert.Equal(t, codeFailed, code)
	assert.Empty(t, stdout.String(), "nothing a script could mistake for a percentage")
	assert.Equal(t, "total: no such package or file in the profile: \"m/nope\"\n", stderr.String())
}

// An empty path is refused rather than read as the whole tree, which is what `total "$PKG"` spells
// when PKG is unset or misspelled — and the whole tree passing a gate the package would have failed
// is the one way this command can be silently wrong in CI.
//
// The bar is set below the tree's own coverage so that reading it as the whole tree would exit 0.
// Asserting only the exit code would hold either way.
func TestRunTotalRefusesAnEmptyPath(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run(
		[]string{"total", "", "--fail-under", "1", "--profile", writeProfile(t, totalShaped)},
		stdout,
		stderr,
	)

	assert.Equal(t, codeFailed, code)
	assert.Empty(t, stdout.String(), "nothing a script could mistake for a percentage")
	assert.Contains(t, stderr.String(), "want a path, or total on its own for the whole tree")
}

// The argument being optional is what makes the case above possible to get wrong: `total` and
// `total ""` are one string apart, and only a pointer tells them apart at all.
func TestRunTotalWithNoPathIsTheWholeTree(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"total", "--profile", writeProfile(t, totalShaped)}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Empty(t, stderr.String())
	assert.NotEmpty(t, stdout.String(), "the whole tree still has a percentage")
}

// A row is drawn with its own segment, so reading `pkg - 96.41` off a report and asking for "pkg"
// is the obvious next thing to type and the wrong one — the tree holds it under the whole module
// path. It resolves rather than refusing, which is safe because the prefixed path is checked against
// the tree before it is used and the literal spelling is tried first.
func TestRunTotalResolvesThePathUnderTheRoot(t *testing.T) {
	t.Parallel()

	// The root is a run of single-child directories, as a module path is, so the suggestion has to
	// descend it rather than look one level down.
	const shaped = "mode: set\n" +
		"example.com/m/pkg/a.go:1.1,2.2 1 1\n" +
		"example.com/m/web/b.go:1.1,2.2 1 1\n"

	tests := map[string]struct{ want, total string }{
		// Resolved, not suggested: a path read off a row is missing the root the report collapsed
		// away, and putting it back is something the tree can do rather than ask about.
		"a row's own label": {want: "pkg", total: "100.00\n"},
		"a deeper path":     {want: "web/b.go", total: "100.00\n"},
		// Still there under its own spelling, which is what makes the prefixing safe: Get is asked
		// first, so nothing that resolves literally is ever rewritten.
		"the path the profile holds": {want: "example.com/m/pkg", total: "100.00\n"},
		// The root is real but the leaf is not, so there is nothing to resolve to.
		"nothing like it":        {want: "nope"},
		"right root, wrong leaf": {want: "pkg/nope"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(
				[]string{"total", tc.want, "--profile", writeProfile(t, shaped)},
				stdout,
				stderr,
			)

			if tc.total == "" {
				assert.Equal(t, codeFailed, code)
				assert.Empty(t, stdout.String())
				assert.Equal(t,
					"total: no such package or file in the profile: "+strconv.Quote(tc.want)+"\n",
					stderr.String())

				return
			}

			assert.Equal(t, codeOK, code, stderr.String())
			assert.Equal(t, tc.total, stdout.String())
			assert.Empty(t, stderr.String())
		})
	}
}

// A node whose files declare no statements has no percentage, which the whole tree cannot be by here
// but one node can. Graded rather than refused outright, exactly as an empty profile is: exit 2
// would read as "prettycov could not run" where the truth is that there was nothing to cover.
func TestRunTotalRefusesANodeWithNothingToCover(t *testing.T) {
	t.Parallel()

	const shaped = "mode: set\nm/pkg/a.go:1.1,2.2 1 1\nm/doc/doc.go:1.1,2.2 0 0\n"

	tests := map[string]struct {
		args     []string
		wantCode int
		wantErr  string
	}{
		"without a gate": {
			args: []string{"total", "m/doc"}, wantCode: codeFailed,
			wantErr: "total names nothing with statements to cover: \"m/doc\"\n",
		},
		// The same sentence a tree with nothing to cover gets, and the same exit code.
		"with one": {
			args: []string{"total", "m/doc", "--fail-under", "80"}, wantCode: codeBelow,
			wantErr: "total names nothing with statements to cover: \"m/doc\", wanted at least 80.00%\n",
		},
		// A file can be empty too, and calling it a package would be wrong.
		"a file": {
			args: []string{"total", "m/doc/doc.go"}, wantCode: codeFailed,
			wantErr: "total names nothing with statements to cover: \"m/doc/doc.go\"\n",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			args := slices.Concat(tc.args, []string{"--profile", writeProfile(t, shaped)})

			assert.Equal(t, tc.wantCode, app.Run(args, stdout, stderr))
			assert.Empty(t, stdout.String())
			assert.Equal(t, tc.wantErr, stderr.String())
		})
	}
}

// A `go test -coverprofile` that matched no packages produces this, and an empty report exiting 0
// would call it green. --fail-under grades it rather than reporting a tool that could not run.
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
			name:     "total is refused outright",
			args:     []string{"total"},
			wantCode: codeFailed,
			wantErr:  "no statements to cover",
		},
		{
			name:     "the report is refused too",
			args:     []string{"report"},
			wantCode: codeFailed,
			wantErr:  "no statements to cover",
		},
		{
			name: "--fail-under reports it instead", args: []string{"total", "--fail-under", "50"},
			wantCode: codeBelow, wantErr: "no statements to cover, wanted at least 50.00%",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// No --color: this table mixes report and total, and only one of them draws.
			args := slices.Concat(tc.args, []string{"--profile", path})

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			assert.Equal(t, tc.wantCode, app.Run(args, stdout, stderr))
			assert.Empty(t, stdout.String(), "nothing a script could mistake for a number")
			assert.Contains(t, stderr.String(), tc.wantErr)
		})
	}
}

// Settled by whether --exclude took the statements out, not by whether any file came through it.
func TestRunNamesExcludeAsTheReasonTheReportIsEmpty(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, "mode: set\n"+
		"m/a.go:1.1,2.2 10 1\n"+
		"m/doc.go:1.1,2.2 0 0\n")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	args := []string{"report", "--exclude", `a\.go`, "--profile", path, "--color", "never"}

	assert.Equal(t, codeFailed, app.Run(args, stdout, stderr))
	assert.Contains(t, stderr.String(), "--exclude left nothing to report")
	assert.Empty(t, stdout.String())
}

// Rows walks the root's children, so when a profile spans two top-level paths the root itself is
// never drawn. total reports that root, so its number appears in no row.
func TestTotalOverAProfileWithNoSingleRoot(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, "mode: set\n"+
		"example.com/p/a.go:1.1,2.2 2 1\n"+
		"other.com/q/b.go:1.1,2.2 1 0\n")

	total, tree := &bytes.Buffer{}, &bytes.Buffer{}
	require.Equal(t, codeOK, app.Run([]string{"total", "--profile", path}, total, io.Discard))
	require.Equal(t, codeOK, app.Run([]string{"report", "--profile", path, "--color", "never"}, tree, io.Discard))

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
	require.Equal(t, codeOK, app.Run([]string{"total", "--profile", path}, total, io.Discard))
	require.Equal(
		t,
		codeOK,
		app.Run([]string{"report", "--depth", "0", "--profile", path, "--color", "never"}, tree, io.Discard),
	)

	assert.Equal(t, "66.67\n", total.String())
	assert.Contains(t, tree.String(), "66.67", "the tree reports the same figure")
}

// Setting --depth past the bottom of the tree is how the README told people to see all of it, and
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
		{name: "the default is one level below the top row", args: []string{"report"}, wantRows: 2},
		{name: "max reaches the bottom", args: []string{"report", "--depth", "max"}, wantRows: 4},
		{name: "a number past the bottom reaches it too", args: []string{"report", "--depth", "9"}, wantRows: 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := slices.Concat(tc.args, []string{"--profile", writeProfile(t, fourLevels), "--color", "never"})

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
			code := app.Run([]string{"report", "--depth", arg, "--profile", "x.out"}, stdout, stderr)

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

		require.Equal(t, codeOK, app.Run([]string{"report", "--profile", path}, out, io.Discard))
		require.NoError(t, out.Close())

		written, err := os.ReadFile(out.Name())
		require.NoError(t, err)

		assert.NotEmpty(t, written)
		assert.NotContains(t, string(written), "\x1b[")
	})

	// A destination that will not take the report is said, not swallowed. This asserted exit 0,
	// which is what the bug looked like from the outside: bufio holds the first write failure and
	// hands it back at Flush, and the discarded Flush error meant a report nobody received exited
	// as though it had been printed. `prettycov report > /full/disk` was the real case.
	//
	// Exit 2 rather than the gate's 1: the coverage is whatever it is, and what failed is writing
	// it down.
	// total is in the list because it was not: it printed the number, discarded the write error
	// and exited 0, so `COVERAGE := $(shell prettycov total)` on a full disk assigned an empty
	// string to a green build. It is the one command written to be read by a script, which makes
	// a silent failure worth more here than in the two that print for a person.
	for _, command := range []string{"report", "misses", "total"} {
		t.Run(command+" says when the destination will not take it", func(t *testing.T) {
			closed, err := os.CreateTemp(t.TempDir(), "closed")
			require.NoError(t, err)
			require.NoError(t, closed.Close())

			stderr := &bytes.Buffer{}

			assert.Equal(t, codeFailed, app.Run([]string{command, "--profile", path}, closed, stderr))
			assert.Contains(t, stderr.String(), "cannot write the report:")
		})
	}
}

// --hide-covered shapes the report and never the measurement: total and --fail-under read the same
// with it as without, which is what separates it from --exclude.
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
		"unset": {
			args: []string{"report"},
			want: " m - 92.86\n ├ done - 100.00\n └ work - 90.00\n   └ deep - 0.00\n",
		},
		"bare": {
			args: []string{"report", "--hide-covered"},
			want: " m - 92.86\n └ work - 90.00\n   └ deep - 0.00\n",
		},
		"threshold": {
			args: []string{"report", "--hide-covered=90"},
			want: " m - 92.86\n └ work - 90.00\n   └ deep - 0.00\n",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := writeProfile(t, shaped)

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(append(tc.args, "--depth", "max", "--profile", path, "--color", "never"), stdout, stderr)

			assert.Equal(t, codeOK, code)
			assert.Equal(t, tc.want, stdout.String())
			assert.Empty(t, stderr.String(), "a display flag says nothing, as --depth does not")

			// The measurement is untouched whatever was drawn.
			total := &bytes.Buffer{}
			require.Equal(t, codeOK, app.Run(
				[]string{"total", "--profile", path}, total, io.Discard))
			assert.Equal(t, "92.86\n", total.String())
		})
	}
}

// The threshold can take every row a depth draws. The exit code is unchanged, but a command that
// prints nothing reads as one that failed, so it says which flag emptied it — and does not claim
// the profile holds nothing below the bar, which the deeper run here disproves.
func TestRunSaysWhenHideCoveredTookEveryRow(t *testing.T) {
	t.Parallel()

	// lo/ is at 0.00, so there is plenty below the bar; the shallow depth is what stops it drawing.
	holdsWork := "mode: set\nm/hi/a.go:1.1,2.2 9 1\nm/lo/b.go:1.1,2.2 1 0\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run(
		[]string{
			"report",
			"--hide-covered=80",
			"--depth",
			"0",
			"--profile",
			writeProfile(t, holdsWork),
			"--color",
			"never",
		},
		stdout,
		stderr,
	)

	assert.Equal(t, codeOK, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "nothing to show at --depth=0, --hide-covered=80")
	assert.NotContains(t, stderr.String(), "nothing is below",
		"the profile holds a package at 0.00; only this depth hides it")

	// And the deeper run proves it.
	deep, deepErr := &bytes.Buffer{}, &bytes.Buffer{}
	require.Equal(
		t,
		codeOK,
		app.Run(
			[]string{
				"report",
				"--hide-covered=80",
				"--depth",
				"max",
				"--profile",
				writeProfile(t, holdsWork),
				"--color",
				"never",
			},
			deep,
			deepErr,
		),
	)
	assert.Equal(t, " m - 90.00\n └ lo - 0.00\n", deep.String())
	assert.Empty(t, deepErr.String())
}

// The ends of the range are thresholds, not errors: 100 is the bare form written out, and 0 asks
// for everything with a percentage to go. The rejections and the boundaries they bound belong in
// one table, as --fail-under's are — both flags read the same percentage now.
func TestRunHideCoveredPercentage(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		value    string
		wantCode int
	}{
		"over a hundred": {value: "101", wantCode: codeFailed},
		"negative":       {value: "-5", wantCode: codeFailed},
		"not a number":   {value: "abc", wantCode: codeFailed},
		"NaN":            {value: "nan", wantCode: codeFailed},
		"a hundred":      {value: "100", wantCode: codeOK},
		"zero":           {value: "0", wantCode: codeOK},
		// BoolFunc advertises the flag as boolean, so every spelling ParseBool takes is its own
		// contract — recognising two of the twelve and calling the rest "not a percentage" had the
		// flag arguing with its usage line. Capitalisation is whatever the shell handed over.
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run(
				[]string{
					"report",
					"--hide-covered=" + tc.value,
					"--profile",
					writeProfile(t, profile),
					"--color",
					"never",
				},
				stdout,
				stderr,
			)

			assert.Equal(t, tc.wantCode, code)

			if tc.wantCode == codeFailed {
				assert.Empty(t, stdout.String())
				assert.Contains(t, stderr.String(), "want a percentage from 0 to 100")

				return
			}

			assert.NotContains(t, stderr.String(), "want a percentage")
		})
	}
}

// "0" and "1" are the one place the percentage and the boolean vocabularies collide, and they stay
// percentages: the value this flag documents is a percentage and both are in range. So
// --hide-covered=0 hides every row that has one, where =false leaves the report alone.
func TestRunHideCoveredReadsZeroAsAPercentageNotAsOff(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ value, wantOut string }{
		"zero is a threshold": {value: "0", wantOut: ""},
		// And "1" the same way, or the guard that keeps it a percentage is untested.
		"one is a threshold": {value: "1", wantOut: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run([]string{
				"report", "--hide-covered=" + tc.value, "--depth", "0",
				"--profile", writeProfile(t, profile), "--color", "never",
			}, stdout, stderr)

			assert.Equal(t, codeOK, code)
			assert.Equal(t, tc.wantOut, stdout.String())
		})
	}
}

// misses replaces the report: positions on stdout, no tree, and the gate still reads the tree.
func TestRunMisses(t *testing.T) {
	t.Parallel()

	// own.go belongs to m itself; deep/ is a level further down.
	// deep/ holds two files, so it does not merge into one of them and its files sit a level below
	// it — which is what lets the depth tell the two cases apart.
	shaped := "mode: set\n" +
		"m/own.go:5.2,6.3 1 0\n" +
		"m/deep/a.go:9.2,10.3 1 0\n" +
		"m/deep/a.go:11.2,12.3 1 0\n" +
		"m/deep/b.go:3.2,4.3 1 1\n" +
		"m/covered/c.go:3.2,4.3 1 1\n"

	// covered/c.go and deep/b.go are in the profile and in none of these: a covered block is not a
	// miss. The two blocks of deep/a.go abut, so they are one position carrying both statements.
	//
	// A file is an entry of the package holding it, so it sits a level below that package: own.go
	// is a row at depth 1 and deep/a.go one at depth 2.
	const (
		whole = "m/deep/a.go:9:2: 2 uncovered\n" +
			"m/own.go:5:2: 1 uncovered\n"
		topOnly = "m/own.go:5:2: 1 uncovered\n"
	)

	tests := map[string]struct {
		args     []string
		want     string
		wantNote string
	}{
		"the whole tree": {args: []string{"misses", "--depth", "max"}, want: whole},
		// A list that stops early looks exactly like a short one, so the count is the only thing
		// that tells them apart: two of the three statements are a level below what this draws.
		"one level": {
			args:     []string{"misses", "--depth", "1"},
			want:     topOnly,
			wantNote: "--depth=1 lists 1 of 3 uncovered statements\n",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			// No --color: misses draws nothing a palette reaches, so only report takes the flag.
			code := app.Run(slices.Concat(tc.args,
				[]string{"--profile", writeProfile(t, shaped)}), stdout, stderr)

			assert.Equal(t, codeOK, code)
			assert.Equal(t, tc.want, stdout.String())
			assert.Equal(t, tc.wantNote, stderr.String(), "a full list has nothing to say about itself")
		})
	}
}

// The gate reads the tree, not the rows, so --fail-under means the same beside the positions as it
// does beside the table.
func TestRunMissesStillGrades(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run(
		[]string{"misses", "--fail-under", "80", "--profile", writeProfile(t, profile)},
		stdout,
		stderr,
	)

	assert.Equal(t, codeBelow, code)
	assert.NotEmpty(t, stdout.String(), "the positions still go out")
	assert.Contains(t, stderr.String(), "total coverage 60.00% is below 80.00%")
}

// A fully covered profile has no positions to print, which is news rather than a failure — an empty
// stdout from a command that exits 0 reads as one that did not run.
func TestRunMissesSaysWhenThereAreNone(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"misses",
		"--profile",
		writeProfile(t, "mode: set\nm/a.go:1.1,2.2 3 1\n"),
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "nothing left to cover")
}

// An empty list has two causes and they are opposite news. Saying "nothing left to cover" when the
// shaping flags took it all is a false all-clear on a profile with work left in it, and it exits 0.
//
// The count is the tree's own, which is why it is right whichever flag did it: --depth and
// --hide-covered shape the report and never the measurement, so the total is what it always was.
func TestRunNamesWhatEmptiedTheOutput(t *testing.T) {
	t.Parallel()

	// pkg holds two files, so nothing merges and both sit at level 2 — the default --depth=1 draws
	// pkg itself and reaches neither of them. web holds one, which merges into a row of its own at
	// level 1, and it is covered: without it the whole chain would collapse to a single file row at
	// level 0 and the depth would reach it after all.
	const twoDeep = "mode: set\n" +
		"m/pkg/a.go:9.2,10.3 1 0\n" +
		"m/pkg/b.go:40.2,41.3 1 0\n" +
		"m/web/c.go:1.1,2.2 3 1\n"

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no file sits at this depth",
			args: []string{"misses"},
			want: "nothing to show at --depth=1; 2 uncovered statements left",
		},
		{
			name: "the bar hides every file holding one",
			// The threshold needs "=": --hide-covered takes an optional value, so a separate one is
			// read as the profile path.
			args: []string{"misses", "--depth", "max", "--hide-covered=0"},
			want: "nothing to show at --depth=max, --hide-covered=0; 2 uncovered statements left",
		},
		// The same sentence from the other printer, which is the point of it: the filters emptied
		// the output, and naming them is the answer whichever one was holding the pen. A message per
		// printer is a second place with an opinion about what those filters do.
		{
			name: "the bar hides every row of the tree",
			args: []string{"report", "--depth", "max", "--hide-covered=0"},
			want: "nothing to show at --depth=max, --hide-covered=0; 2 uncovered statements left",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			// No --color: this table mixes misses and report, and only report takes it.
			args := slices.Concat(tc.args, []string{"--profile", writeProfile(t, twoDeep)})

			code := app.Run(args, stdout, stderr)

			assert.Equal(t, codeOK, code)
			assert.Empty(t, stdout.String())
			// The whole of stderr, which is what rules out the all-clear: any sentence claiming
			// completion is a different string.
			assert.Equal(t, tc.want+"\n", stderr.String())
		})
	}
}

// --exclude acts on the profile before the tree, so an excluded block is not a miss — which is what
// makes the two compose: the positions this prints are the coordinates that flag takes.
func TestRunMissesHonoursExclude(t *testing.T) {
	t.Parallel()

	shaped := "mode: set\nm/a.go:9.2,10.3 1 0\nm/b.go:40.2,41.3 1 0\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"misses", "--depth", "max", "--exclude", `a\.go:9`,
		"--profile", writeProfile(t, shaped),
	}, stdout, stderr)

	assert.Equal(t, codeOK, code)
	assert.Equal(t, "m/b.go:40:2: 1 uncovered\n", stdout.String())
	assert.Contains(t, stderr.String(), `--exclude "a\\.go:9" left out 1 statement in 1 block`)
}

// The error paths out of each command's Run: a flag that parses but cannot be used, reached through
// every command so that none of them swallows it.
func TestRunReportsABadValueFromEveryCommand(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"report", "misses", "total"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run([]string{
				command, "--old", "example.com/p", "--new", "p",
				"--profile", "no/such/profile.out",
			}, stdout, stderr)

			assert.Equal(t, codeFailed, code)
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "cannot read coverage profile")
		})
	}
}

// --depth is read where it is registered, so a value it cannot parse is refused by the two commands
// that take it and unknown to the one that does not.
func TestRunRefusesABadDepthOnBothDrawingCommands(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"report", "misses"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run([]string{
				command, "--depth", "abc",
				"--profile", writeProfile(t, profile),
			}, stdout, stderr)

			assert.Equal(t, codeFailed, code)
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "--depth")
		})
	}
}

// ExitCodeOf is the contract between the handlers and the composition root: an error a handler has
// already reported carries its own status, and anything else is a usage failure for app to print.
//
// Only the "not reported" half can be built from outside, which is the point — a status is not
// something a caller of this package can invent.
var errSomethingWentWrong = errors.New("something went wrong")

func TestExitCodeOfReportsOnlyWhatAHandlerSet(t *testing.T) {
	t.Parallel()

	code, reported := cli.ExitCodeOf(errSomethingWentWrong)
	assert.False(t, reported, "a plain error carries no status of its own")
	assert.Equal(t, cli.ExitFailed, code, "and app treats it as a usage failure")

	_, reported = cli.ExitCodeOf(nil)
	assert.False(t, reported)
}

// A bare `prettycov` is someone finding out what this does, not a mistake. Kong would answer
// "expected one of report, misses, total, version"; the help says that and more, and exits 0.
func TestRunWithNoArgumentsPrintsHelp(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	assert.Equal(t, codeOK, app.Run(nil, stdout, stderr))
	assert.Contains(t, stdout.String(), "Usage: prettycov <command>")
	assert.Contains(t, stdout.String(), "report")
	assert.Empty(t, stderr.String(), "help that was asked for is output, not a diagnostic")
}

// A flag that parses but cannot be used fails from every command, not only the one whose tests
// happened to cover it. --exclude is the one that can: kong takes any string, and the pattern is
// compiled after.
func TestRunRefusesABadPatternFromEveryCommand(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"report", "misses", "total"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			code := app.Run([]string{command, "--exclude", "[", "--profile", writeProfile(t, profile)},
				stdout, stderr)

			assert.Equal(t, codeFailed, code)
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "error parsing regexp")
		})
	}
}

// --color belongs to report, which is the only command that draws anything a palette reaches.
// misses prints file:line:col and DisplayMisses never reads Options.Color, so the flag was accepted
// and inert — `misses --color=always` emitted byte-identical output to `--color=never`. That is the
// same defect that took --color off total.
func TestOnlyReportTakesColor(t *testing.T) {
	t.Parallel()

	path := writeProfile(t, profile)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	require.Equal(t, codeOK, app.Run(
		[]string{"report", "--color", "always", "--profile", path}, stdout, stderr), stderr.String())
	assert.Contains(t, stdout.String(), "\x1b[", "report colours when told to")

	for _, command := range []string{"misses", "total", "version"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			assert.Equal(t, codeFailed,
				app.Run([]string{command, "--color", "always", "--profile", path}, stdout, stderr))
			assert.Contains(t, stderr.String(), "unknown flag --color")
		})
	}
}

// A word that is not a command is refused by name, rather than answered with the root's help as
// though it had been understood. `help` is one of those words: kong writes `<command> --help` in its
// own footer, and a help command alongside it printed the same bytes under a second spelling.
func TestRunRefusesAnUnknownCommand(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"nope", "help"} {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			assert.Equal(t, codeFailed, app.Run([]string{arg}, stdout, stderr))
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), arg)
		})
	}
}

// --help is the one spelling, and it reaches a command as well as the root. Byte-identical to what
// `help report` printed before it was deleted, which is why the command was duplication.
func TestRunPrintsACommandsHelpThroughTheFlag(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	assert.Equal(t, codeOK, app.Run([]string{"report", "--help"}, stdout, stderr))
	assert.Contains(t, stdout.String(), "Usage: prettycov report")
	assert.Contains(t, stdout.String(), "--depth", "including the flags only report has")
	assert.Empty(t, stderr.String())
}
