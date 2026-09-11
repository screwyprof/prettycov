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

## Open questions

- Does ranking by miss count reproduce Marick's problem in a new coordinate? "Biggest number" is not
  "riskiest", and CodeScene's answer to prioritisation deliberately excludes coverage. Ranking is
  triage, not risk assessment, and should be described as such.
- Whether cumulative and per-directory ("flat") miss counts are both needed, or whether cumulative
  alone suffices for a report this size. `rollUp` currently computes cumulative and discards flat.
- Whether a ranked flat list should be a separate output shape from the tree, or whether asking for
  a ranking implicitly *is* the request for one.
