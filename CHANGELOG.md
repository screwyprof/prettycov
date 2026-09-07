# Changelog

Pre-1.0: flags, output format and the Go API may all change between minor versions.

Entries that change what an existing invocation prints or accepts are labelled **Breaking**,
because a version number cannot say it — Go modules have no sub-major inside v0, so `go get -u`
walks across any of these without comment. Read the labels, not the digits.

Only user-visible changes are listed; `git log` has the rest. Releases before 0.5.0 had no notes,
so those entries are reconstructed from the history and checked against binaries built from the
tags.

## Unreleased

### Added

- `-depth=max` shows the whole tree. The README told people to set `-depth` past the bottom to see
  everything, and guessing that number is wrong in both directions: too small truncates without
  saying it did, and on kubernetes — 3232 rows — even 9 is six levels short.

  The default stays 1. Measured across 16 repositories it is the only fixed value that stays on a
  screen everywhere: worst case is hugo at 37 rows, where depth 2 gives 152 and gitea 196.

- `make publish` requests the current `./VERSION` from proxy.golang.org, and `make release` now
  runs it after pushing the tag. proxy.golang.org caches a version the first time anyone asks for
  it and index.golang.org lists what the proxy learned, which is what pkg.go.dev builds from — so
  without it a release stayed unpublished until some user pulled it through by accident.

## [0.6.0] — 2026-09-07

### Added

- `-total` prints the total percentage and nothing else, so a Makefile or a badge can read it. It
  changes how the report is printed, not what gets measured: it is the whole profile's total, so
  `-exclude` changes it just as it changes the tree and `-fail-under` still grades it. That total
  is the tree's top row, except on a profile spanning two top-level paths, where it is the union of
  both roots and so appears in no row.

  Two decimals, rendered by the same code as the tree, so a summary line cannot disagree with the
  report it summarises by rounding — `go tool cover -func | awk 'END{print $NF}'` gives one decimal
  and a `%` sign. A profile with nothing to cover exits 2 rather than printing `n/a` or `0.00` into
  a shell variable, unless `-fail-under` was given, in which case that reports instead.

  The figure is rounded to two decimals while `-fail-under` compares the exact ratio, so 79.999%
  prints as `80.00` on a run the gate fails. Use `-fail-under` rather than comparing the printed
  number to a threshold.

### Go API

- `Percentage(stats CoverageStats) (string, bool)` renders a ratio the way every row does, so a
  caller of the library shows the same figures the report does. `ok` is false when there is nothing
  to cover, which is not 0%. Additive: nothing existing changed shape.

### Changed

- **Breaking:** `100.00` is never rounded up to, in the tree or in `-total`: 73999 of 74000
  statements now reads `99.99`. It claimed full coverage for code that was not fully covered, and
  100% is what a badge shows and what stops someone writing another test.

  Only ratios in `(99.995, 100)` change, so nothing this repository or its fixtures report moves.
  It is a deliberate divergence from `go tool cover -func`, which rounds at one decimal and prints
  `100.0%` from 99.95% upwards.

## [0.5.0] — 2026-09-06

### Added

- `-exclude=REGEXP` drops files whose path matches, before anything is totalled. Unanchored, matched
  against the full path, repeatable. Each pattern reports on stderr what it took out — including
  nothing, which is how a typo shows up rather than reading as a lower number.

  An empty pattern is refused with exit 2 rather than honoured. It matches every file, so it would
  empty the report and, with no `-fail-under`, exit 0 having measured nothing — which is what an
  unset variable in `prettycov -exclude=$(EXCLUDES)` would have done.

  It filters the report, not the profile on disk: `go tool cover -html` and anything else reading
  the file still sees everything in it.

## [0.4.1] — 2026-09-06

Build only. No Go source changed, so prettycov prints for any given profile exactly what 0.4.0
printed.

- Pinned Go 1.26 for building and testing prettycov itself. Go 1.27 splits a straight-line block at
  blank lines and writes the whole run's statement count into each piece ([golang/go#80974][80974]),
  which inflated this repository's own CI figure: 99.61% on 1.27 against 99.36% on 1.26.

  Not a prettycov change, but worth knowing — a profile *you* produce with a 1.27 toolchain carries
  the same inflation, and nothing downstream can correct it.

## [0.4.0] — 2026-09-03

### Fixed

- `-fail-under=nan` exited 0 at any coverage: `total < NaN` is false, so the gate silently passed.
  `-Inf`, negatives and values above 100 did the same. All are refused now, so a CI config
  templating a bad value fails loudly instead of never failing. **Breaking** for anyone whose gate
  was passing only because its threshold was nonsense.

## [0.3.1] — 2026-09-03

### Fixed

- Control characters coming out of the profile are neutralised. `cover` quotes the offending line
  into its error message, so a corrupt or hostile profile could write ANSI escapes straight to the
  terminal through prettycov's error path.
- A profile whose statement counts overflow when summed is refused rather than reported. One such
  profile printed `-461168601842738790400.00`; another read 100.00%, its uncovered statements having
  wrapped past zero.
- `-old` matches on a path boundary. `-old=github.com/foo` used to rewrite the unrelated
  `github.com/foobar`; a trailing slash is trimmed, so `-old=github.com/foo/` means the same thing.
  **Breaking** if you relied on the mid-segment match.
- `-new` without `-old` no longer prepends itself to every path.
- `-help` printed its usage text with an empty flag list, and sent it to stderr — so
  `prettycov -help | less` showed nothing.
- `prettycov -- a.out -depth=2` set the depth instead of reporting two profile paths.
- `prettycov version` could print a bare newline, which a script would read as a version string.

### Go API

- **Breaking:** `PkgCoverage` removed, and `PathTree.Put` unexported to `put`.

## [0.3.0] — 2026-09-03

### Added

- `-fail-under=N` exits 1 when total coverage is below N, which is what turns this from something
  you look at into something you put in CI. Exit 1 stays distinct from exit 2, so a step can tell
  "coverage dropped" from "prettycov could not run".
- `-color=auto|never|always`, grading percentages red, yellow and green at 50 and 80. Base ANSI
  only, so your terminal theme decides the shades. Off when piped, honouring `NO_COLOR` and
  `TERM=dumb`.
- `-h` as shorthand for `-help`.

### Changed

- Bare `prettycov` reads `./coverage.out` and prints the report. It used to demand `-profile` and,
  given nothing, print usage. The profile may also be given positionally.

### Go API

- **Breaking:** `DisplayTree(w, tree, depth uint)` became `DisplayTree(w, tree, opts Options)`, and
  `Walker` and `PathTree.Walk` were removed.

## [0.2.0] — 2026-09-03

**Breaking: the default view changed shape.** Totals are unchanged.

- Rows are sorted, and a run of directories that each hold nothing but the next renders as one row.
  The default view used to spend three levels on the import path before reaching anything worth
  reading, which is what `-old`/`-new` existed to work around:

  ```
  before                          after
   github.com - 94.01              github.com/screwyprof/delegator - 94.01
   └ screwyprof - 94.01            ├ pkg - 96.41
                                   ├ scraper - 90.00
                                   └ web - 95.15
  ```

  `-depth` counts the same as it did — a level now spans several path segments, so the same number
  reaches further down the tree.

### Go API

- **Breaking:** `CoverageStats.Ratio` became a method, `Ratio() (pct float64, ok bool)`. `ok` is
  false when there are no statements to cover, which is not 0%.

## [0.1.5] — 2026-09-02

**Breaking: totals change wherever a directory is both a package and the parent of packages.**

- Each statement is counted once when rolling up. Those two contributions were conflated rather than
  summed, and the error runs both ways — a node holding a package above less-covered children read
  75.00 where the answer is 61.11, while a root over siblings with differing children read 10.00
  where the answer is 25.00. A chain of single-child directories was never affected.

## [0.1.4] — 2026-09-02

**Breaking: the input and the arithmetic both changed.**

- The coverage profile is parsed directly. It used to shell out to `go tool cover -html` and scrape
  per-file *percentages* out of the HTML, then average them unweighted — a one-statement file
  counted as much as a five-hundred-statement one. Statements are counted now, so numbers move:
  delegator's profile read 93.95 before and 94.01 after.
- Files with no coverage at all appear in the tree. They were dropped outright before, so a package
  nobody tested was simply invisible.

## 0.1.0 – 0.1.3 — 2022-09-02 … 2022-09-07

Initial release: a prefix tree of package paths and coverages, rendered to the terminal, with
`-depth`, `-old` and `-new`.

[80974]: https://github.com/golang/go/issues/80974

[0.6.0]: https://github.com/screwyprof/prettycov/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/screwyprof/prettycov/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/screwyprof/prettycov/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/screwyprof/prettycov/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/screwyprof/prettycov/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/screwyprof/prettycov/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/screwyprof/prettycov/compare/v0.1.5...v0.2.0
[0.1.5]: https://github.com/screwyprof/prettycov/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/screwyprof/prettycov/compare/v0.1.3...v0.1.4
