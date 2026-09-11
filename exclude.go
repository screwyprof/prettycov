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
// Each pattern is tried unanchored against two strings, and which one matches decides how much
// goes:
//
//   - the file's full path, "internal/app/version.go". A package is a path its files share,
//     "/pkg/logger/"; files rather than packages, because ignore lists name files — etcd's
//     codecov.yml drops **/*.pb.go.
//   - one block's position, "internal/app/version.go:32:9", and the same without the column, so a
//     pattern can anchor on the line.
//
// The path is tried first and wins, or a pattern aimed at a package would be charged one block at
// a time. A path `go test` writes holds no colon, so neither can be taken for the other — x/tools
// parses the filename as a greedy .+ and would accept one, and a hand-written profile naming
// "m/a.go:3x/y.go" hands a coordinate pattern the whole file. The report says so: 1 file, not 1
// block. A pattern naming a block inside a file some other pattern took whole is still credited,
// as an overlap.
//
// Unanchored means the line is a prefix: "a.go:3" reaches 3, 30 and 300. "a.go:3$" is the one
// line.
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

// noteBlocksAlreadyGone credits a pattern that names a block inside a file another pattern took
// whole. Without it such a pattern reports "matched nothing", which reads as a typo and invites
// deleting it — and the day the path pattern narrows, the block silently returns to the
// denominator. Patterns that took the path are skipped: a path is a prefix of every coordinate in
// its file, so they would be charged twice for the same match.
func noteBlocksAlreadyGone(dropped []Exclusion, patterns []*regexp.Regexp, item FileCoverage) {
	// Asked once, not once per block: whether a pattern took the path is a fact about the file.
	// Answered here rather than carried from chargeFile, so neither has to know what the other did.
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
// reports whether anything is left to draw. A file with no statements left is excluded in every
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
			left.Add(block.Coverage)
		}
	}

	switch {
	case len(blocks) == len(item.Blocks):
		return item, true
	// Not len(blocks) == 0: cmd/cover emits blocks declaring no statements, and one of those left
	// behind kept a file in the report that the patterns had taken everything from.
	case left.Total() == 0:
		return FileCoverage{}, false
	default:
		item.Blocks, item.Coverage = blocks, left

		return item, true
	}
}

// Exclusion is what one pattern took out.
//
// Per pattern, since unanchored matching needs showing: "cmd/" also takes pkg/subcmd. Charged
// nothing and overlapped nothing — no files, no blocks — means a typo, or code that has moved.
type Exclusion struct {
	Pattern string
	// Files, Blocks and Statements are what this pattern was charged, which is what it matched
	// first. Files counts whole files; Blocks counts what it took from inside files it did not
	// take whole. Both are reported, because "left out 3 statements" says nothing about whether a
	// package went or three lines did.
	Files      int
	Blocks     int
	Statements int
	// OverlappedFiles and OverlappedBlocks are what it matched that another pattern was charged
	// for. Usually an earlier one, which is what first-match-wins means among equals — but a path
	// takes precedence over a coordinate wherever it sits in the list, so a pattern naming a block
	// can be overlapped by a later one that took the file whole.
	//
	// Separate from the charges, because a pattern can be doing its job and still be charged
	// nothing; separate from each other, so the message can say which it was rather than retreating
	// to a word that covers both.
	OverlappedFiles  int
	OverlappedBlocks int
}

// Overlapped is everything this pattern matched that another one was charged for.
func (e Exclusion) Overlapped() int { return e.OverlappedFiles + e.OverlappedBlocks }
