# Changelog

Pre-1.0: flags, output format and the Go API may all change between minor versions.

Entries that change what an existing invocation prints or accepts are labelled **Breaking**,
because a version number cannot say it — Go modules have no sub-major inside v0, so `go get -u`
walks across any of these without comment. Read the labels, not the digits.

Only user-visible changes are listed; `git log` has the rest. Releases before 0.5.0 had no notes,
so those entries are reconstructed from the history.

## [0.5.0] — 2026-09-06

### Added

- `-exclude=REGEXP` drops files whose path matches, before anything is totalled. Unanchored, matched
  against the full path, repeatable. Each pattern reports on stderr what it took out — including
  nothing, which is how a typo shows up rather than reading as a lower number.

  It filters the report, not the profile on disk: `go tool cover -html` and anything else reading
  the file still sees everything in it.

## [0.4.1] — 2026-09-06

**Breaking: every reported number changes.** No behaviour changed, but the toolchain that measures
it did.

- Pinned Go 1.26. Go 1.27 splits a straight-line block at blank lines and writes the whole run's
  statement count into each piece ([golang/go#80974][80974]), inflating every figure. This
  repository's own coverage read 99.61% on 1.27 and 99.36% on 1.26 — the second is the true one.

  A profile produced by a 1.27 toolchain is still inflated whatever prettycov reads it.

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

## [0.2.0] — 2026-09-03

**Breaking: the default view changed shape.**

- Rows are sorted, and a run of directories that each hold nothing but the next renders as one row.
  The default view used to spend three levels on the import path before reaching anything worth
  reading, which is what `-old`/`-new` existed to work around:

  ```
  before                          after
   github.com - 93.95              github.com/screwyprof/delegator - 94.01
   └ screwyprof - 93.95            ├ pkg - 96.41
                                   ├ scraper - 90.00
                                   └ web - 95.15
  ```

- `-depth` counts levels below the top row, the way `tree -L` counts them.

## [0.1.5] — 2026-09-02

**Breaking: directory totals change.**

- Each statement is counted once when rolling up. A directory that is both a package and the parent
  of packages had its two contributions conflated rather than summed, so its totals grew by a factor
  of its child count. Every parent node above a leaf was inflated before this.

## [0.1.4] — 2026-09-02

**Breaking: the input changed.**

- The coverage profile is parsed directly. It used to shell out to `go tool cover -html` and scrape
  the HTML, which is also why numbers move here: `-func` and `-html` did not agree.

## 0.1.0 – 0.1.3 — 2022-09-02 … 2022-09-07

Initial release: a prefix tree of package paths and coverages, rendered to the terminal, with
`-depth`, `-old` and `-new`.

[80974]: https://github.com/golang/go/issues/80974

[0.5.0]: https://github.com/screwyprof/prettycov/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/screwyprof/prettycov/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/screwyprof/prettycov/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/screwyprof/prettycov/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/screwyprof/prettycov/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/screwyprof/prettycov/compare/v0.1.5...v0.2.0
[0.1.5]: https://github.com/screwyprof/prettycov/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/screwyprof/prettycov/compare/v0.1.3...v0.1.4
