package prettycov

import (
	"regexp"
	"slices"
)

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
		// First match wins, so a file two patterns both take is charged once and the totals still
		// add up to what left the report.
		at := slices.IndexFunc(patterns, func(re *regexp.Regexp) bool { return re.MatchString(item.File) })
		if at < 0 {
			kept = append(kept, item)

			continue
		}

		dropped[at].Files++
		dropped[at].Statements += item.Coverage.Covered + item.Coverage.Uncovered
	}

	return kept, dropped
}

// Exclusion is what one pattern took out.
//
// Per pattern, since unanchored matching needs showing: "cmd/" also takes pkg/subcmd. Zero files
// means a typo, or code that has moved.
type Exclusion struct {
	Pattern    string
	Files      int
	Statements int
}
