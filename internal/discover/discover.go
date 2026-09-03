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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	errNoModulePath  = errors.New("go.mod declares no module path")
	errBadListOutput = errors.New("unexpected go list output")
)

// Module is one go.mod and the packages beneath it, up to the next module.
type Module struct {
	// Path is the module's import path, as declared in its go.mod.
	Path string

	// Dir is the absolute directory holding the go.mod.
	Dir string

	// InWorkspace records whether a go.work at the root lists this module. False for every module
	// when there is no workspace at all.
	InWorkspace bool

	Packages []Package
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

// Modules finds every module in the tree rooted at dir.
func Modules(ctx context.Context, dir string) ([]Module, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving %q: %w", dir, err)
	}

	dirs, err := moduleDirs(root)
	if err != nil {
		return nil, err
	}

	inWorkspace, err := workspaceMembers(root)
	if err != nil {
		return nil, err
	}

	modules := make([]Module, 0, len(dirs))

	for _, moduleDir := range dirs {
		path, err := modulePath(ctx, moduleDir)
		if err != nil {
			return nil, err
		}

		packages, err := packagesIn(ctx, moduleDir)
		if err != nil {
			return nil, err
		}

		modules = append(modules, Module{
			Path:        path,
			Dir:         moduleDir,
			InWorkspace: inWorkspace[moduleDir],
			Packages:    packages,
		})
	}

	return modules, nil
}

// moduleDirs walks for go.mod. A directory holding one is a module root; the walk keeps going
// beneath it, because a module may contain further modules.
func moduleDirs(root string) ([]string, error) {
	var dirs []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
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
		return nil, fmt.Errorf("walking %q: %w", root, err)
	}

	return dirs, nil
}

// workspaceMembers reads go.work, if there is one, and returns the directories it lists. The file
// is parsed rather than queried through `go list -m`, because the question is what the workspace
// names, not what the module graph resolves to.
func workspaceMembers(root string) (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.work"))
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("reading go.work: %w", err)
	}

	members := map[string]bool{}
	inUseBlock := false

	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)

		switch {
		case line == "use (":
			inUseBlock = true
		case inUseBlock && line == ")":
			inUseBlock = false
		case inUseBlock && line != "":
			members[filepath.Join(root, filepath.FromSlash(line))] = true
		case strings.HasPrefix(line, "use "):
			members[filepath.Join(root, filepath.FromSlash(strings.TrimSpace(line[4:])))] = true
		}
	}

	return members, nil
}

// modulePath asks the go tool for the module's own path rather than parsing go.mod, so the answer
// stays right as the file format grows.
func modulePath(ctx context.Context, dir string) (string, error) {
	out, err := goList(ctx, dir, "-m", "-f", "{{.Path}}")
	if err != nil {
		return "", err
	}

	path := strings.TrimSpace(out)
	if path == "" {
		return "", fmt.Errorf("%w: %s", errNoModulePath, dir)
	}

	return path, nil
}

// packagesIn lists the module's own packages. Run from the module directory, ./... covers exactly
// it and nothing else — which is the one job this pattern does correctly.
func packagesIn(ctx context.Context, dir string) ([]Package, error) {
	const format = "{{.ImportPath}}\t{{.Dir}}\t{{len .TestGoFiles}}\t{{len .XTestGoFiles}}"

	out, err := goList(ctx, dir, "-e", "-f", format, "./...")
	if err != nil {
		return nil, err
	}

	var packages []Package

	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("%w: %q", errBadListOutput, line)
		}

		internal, _ := strconv.Atoi(fields[2])
		external, _ := strconv.Atoi(fields[3])

		packages = append(packages, Package{
			ImportPath: fields[0],
			Dir:        fields[1],
			HasTests:   internal+external > 0,
		})
	}

	return packages, nil
}

// goList runs `go list` in dir with the workspace disabled. Inside a module that go.work omits,
// workspace mode answers about the workspace instead of the module you are standing in — `go list
// -m` there names the workspace's modules, not this one — and `go list ./...` fails outright.
// GOWORK=off asks the module about itself, and still resolves imports between workspace siblings.
func goList(ctx context.Context, dir string, args ...string) (string, error) {
	// The arguments are this package's own literals and a caller-supplied directory; nothing here
	// comes from a coverage profile or any other untrusted input.
	//nolint:gosec // see above.
	cmd := exec.CommandContext(ctx, "go", append([]string{"list"}, args...)...)
	cmd.Dir = dir

	cmd.Env = append(os.Environ(), "GOWORK=off")

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list in %q: %w", dir, err)
	}

	return string(out), nil
}
