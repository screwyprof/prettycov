# prettycov reference

Everything the [README](../README.md) leaves out: what each command does in detail, how the tree is
built, and why the output is shaped the way it is.

## Reading the report


Directories that hold nothing but one another are drawn as a single row. `github.com` holds only
`screwyprof`, which holds only `delegator`: three directories, one path through them, no choice to
make at any step. Drawn one per level, the default report would be this:

```
 github.com - 91.54
 └ screwyprof - 91.54
   └ delegator - 91.54
     └ …
```

Three rows carrying the same number, and `--depth=1` would stop before reaching a package. What
you get instead:

```shell
❯ prettycov report
 github.com/screwyprof/delegator - 91.54
 ├ pkg - 93.33
 ├ scraper - 88.00
 └ web - 93.94
```

The first block is an illustration; every block with a `❯` on this page is output from a real run.
That collapsed chain is what the rest of this page calls the *root*: what `--old` renames, and what
`total` puts back in front of a path you copied off a row.

The profile's files are the tree's leaves, so every row is the sum of what is drawn beneath it.
`scraper`'s 150 statements are `config/config.go`'s 1, `service.go`'s 65, `store`'s 51 and
`subscriber.go`'s 33. That is what makes a report with `--counts` addable. Without `--files` those
leaves simply are not drawn, so a package holding both files and subpackages shows a total larger
than its visible children; `du` behaves the same way, and `du -a` is its `--files`. A file costs a
`--depth` level exactly as a subpackage does, being one of a directory's entries.

A package whose whole content is one file is one row, named for both: `tzkt/client.go` rather
than `tzkt` above an identical `client.go`. That row is the package's, so it costs the one level
the package did and not a second for the file. A row's label is a property of the node, so raising
`--depth` adds rows below rather than renaming the ones already drawn.

## Leave out what is finished


`--hide-covered` drops a subtree when it and everything drawn inside it is fully covered, so what is
left is what there is still work in. On delegator at `--depth=max` that is 18 rows down to 12:

```shell
❯ prettycov report --hide-covered --depth=max
 github.com/screwyprof/delegator - 91.54
 ├ pkg - 93.33
 │ ├ httpkit - 96.30
 │ ├ logger - 92.50
 │ └ pgxdb - 75.00
 ├ scraper - 88.00
 │ └ store - 74.51
 │   └ pgxstore - 72.34
 └ web - 93.94
   ├ handler - 87.23
   │ └ bind - 83.33
   └ store/pgxstore - 95.12
```

It pays where a remaining gap is hardest to find and does nothing where gaps are everywhere: across
two repositories at `--depth=max --files`, gin has 42 of 54 rows fully covered and dive has 1 of
100.

`--hide-covered=90` moves the bar. A subtree goes when every row *drawn* beneath it is at the bar or
above, which makes it a conjunction with `--depth` and `--files`: a row those already cut cannot be
the reason its parent stays, and a collapsed run costs one level here because it is one row there.

```shell
❯ prettycov report --hide-covered=90 --files --depth=2
 github.com/screwyprof/delegator - 91.54
 ├ pkg - 93.33
 │ └ pgxdb/pgxdb.go - 75.00
 ├ scraper - 88.00
 │ └ store - 74.51
 └ web - 93.94
   └ handler - 87.23
```

Raising the depth brings a branch back the moment there is something under it worth reading,
`logger` returns at `--depth=3`, because the `logger.go` at 86.67 that justifies it is only a row
there:

```shell
❯ prettycov report --hide-covered=90 --files --depth=3
 github.com/screwyprof/delegator - 91.54
 ├ pkg - 93.33
 │ ├ logger - 92.50
 │ │ └ logger.go - 86.67
 │ └ pgxdb/pgxdb.go - 75.00
 ├ scraper - 88.00
 │ └ store - 74.51
 │   └ pgxstore/store.go - 72.34
 └ web - 93.94
   └ handler - 87.23
     ├ bind/bind.go - 83.33
     └ tezos_get_delegations.go - 89.66
```

Below 100 it hides misses along with the rows, which is the point and worth knowing: at 90 on this
profile, 9 of the 34 uncovered statements stop being drawn.

The value is a percentage and nothing else: `0` hides every row that has one, and anything that is
not a number in range is refused rather than guessed at. Leave the flag off to turn it off. The
threshold needs `=`, because a bare `--hide-covered` already means 100 and `--hide-covered 90` is
that plus a stray argument.

Like `--depth` and `--files`, it shapes the report and never the measurement. `total` and
`--fail-under` read the same with it as without. That is what separates it from `--exclude`, which
takes files out before anything is totalled.

## Just the number


`total` prints the total percentage and nothing else, so a Makefile or a badge can read it:

```make
COVERAGE := $(shell go tool cover -func coverage.out | awk 'END{print $$NF}')   # 91.5%
COVERAGE := $(shell prettycov total)                                           # 91.54
```

It changes how the report is printed, not what gets measured, so every flag that changes the
measurement still applies. `--exclude` moves it just as it moves the tree, and `--fail-under` still
grades it. Flags that only decorate the tree, like `--counts`, go with the glyphs. Filter accounting
goes to stderr, so `$(shell prettycov total)` stays clean.

Two decimals, rendered by the same code as the tree, so a summary line and the report it summarises
cannot round differently. (The figure is the whole profile's total, which is the tree's root. With
a profile spanning two top-level paths it is the union of both, and so appears in no single row.)

The printed figure is rounded while `--fail-under` compares the exact ratio, so do not build a
second gate by comparing this number to a threshold. It goes wrong both ways: 79.999% prints as
`80.00` on a run `--fail-under=80` fails, and 99.9996% prints as `99.99` on one
`--fail-under=99.995` passes. Use `--fail-under`.

One deliberate difference from `go tool cover`: **`100.00` is never rounded up to.** 73999 of 74000
statements reads as `99.99` here, where `go tool cover -func` rounds at one decimal and reports
`100.0%` from 99.95% upwards. 100% is what a badge shows and what stops someone writing another
test, so it is only printed when every statement is covered. Everything else rounds to nearest.

A profile with nothing to cover has no total, so it exits 2 with a message rather than printing
`n/a` or `0.00` into your variable, unless `--fail-under` was given, in which case that reports the
shortfall and exits 1 instead.

### One package's number, or one file's

`total` on its own reports the whole tree. Given a path it reports that node, the number the
report already draws, which nothing else could hand back:

```shell
❯ prettycov report --old=github.com/screwyprof/delegator --new=. --depth=3 --files
 pkg - 93.33
 ├ clock/clock.go - 100.00
 ├ httpkit/httpkit.go - 96.30
 ├ logger - 92.50
 │ ├ logger.go - 86.67
 │ └ middleware.go - 96.00
 …

❯ prettycov total
91.54
❯ prettycov total pkg/logger
92.50
❯ prettycov total pkg/logger/logger.go
86.67
```

The path is spelled as the report prints it. A row is drawn with its own segment only, so what you
read off one is `pkg/logger` where the profile holds
`github.com/screwyprof/delegator/pkg/logger`, and both work: the module root the report collapsed
away is put back for you when the bare path is not there itself. That needs a root to put back, so
it holds for a profile naming one module. A profile naming several — a `go.work` workspace built
with `-coverpkg` across two of them — collapses nothing into a shared top row, draws each tree
under its own, and can give two of them a row reading `pkg/logger`. Ask for those by the full path
the report prints above them. A row that
`--files` merges into one label (`main.go` for a file the profile gave no directory) answers to that
label as well as to its full path. `total ""` is refused rather than read as the whole tree, so an
unset `total "$PKG"` fails instead of quietly gating the repository. That is the opposite of
`--exclude`, which matches the profile's own paths, and it is the right way round here: you read a
row, then ask for its number. A path the profile does not hold is exit 2 rather than `0.00`, which a
script would read as a real and terrible figure.

Being a positional argument, it takes any path a profile can hold, including a package named `t`,
`true` or `1`, which as a flag value would have had to be told apart from a boolean first.

Because it is one argument with one value, "two packages at once" is not expressible, which is the
point, since the output is a single number. It composes with `--fail-under`, and the number graded
is the number printed:

```shell
❯ prettycov total scraper/store --fail-under=85
74.51
total coverage 74.51% is below 85.00%      # exit 1
```

That gates one package without building a second profile. `--depth`, `--files`, `--counts` and
`--hide-covered` are not `total`'s to take: there is no report being drawn, only a number being
read.

## Where the uncovered statements are


A percentage says how much is untested; `misses` says where. One line per run of statements the
tests never reached, as `file:line:col: N uncovered`, the shape `go vet` prints and an editor's
error format parses:

```shell
❯ prettycov misses --old=github.com/screwyprof/delegator --new=. --depth=max
pkg/httpkit/httpkit.go:62:2: 1 uncovered
pkg/logger/logger.go:22:16: 1 uncovered
pkg/logger/logger.go:44:26: 1 uncovered
pkg/logger/middleware.go:90:2: 1 uncovered
pkg/pgxdb/pgxdb.go:23:16: 1 uncovered
pkg/pgxdb/pgxdb.go:44:16: 1 uncovered
pkg/pgxdb/pgxdb.go:48:39: 2 uncovered
scraper/service.go:94:16: 2 uncovered
scraper/service.go:158:20: 1 uncovered
scraper/service.go:165:16: 1 uncovered
scraper/service.go:188:16: 1 uncovered
scraper/store/pgxstore/store.go:44:35: 1 uncovered
scraper/store/pgxstore/store.go:47:16: 1 uncovered
scraper/store/pgxstore/store.go:56:27: 1 uncovered
scraper/store/pgxstore/store.go:64:16: 1 uncovered
scraper/store/pgxstore/store.go:69:51: 1 uncovered
scraper/store/pgxstore/store.go:73:56: 1 uncovered
scraper/store/pgxstore/store.go:77:56: 1 uncovered
scraper/store/pgxstore/store.go:81:65: 1 uncovered
scraper/store/pgxstore/store.go:85:38: 1 uncovered
scraper/store/pgxstore/store.go:104:16: 1 uncovered
scraper/store/pgxstore/store.go:118:16: 1 uncovered
scraper/store/pgxstore/store.go:132:16: 1 uncovered
scraper/store/pgxstore/store.go:147:16: 1 uncovered
web/handler/bind/bind.go:26:16: 1 uncovered
web/handler/bind/bind.go:31:16: 1 uncovered
web/handler/bind/bind.go:36:16: 1 uncovered
web/handler/tezos_get_delegations.go:40:16: 1 uncovered
web/handler/tezos_get_delegations.go:46:16: 1 uncovered
web/handler/tezos_get_delegations.go:52:16: 1 uncovered
web/store/pgxstore/store.go:43:16: 1 uncovered
web/store/pgxstore/store.go:50:16: 1 uncovered
```

Vim reads it as a quickfix list, but not off a pipe: `-q` takes a filename, and `-` is not special
to it, so `prettycov misses | vim -q -` is `E40: Can't open errorfile -`. Vim also wants the
terminal that the pipe took. Give it the list as a file instead:

```shell
❯ vim -q <(prettycov misses --old=$MODULE --new=. --depth=max)
❯ prettycov misses --old=$MODULE --new=. --depth=max > misses.txt && vim -q misses.txt
```

Already inside Vim, `:cexpr system('prettycov misses --old=$MODULE --new=. --depth=max')` does
the same.
`system()` merges the two streams, so add `2>/dev/null` when the run has anything to say on stderr:
a filter note lands in the quickfix list as an entry pointing nowhere.

reviewdog reads the same output under `-f=golint`, which is
[errorformat](https://github.com/reviewdog/errorformat)'s name for `%f:%l:%c: %m` rather than a
claim about golint. It parses rather than passes through: a line that is not a diagnostic is
dropped. Which reporter to hand it, `local` at a terminal or one of the `github-*` ones in CI, is
reviewdog's question and not this format's.

`sourcefile:lineno:column: message` is one of the two forms the [GNU coding
standards](https://www.gnu.org/prep/standards/html_node/Errors.html) give for a compiler naming a
column, and it is the one `go vet`, `gcc` and `golangci-lint` all emit.

The count is not decoration. Without a message after the position, that error format cannot match
and falls back to `file:line:message`, reading the column as the text, so `a.go:62:2` opens line 62
at column 1 and the column is lost. Vim's default
[`errorformat`](https://vimhelp.org/quickfix.txt.html#errorformat) tries `%f:%l:%c:%m` before
`%f:%l:%m`, and Emacs' `gnu` rule in
[`compile.el`](https://github.com/emacs-mirror/emacs/blob/master/lisp/progmodes/compile.el) wants
the same trailing colon, so both need it. It is also the number a position cannot carry: one untaken
branch and a whole untested function look alike until you see it.

The column is a byte offset, counting a tab as one.
[`go/token.Position.Column`](https://pkg.go.dev/go/token#Position) is documented that way, `go vet`
[prints it
unchanged](https://cs.opensource.google/go/x/tools/+/master:internal/analysis/driverutil/print.go),
and golangci-lint [indexes the line by
byte](https://github.com/golangci/golangci-lint/blob/main/pkg/printers/text.go) to place its own
`^`. prettycov passes through what `cmd/cover` recorded, so it agrees with those. The GNU text says
to count display width instead, with tab stops every 8, which is what Emacs assumes:
[`compilation-error-screen-columns`](https://www.gnu.org/software/emacs/manual/html_node/emacs/Compilation-Mode.html)
defaults to `t`. On gofmt'd source, which is tab-indented, that puts the cursor inside the leading
tabs. Setting it to `nil` reads the column as Go writes it, and fixes `go vet` and golangci-lint
output in the same stroke.

Editors that hyperlink terminal output rather than parse an error format are looser: VS Code's
[`terminalLinkParsing.ts`](https://github.com/microsoft/vscode/blob/main/src/vs/workbench/contrib/terminalContrib/links/browser/terminalLinkParsing.ts)
also takes `file(12,3)`, `file#12` and `file on line 12`, and needs no message at all.

It replaces the report rather than decorating it: the tree is the summary, these are the drill-down.

The path in the profile names a Go package rather than a file on disk, so the root in front of every
one is a prefix no editor resolves. `--old=$MODULE --new=.` takes it off, and what is left
resolves against the directory you ran `go test` in, which is where `coverage.out` is.

Both flags, rather than a root read off the profile: the deepest directory every path shares is the
module root when the profile spans the module and one level too deep when it covers a single
package, and nothing here reads `go.mod` to tell those apart. A wrong guess prints a path that looks
openable and is not, which is worse than one that plainly is not a file.

Blocks that abut fold into one entry. `cmd/cover` emits one per branch, so a function nothing covers
arrives as a dozen of them. That halves the list on a badly covered profile and changes almost
nothing on a good one, where misses are scattered single statements. A covered block between two
uncovered ones stops the fold, or the entry would claim a statement the tests do reach.

`--depth` and `--hide-covered` narrow it as they narrow the tree under `--files`, being the same
filtering with that one option set for you: a file is an entry of the package holding it, so it sits
one level below that package. `--depth=1`, the default here as for a report, gives 8 of the 32
entries a full listing has, and the count on stderr says so. `--depth=max` gives all of them.
`--hide-covered=90` leaves out the ones in subtrees already at the bar.

Stripping the root takes a level off every path, so a depth here is one less than the same view
costs under a report drawing the module as its top row. Against the *default* tree the two part
company, since asking for files is also what merges a package holding one into a single row:
`misses --depth=2` reaches a file that `report --depth=2` alone stops one row above.

That level is worth counting before reaching for `--depth`. With the root off, `--depth=1` reaches
the files one level under a top-level package, `pkg/httpkit/httpkit.go` among them because a
package holding a single file merges into that row, and stops above anything deeper:
`web/handler/bind/bind.go` is cut even though its package holds one file too. Without the rename
every path moves down one, and `--depth=1` reaches no file in this profile at all.

You are told whenever a filter shortens the list, because a short one and a whole one look alike:

```shell
❯ prettycov misses --old=github.com/screwyprof/delegator --new=. --depth=1
pkg/httpkit/httpkit.go:62:2: 1 uncovered
…
--depth=1 lists 10 of 34 uncovered statements                        # on stderr
```

A tree carries its subtree's count on every row, so a shallow one is a summary and says so. A list
has no such row, and eight positions read the same whether they are all of them or a quarter,
which matters most where it is piped, since a quickfix list that stops early looks like one you have
finished. The count is on stderr, so the pipe is unaffected. An empty list names the filters the
same way, rather than guessing which of them did it: `nothing to show at --depth=1; 34 uncovered
statements left`.

`misses` has no `--files`: it adds files to the *tree's* output, and a list of positions is made of
them either way. Passing it is exit 2, like any other flag a command does not take.

`--exclude` removes them outright, since it acts on the profile before any of this, and it takes the
same `file:line:col` spelling, so a position you judge unreachable pastes back as a pattern. It
matches the paths the profile holds. Under `--new=.` a printed position pastes back as printed,
since taking a root off leaves what is left sitting inside the profile's own spelling and patterns
are unanchored. Under any other destination it does not: `--new=SRC` prints `SRC/b/b.go:1:1`, and
the profile holds no `SRC`. Paste the profile's own path when you renamed to anything but `.`.

Without `--new`, `misses` prints the profile's own spelling, which is what `--exclude` matches and
what a position pastes back as.

Anchor it with `$`. Patterns are unanchored, so the column is a prefix like the line is:
`a\.go:9:2` also matches `a\.go:9:24`, which is an ordinary second block on the same line. `if err
!= nil {` at column 2 and a closure at column 24. Pasting the position bare drops both, and the
denominator moves with them:

```shell
❯ prettycov total --profile testdata/two-blocks-one-line.out
20.00
❯ prettycov total --exclude='a\.go:9:2' --profile testdata/two-blocks-one-line.out
--exclude "a\\.go:9:2" left out 4 statements in 2 blocks
100.00
❯ prettycov total --exclude='a\.go:9:2$' --profile testdata/two-blocks-one-line.out
--exclude "a\\.go:9:2$" left out 1 statement in 1 block
25.00
```

Four of the five statements left the denominator on the first pattern, and 100.00 is not a coverage
figure. It is what remains after a pattern took more than was meant. Anchoring is what makes the
round trip exact.

What it matches is the block that opens there, not the whole region. A position is the *first* block
of a fold while the count beside it is the region's, so excluding one that reads `2 uncovered` takes
one statement out and leaves the next block listed at its own position. Repeat until the region is
gone, or aim a pattern at the file. `--exclude` works in blocks, which is what makes a coordinate
mean one thing.

## Stop counting code you never meant to test


`--exclude` drops files whose path matches a regexp, before anything is totalled: generated code,
mocks, a migrator you never intended to cover. Patterns are unanchored and match the full path, so a
short one reaches the whole tree. The flag is repeatable, and each pattern reports what it took out,
including nothing, which is how you spot a typo:

```shell
❯ prettycov report --exclude='/store/' --exclude='\.pb\.go$'
--exclude "/store/" left out 92 statements in 4 files
--exclude "\\.pb\\.go$" matched nothing
 github.com/screwyprof/delegator - 93.87
 ├ pkg - 93.33
 ├ scraper - 94.95
 └ web - 93.41
```

A pattern matching `file:line:col` takes one block instead of a whole file, so a single unreachable
statement stops being counted without dropping the file it lives in:

```shell
❯ prettycov report --exclude='httpkit\.go:62' --counts
--exclude "httpkit\\.go:62" left out 1 statement in 1 block
 github.com/screwyprof/delegator - 91.77  33/401 uncovered
 ├ pkg - 94.12  7/119 uncovered
 ├ scraper - 88.00  18/150 uncovered
 └ web - 93.94  8/132 uncovered
```

The position is the block's start, which `cmd/cover` opens just after the brace, so `if !ok {` on
line 32 owns the `return` on line 33. Read it from the profile rather than off the source. The
column is optional, and only tells two blocks opening on one line apart.

Patterns are unanchored here as everywhere, so a bare line number is a prefix: `a\.go:3` reaches
lines 3, 30, and 300. Anchor it when you mean one line: `a\.go:3$`, or `a\.go:3:2$` to pin the
column too.

The accounting goes to stderr, so the report itself stays pipeable. It filters the report, not the
profile on disk: `go tool cover -html` and anything else reading the file still sees everything in
it, and changing your mind costs a re-render rather than a re-run.

Excluding here gives the same total as narrowing `-coverpkg`, since one package's coverage never
enters another's ratio, and it leaves `-coverpkg` a single pattern rather than a `go list | grep -v`
list that can drift from the build it feeds:

```make
COVERAGE_EXCLUDE := migrator|testcfg|cmd|web/config

coverage:
	go test -covermode=atomic -coverprofile=coverage.out -coverpkg=work work
	prettycov report --exclude='$(COVERAGE_EXCLUDE)' --profile coverage.out
```

## Colour


Percentages are graded red, yellow and green using only the base ANSI colours, so your own terminal
theme decides the shades. Colour is on when writing to a terminal and off when piped, honouring
[`NO_COLOR`](https://no-color.org) and `TERM=dumb`. Override with `--color=always` or
`--color=never`.

## What a percentage does not tell you

Coverage is a weak predictor of whether a suite catches bugs once suite size is controlled for
([Inozemtseva & Holmes, ICSE
2014](https://www.cs.ubc.ca/~rtholmes/papers/icse_2014_inozemtseva.pdf)); what predicts it is how
much the tests assert, which no profile can see. Treat `--fail-under` as a floor, not a goal.

The unit is the statement, so anything the profile records without one cannot move the figure. An
empty switch arm is the case that bites: `cmd/cover` writes it as a block declaring no statements,
`x.go:5.9,5.9 0 0`, where the trailing count still says whether the arm was ever taken. Summing
statements, a zero adds to neither side, so this tool and `go tool cover -func` both read straight
past an arm no test reached, and `misses` has no position to list. A line-based service reads the
same block as an uncovered line, which is the one thing codecov can say here that this cannot.

## How it works

It parses the coverage profile into a prefix tree of paths and coverages, with the profile's files
as the leaves, then rolls each node up from the leaves so every node reports its own statements plus
everything below it. It then draws the top row plus `--depth` levels beneath it, collapsing a run of
directories that each hold nothing but the next one into a single row, and drawing the file leaves
only when `--files` asks for them.

It reads the profile and nothing else. There is no source tree to find and no `go.mod` to parse, so
it works on a CI artefact, a colleague's file, or a repository you do not have checked out.
