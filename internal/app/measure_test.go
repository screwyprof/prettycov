package app_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov"
	"github.com/screwyprof/prettycov/internal/app"
)

// go test's output reaches stdout untouched, and nothing of ours joins it there — a -json consumer
// is reading that stream and we cannot know what else is.
func TestMeasurePassesGoTestsOutputThrough(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeMod(t, root, ".", "meas.test")
	writeMod(t, root, "svc", "meas.test/svc")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"measure", root, "-profile", filepath.Join(t.TempDir(), "c.out")}, stdout, stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout.String(), "meas.test/svc", "go test's own output")
	assert.NotContains(t, stdout.String(), "prettycov:", "ours belongs on stderr")
}

// go test's status is the command's: 1 for a failing test, which is all go distinguishes.
func TestMeasureTakesGoTestsExitStatus(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeMod(t, root, ".", "fail.test")
	write(t, filepath.Join(root, "f_test.go"),
		"package m\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { t.Fatal(\"no\") }\n")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"measure", root, "-profile", filepath.Join(t.TempDir(), "c.out")}, stdout, stderr)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "prettycov:", "the failing module is named")
}

// Anything after -- is go test's. -tags is the one flag read on the way past, because discovery
// must scan under the tags the run compiles with.
func TestMeasureForwardsArgsAndReadsTags(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module tag.test\n\ngo 1.27\n")
	write(t, filepath.Join(root, "m.go"), "//go:build special\n\npackage m\n\nfunc M() int { return 1 }\n")
	write(t, filepath.Join(root, "m_test.go"), "//go:build special\n\npackage m\n\nimport \"testing\"\n\n"+
		"func TestM(t *testing.T) {\n\tif M() != 1 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n")

	profile := filepath.Join(t.TempDir(), "coverage.out")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	code := app.Run([]string{"measure", root, "-profile", profile, "--", "-tags=special"}, stdout, stderr)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	items, err := prettycov.ParseProfile(profile)
	require.NoError(t, err)
	assert.NotEmpty(t, items, "the tagged package was found and measured")
}

// Without the tag the same repository holds no packages at all, which is an empty answer rather
// than a failure.
func TestMeasureReportsNothingToMeasure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module tag.test\n\ngo 1.27\n")
	write(t, filepath.Join(root, "m.go"), "//go:build special\n\npackage m\n\nfunc M() int { return 1 }\n")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"measure", root, "-profile", filepath.Join(t.TempDir(), "c.out")}, stdout, stderr)

	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "nothing to measure")
}

func TestMeasureRejectsASecondDirectory(t *testing.T) {
	t.Parallel()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"measure", "one", "two"}, stdout, stderr)

	assert.Equal(t, 2, code, "prettycov's own failure, which go test never returns")
	assert.Contains(t, stderr.String(), "at most one directory")
}

func writeMod(t *testing.T, root, dir, path string) {
	t.Helper()

	write(t, filepath.Join(root, dir, "go.mod"), "module "+path+"\n\ngo 1.27\n")
	write(t, filepath.Join(root, dir, "m.go"), "package m\n\nfunc M() int { return 1 }\n")
	write(t, filepath.Join(root, dir, "m_test.go"), "package m\n\nimport \"testing\"\n\n"+
		"func TestM(t *testing.T) {\n\tif M() != 1 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n")
}

func write(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// The modules are joined into one profile, under one mode line. A second would make the file
// unparseable, and cover merges the blocks a repeat describes when it is read.
func TestMeasureJoinsModulesIntoOneProfile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeMod(t, root, ".", "join.test")
	writeMod(t, root, "svc", "join.test/svc")
	writeMod(t, root, "other", "join.test/other")

	profile := filepath.Join(t.TempDir(), "coverage.out")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	require.Equal(t, 0, app.Run([]string{"measure", root, "-profile", profile}, stdout, stderr),
		"stderr: %s", stderr)

	data, err := os.ReadFile(profile)
	require.NoError(t, err)

	assert.Equal(t, 1, bytes.Count(data, []byte("mode: ")), "exactly one mode line")
	assert.True(t, bytes.HasPrefix(data, []byte("mode: ")), "and it comes first")

	for _, module := range []string{"join.test/m.go", "join.test/svc/m.go", "join.test/other/m.go"} {
		assert.Contains(t, string(data), module)
	}

	items, err := prettycov.ParseProfile(profile)
	require.NoError(t, err)
	assert.Len(t, items, 3, "the joined profile parses back to one entry per module")
}

// go overwrites -tags on each occurrence, so the last is the one the tests compile under. Reading
// the first would scan discovery under tags the run never uses.
func TestMeasureTakesTheLastTags(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module tags.test\n\ngo 1.27\n")
	write(t, filepath.Join(root, "m.go"), "//go:build beta\n\npackage m\n\nfunc M() int { return 1 }\n")
	write(t, filepath.Join(root, "m_test.go"), "//go:build beta\n\npackage m\n\nimport \"testing\"\n\n"+
		"func TestM(t *testing.T) {\n\tif M() != 1 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n")

	profile := filepath.Join(t.TempDir(), "c.out")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	code := app.Run(
		[]string{"measure", root, "-profile", profile, "--", "-tags=alpha", "-tags=beta"},
		stdout, stderr)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	items, err := prettycov.ParseProfile(profile)
	require.NoError(t, err)
	assert.NotEmpty(t, items, "discovery scanned under alpha, not the beta the tests were built with")
}

// Nothing to measure is not a measurement. Writing the profile anyway truncates whatever was there
// and says nothing, and a later -fail-under then reports "no statements to cover" with no cause.
func TestMeasureRefusesATreeWithNoModules(t *testing.T) {
	t.Parallel()

	profile := filepath.Join(t.TempDir(), "keep.out")
	write(t, profile, "mode: set\nex.test/a/a.go:3.16,3.26 1 1\n")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"measure", t.TempDir(), "-profile", profile}, stdout, stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "no modules")

	kept, err := os.ReadFile(profile)
	require.NoError(t, err)
	assert.Contains(t, string(kept), "ex.test/a/a.go", "the existing profile was overwritten")
}

// A symlinked root is lstat'd by WalkDir and visited as a single leaf, so the walk finds no modules
// at all — and finds them fine through the real path.
func TestMeasureFollowsASymlinkedRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	target := filepath.Join(base, "target")
	writeMod(t, target, ".", "link.test")

	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(target, link))

	profile := filepath.Join(t.TempDir(), "c.out")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	require.Equal(t, 0, app.Run([]string{"measure", link, "-profile", profile}, stdout, stderr),
		"stderr: %s", stderr)

	items, err := prettycov.ParseProfile(profile)
	require.NoError(t, err)
	assert.NotEmpty(t, items, "the walk stopped at the symlink")
}

// Without -profile the tests run and nothing is instrumented, which is what `go test` does unasked.
// Coverage costs compilation, so it is requested rather than assumed.
func TestMeasureWithoutAProfileMeasuresNothing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeMod(t, root, ".", "bare.test")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{"measure", root}, stdout, stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout.String(), "bare.test", "the tests still ran")
	assert.NotContains(t, stdout.String(), "coverage:", "and were not instrumented")
}

// One go test per module is one profile path per module, so a caller's own -coverprofile cannot be
// honoured: the next module would truncate it, and ours wins by position, leaving theirs looking
// accepted and doing nothing.
func TestMeasureRefusesACallersCoverProfile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeMod(t, root, ".", "reject.test")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := app.Run([]string{
		"measure", root, "-profile", filepath.Join(t.TempDir(), "c.out"),
		"--", "-coverprofile=/tmp/theirs.out",
	}, stdout, stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "use -profile for the joined result")
}
