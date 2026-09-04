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

	// Unreadable are directories the walk could not descend into, usually for want of permission.
	// Any of them may hold modules, so they are the difference between a smaller answer and a
	// wrong one, and the report has to say so.
	Unreadable []string
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

// Package is one importable directory.
type Package struct {
	ImportPath string
	Dir        string

	// HasTests is whether any _test.go file exists here. It separates two things a coverage report
	// otherwise renders identically: a package nothing tests, and a package whose tests ran and
	// covered none of it.
	HasTests bool
}

// skipDir reports directories a walk must not descend into. vendor holds whole modules that are
// not yours; the go tool itself ignores testdata and anything starting with . or _, so a module
// parked there is deliberately outside the build.
func skipDir(name string) bool {
	return name == "vendor" || name == "testdata" ||
		strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// Scan finds every module in the tree rooted at dir.
//
// tags are the build tags the caller will run tests under. They are not decoration: a directory
// whose files are all excluded by constraints is not a package at all, so `go list ./...` does not
// match it and -e does not rescue it. Discovery run without the tags the tests use silently misses
// whole suites — delegator keeps its acceptance tests that way.
func Scan(ctx context.Context, dir string, tags ...string) (Repo, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return Repo{}, fmt.Errorf("resolving %q: %w", dir, err)
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
		Modules:    scanModules(ctx, dirs, inWorkspace, tags),
		Unreadable: unreadable,
	}, nil
}

// scanModules reads every module. The work is one `go list` subprocess per module and they do not
// depend on each other, so they run together: on grafana's 39 modules the walk itself costs 13ms
// and the sequential subprocesses cost 5.3s.
//
// Each goroutine writes its own index, which keeps the result in the walk's order.
func scanModules(ctx context.Context, dirs []string, inWorkspace map[string]bool, tags []string) []Module {
	modules := make([]Module, len(dirs))
	limit := make(chan struct{}, runtime.NumCPU())

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
func moduleDirs(root string) (dirs, unreadable []string, err error) {
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			// WalkDir reports an error in two places only: on the root, whose entry is then nil,
			// and on a directory whose contents it could not read. A root that cannot be read is
			// not a tree with a gap in it, it is no tree at all.
			if entry == nil {
				return err
			}

			unreadable = append(unreadable, path)

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
	file, err := workspaceFile(ctx, root)
	if err != nil || file == "" {
		return "", map[string]bool{}, err
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

// workspaceFile asks the go tool which go.work governs root, returning "" when none does.
//
// The file is not always at root: go searches parent directories for it, so scanning a services/
// subdirectory of a workspace still has a workspace. Looking only at root/go.work would report
// every module there as outside a workspace that in fact lists them.
func workspaceFile(ctx context.Context, root string) (string, error) {
	out, err := gocmd.Env(ctx, root, "GOWORK")
	if err != nil {
		return "", fmt.Errorf("finding the workspace for %q: %w", root, err)
	}

	// GOWORK is also how a user turns workspace mode off, and reads back as "off" when they have.
	if out == "off" {
		return "", nil
	}

	return out, nil
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

	mod, err := modfile.Parse(file, data, nil)
	if err != nil {
		return "", fmt.Errorf("parsing go.mod: %w", err)
	}

	if mod.Module == nil || mod.Module.Mod.Path == "" {
		return "", fmt.Errorf("%w: %s", ErrNoModulePath, dir)
	}

	return mod.Module.Mod.Path, nil
}

// Packages iterates every package in the repository, in the order the modules were found.
//
// Almost nothing wants the nesting: a coverage profile names import paths, a report names files,
// and a module is only interesting when something goes wrong with it. Modules remains for when it
// is — Broken is the usual reason.
func (r Repo) Packages() iter.Seq[Package] {
	return func(yield func(Package) bool) {
		for _, module := range r.Modules {
			for _, pkg := range module.Packages {
				if !yield(pkg) {
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

// Complete reports whether the scan saw the whole tree. It is false when a directory could not be
// walked or a module could not be read, which is exactly when a coverage total computed from it
// would be over a denominator nobody chose.
func (r Repo) Complete() bool {
	return len(r.Unreadable) == 0 && len(r.Broken()) == 0
}
