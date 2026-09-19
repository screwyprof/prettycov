package prettycov

import (
	"errors"
	"regexp"
	"strings"
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
// there. x/tools parses the filename as a greedy .+, so a profile can name a file "m/a.go:3x/y.go"
// and hand a coordinate pattern the whole file. Profiles are untrusted input here.
//
// Unanchored means the line is a prefix: "a.go:3" reaches 3, 30 and 300; "a.go:3$" is the one line.
func Exclude(items []FileCoverage, patterns []*regexp.Regexp) ([]FileCoverage, []Exclusion) {
	if len(patterns) == 0 {
		return items, nil
	}

	dropped := make([]Exclusion, len(patterns))
	matchers := make([]matcher, len(patterns))

	for i, re := range patterns {
		dropped[i].Pattern = re.String()
		matchers[i] = matcher{re: re, endAnchored: endAnchored(re)}
	}

	kept := make([]FileCoverage, 0, len(items))

	// One buffer for every position built below, since each is thrown away as soon as the patterns
	// have been asked about it. See Block.at. Given a capacity rather than left nil: at[:0] on a nil
	// slice appends into a fresh array every time, which is the allocation this exists to remove.
	// A path longer than this reallocates once per file, since the helpers keep what they grew.
	at := make([]byte, 0, 512)

	for _, item := range items {
		if chargeFile(dropped, matchers, item) {
			noteBlocksAlreadyGone(dropped, matchers, item, at)

			continue
		}

		if trimmed, ok := chargeBlocks(dropped, matchers, item, at); ok {
			kept = append(kept, trimmed)
		}
	}

	return kept, dropped
}

// noteBlocksAlreadyGone credits a pattern naming a block inside a file another pattern took whole.
// Without it such a pattern reports "matched nothing", which invites deleting it, and the day the
// path pattern narrows, the block returns to the denominator. Patterns that took the path are
// skipped, being a prefix of every coordinate in it.
func noteBlocksAlreadyGone(dropped []Exclusion, patterns []matcher, item FileCoverage, at []byte) {
	// A fact about the file, so asked once. Answered here rather than carried from chargeFile.
	tookPath := make([]bool, len(patterns))
	for i, m := range patterns {
		tookPath[i] = m.re.MatchString(item.File)
	}

	for _, block := range item.Blocks {
		withCol, toLine := block.at(at, item.File)
		// Kept, or a path over the buffer's capacity reallocates for every block rather than once.
		at = withCol

		for i, m := range patterns {
			if !tookPath[i] && names(m, withCol, toLine) {
				dropped[i].OverlappedBlocks++
			}
		}
	}
}

// A matcher is one --exclude pattern and whether its second spelling can ever answer differently.
// Built once per run, since Exclude asks every pattern about every block of every file.
type matcher struct {
	re          *regexp.Regexp
	endAnchored bool
}

// endAnchored reports whether a pattern could match a position without its column but not with it.
//
// toLine is a prefix of withCol, and Go's regexp has no lookaround, so any match found inside the
// prefix is a match inside the whole: asking twice can only add an answer for a pattern that
// anchors at the end. Read off the source text and deliberately over-approximating — an escaped
// `\$` or a `$` inside a character class costs one redundant match and nothing else.
func endAnchored(re *regexp.Regexp) bool {
	return strings.Contains(re.String(), "$") || strings.Contains(re.String(), `\z`)
}

// names reports whether the pattern picks out a block at either spelling of its position.
func names(m matcher, withCol, toLine []byte) bool {
	if m.re.Match(withCol) {
		return true
	}

	// Only an end anchor can make the shorter spelling answer differently. See endAnchored.
	return m.endAnchored && m.re.Match(toLine)
}

// chargeFile asks every pattern about the path and reports whether the file goes whole. Every
// pattern is asked, not just up to the first hit, or one that only matches files an earlier pattern
// took would report as a typo. First match wins for the statements, so the totals still add up.
func chargeFile(dropped []Exclusion, patterns []matcher, item FileCoverage) bool {
	charged := -1

	for i, m := range patterns {
		if !m.re.MatchString(item.File) {
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
	dropped []Exclusion, patterns []matcher, item FileCoverage, at []byte,
) (FileCoverage, bool) {
	if len(item.Blocks) == 0 {
		return item, true
	}

	// Nil until a block is actually taken, since most files match no coordinate pattern and the
	// copy is then thrown away: one per kept file, against the parser's one for the whole profile.
	// Staying nil is also how "nothing was charged" is known below.
	var (
		blocks []Block
		left   CoverageStats
	)

	for seen, block := range item.Blocks {
		withCol, toLine := block.at(at, item.File)
		// Kept, for the reason noteBlocksAlreadyGone keeps it.
		at = withCol

		if charged := chargeBlock(dropped, patterns, withCol, toLine, block.Coverage.Total()); charged < 0 {
			left = left.Plus(block.Coverage)

			if blocks != nil {
				blocks = append(blocks, block)
			}

			continue
		}

		// The first block to go is where the copy starts, holding everything kept so far.
		if blocks == nil {
			blocks = make([]Block, seen, len(item.Blocks))
			copy(blocks, item.Blocks[:seen])
		}
	}

	switch {
	// Still nil, so no block was ever charged and the file stands as it came.
	case blocks == nil:
		return item, true
	// Not len(blocks) == 0: a zero-statement block left behind kept an emptied file in the report.
	case left.Total() == 0:
		return FileCoverage{}, false
	default:
		item.Blocks, item.Coverage = blocks, left

		return item, true
	}
}

// chargeBlock asks every pattern about one block's position and reports which was charged, or -1.
// The rule chargeFile applies to a path, applied to a coordinate: every pattern is asked rather
// than stopping at the first, so one that only matches what an earlier took still reports as
// working instead of as a typo, and the first match wins the statements so the totals add up.
func chargeBlock(dropped []Exclusion, patterns []matcher, withCol, toLine []byte, stmts int) int {
	charged := -1

	for i, m := range patterns {
		if !names(m, withCol, toLine) {
			continue
		}

		if charged >= 0 {
			dropped[i].OverlappedBlocks++

			continue
		}

		charged = i
		dropped[i].Blocks++
		dropped[i].Statements += stmts
	}

	return charged
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
