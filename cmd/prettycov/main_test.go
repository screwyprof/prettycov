package main_test

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Literal, not the exitOK/exitBelow/exitFailed constants: a script sees these numbers, so
// renumbering one has to fail here.
const (
	codeOK     = 0
	codeBelow  = 1
	codeFailed = 2
)

// 6 of 10 statements covered, so the report reads 60.00 and a -fail-under above that fails.
//
//go:embed testdata/sixty-percent.out
var profile []byte

const buildTimeout = 2 * time.Minute

// binary is the compiled CLI, built once.
//
//nolint:gochecknoglobals // TestMain has no other channel to the tests it runs.
var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "prettycov-binary-test")
	if err != nil {
		panic(err)
	}

	binary = filepath.Join(dir, "prettycov")

	// cancel() by hand: os.Exit below means a defer never runs.
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	out, buildErr := buildBinary(ctx, binary)

	cancel()

	if buildErr != nil {
		_, _ = os.Stderr.Write(out)
		_ = os.RemoveAll(dir)

		panic(buildErr)
	}

	code := m.Run()

	_ = os.RemoveAll(dir)

	os.Exit(code)
}

// main is os.Exit(app.Run(...)), so a status it propagates once it propagates always — the value
// is opaque to it. One case per distinct code proves that; the branch matrix behind each code is
// app_test's, and re-walking it here would cost a process fork per case to learn nothing.
func TestBinaryExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "success", args: nil, want: codeOK},
		{name: "below the threshold", args: []string{"-fail-under", "90"}, want: codeBelow},
		{name: "could not run", args: []string{"-nope"}, want: codeFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, code := runBinary(t, tc.args...)

			assert.Equal(t, tc.want, code)
		})
	}
}

// Swapping the streams leaves `prettycov > report.txt` holding the diagnostics. One case each way
// settles which writer main hands to which stream; which message goes where is app_test's.
func TestBinaryWritesToTheRightStream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantStdout string
		wantStderr string
	}{
		{
			// The rendered row, not the bare number: the gate's message carries "60.00" too.
			name: "the report is stdout", args: []string{"-color", "never"},
			wantStdout: "m - 60.00",
		},
		{
			name: "the gate's complaint is stderr", args: []string{"-color", "never", "-fail-under", "90"},
			wantStdout: "m - 60.00", wantStderr: "is below 90.00%",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, _ := runBinary(t, tc.args...)

			if tc.wantStdout != "" {
				assert.Contains(t, stdout, tc.wantStdout)
				assert.NotContains(t, stderr, tc.wantStdout, "it went to the wrong stream")
			}

			if tc.wantStderr != "" {
				assert.Contains(t, stderr, tc.wantStderr)
				assert.NotContains(t, stdout, tc.wantStderr, "it went to the wrong stream")
			}
		})
	}
}

// The linker stamp is a build-time path: move the package that holds the variable and -ldflags
// keeps succeeding while stamping nothing, which is how a nix build once reported "(devel)".
// Only a build can catch that, so it is checked here rather than by calling pickVersion.
func TestBinaryReportsTheStampedVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stamped := filepath.Join(dir, "stamped")

	ctx, cancel := context.WithTimeout(t.Context(), buildTimeout)
	defer cancel()

	out, err := buildBinary(ctx, stamped,
		"-ldflags", "-X github.com/screwyprof/prettycov/internal/app.version=v9.9.9-test")
	require.NoError(t, err, string(out))

	got, errOut, code := runAt(t, stamped, "version")

	require.Equal(t, codeOK, code, errOut)
	assert.Equal(t, "v9.9.9-test\n", got)
}

// Without a stamp the version comes from the build info, and must still be something a script can
// read. "(devel)" is what an unstamped build reports.
func TestBinaryReportsAVersionWithoutAStamp(t *testing.T) {
	t.Parallel()

	got, errOut, code := runBinary(t, "version")

	require.Equal(t, codeOK, code, errOut)
	assert.NotEmpty(t, strings.TrimSpace(got))
}

// os.Args passed unsliced feeds the program's own path in as a positional.
func TestBinaryPassesItsArguments(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runBinary(t, "-color", "never", "-depth", "0")

	require.Equal(t, codeOK, code, stderr)
	assert.Contains(t, stdout, "60.00")
	assert.NotContains(t, stdout, "a - ", "-depth=0 is the top row alone")
}

// buildBinary compiles the command into out. Always instrumented, so a child's run counts toward
// coverage; extra carries whatever a caller needs on top, such as a linker stamp.
func buildBinary(ctx context.Context, out string, extra ...string) ([]byte, error) {
	args := append([]string{"build", "-cover", "-covermode", "atomic"}, extra...)
	args = append(args, "-o", out, ".")

	//nolint:wrapcheck // the callers report it; there is nothing to add here.
	return exec.CommandContext(ctx, "go", args...).CombinedOutput()
}

// coverDir is where a child writes its counters. The Makefile sets PRETTYCOV_COVERDIR and collects
// them; a bare `go test` gets a throwaway directory.
func coverDir(t *testing.T) string {
	t.Helper()

	if dir := os.Getenv("PRETTYCOV_COVERDIR"); dir != "" {
		return dir
	}

	return t.TempDir()
}

// runBinary runs the CLI in a directory holding a coverage.out, so a bare invocation has something
// to read.
func runBinary(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	return runAt(t, binary, args...)
}

// runAt is runBinary for a binary built by the caller.
func runAt(t *testing.T, bin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "coverage.out"), profile, 0o600))

	var out, errOut bytes.Buffer

	// t.Context ends with the test, so a hung binary is killed instead of hanging the run.
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = &out, &errOut

	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverDir(t))

	// A non-zero exit is an outcome here, not a failure to run.
	var exitErr *exec.ExitError
	if err := cmd.Run(); err != nil && !errors.As(err, &exitErr) {
		require.NoError(t, err, "running the binary")
	}

	return out.String(), errOut.String(), cmd.ProcessState.ExitCode()
}
