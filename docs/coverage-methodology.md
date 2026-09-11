# What coverage is for: the evidence

Companion to [coverage-notes.md](coverage-notes.md), which records what the *toolchain* does. This
one records what the *literature and the practice* say coverage is good for, because prettycov's
entire output is a percentage and that deserves checking rather than assuming.

Sources are cited so every claim can be re-checked. Repository freshness was measured against the
GitHub API on 2026-09-08.

## 1. The number is a weak signal

| finding | study | scale |
| --- | --- | --- |
| Suite **size** correlates moderately-to-very-highly with fault detection; **coverage** correlates only low-to-moderately once size is controlled for | [Inozemtseva & Holmes, ICSE 2014](https://www.cs.ubc.ca/~rtholmes/papers/icse_2014_inozemtseva.pdf) (Distinguished Paper) | 31,000 suites, 5 Java systems, 724k LOC |
| Stronger criteria (decision, condition) give **no** greater insight into effectiveness | same | same |
| **Assertion** count and assertion coverage *are* strongly correlated with effectiveness | [Zhang & Mesbah, FSE 2015](https://people.ece.ubc.ca/amesbah/resources/papers/fse15.pdf) | 6,700 suites, 24,000 assertions, 5 Java projects |

The two together are the useful result. What predicts a good suite is whether outputs are
**checked**, and statement coverage cannot observe an assertion at all — a test with no `assert`
covers exactly as many statements as one with ten. That is a permanent ceiling on what any tool
reading a Go coverage profile can claim, prettycov included.

### The target problem

[Marick, *How to Misuse Code Coverage* (1997/1999)](http://www.exampler.com/testing-com/writings/coverage.pdf)
observed organisations mandating 85% and finding everyone clustered at exactly 85% — *"as if no one
could find any other tests worth writing."* Goodhart's law, two decades before it was fashionable.

## 2. What it *is* for: the misses, not the number

The consensus is unusually tight. Three independent sources, one sentence:

- **Marick**: use coverage as a *hint* about where you haven't tested, then decide in "brain-on"
  mode whether that case is worth covering.
- **Fowler**, [TestCoverage](https://martinfowler.com/bliki/TestCoverage.html): "for identifying
  untested areas of the code, not for assessing the quality of a test suite."
- **Google** (Arguelles, Ivanković, Bender),
  [Code Coverage Best Practices, 2020](https://testing.googleblog.com/2020/08/code-coverage-best-practices.html):
  *"what's not covered is more meaningful than what is."* And specifically: show the **highlighted
  lines, not just a number**, so developers can check whether the *important* code is covered
  rather than debug log lines.

The Go team says the same thing in its own vocabulary. From #53271 (declined), recorded in
[coverage-notes.md](coverage-notes.md): rsc, *"If you have something that reports the uncovered
lines, couldn't it filter them out instead?"* and *"Maybe we should remove the reported percentages
because people focus too much on them?"*

**The misses are the artifact. The percentage is the by-product.**

## 3. What practitioners actually gate on

Nobody credible gates on the total any more. They gate on the diff.

| source | position |
| --- | --- |
| [Chromium](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/docs/testing/code_coverage_in_gerrit.md) | Distinguishes *absolute* from *incremental* coverage (newly added or modified lines only). Named escape hatches: `TESTS_IN_SEPARATE_CL`, `HARD_TO_TEST`, `COVERAGE_UNDERREPORTED`, `LARGE_SCALE_REFACTOR`, `EXPERIMENTAL_CODE` |
| [SonarQube, Clean as You Code](https://docs.sonarsource.com/sonarqube-server/10.6/user-guide/clean-as-you-code) | *"we do not recommend adding conditions for overall code to your quality gate"* |
| [Codecov](https://about.codecov.io/blog/why-patch-coverage-is-more-important-than-project-coverage/) | Patch coverage over project coverage; added `removed_code_behavior` to suppress false failures |

Two failure modes are worth recording because they are mechanical, not cultural:

- **Gate fatigue.** A total threshold on a legacy repo fails on day one and stays red regardless of
  what the current change did. That trains people to ignore the signal — Sonar's stated reason for
  recommending against it.
- **Deletion lowers project coverage.** Removing covered code reduces the covered:total ratio, so
  refactors and deletions fail a project-coverage gate while being unambiguously good. Codecov
  needed a dedicated setting for this.

Both apply directly to `-fail-under` as prettycov ships it.

## 4. Go's own ceiling

- The toolchain measures **statement/basic-block** coverage only. [`cmd/cover`](https://pkg.go.dev/cmd/cover)
  computes approximate basic-block information by studying the source, and **does not probe inside
  `&&` / `||` expressions**. There is no branch or condition coverage in the profile format, so no
  downstream tool can add it. (`rillig/gobco` gets condition coverage by instrumenting separately —
  see the survey in [coverage-notes.md](coverage-notes.md).)
- The profile carries file, line range, column range, statement count and hit count. It carries
  **no** function names, no assertions, no source. Anything a tool wants beyond those five fields
  must come from the working tree.

## 5. Ecosystem freshness

[coverage-notes.md](coverage-notes.md) already surveys what each Go tool *does*. This table adds
the axis that survey lacks: whether it is still being developed. Commit dates mislead here — several
of these have 2025 commits that are CI Go-version bumps. Release dates are the honest signal.

| tool | last release | commits since | state |
| --- | --- | --- | --- |
| `k1LoW/octocov` | — | 2026-09-08 | **active** (508★) |
| `vladopajic/go-test-coverage` | — | 2026-09-07 | **active** (240★) |
| `Azure/gocover` | — | 2026-09-01 | active (17★) |
| `seriousben/go-patch-cover` | — | 2026-04-05 | alive, small (7★) |
| `vearutop/gocovdiff` | v1.4.2, 2023-11 | CI only | mostly frozen |
| `msoap/go-carpet` | v1.10.0, **2022-12** | GH Actions Go bumps only | feature-frozen ~4y (250★) |
| `orlangure/gocovsh` | v0.6.1, **2022-11** | one typo fix, one lint fix | feature-frozen ~4y (388★) |

**The pattern is the finding.** Everything still under development is CI-shaped: gates, diff
coverage, badge publication. Every *local, human-facing* "where is my untested code" tool — the TUI
browser, the source colouriser — has been feature-frozen since 2022, despite healthy star counts.

One prior-art note relevant to §6: `grosser/go-testcov` gates on `// untested sections: 5` — a
budget on the **miss count**, not on a percentage. Someone has already found that the absolute
number of uncovered statements is the more tractable thing to hold a line on.

## 6. What this implies for prettycov

### The constraint is the niche

prettycov reads a profile and **nothing else** — no source tree, no `go.mod`, no git.

- `go tool cover -func` parses the source to find function boundaries.
- `gocovsh` requires `go.mod` and the files.
- `go-carpet` dumps source.
- Every patch-coverage tool needs a git diff.

So prettycov works on a bare profile from a CI artifact, a colleague, a merged multi-module run, or
a repo not checked out. That constraint draws the boundary:

> **prettycov tells you where to look. The source viewers show you the code.**

What still fits inside it: the profile has filenames and line ranges. Files-as-leaves needs no
source. `pkg/store/pgxstore.go:120-134 — 8 statements uncovered` is printable from the profile
alone. That is §2's recommendation carried out to the last step before source is required.

### Three jobs exist; one is ours

| job | evidence | who does it |
| --- | --- | --- |
| **Locate untested code** | Marick, Fowler, Google, rsc | Ours. Every local tool for it is frozen since 2022 |
| **Gate new code** | Sonar, Codecov, Chromium | Solved and actively maintained: `go-test-coverage`, `octocov`, `Azure/gocover`. Needs git |
| **Report an aggregate** | badges, management | Already shipped. Least valuable of the three |

### Consequences

**Drop**, on the evidence above:

- diff/patch coverage — three maintained tools, and it needs git
- source annotation — `gocovsh`, `go-carpet`, `-html`; frozen but adequate, and it needs source
- branch/condition coverage — the profile format cannot express it (§4)
- mutation testing, churn weighting — different tools. Note CodeScene's
  [hotspot model](https://docs.enterprise.codescene.io/versions/4.2.2/guides/technical/hotspots.html)
  is churn × complexity and contains **no coverage term** at all, so "risk-weighted coverage" is not
  a practice anyone follows
- threshold filters (`-under=N`, comparison operators) — a cut on a metric with no defensible cut
  point, which is Marick's 85% in miniature

**Keep**, all inside the profile-only constraint and all serving job #1:

- uncovered statement counts made **visible and rankable**. `CoverageStats.Uncovered` already exists
  and is already rolled up; the renderer discards it. It is also the only metric here that composes
  with the tree — percentages cannot be averaged up a branch, counts sum exactly, and a parent's
  count is always ≥ any child's, so ordering by it leaves the hierarchy intact
- files as leaves — the deepest level the profile can supply
- descending to a subtree — `PathTree.Get` exists and is unused by the CLI

**Write down**, cheapest item on the list: the README currently sells the number. `-total` and
`-fail-under` deserve a paragraph on what they cannot tell you, and on the fact that a
total-coverage gate is the configuration the field has largely abandoned (§3).

## 7. What people ask the toolchain for

Added 2026-09-11. §§1–6 asked what coverage is *for*; this asks what Go users say they cannot get.
Open `cmd/cover` issues, by reactions:

| 👍 | issue | ask |
| --- | --- | --- |
| 20 | [#78205](https://github.com/golang/go/issues/78205) | `-text`: summary table + **uncovered line ranges** + annotated source, in the terminal |
| 11 | [#70306](https://github.com/golang/go/issues/70306) | branch coverage |
| 9 | [#36685](https://github.com/golang/go/issues/36685) | HTML output is unusable with a screen reader |
| 5 | [#31519](https://github.com/golang/go/issues/31519) | number of uncovered lines, because "small packages and giant packages are treated the same" |
| 3 | [#76789](https://github.com/golang/go/issues/76789) | coverage across packages, not only packages with tests |
| 3 | [#66713](https://github.com/golang/go/issues/66713) | a second figure, "97.3% without error handling" |
| 1 | [#45846](https://github.com/golang/go/issues/45846) | flags to print coverage over packages or files |
| 0 | [#75770](https://github.com/golang/go/issues/75770) | an HTML summary page of files and percentages |

Four of these — #78205, #36685, #45846, #75770 — are one request: **a readable summary, in a
terminal, without a browser.** #78205 gives the motivation as SSH and CI logs; #36685 is someone
who cannot use the HTML at all. Nobody asks for statistics.

That is the finding: prettycov is not short of metrics, it is the thing people keep asking the
toolchain for. What it does not yet emit is the other half of #78205 — the line ranges.

## 8. Ranking and filtering, across ecosystems

| tool | ranking | filtering |
| --- | --- | --- |
| [coverage.py](https://coverage.readthedocs.io/en/latest/commands/cmd_report.html) | `--sort=name\|stmts\|miss\|branch\|cover` | `--skip-covered`, `--skip-empty`, `--no-skip-covered` |
| [nyc/istanbul](https://manpages.ubuntu.com/manpages/jammy/man1/nyc.1.html) | — | `--skip-full`, `--skip-empty` |
| [gcovr](https://gcovr.com/en/latest/manpage.html) | `--sort={filename,uncovered-number,uncovered-percent}`, `--sort-reverse` | — |
| [simplecov-console](https://www.rubydoc.info/gems/simplecov-console/0.9.1) | sorts by % **by default**, `max_rows` | fully-covered rows excluded **by default** |

Three things transfer:

- **Ranking is a column sort in every one of them**, because each already prints a flat table. A
  ranked view is not a flag for us, it is a second report.
- **Filtering is the more universal feature** — three of four, one of them by default — and it is
  the one that survives translation to a tree: pruning a fully-covered subtree makes *absence*
  informative, which a filtered table cannot do.
- **Filtering never moves the total.** coverage.py: skipping covered files "changes only the
  display, not the computed totals or `--fail-under` behavior." nyc says the same. Copy this.

One bug to avoid, from [gcovr#918](https://github.com/gcovr/gcovr/pull/918): sorting by uncovered
*percent* made 0-of-1 and 100% collide, interleaving the best and worst files alphabetically. Their
fix — sort key primary, total secondary, filename tertiary, and 0/0 sorting as 100% — is the shape
any ranking here would need, and is a further argument that counts rank cleanly where ratios do not.

## 9. Measurements

Taken 2026-09-11 against four repositories: delegator, [gin](https://github.com/gin-gonic/gin),
[dive](https://github.com/wagoodman/dive), and prettycov itself. Profiles generated with
`go test -covermode=atomic`; delegator's is the corrected 1.26 fixture in `testdata/`.

### Misses are small and scattered

| repo | uncovered blocks | 1-statement blocks | largest block |
| --- | --- | --- | --- |
| delegator | 32 | 93% | 2 |
| gin | 34 | 94% | 2 |
| dive | 1289 | 69% | 21 |
| prettycov | 101 | 80% | 7 |

Uncovered *statements* say how much; uncovered *blocks* say what kind. 34 statements in 32 blocks is
thirty untaken branches; 34 statements in two blocks is one untested function. The two are the same
percentage and not the same afternoon. Concentration varies enough between repositories to carry
information — dive has a single 21-statement hole, delegator's worst is two.

### #66713 has no data, and the data refutes it

The proposal asks for a second figure excluding `if err != nil` blocks, on the grounds that careful
error handling lowers the percentage. It offers no measurement. Classifying each uncovered block by
whether it is error-shaped:

| repo | uncovered blocks that are error handling |
| --- | --- |
| delegator | 90% |
| dive | 32% |
| gin | 23% |
| prettycov | 14% |

So the premise is repository-specific, not general. And on the repository where it holds, the
proposed figure inverts the signal:

```
delegator as reported:              91.54%   (34 of 402 uncovered)
"without error handling" (#66713):  99.19%   ( 3 of 371 uncovered)
hidden:                             31 untested error-handling statements
```

A service that talks to Postgres and an HTTP API would report 99.19% having never exercised a
database or network failure. Against
[Yuan et al., OSDI '14](https://www.usenix.org/conference/osdi14/technical-sessions/presentation/yuan)
— 198 production failures across Cassandra, HBase, HDFS, MapReduce and Redis — that is the wrong
direction: **92%** of catastrophic failures came from incorrect handling of non-fatal errors the
software had explicitly signalled, **58%** were reachable by simple testing of error-handling code,
and **23%** "would have been exposed by 100% statement coverage testing of the error handling
stage." The statements #66713 removes from the denominator are the ones a quarter of catastrophic
failures would have been caught by.

The complaint underneath is still legitimate — the ratio does punish added error handling, and some
branches are untestable without heroic mocking. The answer to that is coverage.py's
`# pragma: no cover`: an explicit, per-line, reviewable decision, not a silent category exclusion.

### A hypothesis that died

Block size was proposed as a profile-only proxy for "is this error handling", which would have let a
profile-only tool answer #66713 without source. Pooled across all four repositories:

```
1-stmt blocks: 1041   error-shaped: 29%
2-stmt blocks:  276   error-shaped: 43%
3+ stmts:       139   error-shaped: 33%
```

No correlation. The proxy does not exist; that question needs source.

### Coverage cannot see the value of a test — demonstrated locally

prettycov's `property_test.go` against its `crosscheck_test.go`, same package, measured separately:

```
crosscheck only:   85 blocks
properties only:   72 blocks
only properties:    0    only crosscheck: 13    shared: 72
```

The property test covers a **strict subset**. By statement coverage it is pure redundancy. It is
also the test that caught the row-ordering bug that reached main through two cleanup passes and a
review. This is §1's ceiling — Inozemtseva & Holmes, Zhang & Mesbah — reproduced on this repository
in one command, and it is the reason no feature here may infer a test's *value* from coverage.

### Filtering pays unevenly

Rows at `-depth=max -files`, and how many are fully covered:

```
gin:   54 rows, 42 fully covered  → --skip-covered hides 77%
dive: 100 rows,  1 fully covered  → --skip-covered hides  1%
```

It helps well-covered repositories, which is where a remaining gap is hardest to find, and does
nothing for badly-covered ones, where gaps are everywhere anyway. A quality-of-life flag, not a
headline.

## 10. Profile-to-profile comparison

Distinct from patch coverage. Every maintained diff tool — `go-test-coverage`, `octocov`,
`gocovdiff`, `go-patch-cover`, Codecov — takes one profile plus a git diff and asks whether changed
lines are covered. Comparing two *profiles* is a different axis, and splits three ways:

| case | needs source? | sound? |
| --- | --- | --- |
| same code, two runs | no | exact — blocks match by position |
| different revisions, per-file or per-package totals | no | exact — no line matching involved |
| different revisions, per-block | yes | lines have shifted; this is what git buys the tools above |

The first is unserved by anything in Go and costs nothing to compute. It must be scoped to *"what
did this run cover"* and never to *"is this test worth keeping"* — see the counter-example in §9.

## Open questions

Answered since the first pass: ranking is a second report rather than a flag (§8); cumulative counts
cannot be ranked meaningfully because a parent always dominates its children, so ranking needs
leaves; and `--skip-covered`-style filtering must leave the total alone (§8).

Still open:

- Does ranking by miss count reproduce Marick's problem in a new coordinate? "Biggest number" is not
  "riskiest", and CodeScene's hotspot model excludes coverage from risk entirely. Ranking is triage,
  not risk assessment, and must be described as such — if it is built at all, which §7 gives no
  evidence for.
- Whether uncovered blocks should be emitted one per block or merged into contiguous regions. #78205
  says "line ranges", which implies merged; one per block is simpler and may be noisier.
- Whether the interesting unit is the file or the block, which probably varies by repository — an
  argument for emitting the raw misses and letting the consumer choose over building a second
  opinionated human-facing view.
