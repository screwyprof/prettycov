package app

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/screwyprof/prettycov/internal/coverage"
	"github.com/screwyprof/prettycov/internal/discover"
)

// errTooManyRoots reports more than one directory to measure.
var errTooManyRoots = errors.New("want at most one directory")

// measureConfig is what the measure command decides.
type measureConfig struct {
	Root       string
	Profile    string
	GoTestArgs []string
}

// parseMeasure reads the measure command's own flags, and hands everything after a bare -- to
// go test untouched.
//
// -- is split off first because the flag package honours it only within one Parse, and because
// what follows is not ours to interpret.
func parseMeasure(args []string) (measureConfig, error) {
	cfg := measureConfig{Root: "."}

	if end := slices.Index(args, "--"); end >= 0 {
		cfg.GoTestArgs = args[end+1:]
		args = args[:end]
	}

	set := flag.NewFlagSet("prettycov measure", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&cfg.Profile, "profile", "",
		"join the modules' coverage into this `file`; without it the tests run unmeasured")

	// Interspersed, so the directory may come before or after the flags. The flag package stops at
	// the first non-flag argument, which would otherwise make `measure ./sub -dir=x` silently treat
	// -dir as a second directory.
	roots, err := parseInterspersed(set, args)
	if err != nil {
		return measureConfig{}, err
	}

	if len(roots) > 1 {
		//nolint:wrapcheck // a sentinel with nothing to add: more than one directory was named.
		return measureConfig{}, errTooManyRoots
	}

	if len(roots) == 1 {
		cfg.Root = roots[0]
	}

	return cfg, nil
}

// runMeasure runs every discovered module's tests under coverage and leaves one profile per module.
//
// It renders nothing. go test's streams pass through untouched — stdout because a -json consumer is
// reading it and we cannot know what else is — and its exit status is this command's.
func runMeasure(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseMeasure(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "prettycov: %v\n", err)

		return exitFailed
	}

	if carriesCoverProfile(cfg.GoTestArgs) {
		_, _ = fmt.Fprintln(stderr,
			"prettycov: -coverprofile cannot be passed through; measure runs one go test per "+
				"module, so use -profile for the joined result")

		return exitFailed
	}

	ctx := context.Background()
	tags := tagsFrom(cfg.GoTestArgs)

	repo, err := discover.Scan(ctx, cfg.Root, discover.Config{Tags: tags})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "prettycov: %v\n", err)

		return exitFailed
	}

	// Nothing to measure is not a measurement. Writing the profile anyway truncates whatever was
	// there to a bare mode line, and a later -fail-under then reports "no statements to cover" with
	// nothing to say why.
	if len(repo.Modules) == 0 {
		_, _ = fmt.Fprintf(stderr, "prettycov: no modules under %s\n", cfg.Root)

		return exitFailed
	}

	result, err := measureRepo(ctx, repo, cfg, tags, stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "prettycov: %v\n", err)

		return exitFailed
	}

	return reportMeasure(repo, result, stderr)
}

// measureRepo runs every module and joins whatever they wrote.
//
// The per-module profiles live in a directory of our own, removed on the way out: they are an
// intermediate, and the artifact is the one file they are joined into.
func measureRepo(ctx context.Context, repo discover.Repo, cfg measureConfig, tags []string,
	stdout, stderr io.Writer,
) (coverage.Result, error) {
	dir, cleanup, err := profileDir(cfg.Profile)
	if err != nil {
		return coverage.Result{}, err
	}

	defer cleanup()

	result, err := coverage.Measure(ctx, repo, coverage.Config{
		Args:   cfg.GoTestArgs,
		Tags:   tags,
		Dir:    dir,
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		//nolint:wrapcheck // the caller prints it; there is nothing to add.
		return coverage.Result{}, err
	}

	if cfg.Profile == "" {
		return result, nil
	}

	if err := joinProfiles(result, cfg.Profile); err != nil {
		return coverage.Result{}, err
	}

	return result, nil
}

// carriesCoverProfile reports whether the caller passed a -coverprofile of their own.
//
// One go test per module means one profile path per module, so theirs cannot be honoured: the next
// module would truncate it. Ours wins by position, which would leave theirs looking accepted.
func carriesCoverProfile(args []string) bool {
	return slices.ContainsFunc(args, func(a string) bool {
		return a == "-coverprofile" || strings.HasPrefix(a, "-coverprofile=")
	})
}

// profileDir is where the per-module profiles go, empty when no coverage was asked for.
//
// Instrumenting costs compilation, and `go test` does not do it unasked either.
func profileDir(profile string) (dir string, cleanup func(), err error) {
	if profile == "" {
		return "", func() {}, nil
	}

	dir, err = os.MkdirTemp("", "prettycov-")
	if err != nil {
		return "", func() {}, fmt.Errorf("cannot create a directory for the profiles: %w", err)
	}

	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// reportMeasure says what could not be measured, and grades the run.
//
// A module that failed is go's answer, so the status is go's: 1 for anything wrong, which is all it
// distinguishes. exitFailed is kept for prettycov's own failures, which go test never returns.
func reportMeasure(repo discover.Repo, result coverage.Result, stderr io.Writer) int {
	failed := false

	for _, run := range result.Runs {
		switch {
		case run.Err != nil:
			failed = true

			_, _ = fmt.Fprintf(stderr, "prettycov: %s: %s\n", name(repo, run.Module), lastLine(run.Err))
		case run.Profile == "":
			_, _ = fmt.Fprintf(stderr, "prettycov: %s: nothing to measure\n", name(repo, run.Module))
		}
	}

	for _, module := range result.Excluded {
		_, _ = fmt.Fprintf(stderr, "prettycov: %s: excluded\n", name(repo, module))
	}

	for _, err := range repo.Unreadable {
		_, _ = fmt.Fprintf(stderr, "prettycov: %v\n", err)
	}

	if failed {
		return exitBelow
	}

	return exitOK
}

// joinProfiles writes every module's profile into one, which is what a reader expects and what
// other tools consume.
//
// Concatenation, not arithmetic: a block appears once per test binary that instrumented it, and
// cover merges the repeats when the file is read. Adding them instead is how a denominator ends up
// several times its real size.
func joinProfiles(result coverage.Result, path string) error {
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("cannot write the coverage profile: %w", err)
	}

	defer func() { _ = out.Close() }()

	if err := writeJoined(result, out); err != nil {
		return fmt.Errorf("cannot write the coverage profile: %w", err)
	}

	return nil
}

// writeJoined copies every module's blocks under a single mode line.
func writeJoined(result coverage.Result, out io.Writer) error {
	wrote := false

	for _, run := range result.Runs {
		if run.Profile == "" {
			continue
		}

		if err := appendProfile(out, run.Profile, &wrote); err != nil {
			return err
		}
	}

	if wrote {
		return nil
	}

	// Nothing was measured, and a profile without a mode line cannot be parsed at all.
	_, err := io.WriteString(out, "mode: set\n")

	//nolint:wrapcheck // the caller names the operation.
	return err
}

// appendProfile copies one module's blocks, emitting the mode line only for the first.
func appendProfile(out io.Writer, path string, wrote *bool) error {
	blocks, err := os.ReadFile(path)
	if err != nil {
		//nolint:wrapcheck // the caller names the operation; this is one of its steps.
		return err
	}

	if line, rest, found := bytes.Cut(blocks, []byte("\n")); found && bytes.HasPrefix(line, []byte("mode: ")) {
		if !*wrote {
			if _, wErr := out.Write(append(bytes.Clone(line), '\n')); wErr != nil {
				//nolint:wrapcheck // as above.
				return wErr
			}

			*wrote = true
		}

		blocks = rest
	}

	_, err = out.Write(blocks)

	//nolint:wrapcheck // as above.
	return err
}

// tagsFrom pulls -tags out of the arguments meant for go test.
//
// Discovery has to scan under the tags the run will use, and so does the dependency walk -coverpkg
// is built from: a package behind a constraint is not a package at all otherwise, and leaves the
// denominator before anything runs. This is the one flag of go's this command reads.
func tagsFrom(args []string) []string {
	// The last one, because that is the one go will use: the flag package overwrites on each
	// occurrence. Reading the first would scan under tags the tests are not compiled with.
	var tags []string

	for i, arg := range args {
		switch {
		case strings.HasPrefix(arg, "-tags="), strings.HasPrefix(arg, "--tags="):
			_, value, _ := strings.Cut(arg, "=")
			tags = strings.Split(value, ",")
		case (arg == "-tags" || arg == "--tags") && i+1 < len(args):
			tags = strings.Split(args[i+1], ",")
		}
	}

	return tags
}

// name is the module's directory relative to the repository root. Its path is empty when the go.mod
// could not be read, which is exactly when a name is most wanted.
func name(repo discover.Repo, module discover.Module) string {
	rel, err := filepath.Rel(repo.Dir, module.Dir)
	if err != nil {
		return module.Dir
	}

	return filepath.ToSlash(rel)
}

// lastLine is the end of go's complaint, which is the part that names the cause.
func lastLine(err error) string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")

	return strings.TrimSpace(lines[len(lines)-1])
}
