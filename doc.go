// Package prettycov reads the coverage profile `go test -coverprofile` writes and answers the
// question that file cannot: how much of each package is covered.
//
// `go tool cover -func` reports one line per function and a single number at the bottom. There is
// no total per package, and none across packages at all (https://go.dev/issue/66506), so on a real
// repository the output is hundreds of lines a reader has to add up. This package builds the paths
// into a tree, rolls the counts up from the leaves, and hands back a node whose every row is the
// sum of what sits beneath it.
//
// It reads the profile and nothing else — no source tree, no go.mod, no git — so it works on a CI
// artefact, a colleague's file, or a repository that is not checked out. Profiles are untrusted
// input for that reason: a path in one reaches a terminal, and the renderers replace any rune a
// terminal would obey.
//
// # Measuring
//
// [Measure] is the whole of it, applying a profile's rename and exclusions in the one order that is
// correct — statements counted before any flag is judged, the root matched against the whole profile
// rather than against what the patterns left, the tree built last. Its example shows the shape.
//
// The error is the profile being unreadable; everything else comes back as an [Outcome], so a
// caller can report it and carry on. [Measurement.Tree] returns the tree and whether there is one:
// false is when the [Outcome] says why not.
//
// The steps are exported separately — [ParseProfile], [Exclude], [Shorten], [Process] — for a
// caller who wants a different order or only one of them. [Measure] is the order that is right.
//
// # Reading the answer
//
// [PathTree.Get] resolves a path as the report draws it, and also accepts one relative to the
// module root the report collapsed away, so "pkg/logger" works as well as the profile's own
// spelling. A miss is nil, and every method answers for a nil node, so lookups chain without a
// check between them. The exported fields do not: reaching into [PathTree.Coverage] or
// [PathTree.Children] after a miss panics, as reaching into any nil pointer does.
//
// [DisplayTree] draws the rows, [DisplayMisses] prints where the uncovered statements are as
// file:line:col, and [Rows] and [Misses] hand back the same decisions as data for a caller writing
// its own renderer. Both writers return the destination's error: a report written to a full disk
// is not a report that was printed.
//
// All four take [Options], which decides what the output holds — how deep, whether files are drawn,
// whether a subtree already at the bar is left out. The zero value prints the top row alone. It
// shapes and never measures: [PathTree.Percentage] reads the same whatever is drawn.
//
// # Stability
//
// Pre-1.0. Flags, output format and this API may all change between minor versions; what changed
// and what broke is in CHANGELOG.md at the repository root.
package prettycov
