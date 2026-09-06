# Go coverage: verified behaviour

Everything here was measured, not recalled. Go 1.27, linux/arm64.

## Toolchain behaviour

| behaviour | verified |
| --- | --- |
| `go list ./...` is module-scoped; does not cross module boundaries | delegator: 3 packages, workspace has 25 |
| `go list all` spans the workspace (plus deps) | delegator: 25 |
| `go list -m` reports the module *graph*, not modules on disk | nested fixture: 1 of 3 |
| Inside a module omitted from `go.work`, `go list -m` reports the **workspace's** modules, not yours | `not-in-workspace` fixture |
| `GOWORK=off` gives the module's own identity, and still resolves workspace-interdependent imports | `wsdep` fixture: `b` resolved `a` |
| `go list -e` lists packages despite load errors | needed for omitted modules |
| `go test ./pkgs/x/...` from parent module: **error** | `directory prefix pkgs/x does not contain main module or its selected dependencies` |
| Per-package test loop is **mandatory** for nested modules (no `go.work`) | — |
| Per-package loop is **not** needed otherwise: one invocation is byte-identical | `loopcheck`: identical `go tool cover -func` output |
| `-coverpkg` matches **import paths**; directory paths silently match nothing | delegator: `[no statements]`, 0 packages |
| `-coverpkg` instruments only what the test binary **links** | etcd: `./api/...` → 8 of 180 packages |
| `-coverpkg` accepts patterns → constant size | delegator: 204 bytes vs 1148 enumerated |
| Untested packages **do** appear in the profile with count 0 | `ninety` fixture |
| A panicking test yields a profile of **only** `mode: atomic` — all data for that package lost | `panics` fixture: `TestA` ran, covered nothing |
| `go test -timeout` does **not** cover work in `TestMain` before `m.Run()` | 10s sleep survived `-timeout=2s` |
| `go tool cover -func` needs packages resolvable in the current module | fails on cobra/chi from outside; **fails entirely on merged multi-module profiles** |
| `go tool cover -func` resolves source from the module cache, not the checkout | testify: 67.1% vs 67.3% |
| go tool ignores dirs starting with `.` or `_`, and `testdata` | fixtures with own `go.mod` under `testdata/` are invisible |
| golangci-lint walks the **filesystem**, not the package graph | tried to format `_reference/`; needs `^_` exclusion |
| `internal/coverage/*` is not importable — no public covdata reader | `use of internal package not allowed` |
| Only supported covdata reader: `go tool covdata textfmt` | — |
| `-coverprofile` is written **incrementally**, per package as each test binary finishes | SIGKILL mid-run: finished package's data present |
| `go test ./...` builds one test binary per package, so a panic or build failure loses only that package | `states.txtar`: 1 of 5 lost; `fail.test/broken [build failed]` with `good` at 100% |
| `-timeout` applies per test binary, not per invocation | NATS: *"Default of 10 minute (per package)"* |
| default `-covermode` is `set`; `-race` silently makes it `atomic` | `mode: set` / `mode: atomic` |
| `-race -covermode=set` is a hard error | `-covermode must be "atomic", not "set", when -race is enabled` |
| a second `go test` **truncates** the same `-coverprofile` | run 1 then run 2: only run 2's package remains |
| overlapping package patterns are absorbed | `go test ./... ./a/...` runs each package once |
| flags may follow the package pattern | `go test ./... -run TestA` works |
| `-C dir` must be the first flag | `-C flag must be first flag on command line` |
| `go list -json=Bogus` exits **0** with the field absent; `-f {{.Bogus}}` exits **1** and names it | `go help list`: `-json=` is *"required field names… to save work"*, not a schema |
| one `go test` over directory paths spans a workspace, and **only** a workspace | delegator: 5 modules, 1 profile. grpc-go (no go.work): 262 of 352 packages |

## Limits

| limit | value | consequence |
| --- | --- | --- |
| `MAX_ARG_STRLEN` (single arg) | **131072 bytes** exactly | `-coverpkg=a,b,c…` dies ~2600 packages |
| `ARG_MAX` (total) | 2097152 bytes | package *list* has ~40k headroom |

Use `go list -m -f '{{.Path}}/...'` patterns for `-coverpkg` → constant size.

## Package states

`0.00` and silence each hide several states. Distinguishable from discovery + profile alone
(`go list -f '{{len .TestGoFiles}} {{len .XTestGoFiles}}'` gives `HasTests` statically):

| has tests | in profile | meaning |
| --- | --- | --- |
| yes | >0 | measured, covered |
| yes | 0 | tests exist, cover nothing of own package |
| yes | **absent** | **data lost** — panic, build failure, or excluded |
| no | >0 | covered by other packages via `-coverpkg` |
| no | 0 | honest zero |
| no | absent | never linked by any test binary |

Rows 3 and 6 are invisible in every existing tool. Row 3 is also unbounded and unsigned — measured
on `internal/discover/testdata/states.txtar`, where one package's test panics:

| what the lost package would have measured | true total | reported |
| --- | --- | --- |
| 100.0% | 42.9% | **33.3%** |
| 0.0% | 20.0% | **33.3%** |

The same number for opposite truths, because the profile is identical either way: the package is
simply not in it. `go test` exits non-zero, which is the only signal, and `\|\| true` — delegator's,
and there so one broken package does not fail the build — is precisely what discards it.

## Counting rules differ by design

Same testify profile:

| rule | result |
| --- | --- |
| statements (Go, `go tool cover`, prettycov, go-test-coverage) | 67.31% (3471/5157) |
| lines — coveralls (`goveralls`, `gcov2lcov`): `for i := startLine; i <= endLine; i++` | 62.54% (3865/6180) |
| lines — codecov (`go.py`): column partials, **drops end line when `endCol <= 2`** | 64.04% (3008/4697) |

Merge semantics also differ: gocovmerge `+=` (count/atomic) / `|=` (set); codecov **max per line**;
goveralls `Count: a.Count + b.Count`. codecov has a `partials_as_hits` yaml knob.

No spec exists to reproduce. Do not add counting modes.

Confirmed against etcd's live codecov totals: `lines 38074, hits 26532, misses 10119, partials 1423`,
and 26532/38074 = 69.68 — the published number is hits over lines, with partials counted in the
denominator and not the numerator. Our statement count for the same exclusions was 39449. So a
comparison against a dashboard is a comparison of units, and agreeing to a point or two means
nothing either way. `sessions: 1` on that snapshot, so the number is one upload rather than the
merge of several.

This is in the README now, because the first thing a user does is compare against their badge.

## Go team position (why this is downstream)

- **#53271** `//go:cover ignore` — **declined** 2022-07-20.
  - rsc: *"If you have something that reports the uncovered lines, couldn't it filter them out instead?"* / *"filtering in that tool instead"* / *"Maybe we should remove the reported percentages because people focus too much on them?"*
  - ianlancetaylor: *"wrap the Go coverage tool with a wrapper that looks for the comments itself"*
- **#34639** — closed. Rob Pike: *"a non-critical build tool, and that is below the bar"*.

Filtering/reporting downstream is the sanctioned design.

## Tool survey

| tool | state | verdict |
| --- | --- | --- |
| `vladopajic/go-test-coverage` | active (2026-09) | owns thresholds (file/package/total), regex excludes, `// coverage-ignore`, diff-vs-base, badge. **Requires source. Emits no profile** → its excludes never reach codecov. Single `go.mod` only, no `go.work`. Counts statements; agrees with us to 0.08%. |
| `wadey/gocovmerge` | stale 2021 | 111 lines, **correct**: mode check, `OVERLAP MERGE/BEFORE/AFTER`. NATS installs it `@latest`. |
| `ory/go-acc` | **archived** | README: *"You do not need this tool, because Go now solves this out of the box."* `--ignore` used `strings.Contains` — same substring bug. |
| `haya14busa/goverage` | dead 2020 | modelled `go test` flags one by one → died of it |
| `dave/courtney` | active | AST excludes (`panic`, `// notest`, `if err != nil {return err}`); emits filtered profile; runs tests |
| `grosser/go-testcov` | active | **ratchet**: `// untested sections: 5` as a budget |
| `rillig/gobco` | active | condition coverage |
| `go-gremlins/gremlins` | active | mutation testing |
| `axw/gocov`, `sozorogami/gover` | archived/dead | — |
| `avito-tech/go-mutesting` | **not installable** | tag `v2.3.1` but `go.mod` lacks `/v2`; also needs `syscall.Dup2` (absent on arm64) |

## How real projects do it

| project | approach |
| --- | --- |
| thanos, argo-cd | GOCOVERDIR everywhere (`-args -test.gocoverdir=`), one `covdata textfmt` merge |
| moby | N profiles → upload directory to codecov, merge outsourced, `flags:` per suite |
| NATS, helm, vault | per-package profiles + concat/gocovmerge |
| gitea, jaeger, grafana, kratos | text profiles |
| kubernetes, terraform, prometheus, istio, rclone, hugo, cli | no coverage config found (k8s inconclusive — CI in `test-infra`) |

Test-kind separation, four incompatible ways: build tags (delegator) · directory lists (temporal) ·
`-run "^TestIntegration"` (grafana) · instrumented Docker images (thanos, argo-cd).

NATS `scripts/cov.sh` — the most careful example found: per-package isolation, empty-profile
detection (`if [[ $(cat ./cov/$2) == "mode: atomic" ]]`), no `-failfast` (flapper tolerance),
`-coverpkg=./server ./test`, nightly cron (`-timeout=1h` per package).

## screwyprof repos

| repo | approach | defect |
| --- | --- | --- |
| form3api (2021) | go-acc bash script | naive concat, no validation |
| skeleton (2022) | same script + greps | **stray `{}` on e2e line** → `malformed import path "{}"`; introduced in transit from form3api |
| interactor (2024) | `go test -coverprofile ./...` | none |
| cqrs (2025) | `grep -v -e .*test` | drops `testutil`, `latest`, `attestation` |
| delegator (2026) | workspace, `-coverpkg`, greps | `\|\| true`; substring greps; hardcoded `PACKAGES` |
| /services/golang | 6 nested modules, no `go.work` | recipe measures root only |

`grep -v -e "cmd/"` also deletes `pkg/subcmd/` — verified.

## Design decisions

**Governing rule: do what `go test ./...` would do if it worked on every topology.** Never diverge
from the toolchain's semantics — only from its scope, which is the part that is broken.

That rule already settles most of the open questions, without a flag for any of them:

| question | answered by | how |
| --- | --- | --- |
| do untested packages count? | yes | a package with no `_test.go` still emits every statement at count 0 |
| counting mode? | statements | no knob |
| exclusions? | none | `./...` excludes nothing |
| modules absent from `go.work`? | included | `./...` never consults `go.work` for scope |

**Ours:** discovery (filesystem walk, not `go list -m`) · module iteration · merge + validation ·
completeness reporting.

**Not ours:** building/running tests · flag semantics · profile formats · suite definition ·
what to exclude · `-coverpkg` (see below).

**Flag contract** — own the two things that make the fan-out work; forward the rest after `--`:

| flag | who |
| --- | --- |
| `-coverprofile` | **ours** — a second `go test` truncates it, so N module runs need N temp paths merged into the one name the user gave. Error if it appears after `--` |
| the package pattern | **ours** — `./...` per module *is* the tool. Harmless if the user also passes one; overlapping patterns are absorbed |
| `-covermode` | **theirs** — identical flags give every module the same mode by construction, so nothing needs normalising. Pinning `set` would break `-race` |
| `-coverpkg` | **theirs** — forwarded, never derived (see below) |
| `-tags` | **theirs**, but *read* — discovery without them misses whole suites. The one flag we parse, and only in the CLI layer |
| everything else | forwarded, never declared — this is what killed goverage |

**Files:** create nothing the user did not name. Per-module intermediates → temp, cleaned.
Per-suite profiles → user-named. Merged result → user-named.

**One invocation = one suite.** etcd needs five, differing in timeout, tags *and* package pattern;
no single flag set expresses them. Multiple suites are multiple runs, merged afterwards — which is
also how sharding and per-invocation `-coverpkg` are expressed.

**Failure policy:** always run everything, always report completeness. Produce exits like `go
test`; the configurable part — `--fail-on=test` / `incomplete` / `never` — belongs to the report,
where the completeness information is. NATS's policy = `incomplete`, and their commit message is
its justification; delegator's `|| true` = `never`.

**Exclusion is deny-only, module-shaped, anchored, and never inferred.** An allow-list fails by
omission and nothing mechanical catches it — delegator's `PACKAGES` forgot `./migrator/...`, NATS's
list is 6 of 20 packages, kubernetes pays for theirs with a CI verify script. A deny-list fails by
sloppy matching, which anchoring cures. Module granularity because that is the unit of invocation:
excluding a package saves no time. And excluding a module removes it from the denominator too,
which is a judgement only the repo owner can make.

**`-coverpkg` is a divergence from `./...`, not a repair for it.** Measured on one module,
`TestQuad` calling `helper.Double`:

| invocation | total |
| --- | --- |
| `go test -coverprofile ./...` | 50.0% (`helper` 0.0%) |
| `go test -coverpkg=./... -coverprofile ./...` | 100.0% |

Plain `./...` attributes coverage only to the package whose own test binary produced it. So
deriving `-coverpkg` would silently inflate every number against the rule above. It is the user's
flag, for when tests live outside the code's module (etcd `tests/`, NATS `-coverpkg=./server
./test`). delegator's Makefile uses it, so its published figure is the inflated kind.

Consequence: per-module `./...` + merge needs no cross-module attribution, so `replace`-target
resolution has no caller. `nested-replace` stays a record that it is *possible* — verified
(`mono2`: `require` + `replace`, naming the sibling's import path in `-coverpkg` measures it) —
not a thing to build.

## go.work is curated, not derived

Both projects **generate** `go.work` by walking for `go.mod` — the walk is upstream of the file.

| project | generator | on disk | in go.work | dropped |
| --- | --- | --- | --- | --- |
| kubernetes | `hack/update-go-workspace.sh` @e2b96b2 — `go work edit -use .` + `git ls-files ':(glob)./staging/src/k8s.io/*/go.mod'` | 34 | 31 | `hack/tools`, `code-generator/examples`, `kms/internal/plugins/_mock` |
| etcd | `scripts/update_go_workspace.sh` — copied from k/k, filter removed: `git ls-files ':(glob)**/go.mod'` | 13 | 13 | none — but `load_workspace_relative_modules_for_bom` subtracts `tools/*` again downstream |

Both **commit** `go.work` and `go.work.sum` (`git ls-files` confirms; neither `.gitignore`s them).
Nothing regenerates them at build time — a human runs the update script and commits the result.
Header in both: `// This is a generated file. Do not edit directly.`

Verification differs, and the difference matters:

| project | verify | catches a new module missing from go.work? |
| --- | --- | --- |
| kubernetes | `hack/verify-go-workspace.sh` → `kube::verify::generated` re-runs the update script and diffs | yes |
| etcd | `PASSES="go_workspace" ./scripts/test.sh` → `go mod download` then `git status --porcelain go.work.sum` | no — checks the **sum** file only |

So a committed `go.work` can be stale, and the file alone cannot say whether an absent module was
filtered on purpose or simply never added. etcd then derives its test patterns from that file:
`go work edit -json | jq -r '.Use[].DiskPath + "/..."'`.

What is dropped is always the same category: tooling, examples, mocks. Never product code.
`_mock` is already outside the build (leading `_`), so a correct walk drops it for free.

Measured on `not-in-workspace` (k8s-shaped, 4 uncovered statements in a code generator):

| scope | total |
| --- | --- |
| go.work (2 modules) | **100.00%** |
| whole tree (3 modules) | **42.86%** |

`go tool cover -func` cannot read the wider profile at all: `no required module provides package
k8s.test/hack/tools`. prettycov renders it.

## Corpus

`internal/discover/testdata/topologies/*.txtar` — 12 shapes, each stating what the go tool reports.
txtar because `cmd/go` uses it, diffs readably, and comes from `x/tools` (already required).

| topology | on disk | `go list -m` | `go list ./...` |
| --- | --- | --- | --- |
| nested | 3 | 1 | 1 |
| not-in-workspace | 3 | 2 | 1 |
| sibling-tools | 2 | 1 | 1 |
| workspace | 2 | 2 | 1 |
| single | 1 | 1 | 2 |

## Triangulation

`TestScanRealRepos` (`PRETTYCOV_REAL=path1:path2`, `#tag,tag` optional) scans checkouts in
`_reference/repos`. Every directory holding Go source must be a discovered package, a directory
`go/build` says is excluded by constraints, or under a module that could not be read. 27 trees, 0
unaccounted. `go list ./...` at the root is what a naive recipe sees.

| repo | kind | modules | in go.work | packages | tested | constrained out | naive `./...` |
| --- | --- | --- | --- | --- | --- | --- | --- |
| kubernetes | infra | 38 | 34 | 3158 | 1625 | 11 | 1472 |
| cosmos-sdk | blockchain | 23 | – | 465 | 228 | 12 | 249 |
| prysm | blockchain | 2 | – | 460 | 250 | 0 | 458 |
| go-ethereum | blockchain | 2 | – | 205 | 139 | 8 | 204 |
| mev-boost | blockchain | 1 | – | 10 | 4 | 0 | 10 |
| grafana | web app | 39 | 34 | 1035 | 645 | 6 | 826 |
| opentelemetry-go | lib | 28 | – | 419 | 137 | 1 | 285 |
| moby | infra | 4 | – | 391 | 237 | 6 | 347 |
| vault | infra | 13 | – | 375 | 230 | 9 | 272 |
| grpc-go | lib | 10 | – | 352 | 155 | 0 | 262 |
| cli (gh) | CLI | 1 | – | 311 | 251 | 0 | 311 |
| hugo | static site | 4 | – | 192 | 143 | 1 | 192 |
| terraform | infra | 11 | – | 191 | 134 | 1 | 179 |
| etcd | infra | 14 | 14 | 180 | 99 | 4 | 12 |
| ebiten | game engine | 1 | – | 172 | 44 | 10 | 172 |
| prometheus | infra | 5 | 5 | 121 | 93 | 2 | 113 |
| helm | CLI | 1 | – | 71 | 59 | 0 | 71 |
| fyne | GUI toolkit | 1 | – | 68 | 46 | 2 | 68 |
| bubbletea | TUI | 3 | – | 64 | 2 | 0 | 1 |
| delegator | web app | 5 | 5 | 26 | 6 | 0 | 3 |
| nats-server | infra | 1 | – | 21 | 13 | 0 | 21 |
| gin | web framework | 1 | – | 7 | 6 | 0 | 7 |

Constrained-out is never noise; it is always a category the project meant to exclude:

| pattern | seen in |
| --- | --- |
| `//go:build tools` (tools.go) | prometheus, opentelemetry-go, terraform, moby (`man`) |
| GOOS — windows, darwin, js, playstation5 | moby (5), kubernetes (3), ebiten (10), fyne (2) |
| `//go:build ignore` | grafana (5) |
| GOARCH/cgo — `cgo && amd64` | etcd (4) |
| bespoke suite tags — `mage`, `fuzzing`, `_testonly`, `blackbox`, `system_test` | grafana, prometheus, vault (9), cosmos-sdk (5) |
| `//go:build dummy` — a Go file whose only job is to stop `go mod vendor` pruning a C directory | go-ethereum, cosmos-sdk (7 each, both `libsecp256k1`) |
| `//go:build acceptance` | delegator — the whole suite, invisible without the tag |

Broken modules exist in released repositories and are not a synthetic worry:

| repo | module | failure |
| --- | --- | --- |
| hugo | `internal/warpc/genwebp` | empty go.mod — a fence around a directory of C |
| bubbletea | `tutorials` | `updates to go.mod needed; to update it: go mod tidy` |

Aborting on either would have reported nothing for hugo's 192 packages or bubbletea's 64.

Scale: 23 trees in 14s total. On grafana the walk is 13ms of 27,313 entries; the cost is one
`go list` per module, run concurrently. The floor is the root module's own `go list`, 1.6s.

## Running it: what a per-module loop produces

`go test -covermode=count -coverprofile=<abs> ./...` per module, on real checkouts.

| repo | modules | packages in profile | entirely uncovered | what they were |
| --- | --- | --- | --- | --- |
| gin | 1 | 7 | 1 | genuine |
| bubbletea | 3 | 64 | **63** | all in the `examples` module |
| helm | 1 | 69 | 10 | *mixed*: test helpers, a vendored copy, real gaps |
| mev-boost | 1 | 9 | 5 | `cmd/*`, `common`, `config`, root |
| nats-server | 1 | — | — | **exit 1 with 45,109 profile lines** (63.86% usable) |
| delegator | 5 | 26 | — | all 5 modules exit 0 without `-tags=acceptance` |

Three run outcomes, all observed: clean; failed **with** data (nats); failed **without** data
(bubbletea/tutorials, stale go.mod). So a run's status and its profile are independent facts.

Granularity: bubbletea's 63 uncovered packages are one module — one exclusion rule, not 63. helm's
10 have no rule that separates helpers from real gaps, and excluding a package saves no time
because it is in the same invocation. **Exclusion is module-shaped; package filtering is
presentation.** 11 of 22 repos carry a non-product module (`examples`, `tools`, `hack`, `docs`).

Per-package invocation tax, measured warm on nats-server: 0.47s for one `./...` against 1.64s for
21 separate invocations — **~55ms per extra invocation**, ~170s at kubernetes' 3158 packages.

## Measuring them: one `go test ./...` per module

`coverage.Measure` over 15 checkouts, `-run=XXXNOMATCH` so every package compiles and is measured
without waiting for the suites. Every module discovery could read either produced a profile or said
why not.

| repo | modules | measured | no packages | no data | outside `go.work` |
| --- | --- | --- | --- | --- | --- |
| grafana | 39 | **39** | 0 | 0 | **5** |
| opentelemetry-go | 28 | 28 | 0 | 0 | – |
| cosmos-sdk | 23 | 20 | 3 | 0 | – |
| etcd | 14 | 14 | 0 | 0 | 0 |
| vault | 13 | 13 | 0 | 0 | – |
| terraform | 11 | 11 | 0 | 0 | – |
| grpc-go | 10 | 10 | 0 | 0 | – |
| prometheus | 5 | 4 | 1 | 0 | 0 |
| delegator | 5 | 5 | 0 | 0 | 0 |
| hugo | 4 | 1 | 2 | 1 | – |
| bubbletea | 3 | 2 | 0 | 1 | – |
| go-ethereum | 2 | 2 | 0 | 0 | – |
| gin, helm, mev-boost, nats-server | 1 | 1 | 0 | 0 | – |

grafana's five are the ones its own `shard.sh` loses: reached here only because a module the
workspace omits is run with `GOWORK=off`, where `./...` otherwise matches nothing.

Every "no packages" is a whole module behind a build tag — prometheus `internal/tools` and
cosmos-sdk's three `//go:build system_test` suites — and each becomes measurable the moment the tag
is passed, because tags reach discovery and the run together. Every "no data" is a module discovery
already flagged: hugo's empty `genwebp/go.mod`, bubbletea's stale `tutorials`.

hugo distinguishes two C-fencing modules that a coarser design would merge:

| | go.mod | state |
| --- | --- | --- |
| `internal/warpc/genavif` | `module gohugoio/hugo/…/genavif` | declares a module, holds no Go — **no packages** |
| `internal/warpc/genwebp` | 0 bytes | declares nothing — **cannot be read** |

## What a switch from a hand-written recipe looks like

delegator is the one repository here whose own recipe we can run: `docker compose up -d`, then the
acceptance suite against a real database. Its Makefile publishes **94.01%**.

Measured three ways, correctly merged (summing raw profile lines double-counts blocks that appear in
more than one file — the same mistake this document warns about elsewhere):

| | coverage | statements | packages |
| --- | --- | --- | --- |
| ours, `./...` per module | 25.20% | 290/1151 | 24 |
| ours, `-coverpkg=./...` per module | 27.45% | 316/1151 | 24 |
| ours, `-tags=acceptance -coverpkg=./...` | 50.22% | 578/1151 | 24 |
| its checked-in profile | 94.01% | 534/**568** | **14** |

With the tag, on the same 14 packages: theirs 94.01%, ours **82.75%** — and 11 of the 14 match to
the statement, including every package that read zero without it:

```
/scraper/store/pgxstore  40/53 = 40/53     /web/handler         34/37 = 34/37
/web/store/pgxstore      55/57 = 55/57     /web/handler/bind    18/21 = 18/21
```

That is the tag plumbing working end to end on a real repository: discovery found the packages
behind `//go:build acceptance` and the run used them, because both read the same tags.

### The last 11 points are cross-module attribution

Every remaining difference was in one module:

| | theirs | ours, per-module |
| --- | --- | --- |
| `/pkg/clock` | 2/2 | 0/2 |
| `/pkg/pgxdb` | 36/40 | 0/40 |
| `/pkg/httpkit` | 39/40 | 26/40 |
| `/pkg/logger` | 93/96 | 80/96 |

These are covered by tests living in the `web` and `scraper` modules. delegator passes one flat
`-coverpkg` list spanning all four (3 root + 6 pkg + 5 scraper + 8 web), which resolves because it
has a `go.work`.

This was first written up here as a ceiling of the per-module design — one `go test` per module
cannot credit a package in another, so cross-module attribution was "not reproducible by
construction". That is wrong, and it was asserted without being tried. The per-module runs are fine.
What has to widen is the `-coverpkg` list, not the invocation: give each module's run a list
spanning the whole workspace and each module's tests credit its siblings, and the merge unions them.
Measured, that is **94.04%** against their 94.01%.

`Config.CrossModule` does exactly that, and is off by default:

- `go test ./...` attributes to the package under test, and `-coverpkg` exists to make widening
  deliberate. Off is go's own answer.
- Off, a module is measured by its own tests, which is what says whether it stands up as something
  publishable alone. `pkg/pgxdb` at 0% is a fact worth knowing: nothing in `pkg` tests it.
- It costs. Every test binary is compiled with every workspace package instrumented, so it grows
  with the square of the repository. delegator's 22 packages are free; grafana's 39 modules are not.

A `go.work` does not argue the other way. It is how a monorepo is structured, not a declaration
about test attribution.

Excluded modules stay out of the list. They were skipped to keep them out of the measurement, and
instrumenting them anyway would put their statements back into every other module's profile.

### Reproducing their recipe

`-exclude` takes the whole Makefile list as one pattern, and the four steps are additive:

| | total |
| --- | --- |
| plain `./...` per module | 25.20% |
| `-exclude` their five patterns | 50.88% |
| `-tags=acceptance -coverpkg=./...` | 50.22% |
| both | 82.81% |
| both, plus `CrossModule` | **94.04%** |
| their published number | 94.01% |

82.81 against the 82.75 the package-level arithmetic predicted — the difference is two files
`-coverpkg` linked and their run did not. `CrossModule` closes the rest, at 94.04%.

Worth noting what the exclusions are worth: 25.20 → 50.88 is the single largest step, and none of
it was reachable before, because `coverage.Config.Exclude` matches whole modules. Of delegator's
five patterns only `./migrator/` names one; `cmd/` alone is 354 statements sitting inside the root
module, untouchable by a module-level filter. Exclusion for speed and exclusion for the denominator
turned out to be two features, and only the one nobody asked for existed.

The lesson is about the report, not the runner: anyone moving from a hand-written recipe watches the
number fall and concludes the tool is broken. It has to be able to say *these packages have no tests
of their own* and *this ran without -tags=acceptance*. That is the completeness output, and it is
not optional.

## The second repository with a number to check against

Of the five repositories here that have a `go.work`, only two publish a Go coverage figure at all.
delegator's Makefile is one. etcd's codecov badge — **69.68%** on main — is the other. prometheus
has no `-coverprofile` anywhere and no codecov config, grafana profiles only its integration
targets, and kubernetes publishes nothing. Two numbers in the whole set is a thin base to validate
against, and worth saying plainly.

etcd, its own tests, everything merged:

| | per-module | CrossModule |
| --- | --- | --- |
| everything | 38.53% | 60.29% |
| less `tests/` | 41.39% | 63.60% |
| less generated `.pb.go` | 43.48% | 63.83% |
| **their codecov ignores** | 47.69% | **68.32%** |

68.32 against 69.68, and it takes both: widening alone leaves 22 points on the table, their ignores
alone leave 21. The remaining 1.36 is unaccounted — codecov merges unit, integration and e2e across
CI shards and this is one pass of `go test`, and two etcd modules fail here.

### How golangci-lint does it

Worth checking before inventing something, since it solves the same problem at scale
(`pkg/result/processors/exclusion_paths.go`, v2.13.1):

- a **list** of compiled regexps, one per configured pattern, not a single alternation
- **unanchored**, against the file's relative path: `pattern.MatchString(issue.RelativePath)`
- a **counter per pattern**, incremented on every exclusion
- reported in `Finish()`: `"Skipped %d issues by pattern %q"`, and with `warn-unused`,
  `"The pattern %q match no issues"`
- plus `PathsExcept` as an inverse allow-list, and `NormalizePathInRegex` for Windows separators

So unanchored matching on the file path is the norm rather than the mistake, and what makes it safe
is not anchoring but the count. `cmd/` also taking `pkg/subcmd` is invisible in a total and obvious
beside the pattern that did it. `-exclude` is a list, it is counted, and both the count and a
pattern that matched nothing are printed to stderr on every run.

The stakes differ, which argues for saying it louder here rather than behind a verbose flag: an
over-broad exclusion in golangci-lint hides a warning, and here it inflates a number that gates CI.

### What etcd changed about exclusion

`-exclude` matched the package path at first, so that a pattern named a directory and could not also
catch a file called the same thing. etcd's ignore list is `**/*.pb.go` and `**/*.pb.gw.go` —
generated protobuf, four points of its total — and no pattern over package paths can express that.
Ignore lists in the wild are file globs. It matches the file's full path now, and
`-exclude='/tests/|\.pb(\.gw)?\.go$'` is their config.

## What widening actually costs

Warm cache, `-run` matching nothing, so the timing is the instrumenting and linking rather than any
test body:

| repo | packages in the workspace | per-module | CrossModule |
| --- | --- | --- | --- |
| delegator | 25 | 1.1s | 1.7s |
| prometheus | 121 | 2.1s | 5.6s |
| etcd | 180 | 5.0s | 13.3s |
| grafana | 1030 | 32.2s | did not finish in 17 min |

Under a couple of hundred packages it is a small multiple. grafana is the one that justifies the
default: its widened pass was still going after 17 minutes against 32 seconds narrow, and had grown
the build cache by about 19 GB when it was stopped — every test binary carrying all 1030 instrumented
packages. The shape of the cost was guessed at here before it was measured, and the guess happened
to be right, which is not the same as having known.

## What `go test` actually does with coverage

Read from cmd/go rather than inferred, because every one of these was a surprise.

`SelectCoverPackages` intersects the `-coverpkg` patterns with `TestPackageList`, which walks each
target's transitive `Imports` plus its `TestImports` and `XTestImports`. So:

```
instrumented = patterns ∩ deps(targets, tests included)
```

`go list -e -deps -test -f {{.ImportPath}} ./...` is the same walk. Naming anything outside the
intersection instruments nothing and prints `no packages being tested depend on matches for pattern
X` on **stderr** — one per unreachable pattern per module, which at 34 workspace modules is around
1100 lines.

Three exclusions no pattern can override, each a hole that would otherwise read as a defect:

| skipped | why |
| --- | --- |
| a package with no non-test Go files | `len(p.GoFiles)+len(p.CgoFiles) == 0` — test-only packages are never instrumented |
| `unsafe` | comes from the compiler |
| `sync/atomic`, `internal/runtime/atomic` under `-covermode=atomic` | the instrumentation itself uses them |

With `-race`, runtime packages become `regonly` — registered for the ID scheme, not counted.

### Which stream carries what

| | stdout | stderr |
| --- | --- | --- |
| test output, plain or `-v` | everything, including a test's own `os.Stderr` writes | empty |
| build error, plain | `FAIL pkg [build failed]` | the compiler error |
| build error, `-json` | all of it, as JSON | empty |
| `-coverpkg` warnings | — | here, in both modes |

Test output never reaches go test's stderr; it is captured and folded into stdout. So **stdout is
the unpredictable stream and must never be written to**, and stderr carries only toolchain
diagnostics — which is where ours belong.

### Flags, and why nothing here parses them

| | |
| --- | --- |
| last `-coverprofile` wins | so ours goes last; a caller's would otherwise send the data somewhere nothing reads |
| last `-coverpkg` wins | same |
| overlapping package patterns | deduped, so a caller's stray pattern is harmless |
| `-coverpkg` alone | implies `-cover`, writes no file |
| `-coverprofile` alone | implies `-cover` |

Position settles every collision. Rejecting instead would mean telling a flag's value from a package
pattern, which needs the arity of every flag we do not own.

### Merging N runs

`go test -json` has no stream-level preamble or terminator; every event is keyed to a package, and
build failures carry `ImportPath` instead. Concatenating N modules' streams is therefore
indistinguishable from one run over the same packages — verified across passing, build-failed and
no-test-files modules, 0 unparseable lines.

That holds **only while runs are sequential**. go itself buffers each package and emits in *listed*
order, so a slow first package blocks everything behind it:

| listing order | fast (1s) emitted | slow (5s) emitted |
| --- | --- | --- |
| slow first | 5.12s | 5.12s |
| fast first | 1.12s | 5.12s |

Parallel modules would need the same buffering, and the same worst case. Sharding avoids the
question: it is external, one invocation per CI job, each writing its own profile.

## The shape this implies

Two commands. The seam is where the side effects are.

```
prettycov measure [dir] [-profile=coverage.out] [-cross-module] [-- go test args]
prettycov [profile...] [-exclude=RE]...
```

Not `test`: it is not a `go test` proxy and cannot be one — the positional is a directory rather
than a package pattern, it runs N invocations, and it owns flags a caller might pass. `measure` is
what the internal API already calls it.

- **stdout** is go test's, byte-for-byte. Never ours — a `-json` consumer would break, and we cannot
  enumerate the consumers.
- **stderr** takes our diagnostics, prefixed.
- **exit** is go test's: 0, or 1 for anything wrong. 2 is reserved for prettycov's own failure,
  which go test never returns.
- **`-exclude` is render-only.** It is reporting policy, and `measure` does not report. Its patterns
  are file paths; instrumentation is decided per package, so using them to skip instrumenting is
  only safe where every file in a package certainly matches.
- **Targets are never filtered.** A package you do not count may hold tests crediting one you do.
  For the same reason, skipping a whole module is unsafe under `-cross-module` — it loses what that
  module's tests credited elsewhere.
- **Scope by directory**, which is the only lever on the instrumented set discovery can reason
  about.

## Sharding: exactly one project does it

grafana, `scripts/ci/backend-tests/shard.sh` — 8 ways for unit, 4 for integration:

```bash
find . -name go.mod -exec dirname {} ';' | awk '{print $1 "/..."}'   # walk for modules
go list -f '{{.Dir}}' -e "${dirs[@]}"                                # list their packages
find "$PKG" -maxdepth 1 -name '*_test.go' … || unset PACKAGES[i]     # keep those with tests
(( (i % m) + 1 != n )) && unset 'PACKAGES[i]'                        # round-robin by index
go test -vet=off -short -timeout=30m "${PACKAGES[@]}"                # ONE invocation per shard
```

An independent reimplementation of `internal/discover`: filesystem walk, `go list -e`, a HasTests
filter. It carries the bug the per-module `GOWORK=off` loop exists to prevent — every module
pattern goes to **one** `go list` from the root, which is workspace-scoped:

| repo | theirs | ours | lost |
| --- | --- | --- | --- |
| grafana | 1030 | 1035 | the 5 modules outside `go.work`; `scripts/modowners` has a test file |
| grpc-go (no go.work) | 262 | 352 | 9 modules of 10 |

Silently — `-e` swallows it, no stderr. `backend-unit-tests.yml` calls `shard.sh` with no `-d`, so
it takes that path; `pkgs-with-tests-named.sh` repeats the same `go list` line.

## Enumerating packages goes stale

NATS's `scripts/cov.sh` is the most careful script in the survey — per-package isolation, panic
detection, flapper tolerance, nightly cron — and its package list is hand-maintained:

| | |
| --- | --- |
| packages in the repo | 20 |
| packages named in cov.sh | **6** |
| missing, with tests | `server/stree` (1294 src / **2059 test** lines), `server/pse`, `server/gsl`, `server/thw`, `server/tpm`, `internal/ocsp`, … |

Their git history shows the maintenance: *"Fix code coverage script (remove auth package that no
longer exists)"* (2017). A package never named cannot produce an empty profile, so their own panic
check cannot catch this.

The panic check itself (2022, six years after the per-package split, which was for `-coverpkg`
scoping) states the policy this tool needs:

> We are ok with a flapper or two… However, if there is a test panic, then all other tests within
> this package will NOT run, which then would have possibly a massive impact in the code coverage
> percentage.

Failing tests tolerable, lost data fatal — `--fail-on=incomplete`, justified in a commit message.

## Test tag vocabulary

Non-platform build tags on `_test.go` across 22 checkouts, most frequent first:

```
258 ignore_autogenerated   44 minimal    38 isolated    30 mobile    24 enterprise
 23 cluster_proxy          18 race       14 system_test 14 skip_js_tests  14 gofuzz
 11 e2e                    10 integration 10 testonly    5 fuzz      4 withdeploy
```

A `--integration` flag guessing `integration|e2e|system_test` catches 35 uses and misses vault's
`isolated` (38 alone), `testonly`, hugo's `withdeploy`, go-ethereum's `integrationtests`. Those
packages' test files then are not in the package at all, so `HasTests` is false and they report as
honest zeros — silent under-measurement by another door. Projects must supply their own tags.

## Survey of 38 repositories

Top-starred Go projects plus the ones already here. Searched three ways, because the first two were
wrong about their own coverage:

| pass | method | files read | `GOCOVERDIR` |
| --- | --- | --- | --- |
| 1 | guessed filenames (`Makefile`, `scripts/*.sh`, `.github/workflows/*`) | ~530 | 1/38 |
| 2 | same, recursive, more types | 1688 | 1/38 |
| 3 | **grep every file for `go test`/coverage, read what matched** | 197 by content | 2/38 → **1 real** |

Pass 1 missed prometheus's `Makefile.common` (its whole test setup) and syncthing's `build.go`
(coverage driven from Go, not make). kubernetes uses Prow, in another repository, so its CI is
invisible here either way. Only pass 3 has a file list that was not invented.

**Findings, evidence-first:**

| signal | repos |
| --- | --- |
| produces a coverage profile | 28/38 |
| `-coverpkg` | 13/38 |
| uploads to codecov | 9/38 |
| `go tool cover -func` | 6/38 |
| merges with `gocovmerge`/`gocov` | 5/38 |
| coveralls | 3/38 |
| module iteration that runs tests | 4/38 |
| `go tool covdata` | **1/38** |
| `go build -cover` / `GOCOVERDIR` | **1/38** |

The single `GOCOVERDIR` user is lazygit. gogs looked like a second and is not — it sets
`GOCOVERDIR=os.TempDir()` on a helper process, discarding the counters.

**Multi-module breakdown:**

| | |
| --- | --- |
| multi-module (≥2 `go.mod`) | **18/38** |
| of those, with a `go.work` | **5** |
| enumerate their modules by hand somewhere in build/CI | **11** |
| enumerate them specifically to run tests | **4** (cosmos-sdk 16 sites, grpc-go 3, etcd 1, terraform 1) |

The eleven do it eleven different ways: `find . -name go.mod`, `go list -m`,
`go work edit -json | jq`, and in grafana's case a 95-line Go program using `x/mod/modfile` with a
skip-tag mechanism reading comments out of `go.work`.

Top-starred skews toward mature CI, so if `go build -cover` is 1-in-38 *there*, the rate across
ordinary repositories is lower, not higher. The sample cannot speak for corporate services with
external e2e suites, which is where that pattern actually lives.

**Two repos worth reading in full:**

*lazygit* hit our exact problem and invented our exact workaround, with the reason in a comment:
*"the GOCOVERDIR env var (which you typically pass to the test binary) will be overwritten by the
test runner. So we're passing LAZYGIT_COCOVERDIR instead"*. It writes pods for everything — unit
included, via `-args -test.gocoverdir` — and converts once at the end, never using `-coverprofile`.

*gitea* validates before merging, because `gocovmerge` fatals on malformed input:

```make
grep '^\(mode: .*\)\|\(.*:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+ [0-9]+ [0-9]+\)$$' coverage.out > coverage-bodged.out
```

## What cross-module instrumentation costs

Warm cache, `-run` matching nothing, so the timing is compilation and linking rather than test
bodies:

| repo | workspace packages | narrow | widened |
| --- | --- | --- | --- |
| delegator | 25 | 1.1s | 1.7s |
| prometheus | 121 | 2.1s | 5.6s |
| etcd | 180 | 5.0s | 13.3s |
| grafana | 1030 | 32.2s | **did not finish in 17 minutes** |

grafana's widened pass had grown the build cache by ~19 GB when it was stopped.

Coverage instrumentation itself is cheap — the same 20 hugo packages cost 1016 MB of build cache
plain and 1113 MB with `-coverprofile`, about 10%. hugo is expensive because every test binary
statically links esbuild, dartsass and image machinery: 68 MB each, 143 of them. `go test ./...`
pays that too.

## What the corpus is actually doing wrong

Three repos, three distinct failures, each measured rather than inferred.

**opentelemetry-go — a filter applied to the wrong list.** Its loop greps `semconv/v.*` and
`third_party` out of the *targets*, but `-coverpkg=./...` re-instruments them, so 1,165 uncovered
generated statements stay in the denominator. Published **89.94%**; what the filter intended is
**94.89%**, which is what `measure` plus `-exclude` produces over the same 331 files.

**grpc-go — nine of ten modules unmeasured.** Its CI is one correct line,
`go test -coverprofile=coverage.out -coverpkg=./... ./...`, which reaches the root module only. The
missing 103 files include **2,065 statements of non-example code**: `security/advancedtls` at 74%,
`gcp/observability` at 71%, `stats/opencensus` at 87%, `cmd/protoc-gen-go-grpc` at 0% — all
reported nowhere.

**vault — a workaround for a bug fixed in 2018.** 55 lines running one `go test` per package and
concatenating, citing #6909, and still single-module so 12 of its 13 go unmeasured.

## An enterprise service, end to end

staking-pukara-backend, single module, 94 packages, with four suites and one number:

| suite | how | coverage |
| --- | --- | --- |
| unit + integration | `go test -tags=integration -coverpkg=$(GO_PACKAGES) ./...` | `coverage.out`, after a seven-pattern `grep -v` |
| system | `go test -c -tags=testrunmain -cover` → run as the server, cucumber drives it | `system.out` — nothing consumes it |
| api-e2e | separate repo, TypeScript + cucumber → `BASE_URL` | none |
| acceptance | separate repo, TypeScript + cucumber → dev/uat | none |

`gocovmerge` is installed by `make deps` and used by no target. Sonar reads `coverage.out` alone.
The mechanism for capturing the e2e coverage already exists — `test-system` builds the server
instrumented, and the e2e suite can point at `localhost` — and the only missing link is that nothing
joins the pieces.

Its exclusion, measured on its own profile: **251 files to 139, 44% removed**. `eth2/spec` 53,
`internal` 30, `.*test` 36, `test_helpers.go` 10, `_gen.go` 2, `backend-server.go` 1. Applied to one
of the four sources.

`backend-types.go` matches nothing in a given run — but it is a guard for a file that appears after
`make api-server-generate`, not a dead pattern. "Matched nothing" is a fact about the run, not a
verdict on the pattern, and a diagnostic that says otherwise would push someone to delete a working
guard.

## Why projects enumerate packages (it predates modules)

`-coverpkg` did not accept patterns until **Go 1.10** (Feb 2018). Its release notes:

> The `go test -coverpkg` flag now interprets its argument as a comma-separated list of **patterns**
> to match against the dependencies of each test, **not as a list of packages to load anew**. […]
> Also, the `go test -coverprofile` option is now supported when **running multiple tests**.

So before 2018 you had to enumerate, and `-coverprofile` could not span packages at all (#6909).
vault's `scripts/coverage.sh` still runs one `go test` per package and concatenates, and still cites
`code.google.com/p/go/issues/detail?id=6909` in its header. Both constraints were lifted in one
release; everything since is Makefiles copied forward.

Most of the corpus does **not** enumerate — they pass a pattern:

| project | modules | `-coverpkg` |
| --- | --- | --- |
| lazygit | 1 | `github.com/jesseduffield/lazygit/pkg/...` |
| kubernetes | 39 | `./pkg/kubelet/...` |
| opentelemetry-go | 28 | `go.opentelemetry.io/otel/...` |
| delegator | 5 | enumerated from `go list` over four module patterns |
| etcd | 14 | enumerated from `go work edit -json` |

The two that enumerate are the two whose set must **cross module boundaries**, which no relative
pattern expressed before `work`. That is a second, much later reason that produces the same-looking
shell as the 2014 one.

`all` is not a substitute: on staking it is 1064 packages against the project's 94, because it means
the module *and every dependency*.

## The `work` pattern (Go 1.25)

`work` joins `all`, `std`, `cmd` and `tool` as a meta-pattern matching every package in the work
modules — proposal **#71294**, accepted 2025-03-06, milestone Go1.25.

| | `go list ./...` | `go list work` |
| --- | --- | --- |
| kubernetes | 1472 | 3926 |
| grafana | 826 | 1030 |
| etcd | 12 | 180 |
| grpc-go (no go.work) | 262 | 262 |
| opentelemetry-go (no go.work) | 285 | 285 |

`-coverpkg=work` is a pattern, so it also sidesteps `MAX_ARG_STRLEN` entirely. On delegator,
`go test -tags=acceptance -covermode=atomic -coverpkg=work work` plus the same exclusions reproduces
its Makefile exactly: **94.01%**.

It does nothing without a `go.work` — 5 of 38 repos have one — and it only covers modules the
workspace *lists*, so grafana's 11 unlisted modules stay invisible.

## What Go itself cannot do

Open issues, which is where the real pain is:

| issue | | |
| --- | --- | --- |
| **#31280** | +45 | exclude statically unreachable code from the denominator, open since 2019 |
| **#78205** | +20 | `cmd/cover`: a `-text` flag for terminal output |
| **#60182** | 18 comments | coverage from other binaries is not collected in tests |
| **#66506** | | *"reported coverage is still per package"* — **there is no total across packages** |
| **#76789** | | coverage is a property of packages *with tests*, not of executed code |
| **#76098** | | proposal: a package to parse coverage data — none in stdlib |

**#66506 is this tool's reason to exist.** `go tool cover -func` gives one line per function plus a
grand total; `covdata percent` gives one line per package and no total. Neither answers "what is
`internal/` at?", which is why staking hand-rolls
`go tool cover -func coverage.out | tail -n 1 | rev | cut -f1 | rev`.

`./...` and `work` answer only *which packages to instrument*. Exclusion, combination, totalling,
cross-binary collection and terminal output are all somewhere else.

## Go 1.27 inflates statement counts (#80974)

Reproduced on go1.27.0. Go 1.27 splits a straight-line block into several profile blocks at blank
lines and writes the whole run's statement count into **each** of them:

```
repro/x.go:4.2,6.1    5 1     // a := 1; b := 2   -> counted as 5
repro/x.go:7.2,9.1    5 1     // c := 3; d := 4   -> also 5
repro/x.go:10.2,10.10 5 1     // if skip          -> also 5
repro/x.go:11.3,12.1  1 0
repro/x.go:14.2,14.22 1 1
```

Seven statements reported as seventeen, and because the inflated blocks are the covered ones,
`go tool cover -func` says **94.1%** where the answer is **85.7%**. A regression from go1.26.6.

**Severity, measured on prettycov's own profile.** The signature is a block whose *end line is blank
in the source*: 51 of 164 adjacent same-count block pairs carry it. Verified by hand in
`exclude.go`, where lines 26 and 28 are one statement each and the profile gives both 2 — the count
for the straight-line run they were split out of.

A first estimate of 30% was about three times over: it counted every adjacent same-count pair, and
most of those are genuinely distinct blocks that happen to share a count. Detecting this needs the
source, not the profile alone.

Every figure in this document from the 1.27 era is affected. Comparisons between two things measured
on the same toolchain still hold — which is why delegator matched its own Makefile exactly — but any
absolute number, and any comparison against a figure produced elsewhere (codecov's stored etcd
value), is suspect.

## Deriving a package set out-of-band is the recurring bug

Any package set computed outside the build that consumes it can disagree with it.

- staking computes `GO_PACKAGES = $(shell go list ./...)` with no tags, then runs
  `go test --tags=integration`: 94 packages against 98, so four `tests/integration` packages are
  instrumented by the run and absent from its own `-coverpkg`.
- our `tagsFrom` took the *first* `-tags`; go takes the last.
- `-race` sets the `race` build constraint, so it changes what a package contains:

```
go list        -f '{{.GoFiles}}'  ->  [base.go linux.go]
go list -race  -f '{{.GoFiles}}'  ->  [base.go linux.go race.go]
```

Discovery forwards `-tags` only, so `measure -- -race` still scans a set that can differ from the
one the tests compile. `GOOS`/`GOARCH` arrive by environment and so agree; `-mod`, `-overlay`,
`-pgo`, `-msan`, `-asan` are in the same class as `-race`.

The defence is to derive less, not to model more.

## Where the value is, on this evidence

`./...` and `work` answer only *which packages to instrument*. Everything that hurts is downstream
of that, and Go has no answer for any of it:

| stage | Go's answer | who needs it here |
| --- | --- | --- |
| discover modules | none | 18/38 multi-module, 11 hand-roll it |
| run per module | none | 4/38 hand-roll it; `work` covers the 5 with a workspace |
| **combine** | `covdata merge` for pods only, nothing for profiles | staking (4 suites, 1 number), 5/38 vendor `gocovmerge` |
| **validate** | none | gitea greps malformed lines out before merging |
| **exclude** | none (#31280 open since 2019) | 2/38 grep the profile, most use codecov's `ignore` |
| **total per subtree** | none (#66506) | everyone; the reason this tool exists |
| terminal output | none (#78205 proposed) | this tool |

Measured against real projects, `measure` is worth nothing to staking (single module) or delegator
(`go.work`, so `-coverpkg=work` covers it), and worth 2,065 uncovered statements to grpc-go. The
aggregation — a total at every level of the tree — is the part nothing else does, and it is
upstream-blocked rather than merely unimplemented.

## Open

- Discovery must use `GOWORK=off` + `go list -e` (workspace mode misreports identity of omitted modules).
- etcd scale: unit suite 530s to compile+instrument with workspace-wide `-coverpkg`; no single
  suite's profile covers all 180 packages (116/106/108) — merging is required, not optional.

Closed: module on disk but absent from `go.work` — included, because `./...` does not consult
`go.work`, and the file is not readable as intent anyway.
