# Prettycov
[![codecov](https://codecov.io/gh/screwyprof/prettycov/graph/badge.svg)](https://codecov.io/gh/screwyprof/prettycov) [![Go](https://github.com/screwyprof/prettycov/actions/workflows/go.yml/badge.svg)](https://github.com/screwyprof/prettycov/actions/workflows/go.yml) [![Release](https://img.shields.io/github/v/release/screwyprof/prettycov)](https://github.com/screwyprof/prettycov/releases/latest) [![Go Reference](https://pkg.go.dev/badge/github.com/screwyprof/prettycov.svg)](https://pkg.go.dev/github.com/screwyprof/prettycov) [![License](https://img.shields.io/github/license/screwyprof/prettycov)](LICENSE)

**Go coverage as a tree, with a total on every row.**

`go tool cover -func` gives you one line per function and a single number at the bottom. What it
cannot give you is a total per package, or across packages at all
([golang/go#66506](https://github.com/golang/go/issues/66506)) — so "how covered is `scraper`?" has
no answer, and on a real repository the per-function output is hundreds of lines you have to add up
yourself.

prettycov reads the profile `go test` already wrote and answers that:

```shell
❯ go test -covermode=atomic -coverprofile=coverage.out ./...
❯ prettycov report
 github.com/screwyprof/delegator - 91.54
 ├ pkg - 93.33
 ├ scraper - 88.00
 └ web - 93.94
```

Every row is the sum of everything beneath it, so you can start at the top and follow the worst
number down.

## Status

Pre-1.0. Flags, output format and the Go API may all change between minor versions — if you gate CI
on it, pin a version. What changed and what broke is in [CHANGELOG.md](CHANGELOG.md).

## Installation
```shell
go install github.com/screwyprof/prettycov/cmd/prettycov@latest
```

## What it is for

**Deciding what to test next.** In the tree below, `handler` is 87.23% covered and
`pgxdb/pgxdb.go` is 75.00% — but `handler` has six untested statements to `pgxdb`'s four. The lower
percentage is just the smaller file. `--counts` prints the statement counts alongside, so you can
rank by how much is left:

```shell
❯ prettycov report --counts --files --depth=2
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

Every row is a sum, so the counts add up: `scraper`'s 18 are `service.go`'s 5 and `store`'s 13.

**A number for CI or a badge.** `total` prints the figure and nothing else, so a Makefile can read
it — and `--fail-under` gates on it, for the whole profile or for one package:

```make
COVERAGE := $(shell prettycov total)                 # 91.54
```

```shell
❯ prettycov total scraper/store --fail-under=85
74.51
total coverage 74.51% is below 85.00%      # exit 1
```

**A list your editor can open.** `misses` prints where the untested statements are, in the format
`go vet` uses, so it pipes into `vim -q -` or reviewdog:

```shell
❯ prettycov misses --depth=max --old=github.com/screwyprof/delegator --new=.
pkg/httpkit/httpkit.go:62:2: 1 uncovered
pkg/logger/logger.go:22:16: 1 uncovered
pkg/logger/logger.go:44:26: 1 uncovered
…
```

The count matters: one untaken branch and a whole untested function look alike without it.

**Leaving out what you never meant to test.** `--exclude` drops generated code, mocks, or a migrator
before anything is totalled, and says what each pattern took, so a typo cannot pass for a clean run:

```shell
❯ prettycov report --exclude='/store/' --exclude='\.pb\.go$'
--exclude "/store/" left out 92 statements in 4 files
--exclude "\\.pb\\.go$" matched nothing
 github.com/screwyprof/delegator - 93.87
 ├ pkg - 93.33
 ├ scraper - 94.95
 └ web - 93.41
```

It reads the profile and nothing else — no source tree, no `go.mod`, no git — so it works on a CI
artefact, a colleague's file, or a repository you do not have checked out.

## Commands

| command | |
| --- | --- |
| `report` | draw the packages and what they cover, one row each |
| `misses` | print where the uncovered statements are, as `file:line:col` |
| `total [PATH]` | print only the number, for a Makefile or a badge |
| `version` | print the version |

`prettycov --help` lists them; `prettycov <command> --help` lists the flags that command takes.
Flags belong to the command that reads them, so each listing is exactly what applies, and they are
written after the command name.

Exit codes are `0`, `1` when `--fail-under` was not met, and `2` when prettycov could not do what
was asked — distinct, so a CI step can tell a bad invocation from a failed gate.

## Contributing

Go and make, nothing else — every tool the build needs is fetched at a pinned version when it is
not already on your PATH. [CONTRIBUTING.md](CONTRIBUTING.md) has the gates and what a change is
expected to carry.

## More

[**docs/reference.md**](docs/reference.md) covers the rest: how the tree is built and why rows
collapse, what `--depth` and `--hide-covered` do to a report, how `total` resolves a path, the
`file:line:col` format and the editor settings that read it, and how `--exclude` accounts for what
it removed.

## License

MIT — see [LICENSE](LICENSE).

With thanks to [antongr](https://github.com/kannman), whose nudge got this started.
