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
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
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

// listArgs is a `go list` over the module in the working directory: -e so a package that fails to
// load stays in the answer rather than losing the whole run, and the tags the caller will build
// under, since a directory whose files are all excluded by constraints is not a package at all.
func listArgs(tmpl string, tags []string, extra ...string) []string {
	args := append([]string{"list", "-e"}, extra...)
	args = append(args, "-f", tmpl)

	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}

	return append(args, "./...")
}

// Packages lists the packages of the module in dir, under the given build tags.
//
// ./... run from a module directory covers exactly that module, which is the one job this pattern
// does correctly. -e keeps a package that fails to load in the answer rather than losing the run.
//
// tags are not decoration: a directory whose files are all excluded by constraints is not a
// package, so ./... does not match it and -e does not rescue it.
func Packages(ctx context.Context, dir string, tags []string) ([]Package, error) {
	out, err := run(ctx, dir, noWorkspace, listArgs(format, tags)...)
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

// TestConfig is one `go test` invocation.
type TestConfig struct {
	// Profile is where go writes the coverage profile. It must be absolute: the command runs in
	// the module's directory, so a relative path would land inside the caller's tree.
	//
	// Empty runs the tests without measuring them at all.
	Profile string

	// Args are forwarded to go test verbatim. This package does not read them.
	Args []string

	// IgnoreWorkspace runs the command as if no go.work existed.
	//
	// Set it for a module the workspace omits, where `./...` otherwise matches nothing at all:
	// "directory prefix . does not contain modules listed in go.work". Leave it clear for a member,
	// whose siblings the workspace is what resolves — a member without replace directives in its
	// go.mod cannot find them any other way.
	IgnoreWorkspace bool

	// Stdout and Stderr receive go test's output as it happens. A run takes minutes, and buffering
	// it to reformat is how a wrapper stops being usable.
	Stdout io.Writer
	Stderr io.Writer
}

// Test runs the tests of the module in dir, under coverage when a Profile is given.
//
// ./... is what a module's whole suite means, and a package pattern the caller also passes is
// harmless since overlapping patterns are absorbed.
//
// Nothing here inspects Args to enforce that. Placing our flags last is enough, and it avoids
// modelling go test's grammar, where telling a flag's value from a package pattern needs the
// arity of every flag we do not own.
//
// The error is non-nil when go test exits non-zero, which includes ordinary test failures. That is
// a result rather than a fault, and the profile may well exist alongside it: a build failure in one
// package still leaves every other package measured.
func Test(ctx context.Context, dir string, cfg TestConfig) error {
	// Ours last. go takes the last of two identical flags, so a caller's -coverprofile would
	// otherwise win and send the data somewhere nothing reads, leaving an empty report and no
	// error to explain it.
	args := append([]string{"test"}, cfg.Args...)

	// No Profile means run the tests and measure nothing. The run still reports its failures, which
	// is the whole reason not to simply skip a module whose coverage nobody wants.
	if cfg.Profile != "" {
		args = append(args, "-coverprofile="+cfg.Profile)
	}

	var env []string
	if cfg.IgnoreWorkspace {
		env = noWorkspace
	}

	return stream(ctx, dir, env, append(args, "./..."), cfg.Stdout, cfg.Stderr)
}

// stream runs the go command in dir, passing its output through as it is produced.
func stream(ctx context.Context, dir string, env, args []string, stdout, stderr io.Writer) error {
	//nolint:gosec // args are this package's literals plus caller-supplied go test flags.
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	// SIGINT rather than the default SIGKILL: go writes the coverage profile incrementally as each
	// test binary finishes, so a clean stop keeps the package that was in flight.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = cancelGrace

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go %s in %q: %w", args[0], dir, err)
	}

	return nil
}

// cancelGrace is how long a cancelled go test has to finish writing before it is killed.
const cancelGrace = 5 * time.Second

// noWorkspace makes a command answer about the module in dir rather than about the workspace.
//
// Inside a module that go.work omits, workspace mode answers about the workspace instead of the
// module you are standing in — `go list -m` there names the workspace's modules, not this one —
// and `go list ./...` fails outright. GOWORK=off asks the module about itself. It does not resolve
// imports between workspace siblings — go.work is what does that, and without it a sibling is
// reachable only through require and replace in go.mod.
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
