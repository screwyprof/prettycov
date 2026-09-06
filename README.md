# Prettycov
[![codecov](https://codecov.io/gh/screwyprof/prettycov/graph/badge.svg)](https://codecov.io/gh/screwyprof/prettycov) [![Go](https://github.com/screwyprof/prettycov/actions/workflows/go.yml/badge.svg)](https://github.com/screwyprof/prettycov/actions/workflows/go.yml)<!-- ALL-CONTRIBUTORS-BADGE:START - Do not remove or modify this section -->
[![All Contributors](https://img.shields.io/badge/all_contributors-1-orange.svg?style=flat-square)](#contributors-)
<!-- ALL-CONTRIBUTORS-BADGE:END --> 

Pretty Golang Coverage.

The other day I wanted to output a pretty overall coverage summary in my terminal.
I wanted to show a table or a tree with top-level packages and their corresponding coverage. 
I tried to search for some ready to use tools which would offer something similar but with not luck.
After that, I decided to build it on my own. So here it is :)

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

You may also specify `-depth` to set how many levels to show below the top row, the way `tree -L` counts them. Set it past the depth of the tree to drill all the way down and find what is dragging coverage:

```shell
❯ prettycov -depth=9
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

### Exclude files
`-exclude` takes a regexp and drops every file whose path matches it, so generated code, `main` packages and test helpers stop dragging the number down. It is repeatable, and it says what each pattern took out:

```shell
❯ prettycov -exclude='/cmd/' -exclude='\.pb\.go$'
-exclude "/cmd/" left out 354 statements in 3 files
-exclude "\.pb\.go$" left out 908 statements in 12 files
 github.com/screwyprof/delegator - 94.04
```

Those lines go to stderr, so the report itself still pipes. They are always printed, because exclusion moves the denominator and a total quietly resting on half the repository is the thing this tool exists to catch. A pattern that matches nothing says so too, which is usually a typo or a path that has moved.

It filters the profile when it is read, not the tests that produced it. A package cannot be left out of `go test` — it is compiled and reported either way — so this saves no time; what it changes is the denominator. Patterns are unanchored and match the file's full path, so `cmd/` reaches every command in the tree and `\.pb\.go$` drops generated protobuf. That is what golangci-lint does too, and like golangci-lint the safeguard is not anchoring but the count: an unanchored `cmd/` also removes `pkg/subcmd`, and the report says so by naming two files rather than one.

That is enough to express what projects already ignore. etcd's `codecov.yml` drops `**/*.pb.go`, `**/*.pb.gw.go` and `tests/**`, which is `-exclude='/tests/|\.pb(\.gw)?\.go$'` and takes its total from 60.29% to 68.32%, against the 69.68% it publishes.

### Colour
Percentages are graded red, yellow and green using only the base ANSI colours, so your own terminal theme decides the shades. Colour is on when writing to a terminal and off when piped, honouring [`NO_COLOR`](https://no-color.org) and `TERM=dumb`. Override with `-color=always` or `-color=never`.

## How it works
It parses the coverage profile to populate a prefix tree of paths and coverages.
Then it traverses the tree from the furthermost leaves to top merging the coverage info. 
Then it draws the top row plus `-depth` levels beneath it. A run of directories that each hold nothing but the next one renders as a single row.

### It counts statements, so it will not match your dashboard
`prettycov` counts **statements**, which is what a Go coverage profile records and what `go tool cover -func` reports. Coverage dashboards count **lines**, and they do not agree with each other either. The same profile, three ways:

| | testify |
| --- | --- |
| statements — `go tool cover`, `prettycov` | **67.31%** (3471/5157) |
| lines — coveralls, via `goveralls` or `gcov2lcov` | 62.54% (3865/6180) |
| lines — codecov, which also counts partly-covered lines separately | 64.04% (3008/4697) |

They differ in how they merge repeated blocks too: `gocovmerge` adds counts, codecov takes the maximum per line, `goveralls` sums. There is no specification to reproduce, so `prettycov` matches the Go toolchain and offers no other mode.

If a badge says 69.68% and `prettycov` says 68.34%, that gap is the unit, not a disagreement about your tests.

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
