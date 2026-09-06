package prettycov_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/screwyprof/prettycov"
)

// Exclusion is what lets a project stop counting code it never intended to test, which is most of
// the distance between a hand-written `go list | grep -v` recipe and this tool's number.
func TestExcludeDropsMatchingPackages(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{
		file("example.com/p/cmd/web/main.go", 0, 0),
		file("example.com/p/cmd/scraper/main.go", 0, 0),
		file("example.com/p/web/handler/handler.go", 0, 0),
		file("example.com/p/web/config/config.go", 0, 0),
		file("example.com/p/web/api/api.pb.go", 0, 0),
	}

	tests := []struct {
		name string
		re   string
		want []string
	}{
		// Unanchored, so one short pattern reaches every command in the tree.
		{name: "a directory anywhere", re: "cmd/", want: []string{
			"example.com/p/web/handler/handler.go", "example.com/p/web/config/config.go",
			"example.com/p/web/api/api.pb.go",
		}},
		{name: "one package by path", re: "web/config", want: []string{
			"example.com/p/cmd/web/main.go", "example.com/p/cmd/scraper/main.go",
			"example.com/p/web/handler/handler.go", "example.com/p/web/api/api.pb.go",
		}},
		// File names are matchable, which is what real ignore lists are written against: etcd's
		// codecov.yml drops **/*.pb.go, and no pattern over package paths could say that.
		{name: "a file suffix anywhere", re: `\.pb\.go$`, want: []string{
			"example.com/p/cmd/web/main.go", "example.com/p/cmd/scraper/main.go",
			"example.com/p/web/handler/handler.go", "example.com/p/web/config/config.go",
		}},
		{name: "everything", re: "example.com", want: []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, _ := prettycov.Exclude(items, []*regexp.Regexp{regexp.MustCompile(tc.re)})

			files := make([]string, 0, len(got))
			for _, item := range got {
				files = append(files, item.File)
			}

			assert.Equal(t, tc.want, files)
		})
	}
}

// No pattern is the common case, and it must not copy or reorder anything.
func TestExcludeWithoutAPatternKeepsEverything(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{file("example.com/p/a.go", 0, 0), {File: "example.com/p/b.go"}}

	kept, dropped := prettycov.Exclude(items, nil)

	assert.Equal(t, items, kept)
	assert.Empty(t, dropped)
}

// What each pattern removed is the whole mitigation for unanchored matching: "cmd/" also taking
// pkg/subcmd is invisible in a total and obvious beside the pattern that did it.
func TestExcludeReportsWhatEachPatternRemoved(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{
		file("ex.com/p/cmd/web/main.go", 1, 9),
		file("ex.com/p/pkg/subcmd/run.go", 4, 0),
		file("ex.com/p/api/api.pb.go", 0, 7),
		file("ex.com/p/web/handler.go", 5, 5),
	}

	kept, dropped := prettycov.Exclude(items, []*regexp.Regexp{
		regexp.MustCompile("cmd/"),
		regexp.MustCompile(`\.pb\.go$`),
		regexp.MustCompile("nothing-is-here"),
	})

	assert.Len(t, kept, 1, "only the handler survives")

	assert.Equal(t, []prettycov.Exclusion{
		// Two files, because unanchored "cmd/" took pkg/subcmd as well. Saying so is the point.
		{Pattern: "cmd/", Files: 2, Statements: 14},
		{Pattern: `\.pb\.go$`, Files: 1, Statements: 7},
		{Pattern: "nothing-is-here", Files: 0, Statements: 0},
	}, dropped)
}

// A file two patterns both match is charged once, so the reported statements add up to what left.
// The loser is still credited with the match: a pattern that only ever meets files an earlier one
// already took is working, and reporting it as matching nothing sends someone to fix what is right.
func TestExcludeChargesAnOverlappingFileOnce(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{
		file("ex.com/p/cmd/gen.pb.go", 2, 3),
	}

	_, dropped := prettycov.Exclude(items, []*regexp.Regexp{
		regexp.MustCompile("cmd/"),
		regexp.MustCompile(`\.pb\.go$`),
	})

	assert.Equal(t, []prettycov.Exclusion{
		{Pattern: "cmd/", Files: 1, Statements: 5},
		{Pattern: `\.pb\.go$`, Overlapped: 1},
	}, dropped)
}

// Overlapping and matching nothing are different states, and only the second is a typo.
func TestExcludeSeparatesOverlapFromNoMatch(t *testing.T) {
	t.Parallel()

	items := []prettycov.FileCoverage{file("ex.com/p/cmd/gen.pb.go", 2, 3)}

	_, dropped := prettycov.Exclude(items, []*regexp.Regexp{
		regexp.MustCompile("cmd/"),
		regexp.MustCompile(`\.pb\.go$`),
		regexp.MustCompile("typo"),
	})

	assert.Equal(t, 1, dropped[1].Overlapped, "matched, but an earlier pattern was charged")
	assert.Equal(t, 0, dropped[2].Overlapped, "never matched at all")
}
