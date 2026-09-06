package coverage_test

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov/internal/coverage"
	"github.com/screwyprof/prettycov/internal/discover"
)

// TestMeasureCoversEveryModule is the whole point: `go test ./...` reaches one module, and a
// repository of three needs three invocations to be measured at all.
func TestMeasureCoversEveryModule(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModule(t, root, ".", "nested.test")
	writeModule(t, root, "svc", "nested.test/svc")
	writeModule(t, root, "svc/inner", "nested.test/svc/inner")

	result := measure(t, root, coverage.Config{})

	assert.Empty(t, result.Excluded)
	assert.Equal(t, map[string]string{
		"nested.test":           "covered",
		"nested.test/svc":       "covered",
		"nested.test/svc/inner": "covered",
	}, outcomes(t, result))
}

// TestMeasureKeepsDataFromAFailedModule records the state nats-server is in on every run: exit
// non-zero, and a profile holding every package that did build.
func TestMeasureKeepsDataFromAFailedModule(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModule(t, root, ".", "partial.test")
	write(t, root, "broken/b.go", "package broken\n\nfunc B() int { return undefinedSymbol }\n")

	result := measure(t, root, coverage.Config{})

	require.Len(t, result.Runs, 1)
	require.Error(t, result.Runs[0].Err, "one package failed to build")
	assert.NotEmpty(t, result.Runs[0].Profile, "the healthy package was still measured")
}

// TestMeasureReachesAModuleTheWorkspaceOmits covers grafana's five: in workspace mode ./... matches
// nothing there, so without GOWORK=off the module is unmeasurable rather than merely uncovered.
func TestMeasureReachesAModuleTheWorkspaceOmits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, root, "go.work", "go 1.27\n\nuse ./member\n")
	writeModule(t, root, "member", "ws.test/member")
	writeModule(t, root, "outside", "ws.test/outside")

	repo, err := discover.Scan(t.Context(), root, discover.Config{})
	require.NoError(t, err)
	require.NotEmpty(t, repo.Workspace, "the fixture has a workspace")

	result, err := coverage.Measure(t.Context(), repo, config(t))
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		"ws.test/member":  "covered",
		"ws.test/outside": "covered",
	}, outcomes(t, result))
}

// TestMeasureExcludesWholeMatchesOnly is the anchoring rule. bubbletea's 63 uncovered packages are
// one module, so exclusion is module-shaped; and `cmd` must never take `pkg/subcmd`, which is what
// every unanchored grep in the survey got wrong.
func TestMeasureExcludesWholeMatchesOnly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModule(t, root, ".", "anchor.test")
	writeModule(t, root, "examples", "examples")
	writeModule(t, root, "pkg/subcmd", "anchor.test/pkg/subcmd")

	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{name: "exact directory", pattern: "examples", want: []string{"examples"}},
		{name: "does not match a suffix", pattern: "cmd", want: nil},
		{name: "prefix needs to be spelled", pattern: "pkg/.*", want: []string{"anchor.test/pkg/subcmd"}},
		{name: "an anchored pattern still works", pattern: "^examples$", want: []string{"examples"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := measure(t, root, coverage.Config{Exclude: regexp.MustCompile(tt.pattern)})

			excluded := make([]string, 0, len(result.Excluded))
			for _, module := range result.Excluded {
				excluded = append(excluded, module.Path)
			}

			assert.Equal(t, tt.want, nilIfEmpty(excluded))
		})
	}
}

// TestMeasureSkipsAModuleDiscoveryCouldNotRead is hugo's internal/warpc/genwebp: an empty go.mod
// fencing off a directory of C. Running go test there would rediscover, slowly, what discovery
// already knows.
func TestMeasureSkipsAModuleDiscoveryCouldNotRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModule(t, root, ".", "fence.test")
	write(t, root, "fence/go.mod", "")
	write(t, root, "fence/fence.c", "int fence(void) { return 3; }\n")

	result := measure(t, root, coverage.Config{})

	assert.Equal(t, map[string]string{
		"fence.test": "covered",
		"":           "unreadable",
	}, outcomes(t, result))
}

// TestMeasureReportsAModuleWithNoPackages distinguishes nothing to do from something gone wrong:
// go warns, writes no file, and exits 0.
func TestMeasureReportsAModuleWithNoPackages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, root, "go.mod", "module empty.test\n\ngo 1.27\n")
	write(t, root, "docs/README.md", "no Go here\n")

	result := measure(t, root, coverage.Config{})

	require.Len(t, result.Runs, 1)
	require.NoError(t, result.Runs[0].Err)
	assert.Empty(t, result.Runs[0].Profile, "no packages, so no profile")
}

// measure scans root and measures it, filling in the parts every test wants the same way.
func measure(t *testing.T, root string, cfg coverage.Config) coverage.Result {
	t.Helper()

	repo, err := discover.Scan(t.Context(), root, discover.Config{})
	require.NoError(t, err)

	cfg.Dir, cfg.Stdout, cfg.Stderr = t.TempDir(), io.Discard, io.Discard

	result, err := coverage.Measure(t.Context(), repo, cfg)
	require.NoError(t, err)

	return result
}

func config(t *testing.T) coverage.Config {
	t.Helper()

	return coverage.Config{Dir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard}
}

// outcomes reduces each run to one word, so a test states what happened rather than how.
func outcomes(t *testing.T, result coverage.Result) map[string]string {
	t.Helper()

	got := map[string]string{}

	for _, run := range result.Runs {
		switch {
		case run.Err != nil && run.Profile == "":
			got[run.Module.Path] = "unreadable"
		case run.Err != nil:
			got[run.Module.Path] = "failed with data"
		case run.Profile == "":
			got[run.Module.Path] = "nothing to measure"
		default:
			got[run.Module.Path] = "covered"
		}
	}

	return got
}

// writeModule creates a module at dir with one covered package, so a run has something to measure.
func writeModule(t *testing.T, root, dir, path string) {
	t.Helper()

	write(t, root, filepath.Join(dir, "go.mod"), "module "+path+"\n\ngo 1.27\n")
	write(t, root, filepath.Join(dir, "m.go"), "package m\n\nfunc M() int { return 1 }\n")
	write(t, root, filepath.Join(dir, "m_test.go"),
		"package m\n\nimport \"testing\"\n\nfunc TestM(t *testing.T) {\n\tif M() != 1 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n")
}

func write(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}

	return s
}
