# Contributing

## What you need

Go, make, and git.

```shell
git clone https://github.com/screwyprof/prettycov
cd prettycov
make check
```

Every tool the build reaches for is a Go program, fetched at its pinned version by `go run` —
golangci-lint, vale, govulncheck, gobco, gremlins. Never from your PATH, whatever is on it: a pin
that defers to whatever happens to be installed is a pin that lies. The first `make check` compiles
them, so it is slow once and fast after.

There is a nix flake, and it is a convenience rather than a requirement: `nix develop` pins the Go
toolchain and puts gopls, pre-commit and tparse on your PATH. It deliberately does not carry
golangci-lint or vale: `make check` fetches those itself at the versions pinned in the Makefile and
never probes PATH, so a copy in your shell would be a second, different version of a gate's own
tool rather than the one that runs.

Renovate opens the dependency, action and tool bumps, and merges them itself once the checks pass.
Anything but a major lands without a human; a major waits for one. It reads the Makefile's tool
pins as well as `go.mod`, so the whole toolchain moves, and [renovate.json](renovate.json) records
what is deliberately held back.

One target is the exception. `make hooks` installs the git pre-commit hooks and needs `pre-commit`,
which is Python rather than Go and so cannot be fetched the same way — `pip install pre-commit`, or
enter the devShell, which registers them on entry. Nothing else asks for it.

Go 1.26, not 1.27. A coverage tool cannot ship on a toolchain that miscounts statements, and 1.27
does ([golang/go#80974](https://github.com/golang/go/issues/80974)): it splits a straight-line block
at a blank line and writes the whole run's count into each piece, inflating every figure this tool
reports. CL 819000 fixed it for Go 1.28 and there is no 1.27 backport, so the pin lifts when 1.28
ships and not before. It lives in [go.mod](go.mod) and [flake.nix](flake.nix).

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

No objection to it. The bar above does not move: you are the author of what you send, which means
you have read it, you can say why it is shaped that way, and you have checked it yourself before a
reviewer does.

`make check` passing is the floor, not the case for the change.

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
