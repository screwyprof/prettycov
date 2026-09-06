// Package coverage measures a repository: it runs each module's tests under coverage and reports
// what each run produced.
//
// `go test ./...` covers one module, so a repository of several needs one invocation each.
// `go test ./pkgs/x/...` from a parent module is an error, so nested modules leave no choice.
package coverage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/screwyprof/prettycov/internal/discover"
	"github.com/screwyprof/prettycov/internal/gocmd"
)

// Config is everything the caller decides about a measurement.
type Config struct {
	// Args are forwarded to go test verbatim. This package models none of them; the caller reads
	// exactly one, -tags, to tell discovery which build tags the run will use.
	//
	// CrossModule is the exception, and writes -coverpkg rather than reading one. A -coverpkg
	// already here wins, since go takes the last of two.
	Args []string

	// Exclude skips a module whose directory, relative to the repository root, it matches in full.
	//
	// Modules rather than packages because a module is the unit of invocation: skipping a package
	// inside one saves nothing, since go compiles and reports it either way.
	Exclude *regexp.Regexp

	// Tags are the build tags the run will use, so discovery scans the same package set the tests
	// compile from. A package behind a constraint is not a package at all without them.
	Tags []string

	// Dir holds one profile per module. Created and removed by the caller, since the profiles
	// outlive this call.
	//
	// Empty runs the tests and measures nothing, which is what `go test` does unasked. Coverage
	// costs compilation, so it is requested rather than assumed.
	Dir string

	// Stdout and Stderr receive go test's output as it happens.
	Stdout io.Writer
	Stderr io.Writer
}

// Run is one module's attempt.
type Run struct {
	Module discover.Module

	// Profile is where this module's coverage landed. Empty when go wrote none: the run died, or
	// there was nothing to measure.
	Profile string

	// Err is why this module yielded no usable data, or less than it should have.
	//
	// Independent of Profile: go test exits non-zero when one package fails to build, and still
	// measures the rest.
	Err error
}

// Result is what a measurement amounts to.
type Result struct {
	// Runs is one entry per module that was not excluded, in discovery order.
	Runs []Run

	// Excluded are the modules Config.Exclude matched. Listed, not dropped: skipping one leaves the
	// denominator as well as the run.
	Excluded []discover.Module
}

// Measure runs the tests of every module in repo that Config.Exclude does not match, under coverage
// when Config.Dir is set.
//
// Errors only for what makes the whole measurement impossible. A module that fails is recorded
// against that module and the run continues.
func Measure(ctx context.Context, repo discover.Repo, cfg Config) (Result, error) {
	pass := pass{repo: repo, cfg: cfg}

	var result Result

	for i, module := range repo.Modules {
		if pass.excludes(module) {
			result.Excluded = append(result.Excluded, module)

			continue
		}

		result.Runs = append(result.Runs, pass.measure(ctx, module, i))
	}

	return result, nil
}

// pass is the fixed state of one Measure call, so the per-module decisions read as questions about
// it rather than as arguments passed down.
type pass struct {
	repo discover.Repo
	cfg  Config
}

// excludes reports whether the pattern matches the module's directory, relative to the repository
// root, in full.
//
// Whole-string: an unanchored `cmd/` also takes pkg/subcmd, and `.*test` takes testutil and
// attestation.
func (p pass) excludes(module discover.Module) bool {
	if p.cfg.Exclude == nil {
		return false
	}

	rel, err := filepath.Rel(p.repo.Dir, module.Dir)
	if err != nil {
		return false
	}

	rel = filepath.ToSlash(rel)

	// Anchored, not measured. Go's regexp is leftmost-first, so `services|services/api` matches only
	// "services" of "services/api" and a span check reads that as no match at all.
	return anchored(p.cfg.Exclude).MatchString(rel)
}

// anchored wraps a pattern so it has to match the whole string.
func anchored(re *regexp.Regexp) *regexp.Regexp {
	if full, err := regexp.Compile("^(?:" + re.String() + ")$"); err == nil {
		return full
	}

	return re
}

// measure runs one module, unless discovery already answered the question.
func (p pass) measure(ctx context.Context, module discover.Module, i int) Run {
	// No subprocess for a module whose go.mod declares no module path: discovery established that.
	if module.Err != nil {
		return Run{Module: module, Err: module.Err}
	}

	// Nor for one holding no packages: `go test ./...` exits 1 there, which would read as failure
	// rather than as the empty answer it is.
	if len(module.Packages) == 0 {
		return Run{Module: module}
	}

	profile := ""
	if p.cfg.Dir != "" {
		profile = filepath.Join(p.cfg.Dir, fmt.Sprintf("%04d.out", i))
	}

	err := gocmd.Test(ctx, module.Dir, gocmd.TestConfig{
		Profile:         profile,
		Args:            p.cfg.Args,
		IgnoreWorkspace: p.ignoresWorkspace(module),
		Stdout:          p.cfg.Stdout,
		Stderr:          p.cfg.Stderr,
	})

	return Run{Module: module, Profile: written(profile), Err: err}
}

// ignoresWorkspace reports whether the go command must not see the repository's go.work.
//
// In workspace mode ./... in an omitted module reports "directory prefix . does not contain modules
// listed in go.work". A member may carry no replace directives and needs the workspace to find its
// siblings.
func (p pass) ignoresWorkspace(module discover.Module) bool {
	return p.repo.Workspace != "" && !module.InWorkspace
}

// written returns path when go left a file there, and "" when it did not.
func written(path string) string {
	if _, err := os.Stat(path); err != nil {
		return ""
	}

	return path
}
