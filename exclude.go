package prettycov

import (
	"errors"
	"regexp"
)

// ErrEmptyExclude reports the empty pattern, which matches every file.
var ErrEmptyExclude = errors.New("want a pattern; an empty one matches every file")

// ParseExclude compiles one exclusion pattern, refusing the empty one.
//
// Here rather than in whatever reads a flag, because it is a fact about what Exclude means: the
// empty pattern takes every file, so the report ends up covering nothing and, with no threshold to
// fail, says so with a zero exit. An unset variable in `prettycov -exclude=$(EXCLUDES)` arrives as
// "" and would turn a coverage gate into a green no-op.
func ParseExclude(s string) (*regexp.Regexp, error) {
	if s == "" {
		//nolint:wrapcheck // a sentinel of this package's own, returned for errors.Is.
		return nil, ErrEmptyExclude
	}

	re, err := regexp.Compile(s)
	if err != nil {
		//nolint:wrapcheck // regexp's message already quotes the expression and the fault in it.
		return nil, err
	}

	return re, nil
}

// Exclude drops every file matching any of patterns, and reports what each one took out.
//
// It filters the parsed profile, not the run. One package's coverage never enters another's ratio,
// so dropping it here gives the same total as leaving it out of -coverpkg — and -coverpkg stays a
// single pattern instead of a list derived out of band, which is what goes stale.
//
// Each pattern is tried unanchored against two strings, and the first that matches decides how
// much goes:
//
//   - the file's full path, "internal/app/version.go". A package is a path its files share,
//     "/pkg/logger/"; files rather than packages, because ignore lists name files — etcd's
//     codecov.yml drops **/*.pb.go.
//   - one block's position, "internal/app/version.go:32:9".
//
// The path is tried first and wins, or a pattern aimed at a package would be charged one block at
// a time. A path holds no colon, so neither can be taken for the other.
func Exclude(items []FileCoverage, patterns []*regexp.Regexp) ([]FileCoverage, []Exclusion) {
	if len(patterns) == 0 {
		return items, nil
	}

	dropped := make([]Exclusion, len(patterns))
	for i, re := range patterns {
		dropped[i].Pattern = re.String()
	}

	kept := make([]FileCoverage, 0, len(items))

	for _, item := range items {
		if chargeFile(dropped, patterns, item) {
			continue
		}

		if trimmed, ok := chargeBlocks(dropped, patterns, item); ok {
			kept = append(kept, trimmed)
		}
	}

	return kept, dropped
}

// chargeFile asks every pattern about the path and reports whether the file goes whole.
//
// Every pattern is asked, not just up to the first hit: one that only ever matches files an earlier
// pattern already took is still a working pattern, and reporting it as if it matched nothing reads
// as a typo. First match wins for the statements, so the totals still add up to what left the
// report.
func chargeFile(dropped []Exclusion, patterns []*regexp.Regexp, item FileCoverage) bool {
	charged := -1

	for i, re := range patterns {
		if !re.MatchString(item.File) {
			continue
		}

		if charged >= 0 {
			dropped[i].OverlappedFiles++

			continue
		}

		charged = i
		dropped[i].Files++
		dropped[i].Statements += item.Coverage.Total()
	}

	return charged >= 0
}

// chargeBlocks takes the blocks a pattern names out of a file the patterns did not take whole, and
// reports whether anything is left to draw. A file whose every block goes is excluded in every
// sense that matters, so it goes too rather than drawing as a row with nothing in it.
//
// A file carrying no blocks is returned untouched: Blocks is optional, and a caller who assembled
// a FileCoverage by hand has no positions to match. Coverage is recomputed only when something was
// actually dropped, so nothing else here can disturb a total it did not change.
func chargeBlocks(
	dropped []Exclusion, patterns []*regexp.Regexp, item FileCoverage,
) (FileCoverage, bool) {
	if len(item.Blocks) == 0 {
		return item, true
	}

	blocks := make([]Block, 0, len(item.Blocks))

	var left CoverageStats

	for _, block := range item.Blocks {
		charged := -1
		at := block.at(item.File)

		for i, re := range patterns {
			if !re.MatchString(at) {
				continue
			}

			if charged >= 0 {
				dropped[i].OverlappedBlocks++

				continue
			}

			charged = i
			dropped[i].Blocks++
			dropped[i].Statements += block.Coverage.Total()
		}

		if charged < 0 {
			blocks = append(blocks, block)
			left.Covered += block.Coverage.Covered
			left.Uncovered += block.Coverage.Uncovered
		}
	}

	switch len(blocks) {
	case len(item.Blocks):
		return item, true
	case 0:
		return FileCoverage{}, false
	default:
		item.Blocks, item.Coverage = blocks, left

		return item, true
	}
}

// Exclusion is what one pattern took out.
//
// Per pattern, since unanchored matching needs showing: "cmd/" also takes pkg/subcmd. Zero files
// and zero overlapped means a typo, or code that has moved.
type Exclusion struct {
	Pattern string
	// Files, Blocks and Statements are what this pattern was charged, which is what it matched
	// first. Files counts whole files; Blocks counts what it took from inside files it did not
	// take whole. Both are reported, because "left out 3 statements" says nothing about whether a
	// package went or three lines did.
	Files      int
	Blocks     int
	Statements int
	// OverlappedFiles and OverlappedBlocks are what it matched that an earlier pattern had already
	// taken. Separate from the charges, because a pattern can be doing its job and still be charged
	// nothing; separate from each other, so the message can say which it was rather than retreating
	// to a word that covers both.
	OverlappedFiles  int
	OverlappedBlocks int
}

// Overlapped is everything this pattern matched that an earlier one had already taken.
func (e Exclusion) Overlapped() int { return e.OverlappedFiles + e.OverlappedBlocks }
