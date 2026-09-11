package prettycov

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/tools/cover"
)

// ErrInvalidProfile reports a profile that could be read but not parsed.
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
		return nil, fmt.Errorf("%w: %w", ErrInvalidProfile, scrubbed{err})
	}

	return parse(profiles)
}

// scrubbed renders an error with its control characters replaced, and still unwraps to the
// original so errors.Is keeps working. cover formats the offending line into its message, so a
// profile can plant the same escape sequences there that the report itself neutralises.
type scrubbed struct{ err error }

func (s scrubbed) Error() string { return sanitize(s.err.Error()) }
func (s scrubbed) Unwrap() error { return s.err }

// parse reduces each profile to its statement counts. The only error it raises is an overflowing
// total: everything else the format admits has already been rejected before we see a profile.
func parse(profiles []*cover.Profile) ([]FileCoverage, error) {
	items := make([]FileCoverage, 0, len(profiles))

	// One backing array for every file's blocks, sized exactly, so a file costs no allocation of
	// its own. Exact matters: a re-alloc part way would leave the slices already handed out
	// pointing at the old array.
	blocks := 0
	for _, profile := range profiles {
		blocks += len(profile.Blocks)
	}

	arena := make([]Block, 0, blocks)

	// Every later sum — per package, then up the tree — adds a subset of these same blocks, so
	// a running total that stays in range here keeps all of them in range too. cover rejects a
	// negative NumStmt, so adding one can only grow the total or wrap it past zero.
	var total int

	for _, profile := range profiles {
		var covered, uncovered int

		start := len(arena)

		for _, block := range profile.Blocks {
			if total += block.NumStmt; total < 0 {
				return nil, fmt.Errorf("%w: statement counts overflow", ErrInvalidProfile)
			}

			// A block is run or not run, never partly: Count is how many times the whole of it
			// executed, so its statements are all covered or all uncovered.
			stats := CoverageStats{Uncovered: block.NumStmt}
			if block.Count > 0 {
				stats = CoverageStats{Covered: block.NumStmt}
			}

			covered += stats.Covered
			uncovered += stats.Uncovered

			arena = append(arena, Block{
				Line:     block.StartLine,
				Col:      block.StartCol,
				Coverage: stats,
			})
		}

		items = append(items, FileCoverage{
			File: profile.FileName,
			Coverage: CoverageStats{
				Covered:   covered,
				Uncovered: uncovered,
			},
			// Capped, so appending to one file's blocks can never reach into the next file's.
			Blocks: arena[start:len(arena):len(arena)],
		})
	}

	return items, nil
}
