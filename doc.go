// Package prettycov reads the coverage profile that `go test -coverprofile` writes and reports how
// much of each package is covered. The profile itself does not say.
//
// `go tool cover -func` prints one line per function and one number at the bottom. It gives no
// total per package, and none across packages (https://go.dev/issue/66506), so on a real repository
// you get hundreds of lines to add up yourself. This package builds the profile's paths into a tree
// and rolls the counts up from the leaves, so every row is the sum of everything below it.
//
// It reads the profile and nothing else. There is no source tree to find and no go.mod to parse, so
// it works on a CI artefact or on a repository you have not checked out. That also makes a profile
// untrusted input: a path in one reaches a terminal, so the renderers replace any rune a terminal
// would obey.
//
// # Measuring
//
// [Measure] does all of it. It applies a profile's rename and exclusions in the order that gives
// the right answer: statements are counted before any flag is judged, the root is matched against
// the whole profile instead of against what the patterns left, and the tree is built last. See its
// example.
//
// The error means the profile could not be read. Everything else comes back as a [Failure], so a
// caller can report it and carry on. [Measurement.Tree] returns the tree and whether there is one;
// when there is not, [Measurement.Failure] says why.
//
// [ParseProfile], [Exclude], [Shorten] and [Process] are exported separately, for a caller who
// wants one of them or a different order.
//
// # Reading the answer
//
// [PathTree.Get] resolves a path the way the report prints it. It also accepts a path relative to
// the module root the report collapsed away, so "pkg/logger" works as well as the profile's own
// spelling. A miss returns nil and every method answers for a nil node, so lookups chain without a
// check in between. The exported fields are not nil-safe: reading [PathTree.Coverage] or
// [PathTree.Children] after a miss panics, as reading through any nil pointer does.
//
// [DisplayTree] draws the rows and [DisplayMisses] prints the uncovered statements as
// file:line:col. [Rows] and [Misses] return the same decisions as data, for a caller writing its own
// renderer. Both writers return the destination's error. The count beside it is of rows decided
// rather than bytes that landed, since bufio holds a write failure until Flush, so read the error.
//
// All four take [Options], which decides how deep the output goes and whether a subtree already at
// the bar is left out. The zero value draws the top row alone, which for [Misses] and
// [DisplayMisses] is nothing at all, since Depth 0 never reaches a file. Both ignore Options.Files,
// because a miss is a file position whether or not a file is drawn as a row.
//
// Options changes what is shown, never what is counted. [PathTree.Percentage] reads the same
// whatever is drawn.
//
// # Stability
//
// Pre-1.0. Flags, output format and this API may all change between minor versions. CHANGELOG.md at
// the repository root records what changed and what broke.
package prettycov
