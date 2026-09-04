// Package gocmd asks the go command the questions only it can answer, and reads its replies.
//
// It exists so that running the command and understanding its output are separable. The output is
// a text format nothing enforces: a row shape chosen here, printed by a program that is upgraded
// independently of this one. Parsing it is the part that can be wrong without failing, so it is
// the part that is a function of a string rather than of a directory on disk.
package gocmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ErrBadOutput reports a row this package cannot read.
var ErrBadOutput = errors.New("unexpected go list output")

// Package is one importable directory, as go list reports it.
type Package struct {
	ImportPath string
	Dir        string

	// HasTests is whether any _test.go file exists here, in this package or its external test
	// package. It separates two things a coverage report otherwise renders identically: a package
	// nothing tests, and a package whose tests ran and covered none of it.
	HasTests bool
}

// format is the -f template ParsePackages reads: one tab-separated row per package. It lives
// beside the parser because nothing else keeps the two in step.
//
// A template rather than -json because of how each fails when a field goes away: `-json=Bogus`
// prints the other fields and exits 0, while `-f {{.Bogus}}` names the field and exits 1. A
// silently absent TestGoFiles would read as HasTests false, which is the one wrong answer here
// that looks like an answer.
const format = "{{.ImportPath}}\t{{.Dir}}\t{{len .TestGoFiles}}\t{{len .XTestGoFiles}}"

// columns is how many fields format produces.
const columns = 4

// Packages lists the packages of the module in dir, under the given build tags.
//
// ./... run from a module directory covers exactly that module, which is the one job this pattern
// does correctly. -e keeps a package that fails to load in the answer rather than losing the run.
//
// tags are not decoration: a directory whose files are all excluded by constraints is not a
// package, so ./... does not match it and -e does not rescue it.
func Packages(ctx context.Context, dir string, tags []string) ([]Package, error) {
	args := []string{"list", "-e", "-f", format}
	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}

	out, err := run(ctx, dir, noWorkspace, append(args, "./...")...)
	if err != nil {
		return nil, err
	}

	return ParsePackages(out)
}

// ParsePackages reads the rows format produces.
//
// A short row or a count that is not a number is an error rather than a zero. HasTests would
// silently become false, and a package that looks untested is exactly what this distinguishes
// from one that is.
func ParsePackages(out string) ([]Package, error) {
	var packages []Package

	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}

		pkg, err := parsePackage(line)
		if err != nil {
			return nil, err
		}

		packages = append(packages, pkg)
	}

	return packages, nil
}

func parsePackage(line string) (Package, error) {
	fields := strings.Split(line, "\t")
	if len(fields) != columns {
		return Package{}, fmt.Errorf("%w: %q", ErrBadOutput, line)
	}

	tests := 0

	for _, count := range fields[2:] {
		n, err := strconv.Atoi(count)
		if err != nil {
			return Package{}, fmt.Errorf("%w: %q", ErrBadOutput, line)
		}

		tests += n
	}

	return Package{ImportPath: fields[0], Dir: fields[1], HasTests: tests > 0}, nil
}

// Workspace returns the path to the go.work governing dir, empty when none does.
//
// go searches parent directories for the file, so this answers for a subdirectory of a workspace
// too. It is also the one call that must leave the environment alone: noWorkspace would make every
// answer "none", and GOWORK reads back the literal "off" when a caller has disabled workspace mode
// themselves — a go command convention, so it is decoded here rather than by whoever asks.
func Workspace(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, nil, "env", "GOWORK")
	if err != nil {
		return "", err
	}

	if file := strings.TrimSpace(out); file != "off" {
		return file, nil
	}

	return "", nil
}

// noWorkspace makes a command answer about the module in dir rather than about the workspace.
//
// Inside a module that go.work omits, workspace mode answers about the workspace instead of the
// module you are standing in — `go list -m` there names the workspace's modules, not this one —
// and `go list ./...` fails outright. GOWORK=off asks the module about itself, and still resolves
// imports between workspace siblings.
//
//nolint:gochecknoglobals // a constant list, and Go has no constant slices.
var noWorkspace = []string{"GOWORK=off"}

// run executes the go command in dir, with env added to the caller's own.
func run(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	// The arguments are this package's own literals plus caller-supplied build tags and a
	// directory; nothing here comes from a coverage profile or any other untrusted input.
	//nolint:gosec // see above.
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir

	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	out, err := cmd.Output()
	if err != nil {
		// The verb alone: the rest is a -f template that would bury go's own message.
		return "", fmt.Errorf("go %s in %q: %w", args[0], dir, withStderr(err))
	}

	return string(out), nil
}

// withStderr puts go's own diagnosis in the error. "exit status 1" names nothing; the reason —
// "go.mod:1: unknown directive" — is only ever on stderr, which Output leaves on the ExitError.
func withStderr(err error) error {
	exit, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return err
	}

	stderr := bytes.TrimSpace(exit.Stderr)
	if len(stderr) == 0 {
		return err
	}

	return fmt.Errorf("%w: %s", err, stderr)
}
