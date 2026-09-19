# Contributing

## What you need

Go, make, and git.

```shell
git clone https://github.com/screwyprof/prettycov
cd prettycov
make check
```

Every tool the build reaches for is a Go program fetched at its pinned version by `go run`:
golangci-lint, vale, govulncheck, gobco, gremlins, benchstat. Never from your PATH, whatever is on it, because
a pin that defers to whatever happens to be installed is a pin that lies. The first `make check`
compiles them, so it is slow once and fast after.

There is a nix flake, though it is a convenience and not a requirement. `nix develop` pins the Go
toolchain and puts gopls, pre-commit and tparse on your PATH. It deliberately leaves out
golangci-lint and vale, since `make check` fetches those itself and never looks at your PATH: a copy
in your shell would be a second version of a gate's own tool, differently pinned.

One target is the exception. `make hooks` installs the git pre-commit hooks and needs `pre-commit`,
which is Python rather than Go and cannot be fetched the same way. Run `pip install pre-commit`, or
enter the devShell, which registers the hooks on entry. Nothing else asks for it.

Go 1.26, not 1.27. A coverage tool cannot ship on a toolchain that miscounts statements, and 1.27
does ([golang/go#80974](https://github.com/golang/go/issues/80974)): it splits a straight-line block
at a blank line and writes the whole run's count into each piece, inflating every figure this tool
reports. CL 819000 fixed it for Go 1.28 and there is no 1.27 backport, so the pin lifts when 1.28
ships and not before. It lives in five places that must move together: [go.mod](go.mod),
[flake.nix](flake.nix), `go-version` in [go.yml](.github/workflows/go.yml) and
[vulns.yml](.github/workflows/vulns.yml), and `constraints.go` in [renovate.json](renovate.json).

## The gates

`make check` is what CI runs. One command and one list, so the two cannot drift. It prints the block
to paste into a pull request:

| target | what it asks |
| --- | --- |
| `make test` | tests, with `-race` and `-shuffle=on` on both passes |
| `make lint-all` | the linters [.golangci.yml](.golangci.yml) enables, over the whole tree |
| `make lint` | the same, narrowed to your diff, which is what CI annotates |
| `make vulns` | govulncheck, reachability-aware |
| `make docs-lint` | Vale over the Markdown, against Google's style guide |
| `make mutate` | mutation testing; a surviving mutant is a test that asserts nothing |
| `make cover-branches` | condition coverage, which statement coverage cannot see |
| `make bench-cmp` | allocations against `origin/main`; a rise fails, timings do not gate |
| `make tidy` | `go mod tidy -diff`, so a stale go.sum cannot reach main |

All of them are expected to pass before a pull request. `make help` lists the rest.

`make bench-cmp` rebuilds the base in a worktree, so it needs no stored history and compares two
runs made on one machine minutes apart. Point it elsewhere with `make bench-cmp BENCH_BASE=<ref>`.

## What a change looks like

### A fix needs a test that fails without it

Not a test that covers the line, but one you have watched fail with the fix reverted. The mutation
gate exists because a passing test that asserts nothing is worse than no test.

### Commands print real output

Every `❯ prettycov …` block in the README and the reference is run against
`testdata/delegator-go126.out`, the profile the golden files pin. Run the command and paste what it
printed. Never retype it, and never copy something that generates itself: a `--help` listing pasted
into the README was wrong within the hour.

### Comments say what the code cannot

A decision, a measurement, or the bug that forced the shape. Not what the next line plainly does.

### Performance claims come with numbers

`make bench` for the timings, `make bench-cmp` for the allocations against `origin/main`, pasted
into the commit body. A result with `~` is not a result. Do not retype either: paste what the
target printed.

## Using an AI assistant

No objection to it. The bar above does not move: you are the author of what you send, so you have
read it, you can say why it is shaped that way, and you have checked it yourself before a reviewer
does. `make check` passing is the floor rather than the case for the change.

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

British spelling, because the CLI's own help text uses it. Vale enforces the rest. Read
[.vale.ini](.vale.ini) before arguing with a finding; it records what this project declines and why.

Commit subjects say what changed and why it matters, not which files moved.
