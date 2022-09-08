package prettycov

import (
	"fmt"

	"golang.org/x/tools/cover"
)

func ParseProfile(path string) ([]FileCoverage, error) {
	profiles, err := cover.ParseProfiles(path)
	if err != nil {
		return nil, fmt.Errorf("cannot parse coverage profile: %w", err)
	}

	return parse(profiles)
}

func parse(profiles []*cover.Profile) ([]FileCoverage, error) {
	items := make([]FileCoverage, 0, len(profiles))

	for _, p := range profiles {
		var (
			covered   int
			uncovered int
		)

		for _, block := range p.Blocks {
			if block.Count > 0 {
				covered += block.NumStmt
			} else {
				uncovered += block.NumStmt
			}
		}

		coverage := float64(covered) / float64(covered+uncovered) * 100

		items = append(items, FileCoverage{
			File: p.FileName,
			Coverage: CoverageStats{
				Covered:   covered,
				Uncovered: uncovered,
				Ratio:     coverage,
			},
		})
	}

	return items, nil
}
