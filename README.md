# Prettycov
[![codecov](https://codecov.io/gh/screwyprof/prettycov/graph/badge.svg)](https://codecov.io/gh/screwyprof/prettycov) [![Go](https://github.com/screwyprof/prettycov/actions/workflows/go.yml/badge.svg)](https://github.com/screwyprof/prettycov/actions/workflows/go.yml)

Pretty Golang Coverage.

The other day I wanted to output a pretty overall coverage summary in my terminal.
I wanted to show a table or a tree with top-level packages and their corresponding coverage. 
I tried to search for some ready to use tools which would offer something similar but with not luck.
After that, I decided to build it on my own. So here it is :)

With thanks to [antongr](https://github.com/kannman), whose nudge got this started.

## Status
Pre-1.0. Flags, output format and the Go API may all change between minor versions — pin a version if you gate CI on it. What changed and what broke is in [CHANGELOG.md](CHANGELOG.md).

## Installation
```shell
go install github.com/screwyprof/prettycov/cmd/prettycov@latest
```

## How to use

Run your tests with coverage, then point prettycov at the profile. With no arguments it reads
`./coverage.out`, which is what `go test -coverprofile` is conventionally pointed at:

```shell
❯ go test -covermode=atomic -coverprofile=coverage.out ./...
❯ prettycov
 github.com/screwyprof/delegator - 91.54
 ├ pkg - 93.33
 ├ scraper - 88.00
 └ web - 93.94
```

That top row is the reason this exists: `go tool cover` cannot give you a total across packages
([golang/go#66506](https://github.com/golang/go/issues/66506)), let alone one per branch of the
tree.

Turn on the two flags that say more, and go a level deeper:

```shell
❯ prettycov -counts -files -depth=2
 github.com/screwyprof/delegator - 91.54  34/402 uncovered
 ├ pkg - 93.33  8/120 uncovered
 │ ├ clock/clock.go - 100.00  0/2 uncovered
 │ ├ httpkit/httpkit.go - 96.30  1/27 uncovered
 │ ├ logger - 92.50  3/40 uncovered
 │ ├ pgxdb/pgxdb.go - 75.00  4/16 uncovered
 │ └ tzkt/client.go - 100.00  0/35 uncovered
 ├ scraper - 88.00  18/150 uncovered
 │ ├ config/config.go - 100.00  0/1 uncovered
 │ ├ service.go - 92.31  5/65 uncovered
 │ ├ store - 74.51  13/51 uncovered
 │ └ subscriber.go - 100.00  0/33 uncovered
 └ web - 93.94  8/132 uncovered
   ├ api/errors.go - 100.00  0/14 uncovered
   ├ handler - 87.23  6/47 uncovered
   ├ store/pgxstore - 95.12  2/41 uncovered
   └ tezos - 100.00  0/30 uncovered
```

`prettycov help` prints the same flags, with an example apiece.

## Flags

| flag | |
| --- | --- |
| `-depth=N` \| `max` | how many levels to show below the top row, the way `tree -L` counts them. Default 1; `max` goes all the way down, which beats guessing a number that is wrong in both directions |
| `-files` | draw the profile's files as well as its packages |
| `-counts` | show `uncovered/total` statements beside each percentage |
| `-total` | print only the number, for a Makefile or a badge |
| `-fail-under=N` | exit 1 when total coverage is below N, so prettycov can gate CI |
| `-exclude=REGEXP` | leave out files whose path matches, or blocks whose `file:line:col` matches, before anything is totalled. Repeatable |
| `-old=PATH -new=PATH` | shorten a long root package path in the labels |
| `-color=auto` \| `always` \| `never` | when to colour |
| `-profile=PATH` | which profile to read. Also accepted as the sole positional argument |
| `-version` | print the version and exit. Also `prettycov version` |
| `-help` \| `-h` | print the flags with an example apiece. Also `prettycov help` |

Exit codes are `0`, `1` when `-fail-under` was not met, and `2` when prettycov could not do what was
asked — distinct, so a CI step can tell a bad invocation from a failed gate.

**A flag that matched nothing is a `2`**: `-exclude` whose pattern hits no file, `-old` naming a root
the profile does not hold. Both did nothing, which is an argument mistake — found a step later than
the rest only because the profile is what answers it. No report goes to stdout, as for any other.

A pattern beaten to every file by an *earlier* pattern still exits `0`. It is doing its job, and
deleting it is what would break.

## What a percentage will not tell you

A percentage hides size, and that changes which package you should open first. In the tree above
`pgxdb/pgxdb.go` reads 75.00 and `handler` reads 87.23 — and `handler` holds **more** untested code
than `pgxdb/pgxdb.go`, six statements against four. Sorting by the percentage points at whichever
row is smallest, not at the one worth opening; the uncovered count is what to act on. That is what
`-counts` is for.

It is worth knowing this before gating on a percentage at all. The studies that looked
([Inozemtseva & Holmes, ICSE 2014](https://www.cs.ubc.ca/~rtholmes/papers/icse_2014_inozemtseva.pdf))
found coverage a weak predictor of whether a suite catches bugs once test-suite size is controlled
for; what predicts it is how much the tests *assert*, which no coverage profile can see. Teams that
mandate a figure tend to land exactly on it and stop. Coverage is good at one thing — showing where
the untested code is — so treat `-fail-under` as a floor, not a goal.

## Reading the report

**A run of directories that each hold nothing but the next one is one row.** Otherwise every report
would spend three levels on `github.com`, `owner`, `repo` before reaching anything worth reading.

**The profile's files are the tree's leaves**, so every row is the sum of what is drawn beneath it —
`scraper`'s 150 statements are `config/config.go`'s 1, `service.go`'s 65, `store`'s 51 and
`subscriber.go`'s 33. That is what makes a report with `-counts` addable. Without `-files` those
leaves simply are not drawn, so a package holding both files and subpackages shows a total larger
than its visible children; `du` behaves the same way, and `du -a` is its `-files`. A file costs a
`-depth` level exactly as a subpackage does, being one of a directory's entries.

**A package whose whole content is one file is one row**, named for both: `tzkt/client.go` rather
than `tzkt` above an identical `client.go`. That row is the package's, so it costs the one level
the package did and not a second for the file. A row's label is a property of the node, so raising
`-depth` adds rows below rather than renaming the ones already drawn.

## Just the number

`-total` prints the total percentage and nothing else, so a Makefile or a badge can read it. It
replaces the recipe every project ends up writing:

```make
COVERAGE := $(shell go tool cover -func coverage.out | awk 'END{print $$NF}')   # 91.5%
COVERAGE := $(shell prettycov -total)                                           # 91.54
```

It changes how the report is printed, not what gets measured, so every flag that changes the
measurement still applies — `-exclude` moves it just as it moves the tree, and `-fail-under` still
grades it. Flags that only decorate the tree, like `-counts`, go with the glyphs. Filter accounting
goes to stderr, so `$(shell prettycov -total)` stays clean.

Two decimals, rendered by the same code as the tree, so a summary line and the report it summarises
cannot round differently. (The figure is the whole profile's total, which is the tree's root — with
a profile spanning two top-level paths it is the union of both, and so appears in no single row.)

The printed figure is rounded while `-fail-under` compares the exact ratio, so do not build a second
gate by comparing this number to a threshold. It goes wrong both ways: 79.999% prints as `80.00` on
a run `-fail-under=80` fails, and 99.9996% prints as `99.99` on one `-fail-under=99.995` passes. Use
`-fail-under`.

One deliberate difference from `go tool cover`: **`100.00` is never rounded up to.** 73999 of 74000
statements reads as `99.99` here, where `go tool cover -func` rounds at one decimal and reports
`100.0%` from 99.95% upwards. 100% is what a badge shows and what stops someone writing another
test, so it is only printed when every statement is covered. Everything else rounds to nearest.

A profile with nothing to cover has no total, so it exits 2 with a message rather than printing
`n/a` or `0.00` into your variable — unless `-fail-under` was given, in which case that reports the
shortfall and exits 1 instead.

## Stop counting code you never meant to test

`-exclude` drops files whose path matches a regexp, before anything is totalled — generated code,
mocks, a migrator you never intended to cover. Patterns are unanchored and match the full path, so a
short one reaches the whole tree. The flag is repeatable, and each pattern reports what it took out:

```shell
❯ prettycov -exclude='/store/' -exclude='/bind/'
-exclude "/store/" left out 92 statements in 4 files
-exclude "/bind/" left out 18 statements in 1 file
 github.com/screwyprof/delegator - 94.52
 ├ pkg - 93.33
 ├ scraper - 94.95
 └ web - 95.89
```

A pattern that matched nothing is a typo, so it exits 2 and the report is not drawn:

```shell
❯ prettycov -exclude='/store/' -exclude='\.pb\.go$'
-exclude "/store/" left out 92 statements in 4 files
-exclude "\\.pb\\.go$" matched nothing
```

A pattern matching `file:line:col` takes one block instead of a whole file. The toolchain has no
`//go:cover ignore` comment — [golang/go#53271](https://github.com/golang/go/issues/53271) was
declined, with the answer that a tool reporting the uncovered lines should filter them instead — so
this is how a single unreachable statement stops being counted without dropping the file it lives
in:

```shell
❯ prettycov -exclude='httpkit\.go:62' -counts
-exclude "httpkit\\.go:62" left out 1 statement in 1 block
 github.com/screwyprof/delegator - 91.77  33/401 uncovered
 ├ pkg - 94.12  7/119 uncovered
 ├ scraper - 88.00  18/150 uncovered
 └ web - 93.94  8/132 uncovered
```

The position is the block's start, which `cmd/cover` opens just after the brace — `if !ok {` on line
32 owns the `return` on line 33 — so read it from the profile rather than off the source. The column
is optional, and only tells two blocks opening on one line apart.

Patterns are unanchored here as everywhere, so a bare line number is a prefix: `a\.go:3` reaches
lines 3, 30 and 300. Anchor it when you mean one line — `a\.go:3$`, or `a\.go:3:2$` to pin the
column too.

The accounting goes to stderr, so the report itself stays pipeable. It filters the report, not the
profile on disk: `go tool cover -html` and anything else reading the file still sees everything in
it, and changing your mind costs a re-render rather than a re-run.

That is the difference from narrowing `-coverpkg`, which is the other place a project can draw this
line. Excluding a package there means it is never instrumented, so the decision is baked into the
run and usually arrives as a `go list | grep -v` package list that can disagree with the build it
feeds. Doing it here gives the same total — one package's coverage never enters another's ratio —
and leaves `-coverpkg` a single pattern:

```make
COVERAGE_EXCLUDE := migrator|testcfg|cmd|web/config

coverage:
	go test -covermode=atomic -coverprofile=coverage.out -coverpkg=work work
	prettycov -exclude='$(COVERAGE_EXCLUDE)' coverage.out
```

## Colour

Percentages are graded red, yellow and green using only the base ANSI colours, so your own terminal
theme decides the shades. Colour is on when writing to a terminal and off when piped, honouring
[`NO_COLOR`](https://no-color.org) and `TERM=dumb`. Override with `-color=always` or `-color=never`.

## How it works

It parses the coverage profile into a prefix tree of paths and coverages, with the profile's files
as the leaves, then rolls each node up from the leaves so every node reports its own statements plus
everything below it. It then draws the top row plus `-depth` levels beneath it, collapsing a run of
directories that each hold nothing but the next one into a single row, and drawing the file leaves
only when `-files` asks for them.

It reads the profile and nothing else — no source tree, no `go.mod`, no git — so it works on a CI
artefact, a colleague's file, or a repository you do not have checked out.
