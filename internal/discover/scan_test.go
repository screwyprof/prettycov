package discover_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/screwyprof/prettycov/internal/discover"
)

// The cases here need something a txtar corpus entry cannot state: a second scan of the same
// archive under different inputs, a file mode, or an environment variable.

// TestScanUsesBuildTags scans the build-tags fixture again, this time with the tag its acceptance
// suite is behind. The counts in that fixture are what discovery reports without it: one package,
// with the whole suite absent and nothing saying so.
func TestScanUsesBuildTags(t *testing.T) {
	t.Parallel()

	root := extractArchive(t, filepath.Join("topologies", "build-tags.txtar"))

	repo, err := discover.Scan(t.Context(), root, "acceptance")

	require.NoError(t, err)
	require.Len(t, repo.Modules, 1)

	paths := map[string]bool{}
	for _, pkg := range repo.Modules[0].Packages {
		paths[pkg.ImportPath] = pkg.HasTests
	}

	assert.Equal(t, map[string]bool{"tags.test": true, "tags.test/api": true}, paths)
}

// TestScanFindsParentWorkspace scans below the directory holding go.work. The go tool searches
// parents for it, so a services/ subdirectory of a workspace is still in one, and discovery has to
// agree or every module there looks deliberately excluded.
func TestScanFindsParentWorkspace(t *testing.T) {
	t.Parallel()

	root := extractArchive(t, filepath.Join("topologies", "parent-workspace.txtar"))

	repo, err := discover.Scan(t.Context(), filepath.Join(root, "services"))
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(root, "go.work"), repo.Workspace)

	require.Len(t, repo.Modules, 1)
	assert.True(t, repo.Modules[0].InWorkspace, "the workspace above lists this module")
}

// TestScanRecordsUnreadableDir keeps a directory it cannot open from hiding the rest of the tree.
// A root-owned build artefact or a cache is enough to cause this, and reporting nothing at all
// would be a worse answer than reporting what is readable and saying where it stopped.
//
// No txtar fixture can express a mode, so the tree is built here.
func TestScanRecordsUnreadableDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	locked := filepath.Join(root, "locked")

	write(t, root, "go.mod", "module locked.test\n\ngo 1.27\n")
	write(t, root, "f.go", "package locked\n")
	require.NoError(t, os.Mkdir(locked, 0o000))

	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("running as a user that can read mode 000")
	}

	repo, err := discover.Scan(t.Context(), root)

	require.NoError(t, err)
	require.Len(t, repo.Modules, 1)
	require.NoError(t, repo.Modules[0].Err)
	assert.Equal(t, []string{locked}, repo.Unreadable)
	assert.Equal(t, "locked.test", repo.Modules[0].Path)
}

// TestScanRejectsUnreadableRoot is the other side of that: a gap in the tree is worth reporting
// around, but a root that cannot be read is not a tree with a gap, it is no tree at all.
func TestScanRejectsUnreadableRoot(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "absent")

	_, err := discover.Scan(t.Context(), missing)

	require.Error(t, err)
	assert.Contains(t, err.Error(), missing)
}

// TestScanRejectsMalformedWorkspace fails the scan outright, unlike an unreadable module. go does
// the same — `go list ./...` and `go build ./...` both refuse with "errors parsing go.work" — and
// a workspace nobody can parse is a fact about the tree, not about one module in it.
func TestScanRejectsMalformedWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// An unterminated use block: valid until the file ends.
	write(t, root, "go.work", "go 1.27\n\nuse (\n\t./m\n")
	write(t, root, "m/go.mod", "module malformed.test/m\n\ngo 1.27\n")
	write(t, root, "m/m.go", "package m\n")

	_, err := discover.Scan(t.Context(), root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing go.work")
}

// TestScanHonoursGOWORKOff reports no workspace when the environment turns workspace mode off,
// because then there is none: go itself ignores the file, and calling a module "not in the
// workspace" would be answering a question nobody asked.
func TestScanHonoursGOWORKOff(t *testing.T) {
	t.Setenv("GOWORK", "off")

	repo, err := discover.Scan(t.Context(), extractArchive(t, filepath.Join("topologies", "workspace.txtar")))
	require.NoError(t, err)

	assert.Empty(t, repo.Workspace)
	require.Len(t, repo.Modules, 2)

	for _, module := range repo.Modules {
		assert.False(t, module.InWorkspace, "%s", module.Path)
	}
}

// TestScanRecordsUnlistableModule keeps a module whose packages cannot be listed, and says why.
//
// The module here needs a Go the toolchain does not have, which is ordinary in a monorepo mid
// upgrade. GOTOOLCHAIN=local makes go refuse locally instead of going to the network for one.
func TestScanRecordsUnlistableModule(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "local")

	root := t.TempDir()

	write(t, root, "go.mod", "module future.test\n\ngo 1.27\n")
	write(t, root, "ok.go", "package future\n")
	write(t, root, "next/go.mod", "module future.test/next\n\ngo 1.99.0\n")
	write(t, root, "next/next.go", "package next\n")

	repo, err := discover.Scan(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, repo.Modules, 2)

	require.NoError(t, repo.Modules[0].Err, "the healthy module survives its neighbour")
	assert.Len(t, repo.Modules[0].Packages, 1)

	// go's own diagnosis, not "exit status 1": the reason is only ever on stderr.
	require.Error(t, repo.Modules[1].Err)
	assert.Contains(t, repo.Modules[1].Err.Error(), "go.mod requires go >= 1.99.0")
}

// TestScanReportsModuleWithoutPackages distinguishes a module that contributes nothing from one
// that failed. `go list ./...` prints a warning and no packages, which is an answer.
func TestScanReportsModuleWithoutPackages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	write(t, root, "go.mod", "module empty.test\n\ngo 1.27\n")
	write(t, root, "docs/README.md", "no Go here\n")

	repo, err := discover.Scan(t.Context(), root)
	require.NoError(t, err)

	require.Len(t, repo.Modules, 1)
	require.NoError(t, repo.Modules[0].Err)
	assert.Empty(t, repo.Modules[0].Packages)
}

// write creates a file under root, making its directories.
func write(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
