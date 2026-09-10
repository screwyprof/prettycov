# Prettycov
[![codecov](https://codecov.io/gh/screwyprof/prettycov/graph/badge.svg)](https://codecov.io/gh/screwyprof/prettycov) [![Go](https://github.com/screwyprof/prettycov/actions/workflows/go.yml/badge.svg)](https://github.com/screwyprof/prettycov/actions/workflows/go.yml)<!-- ALL-CONTRIBUTORS-BADGE:START - Do not remove or modify this section -->
[![All Contributors](https://img.shields.io/badge/all_contributors-1-orange.svg?style=flat-square)](#contributors-)
<!-- ALL-CONTRIBUTORS-BADGE:END --> 

Pretty Golang Coverage.

The other day I wanted to output a pretty overall coverage summary in my terminal.
I wanted to show a table or a tree with top-level packages and their corresponding coverage. 
I tried to search for some ready to use tools which would offer something similar but with not luck.
After that, I decided to build it on my own. So here it is :)

## Status
Pre-1.0. Flags, output format and the Go API may all change between minor versions — pin a version if you gate CI on it. What changed and what broke is in [CHANGELOG.md](CHANGELOG.md).

## Installation
```shell
go install github.com/screwyprof/prettycov/cmd/prettycov@latest
```

## How to use
### Getting built-in help
Run `prettycov help`, `prettycov -help` or `prettycov -h` for the built-in usage info. Bare `prettycov` reads `./coverage.out` and prints the report.

### Run your tests with coverage
`prettycov` works by parsing coverage profile, so the first thing to do is to run tests with coverage:

`go test -cover -coverprofile=coverage.out` ./...

### Show coverage summary up to the given depth
The profile defaults to `./coverage.out`. Name another one positionally (`prettycov path/to/cov.out`) or with `-profile`.

You may also specify `-depth` to set how many levels to show below the top row, the way `tree -L` counts them. `-depth=max` goes all the way down, which beats guessing a number: too small truncates the tree without saying so.

```shell
❯ prettycov -depth=max
 github.com/screwyprof/delegator - 94.01
 ├ pkg - 96.41
 │ ├ clock - 100.00
 │ ├ httpkit - 97.50
 │ ├ logger - 96.88
 │ ├ pgxdb - 90.00
 │ └ tzkt - 100.00
 ├ scraper - 90.00
 │ ├ config - 100.00
 │ └ store - 77.97
 │   ├ dbrow - 100.00
 │   └ pgxstore - 75.47
 └ web - 95.15
   ├ api - 100.00
   ├ handler - 89.66
   │ └ bind - 85.71
   ├ store/pgxstore - 96.49
   └ tezos - 100.00
```

### Show coverage summary with replaced paths
Sometimes the project may have a long project path (package path to be more precise) which clutters the output. 
In this case you may want to replace it with a shorter name:

```shell
❯ prettycov -depth=2 -old github.com/screwyprof/delegator -new delegator
 delegator - 94.01
 ├ pkg - 96.41
 │ ├ clock - 100.00
 │ ├ httpkit - 97.50
 │ ├ logger - 96.88
 │ ├ pgxdb - 90.00
 │ └ tzkt - 100.00
 ├ scraper - 90.00
 │ ├ config - 100.00
 │ └ store - 77.97
 └ web - 95.15
   ├ api - 100.00
   ├ handler - 89.66
   ├ store/pgxstore - 96.49
   └ tezos - 100.00
```

### Getting top-level coverage info
This is what I created this tool for. You may get a nice top-level package coverage:

```shell
❯ prettycov
 github.com/screwyprof/delegator - 94.01
 ├ pkg - 96.41
 ├ scraper - 90.00
 └ web - 95.15
```

### Fail below a threshold
`-fail-under` exits 1 when total coverage is under the given percentage, so `prettycov` can gate CI. It stays distinct from exit 2, which means prettycov could not run at all:

```shell
❯ prettycov -fail-under=99
 github.com/screwyprof/delegator - 94.01
 ├ pkg - 96.41
 ├ scraper - 90.00
 └ web - 95.15
total coverage 94.01% is below 99.00%
❯ echo $?
1
```

### Just the number
`-total` prints the total percentage and nothing else, so a Makefile or a badge can read it.

It changes how the report is printed, not what gets measured. The number is the tree's top row with the label and the glyphs stripped off, so every flag that changes the measurement still applies: `-exclude` changes it just as it changes the tree, and `-fail-under` still grades it. Flags that only decorate the tree, like `-counts`, go with the glyphs.

```shell
❯ prettycov -total
94.01
❯ prettycov -total -exclude='/store/'
-exclude "/store/" left out 116 statements in 4 files
95.80
```

The accounting still goes to stderr, so `COVERAGE := $(shell prettycov -total)` stays clean. It replaces the recipe every project ends up writing:

```make
COVERAGE := $(shell go tool cover -func coverage.out | awk 'END{print $$NF}')   # 94.0%
COVERAGE := $(shell prettycov -total)                                           # 94.01
```

Two decimals, rendered by the same code as the tree, so a summary line and the report it summarises cannot round differently. (The figure is the whole profile's total, which is the root of the tree — with a profile spanning two top-level paths it is the union of both, and so appears in no single row.)

A profile with no statements to cover has no total, so it exits 2 with a message rather than printing `n/a` or `0.00` into your variable — unless `-fail-under` was given, in which case that reports the shortfall and exits 1 instead.

The printed figure is rounded to two decimals while `-fail-under` compares the exact ratio, so don't build a second gate by comparing this number to a threshold. It goes wrong both ways: 79.999% prints as `80.00` on a run `-fail-under=80` fails, and 99.9996% prints as `99.99` on one `-fail-under=99.995` passes. Use `-fail-under`.

One deliberate exception, and it differs from `go tool cover`: **`100.00` is never rounded up to.** 73999 of 74000 statements reads as `99.99` here, where `go tool cover -func` rounds at one decimal and reports `100.0%` from 99.95% upwards. 100% is what a badge shows and what stops someone writing another test, so it is only printed when every statement is covered. Everything else rounds to nearest, as before.

### See how much work a percentage stands for
A percentage hides size. `-counts` puts the fraction beside it, as uncovered over total:

```shell
❯ prettycov -counts
 github.com/screwyprof/delegator - 94.01  34/568 uncovered
 ├ pkg - 96.41  8/223 uncovered
 ├ scraper - 90.00  18/180 uncovered
 └ web - 95.15  8/165 uncovered
```

The uncovered count is the number to act on, and it ranks packages differently from the percentage: at `-depth=max` this profile shows `pkg/logger` at 96.88 and `web/handler/bind` at 85.71 holding exactly the same three untested statements. Ranking by the percentage alone points at the smaller package with the louder number.

That is the general problem with the percentage, and it is worth knowing before gating on one. The studies that looked ([Inozemtseva & Holmes, ICSE 2014](https://www.cs.ubc.ca/~rtholmes/papers/icse_2014_inozemtseva.pdf)) found coverage a weak predictor of whether a suite catches bugs — what predicts it is how much the tests *assert*, which no coverage profile can see — and teams that mandate a figure tend to land exactly on it and stop. Coverage is good at one thing: showing where the untested code is. `-counts` is for that; `-fail-under` is a floor, not a goal.

### Go down to the files
By default the report is about packages. `-files` draws the profile's files as well, one level below the package holding them — so a package's own files sit beside its subpackages, the way `ls`, `tree` and `du -a` list a directory's entries:

```shell
❯ prettycov -files -counts -depth=2
 github.com/screwyprof/delegator - 94.01  34/568 uncovered
 ├ pkg - 96.41  8/223 uncovered
 │ ├ clock - 100.00  0/2 uncovered
 │ ├ httpkit - 97.50  1/40 uncovered
 │ ├ logger - 96.88  3/96 uncovered
 │ ├ pgxdb - 90.00  4/40 uncovered
 │ └ tzkt - 100.00  0/45 uncovered
 ├ scraper - 90.00  18/180 uncovered
 │ ├ config - 100.00  0/1 uncovered
 │ ├ service.go - 94.25  5/87 uncovered
 │ ├ store - 77.97  13/59 uncovered
 │ └ subscriber.go - 100.00  0/33 uncovered
 └ web - 95.15  8/165 uncovered
   ├ api - 100.00  0/14 uncovered
   ├ handler - 89.66  6/58 uncovered
   ├ store/pgxstore - 96.49  2/57 uncovered
   └ tezos - 100.00  0/36 uncovered
```

Files are leaves, so **every row is the sum of what is drawn beneath it** — `scraper`'s 180 statements are `config`'s 1, `service.go`'s 87, `store`'s 59 and `subscriber.go`'s 33. That is what makes a report with `-counts` addable.

Without `-files` those file rows are simply not drawn, and a package that holds both files and subpackages shows a total larger than its visible children — `du` behaves the same way, and `du -a` is its `-files`. Since a file is one of a directory's entries, it costs a `-depth` level exactly as a subpackage does.

### Stop counting code you never meant to test
`-exclude` drops files whose path matches a regexp, before anything is totalled. Patterns are unanchored and match the full path, so a short one reaches the whole tree. The flag is repeatable, and each pattern reports what it took out — including nothing, which is how you spot a typo. Patterns that between them take every file are an error rather than a green run: exit 2 with `-exclude left nothing to report`, or, under `-fail-under`, the failed gate any profile with nothing to cover gets.

```shell
❯ prettycov
 github.com/screwyprof/delegator - 94.01
 ├ pkg - 96.41
 ├ scraper - 90.00
 └ web - 95.15

❯ prettycov -exclude='/store/' -exclude='\.pb\.go$'
-exclude "/store/" left out 116 statements in 4 files
-exclude "\\.pb\\.go$" matched nothing
 github.com/screwyprof/delegator - 95.80
 ├ pkg - 96.41
 ├ scraper - 95.87
 └ web - 94.44
```

The accounting goes to stderr, so the report itself stays pipeable. It filters the report, not the profile on disk: `go tool cover -html` and anything else reading the file still sees everything in it.

This is what lets `-coverpkg` stay a single pattern. One package's coverage never enters another's ratio, so excluding it here gives the same total as leaving it out of `-coverpkg` — without a package list computed by a `go list | grep -v` that can disagree with the build it feeds:

```make
COVERAGE_EXCLUDE := migrator|testcfg|cmd|web/config

coverage:
	go test -covermode=atomic -coverprofile=coverage.out -coverpkg=work work
	prettycov -exclude='$(COVERAGE_EXCLUDE)' coverage.out
```

### Colour
Percentages are graded red, yellow and green using only the base ANSI colours, so your own terminal theme decides the shades. Colour is on when writing to a terminal and off when piped, honouring [`NO_COLOR`](https://no-color.org) and `TERM=dumb`. Override with `-color=always` or `-color=never`.

## How it works
It parses the coverage profile to populate a prefix tree of paths and coverages.
Then it traverses the tree from the furthermost leaves to top merging the coverage info. 
Then it draws the top row plus `-depth` levels beneath it. A run of directories that each hold nothing but the next one renders as a single row. The profile's files are the tree's leaves, so every node's total is the sum of what hangs below it; `-files` decides whether those leaves are drawn.

## Contributors ✨
Thanks goes to these wonderful people ([emoji key](https://allcontributors.org/docs/en/emoji-key)):

<!-- ALL-CONTRIBUTORS-LIST:START - Do not remove or modify this section -->
<!-- prettier-ignore-start -->
<!-- markdownlint-disable -->
<table>
  <tbody>
    <tr>
      <td align="center"><a href="https://github.com/kannman"><img src="https://avatars.githubusercontent.com/u/40325995?v=4?s=100" width="100px;" alt=""/><br /><sub><b>antongr</b></sub></a><br /><a href="https://github.com/screwyprof/prettycov/commits?author=kannman" title="Code">💻</a></td>
    </tr>
  </tobdy>
</table>

<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->

<!-- ALL-CONTRIBUTORS-LIST:END -->

This project follows the [all-contributors](https://github.com/all-contributors/all-contributors) specification. Contributions of any kind welcome!
