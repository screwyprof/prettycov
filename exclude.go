package prettycov

import (
	"errors"
	"regexp"
)

// ErrEmptyExclude reports the empty pattern, which matches every file.
var ErrEmptyExclude = errors.New("want a pattern; an empty one matches every file")

// ParseExclude compiles one exclusion pattern, refusing the empty one, which takes every file:
// what an unset `--exclude=$(EXCLUDES)` expands to. Refused here so the message names the flag,
// rather than letting the run reach "--exclude left nothing to report" with the cause a step back.
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

// Exclude drops every file matching any of patterns, and reports what each took out. It filters the
// parsed profile, not the run: one package's coverage never enters another's ratio, so this gives
// the same total as leaving it out of -coverpkg.
//
// Each pattern is tried unanchored against the file's path and against each block's position, with
// and without the column. The path is tried first and wins, or a pattern aimed at a package would
// be charged one block at a time. A path `go test` writes holds no colon, so the two do not collide
// there. x/tools parses the filename as a greedy .+, so a FileCoverage a caller built by hand can
// name "m/a.go:3x/y.go" and hand a coordinate pattern the whole file.
//
// Unanchored means the line is a prefix: "a.go:3" reaches 3, 30 and 300; "a.go:3$" is the one line.
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
			noteBlocksAlreadyGone(dropped, patterns, item)

			continue
		}

		if trimmed, ok := chargeBlocks(dropped, patterns, item); ok {
			kept = append(kept, trimmed)
		}
	}

	return kept, dropped
}

// noteBlocksAlreadyGone credits a pattern naming a block inside a file another pattern took whole.
// Without it such a pattern reports "matched nothing", which invites deleting it, and the day the
// path pattern narrows, the block returns to the denominator. Patterns that took the path are
// skipped, being a prefix of every coordinate in it.
func noteBlocksAlreadyGone(dropped []Exclusion, patterns []*regexp.Regexp, item FileCoverage) {
	// A fact about the file, so asked once. Answered here rather than carried from chargeFile.
	tookPath := make([]bool, len(patterns))
	for i, re := range patterns {
		tookPath[i] = re.MatchString(item.File)
	}

	for _, block := range item.Blocks {
		withCol, toLine := block.at(item.File)

		for i, re := range patterns {
			if !tookPath[i] && names(re, withCol, toLine) {
				dropped[i].OverlappedBlocks++
			}
		}
	}
}

// names reports whether the pattern picks out a block at either spelling of its position.
func names(re *regexp.Regexp, withCol, toLine string) bool {
	return re.MatchString(withCol) || re.MatchString(toLine)
}

// chargeFile asks every pattern about the path and reports whether the file goes whole. Every
// pattern is asked, not just up to the first hit, or one that only matches files an earlier pattern
// took would report as a typo. First match wins for the statements, so the totals still add up.
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

// chargeBlocks takes the blocks a pattern names out of a file no pattern took whole, and reports
// whether anything is left to draw. A file carrying no blocks is returned untouched, since Blocks is
// optional. Coverage is recomputed only when something was dropped.
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
		withCol, toLine := block.at(item.File)

		for i, re := range patterns {
			if !names(re, withCol, toLine) {
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
			left = left.Plus(block.Coverage)
		}
	}

	switch {
	case len(blocks) == len(item.Blocks):
		return item, true
	// Not len(blocks) == 0: a zero-statement block left behind kept an emptied file in the report.
	case left.Total() == 0:
		return FileCoverage{}, false
	default:
		item.Blocks, item.Coverage = blocks, left

		return item, true
	}
}

// Exclusion is what one pattern took out. Per pattern, since unanchored matching needs showing:
// "cmd/" also takes pkg/subcmd. Charged and overlapped both zero means a typo, or code that moved.
type Exclusion struct {
	Pattern string
	// What this pattern was charged, being what it matched first. Files and Blocks are counted
	// apart because "left out 3 statements" cannot say whether a package went or three lines did.
	Files      int
	Blocks     int
	Statements int
	// What it matched that another pattern was charged for, usually an earlier one, since first
	// match wins. A path beats a coordinate wherever it sits, so a coordinate pattern can also be
	// overlapped by a later path pattern that took the file whole. Kept apart from the charges,
	// since a pattern can work and still be charged nothing.
	OverlappedFiles  int
	OverlappedBlocks int
}

// Overlapped is everything this pattern matched that another one was charged for.
func (e Exclusion) Overlapped() int { return e.OverlappedFiles + e.OverlappedBlocks }
