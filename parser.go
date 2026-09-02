package prettycov

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/tools/cover"
)

// ErrInvalidProfile reports a profile that could be read but not understood.
//
// The cover package offers nothing to match on: it builds every error with fmt.Errorf and %v, so
// they carry no wrapped chain and there are no exported sentinels or types. Callers would be left
// matching message text, which changes whenever x/tools does. This gives them something stable
// instead. Failures to open the file keep their own cause, so errors.Is(err, fs.ErrNotExist) and
// friends still work.
var ErrInvalidProfile = errors.New("invalid coverage profile")

// ParseProfile reads a coverage profile produced by `go test -coverprofile`.
func ParseProfile(path string) ([]FileCoverage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read coverage profile: %w", err)
	}

	defer func() { _ = file.Close() }()

	profiles, err := cover.ParseProfilesFromReader(file)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidProfile, err)
	}

	return parse(profiles), nil
}

// parse reduces each profile to its statement counts. It cannot fail: every error the format
// admits has already been raised before we see a profile.
func parse(profiles []*cover.Profile) []FileCoverage {
	items := make([]FileCoverage, 0, len(profiles))

	for _, profile := range profiles {
		var covered, uncovered int

		for _, block := range profile.Blocks {
			if block.Count > 0 {
				covered += block.NumStmt
			} else {
				uncovered += block.NumStmt
			}
		}

		items = append(items, FileCoverage{
			File: profile.FileName,
			Coverage: CoverageStats{
				Covered:   covered,
				Uncovered: uncovered,
				Ratio:     ratio(covered, uncovered),
			},
		})
	}

	return items
}
