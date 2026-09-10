# Changelog

Pre-1.0: flags, output format and the Go API may all change between minor versions.

Entries that change what an existing invocation prints or accepts are labelled **Breaking**,
because a version number cannot say it — Go modules have no sub-major inside v0, so `go get -u`
walks across any of these without comment. Read the labels, not the digits.

Only user-visible changes are listed; `git log` has the rest. Releases before 0.5.0 had no notes,
so those entries are reconstructed from the history and checked against binaries built from the
tags.

## [Unreleased]

### Changed

- With `-files`, a package whose whole content is one file is drawn as one row named for both —
  `tzkt/client.go`, not `tzkt` above an identical `client.go`. The two rows carried the same number
  twice. A row's label is a property of its node, so raising `-depth` adds rows below rather than
  renaming the ones already drawn, and the merged row costs the one `-depth` level the package did
  rather than a second for the file. On delegator this is 37 rows down to 28. Without `-files`,
  unchanged.

- Every row is now the sum of what is drawn beneath it with no exception. 0.8.0 had one: a file
  and a directory sharing a name were a single node carrying both. They are two nodes now, so the
  report adds up whatever the profile holds.

### Go API

- **Breaking:** `PathTree.Files` holds what the profile named in a directory; `Children` is the
  directories below it and nothing else, as it was before 0.8.0. `IsFile` is **removed** — a file
  and a directory of the same name are separate nodes, so nothing has to ask which a node is.

- **Breaking:** `Get` resolves directories, so `Get("m/x/a.go")` is nil where 0.8.0 returned the
  file. This one does not announce itself: it still compiles. Read a file from the `Files` map of
  the directory holding it.

## [0.8.0] — 2026-09-10

### Added

- `-counts` writes the statement counts after each percentage, as uncovered over total:
  `scraper - 88.00  18/150 uncovered`. Off by default; `-total` ignores it.

- `-files` draws the profile's files as well as its packages, one level below the package holding
  them, so a file costs a `-depth` level as a subpackage does. Every row is then the sum of what is
  drawn beneath it. Off by default.

  One shape is exempt: a file and a directory sharing a name — `m/a.go` beside `m/a.go/b.go` — get
  a single row carrying both, which therefore reads higher than the rows below it. No `go test` run
  produces such a profile; merging two can.

### Fixed

- Labels lose bidi controls (`U+202E` and its relatives), line and paragraph separators (`U+2028`,
  `U+2029`) and `U+FEFF`, not only control characters — a path holding one could draw a file under
  a name it does not have. The joiners `U+200C` and `U+200D` still render.

- An empty report gives the same reason whichever flags asked for it. With `-fail-under` it read
  `no statements to cover` even when `-exclude` had emptied it, and now reads
  `-exclude left nothing to report, wanted at least 80.00%`.

- A file directly at the filesystem root draws under `/` rather than under a blank label, and gets
  one row rather than two. `-new=/` reaches this from an ordinary profile.

### Go API

- **Breaking:** `Rows` takes `Options` in place of a bare `Depth`, and reads `Depth` and `Files`
  from it. Callers pass `prettycov.Options{Depth: d}` for what used to be `Rows(tree, d)`.

- **Breaking:** `PathTree.Children` holds the profile's files as well as its directories. This one
  still compiles and returns different data; `IsFile` tells them apart.

- `CoverageStats.Total` is the statements a node holds, covered or not.

- `Row.Level` is how far a row sits below the top one, which `Prefix` says in box-drawing
  characters.

## [0.7.1] — 2026-09-10

### Changed

- **Breaking:** a profile with no statements to cover exits 2 on every path, with
  `no statements to cover`, where the tree used to print a lone `n/a` and exit 0 — which is what
  a `go test -coverprofile` that matched no packages produces, and a coverage step would call
  green. `-total` already refused it. When `-exclude` patterns are what took the statements out,
  the message says so: `-exclude left nothing to report`. The empty pattern was already refused
  for this reason; `.*`, or `.go` typed for `\.pb\.go$`, empty the report just as well and were
  not. With `-fail-under` the gate still reports it and exits 1, and no longer draws the `n/a`
  row above the message — an empty report reads the same now whichever flags asked for it.
- `make publish` is the one `go list -m` the Go publishing guide prescribes, dropping the `curl`
  requirement and the retry loop around it. `make release` no longer refuses a version ./VERSION
  has already been tagged at: `git tag` says so itself, and `.SHELLFLAGS` carries `-e`, so the
  recipe still stops before pushing.

### Go API

- `Depth.String` writes `DepthAll` back as `"max"`, the way `ParseDepth` reads it, instead of the
  sentinel `18446744073709551615`.

## [0.7.0] — 2026-09-08

### Added

- `-depth=max` shows the whole tree. The README told people to set `-depth` past the bottom to see
  everything, and guessing that number is wrong in both directions: too small truncates without
  saying it did, and on kubernetes — 3232 rows — even 9 is six levels short.

  The default stays 1; the `defaultDepth` comment carries the measurement behind that.

  **Breaking:** `-depth` now reads its argument as decimal. The flag package read it as base 0, so
  `-depth=0x3` and `-depth=1_0` were accepted and are now refused, and `-depth=010` meant 8 where
  it now means 10 — on a 13-level tree that is 9 rows against 11. The silent one is the reason this
  is labelled: a rejected argument says so, a reinterpreted one does not.

- `make release` publishes the tag to the module proxy, so pkg.go.dev indexes it; `make publish`
  does that step alone. Releases used to sit unindexed until some user pulled one through.

### Changed

- `-color=auto` asks the descriptor whether it is a terminal, through
  `golang.org/x/term`, instead of stat'ing it for a character device. `/dev/null` and
  `/dev/urandom` are character devices too and were being coloured; a terminal still is one, so
  nothing a reader sees changes.

### Go API

**Breaking**, and the tool is CLI-first — the library is a by-product, and this is what pre-1.0 is
for. Concepts that were loose numbers with their rules scattered around them are now types that
carry those rules, and the environment-reading moved out to the CLI:

- `ColorMode`, `ColorAuto`, `ColorNever` and `ColorAlways` are **removed**. `Options.Color` is a
  `Palette` — `Plain` or `ANSI` — so `DisplayTree` renders what it is told instead of reading
  `NO_COLOR`, `TERM` and the file descriptor to decide. Resolving `auto` needs those, and they are
  questions about the world rather than about coverage, so the CLI asks them and passes the answer.

  This is the one that does not announce itself. Naming a removed constant fails to compile, but
  `Options{Depth: 2}` still builds and its zero `Color` used to mean "decide for me" and now means
  `Plain` — so a caller that left the field out stops colouring a terminal, silently.

- `Depth` replaces the `uint` on `Options.Depth` and `Rows`. `DepthAll` is the whole tree, and
  `ParseDepth` reads `"max"` or a level count. The parsing, the clamping and the two ways of
  getting it wrong lived in the CLI, which is where a magic `math.MaxUint` had to be known about.
- `ParseExclude` compiles one `-exclude` pattern and refuses the empty one, which matches every
  file and would silently zero the report. That guard used to live in the CLI, so it protected only
  the flag; it is on the way in now, for any caller that goes through it. `Exclude` still takes
  compiled patterns and asks no questions about them, the same way a hand-built `Depth` skips
  `ParseDepth`'s clamp.
- `Percentage` replaces `CoverageStats.Ratio` and the `Percentage(CoverageStats)` function.
  `CoverageStats.Percentage() (Percentage, bool)` builds one — `ok` is false when there is nothing
  to cover — so a percentage from there always has a number to show. The zero value is still a
  `Percentage`, and it renders `0.00`, so declare one and the report says the code is uncovered
  rather than that something is wrong.
  `String` rounds and never reads 100.00 for code that is not fully covered; `Float` does not
  round, so `-fail-under` compares the exact ratio.

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

[Unreleased]: https://github.com/screwyprof/prettycov/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/screwyprof/prettycov/compare/v0.7.1...v0.8.0
[0.7.1]: https://github.com/screwyprof/prettycov/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/screwyprof/prettycov/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/screwyprof/prettycov/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/screwyprof/prettycov/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/screwyprof/prettycov/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/screwyprof/prettycov/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/screwyprof/prettycov/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/screwyprof/prettycov/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/screwyprof/prettycov/compare/v0.1.5...v0.2.0
[0.1.5]: https://github.com/screwyprof/prettycov/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/screwyprof/prettycov/compare/v0.1.3...v0.1.4
