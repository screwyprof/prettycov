package app_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov/internal/app"
)

// Exercised through app.Run from this package, rather than only through internal/cli's tests, so
// the condition gate can see it. gobco instruments one package against its own tests: internal/cli
// drives app.Run for almost every case in this repository, and every one of them is invisible to
// gobco here, which reported 0/14 for the package holding the exit-code mapping.
//
// status is what the mapping is, and it is the piece a rewrite is most likely to get subtly wrong:
// an exit code is the whole interface a CI pipeline reads, and there is no output to notice it by.
const (
	codeOK     = 0
	codeBelow  = 1
	codeFailed = 2
)

// halfCovered is 1 of 2 statements, so a gate at anything above 50 fails and the report is not
// empty either way.
const halfCovered = "mode: atomic\nm/a.go:1.1,2.2 1 1\nm/b.go:1.1,2.2 1 0\n"

func writeProfile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "coverage.out")
	require.NoError(t, os.WriteFile(path, []byte(halfCovered), 0o600))

	return path
}

// Run's four branches and status's three, named by what each says to whoever called the binary.
func TestRunExitCodes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		args     []string
		profile  bool // only the commands that read one take --profile; it is not a root flag
		wantCode int
		wantOut  string // substring, stdout
		wantErr  string // substring, stderr
	}{
		{
			// No command is someone finding out what this does. kong would answer "expected one of",
			// and the help says that and more — so args is replaced and kong exits through the
			// recorder, which is the only way `done` is ever set.
			name: "no arguments print the help and succeed",
			args: nil, wantCode: codeOK, wantOut: "Usage: prettycov <command>",
		},
		{
			// The other way into the recorder, and the one that proves `done >= 0` is read before
			// the parse error is: --version is a flag kong answers itself.
			name: "--version prints and succeeds",
			args: []string{"--version"}, wantCode: codeOK,
		},
		{
			// done stays -1 and Parse returns an error, which is the usage failure app prints
			// rather than the handler.
			name: "an unknown flag is a usage failure",
			args: []string{"--nonsense"}, wantCode: codeFailed, wantErr: `run "prettycov --help" for usage`,
		},
		{
			// status(nil): nothing reported, no error, exit 0.
			name: "a report that runs is exit 0",
			args: []string{"report"}, profile: true, wantCode: codeOK, wantOut: "50.00",
		},
		{
			// status of an error carrying its own code — the handler already printed, so app must
			// return the code and add nothing. Exit 1, not 2: coverage was measured and was low.
			name:    "a gate the coverage misses is exit 1",
			args:    []string{"report", "--fail-under", "99"},
			profile: true, wantCode: codeBelow, wantErr: "is below 99.00%",
		},
		{
			// status of an error that reported nothing: app is what prints it. errEmptyTotalPath is
			// a plain sentinel, so this is the branch a handler-reported error never reaches.
			name: "an error no handler reported is printed by app",
			args: []string{"total", ""}, profile: true, wantCode: codeFailed, wantErr: "want a path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			args := tc.args
			if tc.profile {
				args = append(append([]string{}, args...), "--profile", writeProfile(t))
			}

			assert.Equal(t, tc.wantCode, app.Run(args, stdout, stderr))

			if tc.wantOut != "" {
				assert.Contains(t, stdout.String(), tc.wantOut)
			}

			if tc.wantErr != "" {
				assert.Contains(t, stderr.String(), tc.wantErr)
			}
		})
	}
}

// The version is the linker's when it was given one and the build's otherwise, and either way it is
// one line on stdout. Pinned because `go install prettycov@v1.2.3` passes no ldflags, so the
// fallback is the path most installed copies take — under `go test` that is "(devel)", which the
// toolchain writes for a main module with no version.
//
// Trimmed before the assertion, and that is the whole point of it: asserting the buffer is
// non-empty passes on a lone newline, so negating buildVersion's `version != ""` — which makes it
// return the empty linker variable — printed nothing and survived as a mutant.
func TestRunVersionIsPrinted(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	require.Equal(t, codeOK, app.Run([]string{"version"}, stdout, stderr))

	assert.NotEmpty(t, strings.TrimSpace(stdout.String()), "a version, not a bare newline")
	assert.Empty(t, stderr.String())
}
