package main_test

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

	// -cover so the child's run counts toward coverage. cancel() by hand: os.Exit skips defers.
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	out, buildErr := exec.CommandContext(ctx, "go", "build", "-cover", "-covermode", "atomic", "-o", binary, ".").
		CombinedOutput()

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

// Nothing else checks the process exit code; in-process tests only see run()'s return value.
func TestBinaryExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "clean run", args: []string{"-color", "never"}, want: codeOK},
		{name: "above the threshold", args: []string{"-fail-under", "50"}, want: codeOK},
		{name: "below the threshold", args: []string{"-fail-under", "90"}, want: codeBelow},
		{name: "unreadable profile", args: []string{"-profile", "no-such-file.out"}, want: codeFailed},
		{name: "unknown flag", args: []string{"-nope"}, want: codeFailed},
		// Distinct from below-threshold on purpose: a CI step has to tell "coverage dropped"
		// from "prettycov could not run".
		{name: "help", args: []string{"-help"}, want: codeOK},
		{name: "version", args: []string{"version"}, want: codeOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, code := runBinary(t, tc.args...)

			assert.Equal(t, tc.want, code)
		})
	}
}

// Swapping the streams leaves `prettycov > report.txt` holding the diagnostics.
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
		{
			name: "a bad flag is stderr", args: []string{"-nope"},
			wantStderr: "not defined: -nope",
		},
		{
			// Help that was asked for is output, not a diagnostic.
			name: "requested help is stdout", args: []string{"-help"},
			wantStdout: "Prettycov:",
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

// os.Args passed unsliced feeds the program's own path in as a positional.
func TestBinaryPassesItsArguments(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runBinary(t, "-color", "never", "-depth", "0")

	require.Equal(t, codeOK, code, stderr)
	assert.Contains(t, stdout, "60.00")
	assert.NotContains(t, stdout, "a - ", "-depth=0 is the top row alone")
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

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "coverage.out"), profile, 0o600))

	var out, errOut bytes.Buffer

	// t.Context ends with the test, so a hung binary is killed instead of hanging the run.
	cmd := exec.CommandContext(t.Context(), binary, args...)
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
