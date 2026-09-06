// Package discover works out what a repository contains before anything is measured.
//
// The go tool answers this only for the module you are standing in. `go list ./...` does not cross
// module boundaries, and `go list -m` reports the module graph, which cannot see a sibling module
// that no go.work mentions. In a repository laid out as services/golang with pkgs/*/go.mod — a
// shape with no workspace file — both report one module where three exist, and neither says so.
//
// So discovery starts from the filesystem, and asks the go tool only about things it is the
// authority on: what a module's path is, and which of its packages carry tests.
package discover

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"

	"github.com/screwyprof/prettycov/internal/gocmd"
)

// ErrNoModulePath reports a go.mod that declares no module. hugo keeps an empty one to fence off
// a directory of C.
var ErrNoModulePath = errors.New("go.mod declares no module path")

// maxParallelList caps how many `go list` subprocesses run at once. See scanModules.
const maxParallelList = 8

// Repo is what a directory tree contains. The workspace lives here rather than on each module,
// because "not in the workspace" and "there is no workspace" are different facts and a bool on
// Module cannot tell them apart: every module in a repository without a go.work would claim the
// same thing as the one module a go.work deliberately leaves out.
type Repo struct {
	// Dir is the root the walk started from.
	Dir string

	// Workspace is the path to the go.work governing this tree, empty when there is none. Only
	// when it is set does Module.InWorkspace carry any meaning.
	Workspace string

	Modules []Module

	// Unreadable are the directories the walk could not descend into. Any of them may hold
	// modules, so they are the difference between a smaller answer and a wrong one.
	//
	// They are errors rather than paths for the same reason Module.Err is: the cause is the
	// actionable part. fs.PathError carries the path and tells a caller whether they need
	// permission or whether the tree moved underneath them.
	Unreadable []error
}

// Module is one go.mod and the packages beneath it, up to the next module.
type Module struct {
	// Path is the module's import path, as declared in its go.mod.
	Path string

	// Dir is the absolute directory holding the go.mod.
	Dir string

	// InWorkspace records whether the repository's go.work lists this module. Meaningless unless
	// Repo.Workspace is set.
	InWorkspace bool

	Packages []Package

	// Err is why this module could not be read, and leaves Path or Packages empty. One unreadable
	// go.mod is not a reason to report nothing about the rest of the tree — a template, a fixture,
	// or a half-finished module would otherwise hide every module beside it.
	Err error
}

// Package is one importable directory. It is discover's own type rather than gocmd's so that the
// two can diverge: gocmd reports what go list says, this reports what a repository contains.
type Package struct {
	ImportPath string
	Dir        string
	HasTests   bool
}

// skipDir reports directories a walk must not descend into. vendor holds whole modules that are
// not yours; the go tool itself ignores testdata and anything starting with . or _, so a module
// parked there is deliberately outside the build.
func skipDir(name string) bool {
	return name == "vendor" || name == "testdata" ||
		strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// Config tunes a scan. Its zero value scans as the go tool would with no flags.
type Config struct {
	// Tags are the build tags the caller will run tests under. Scanning without the tags the tests
	// use silently misses whole suites: delegator keeps its acceptance tests behind //go:build
	// acceptance, and vault, cosmos-sdk and grafana all do the same. See gocmd.Packages.
	Tags []string
}

// Scan finds every module and package in the tree rooted at dir.
func Scan(ctx context.Context, dir string, cfg Config) (Repo, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return Repo{}, fmt.Errorf("resolving %q: %w", dir, err)
	}

	// Through the link, not to it. WalkDir lstats its root, so a symlink to a directory is visited
	// as a single leaf and the walk finds nothing — no modules, no error, no output.
	if resolved, linkErr := filepath.EvalSymlinks(root); linkErr == nil {
		root = resolved
	}

	dirs, unreadable, err := moduleDirs(root)
	if err != nil {
		return Repo{}, err
	}

	workspace, inWorkspace, err := workspaceMembers(ctx, root)
	if err != nil {
		return Repo{}, err
	}

	return Repo{
		Dir:        root,
		Workspace:  workspace,
		Modules:    scanModules(ctx, dirs, inWorkspace, cfg.Tags),
		Unreadable: unreadable,
	}, nil
}

// scanModules reads every module. The work is one `go list` subprocess per module and they do not
// depend on each other, so they run together: on grafana's 39 modules the walk itself costs 13ms
// and the sequential subprocesses cost 5.3s.
//
// The bound is not NumCPU, because the scarce resource is not the CPU. One `go list` over
// grafana's root module peaks at 217MB, and go list is already parallel inside itself — it uses
// 2.17s of CPU for 1.66s of wall clock — so the wall-clock gain flattens long before the memory
// does. Unbounded by cores, a 32-core runner would hold about 7GB of go processes.
//
// Each goroutine writes its own index, which keeps the result in the walk's order.
func scanModules(ctx context.Context, dirs []string, inWorkspace map[string]bool, tags []string) []Module {
	modules := make([]Module, len(dirs))
	limit := make(chan struct{}, min(runtime.NumCPU(), maxParallelList))

	var wg sync.WaitGroup

	for i, dir := range dirs {
		wg.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()

			modules[i] = scanModule(ctx, dir, inWorkspace[dir], tags)
		})
	}

	wg.Wait()

	return modules
}

// scanModule reads one module, recording rather than returning its failure. Scan's own error is
// reserved for what makes the whole tree unreadable.
func scanModule(ctx context.Context, dir string, inWorkspace bool, tags []string) Module {
	module := Module{Dir: dir, InWorkspace: inWorkspace}

	module.Path, module.Err = modulePath(dir)
	if module.Err != nil {
		return module
	}

	rows, err := gocmd.Packages(ctx, dir, tags)
	if err != nil {
		module.Err = err

		return module
	}

	module.Packages = make([]Package, 0, len(rows))
	for _, row := range rows {
		module.Packages = append(module.Packages, Package(row))
	}

	return module
}

// moduleDirs walks for go.mod. A directory holding one is a module root; the walk keeps going
// beneath it, because a module may contain further modules.
//
// A directory it cannot read is noted and stepped over rather than ending the walk. One such
// directory — a root-owned build artefact, a cache — would otherwise hide every module in the
// tree, which is the same trade already made for an unreadable go.mod.
func moduleDirs(root string) (dirs []string, unreadable []error, err error) {
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			// WalkDir reports an error in two places only: on the root, whose entry is then nil,
			// and on a directory whose contents it could not read. A root that cannot be read is
			// not a tree with a gap in it, it is no tree at all.
			if entry == nil {
				return err
			}

			unreadable = append(unreadable, err)

			return filepath.SkipDir
		}

		if entry.IsDir() {
			if path != root && skipDir(entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if entry.Name() == "go.mod" {
			dirs = append(dirs, filepath.Dir(path))
		}

		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("walking %q: %w", root, err)
	}

	return dirs, unreadable, nil
}

// workspaceMembers reads go.work, if there is one, and returns the directories it lists. The file
// is parsed rather than queried through `go list -m`, because the question is what the workspace
// names, not what the module graph resolves to.
//
// modfile is cmd/go's own parser. A hand-rolled scan of the use block read two of grafana's 34
// modules as absent, because it kept the trailing comments those lines carry:
//
//	. // skip:golangci-lint
func workspaceMembers(ctx context.Context, root string) (string, map[string]bool, error) {
	file, err := gocmd.Workspace(ctx, root)
	if err != nil {
		return "", nil, fmt.Errorf("finding the workspace for %q: %w", root, err)
	}

	if file == "" {
		return "", nil, nil
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return "", nil, fmt.Errorf("reading go.work: %w", err)
	}

	work, err := modfile.ParseWork(file, data, nil)
	if err != nil {
		return "", nil, fmt.Errorf("parsing go.work: %w", err)
	}

	// use paths are relative to the go.work, which is not always the scanned root.
	base := filepath.Dir(file)

	members := make(map[string]bool, len(work.Use))
	for _, use := range work.Use {
		members[filepath.Join(base, filepath.FromSlash(use.Path))] = true
	}

	return file, members, nil
}

// modulePath reads the module's own path out of go.mod with cmd/go's parser.
//
// `go list -m` would answer too, at the cost of a subprocess that resolves far more than the
// question needs: a module declaring a Go version this toolchain lacks sends it off to download
// one, so a path sitting in a file on disk becomes a network call that can fail. Parsing also
// says what is wrong — hugo's internal/warpc/genwebp holds an empty go.mod, a fence around a C
// build directory, which is ErrNoModulePath rather than "exit status 1".
func modulePath(dir string) (string, error) {
	file := filepath.Join(dir, "go.mod")

	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}

	// ParseLax reads the module line and ignores the rest. Parse would reject a go.mod using any
	// directive newer than the x/mod pinned here, turning a module the go tool reads perfectly
	// into a broken one — the drift this function avoids shelling out to escape.
	mod, err := modfile.ParseLax(file, data, nil)
	if err != nil {
		return "", fmt.Errorf("parsing go.mod: %w", err)
	}

	if mod.Module == nil || mod.Module.Mod.Path == "" {
		return "", fmt.Errorf("%w: %s", ErrNoModulePath, dir)
	}

	return mod.Module.Mod.Path, nil
}

// Packages iterates every package in the repository, in the order the modules were found, with the
// module each belongs to.
//
// The module comes along because `go test` is module-scoped, so anything that runs the tests has
// to group by it, and the grouping is not recoverable afterwards: matching a package directory
// against the module directories needs the longest prefix, and the obvious shortest-prefix version
// is wrong on exactly the nested layouts this package exists for. Callers that do not care write
// `for _, pkg := range`.
func (r Repo) Packages() iter.Seq2[Module, Package] {
	return func(yield func(Module, Package) bool) {
		for _, module := range r.Modules {
			for _, pkg := range module.Packages {
				if !yield(module, pkg) {
					return
				}
			}
		}
	}
}

// Broken returns the modules that could not be read. They are the difference between a repository
// with less in it than you thought and a report that quietly left some out.
func (r Repo) Broken() []Module {
	var broken []Module

	for _, module := range r.Modules {
		if module.Err != nil {
			broken = append(broken, module)
		}
	}

	return broken
}
