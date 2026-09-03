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

**Flag contract** — own only what makes the merge valid; forward the rest unparsed after `--`:

| flag | who |
| --- | --- |
| `-coverprofile` | ours, always (temp per run); error if user passes it |
| `-coverpkg` | **theirs** — forwarded, never derived |
| `-covermode` | normalised across runs (mode mismatch = merge failure) |
| everything else | forwarded, never declared — this is what killed goverage |

**Files:** create nothing the user did not name. Per-module intermediates → temp, cleaned.
Per-suite profiles → user-named. Merged result → user-named.

**Failure policy:** always run everything, always report completeness; only the exit code is
configurable — `--fail-on=test` (default) / `incomplete` / `never`. NATS's policy = `incomplete`;
delegator's `|| true` = `never`.

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

`internal/discover/testdata/topologies/*.txtar` — 9 shapes, each stating what the go tool reports.
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

## Open

- Discovery must use `GOWORK=off` + `go list -e` (workspace mode misreports identity of omitted modules).
- etcd scale: unit suite 530s to compile+instrument with workspace-wide `-coverpkg`; no single
  suite's profile covers all 180 packages (116/106/108) — merging is required, not optional.

Closed: module on disk but absent from `go.work` — included, because `./...` does not consult
`go.work`, and the file is not readable as intent anyway.
