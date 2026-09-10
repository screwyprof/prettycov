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
			// report your own pattern emptied is what emptyReason exists to prevent, and the
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
