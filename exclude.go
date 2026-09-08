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
// Patterns match the file's full path, unanchored. The file rather than the package, because ignore
// lists name files: etcd's codecov.yml drops **/*.pb.go.
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
		charged := -1

		// Every pattern is asked, not just up to the first hit: one that only ever matches files
		// an earlier pattern already took is still a working pattern, and reporting it as if it
		// matched nothing reads as a typo. First match wins for the statements, so the totals
		// still add up to what left the report.
		for i, re := range patterns {
			if !re.MatchString(item.File) {
				continue
			}

			if charged >= 0 {
				dropped[i].Overlapped++

				continue
			}

			charged = i
			dropped[i].Files++
			dropped[i].Statements += item.Coverage.Covered + item.Coverage.Uncovered
		}

		if charged < 0 {
			kept = append(kept, item)
		}
	}

	return kept, dropped
}

// Exclusion is what one pattern took out.
//
// Per pattern, since unanchored matching needs showing: "cmd/" also takes pkg/subcmd. Zero files
// and zero overlapped means a typo, or code that has moved.
type Exclusion struct {
	Pattern string
	// Files and Statements are what this pattern was charged, which is what it matched first.
	Files      int
	Statements int
	// Overlapped is what it matched that an earlier pattern had already taken. Separate, because
	// a pattern can be doing its job and still be charged nothing.
	Overlapped int
}
