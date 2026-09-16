# Contributing

## What you need

Go, make, and git.

```shell
git clone https://github.com/screwyprof/prettycov
cd prettycov
make check
```

Every tool the build reaches for is a Go program, fetched at a pinned version by `go run` when it
is not already on your PATH — golangci-lint, vale, govulncheck, gobco, gremlins. The first
`make check` compiles them, so it is slow once and fast after.

There is a nix flake, and it is a convenience rather than a requirement: `nix develop` gives you
the same tools prebuilt, pinned to the same versions, without the first-run compile. Nothing
`make check` runs needs it.

One target is the exception. `make hooks` installs the git pre-commit hooks and needs `pre-commit`,
which is Python rather than Go and so cannot be fetched the same way — `pip install pre-commit`, or
enter the devShell, which registers them on entry. Nothing else asks for it.

Go 1.26, not 1.27. A coverage tool cannot ship on a toolchain that miscounts statements, and 1.27
does ([golang/go#80974](https://github.com/golang/go/issues/80974)) — the reasoning and the
condition for lifting the pin are in [.modernize](.modernize).

## The gates

`make check` is what CI runs — one command, one list, so the two cannot drift. It prints the block
to paste into a pull request:

| target | what it asks |
| --- | --- |
| `make test` | tests, with `-race` and `-shuffle=on` on both passes |
| `make lint-all` | 74 linters, including nilaway compiled in as a plugin |
| `make lint` | the same, narrowed to your diff — what CI annotates on the pull request |
| `make vulns` | govulncheck, reachability-aware |
| `make docs-lint` | Vale over the Markdown, against Google's style guide |
| `make mutate` | mutation testing; a surviving mutant is a test that asserts nothing |
| `make cover-branches` | condition coverage, which statement coverage cannot see |
| `make tidy` | `go mod tidy -diff`, so a stale go.sum cannot reach main |

All of them are expected to pass before a pull request. `make help` lists the rest.

## What a change looks like

**A fix needs a test that fails without it.** Not a test that covers the line — one you have watched
fail with the fix reverted. The mutation gate exists because a passing test that asserts nothing is
worse than no test.

**Commands print real output.** Every `❯ prettycov …` block in the README and the reference is run
against `testdata/delegator-go126.out`, the profile the golden files pin. Run the command and paste
what it printed; never retype it, and never copy something that generates itself — a `--help`
listing pasted into the README was wrong within the hour.

**Comments say what the code cannot.** A decision, a measurement, the bug that forced the shape.
Not what the next line plainly does.

**Performance claims come with numbers.** `go test -bench=. -count=10` through `benchstat`, pasted
into the commit body. A result with `~` is not a result.

## Using an AI assistant

No objection to it. The bar does not move: you are the author of what you send, which means you
have read it, you can say why it is shaped that way, and you have watched the test fail with the
fix reverted.

Check it before you open the pull request, not after a reviewer does. Three things that come back
wrong often enough to be worth naming:

- **Output pasted from memory.** Every `❯ prettycov …` block in the docs is real output from a real
  run. Run the command.
- **Comments that restate the line below them.** A comment here records a decision, a measurement,
  or the bug that forced the shape — the code already says what it does.
- **A test written around the implementation.** `make mutate` holds at zero survivors precisely
  because a test that executes a line without asserting on it passes coverage and proves nothing.

`make check` passing is the floor, not the case for the change. The commit message is where you
explain why it is right.

## Layout

| | |
| --- | --- |
| root package `prettycov` | the domain: pure functions over a parsed profile, no CLI knowledge |
| `internal/cli` | commands, flags, and every sentence the tool says |
| `internal/app` | composition root: builds the parser, maps errors to exit codes |
| `cmd/prettycov` | the binary, which does nothing but exit |

A depguard rule in [.golangci.yml](.golangci.yml) enforces the direction: the domain cannot import
kong or anything under `internal/`.

## Conventions

British spelling, because the CLI's own help text uses it. Vale enforces the rest — read
[.vale.ini](.vale.ini) before arguing with a finding; it records what this project declines and why.

Commit subjects say what changed and why it matters, not which files moved.
