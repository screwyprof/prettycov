---
name: prose
description: Review this repo's prose — README, docs/, CHANGELOG, commit messages, code comments — for structure and sentence-level faults. Use when writing or reviewing any of them, or when asked to check the writing.
---

Review prose the way `/ponytail-review` reviews code: one line per finding, location, what is wrong,
what replaces it. The best outcome is that the text gets shorter and states more.

Do not rewrite silently. Report, then apply what is accepted.

## Format

`<file>:L<line>: <tag> <what>. <replacement>.`

Tags: `withheld`, `abstract-first`, `antithesis`, `unreal`, `hedge`, `recite`, `misplaced`, `cut`.

End with `net: -<N> lines possible.` Nothing to cut: `Reads clean. Ship.`

## The faults, in the order they cost a reader most

**`withheld:` announcing a fact instead of stating it.** "tells you what a percentage will not",
"there is a reason for this", "which is the interesting part". The reader must go looking for what
you already know. State it.

**`abstract-first:` the maxim before the example.** "A percentage hides size, and that changes where
to start." The reader meets an abstraction with nothing to attach it to. Lead with the concrete
case, generalise after — Williams' known–new contract: open with what the reader already holds, end
with the new thing.

**`antithesis:` "X rather than Y", "not X, it is Y", "X, not Y".** Fine once, correcting a view the
reader plausibly holds. At density it becomes rhythm, and frames every sentence as rebutting an
opinion nobody voiced. Count them per file. In docs, most correct no real misreading — cut those.
In commit messages it more often earns its place, because the wrong belief was genuinely held and
recording it is the message's job.

**`unreal:` a metaphor that names nothing that happens.** "the package worth opening" — you open a
file; a package is a directory. Test every image against the actual action. If you cannot name the
keystroke, the phrase is decoration.

**`hedge:` judgement doing a fact's work.** "the worse percentage" where "the lower percentage" is
both true and neutral. Also "simply", "just", "of course", "obviously" — each tells the reader how
to feel instead of what is so.

**`recite:` documentation copied from something that generates it.** Help output, flag tables,
command lists. It drifts the moment the source changes, and the source is one command away. Link or
name the command.

**`misplaced:` the right fact in the wrong place.** A caveat that changes whether someone installs
belongs before the install, not after it. A reference paragraph in a pitch belongs in the reference.

## Structure, before any sentence

Diátaxis (<https://diataxis.fr>): a document serves exactly one of

- **tutorial** — learning, by doing
- **how-to** — a goal, by steps
- **reference** — description, consulted not read
- **explanation** — understanding, why it is like this

Mixing them is why documents grow without getting more useful. This repo splits it as:

| file | kind |
|---|---|
| `README.md` | pitch: the problem, the shape of the answer, what it is good for |
| `docs/reference.md` | reference + explanation |
| `CHANGELOG.md` | what changed, what broke, why |
| commit messages | explanation: the belief that was wrong, and the evidence |
| code comments | explanation: what is not deducible from the code |

If a README paragraph would be at home in the reference, move it.

## Mechanics

Follow the [Google developer documentation style guide](https://developers.google.com/style) where
this file is silent. Second person, present tense, active voice. Sentence case in headings.

## Repo conventions

- British spelling ("colour", "behaviour"), which the existing prose uses throughout.
- Em dash with spaces around it, as the existing prose does.
- Every command shown must be real output from `testdata/delegator-go126.out`, the profile
  `golden_test.go` pins. Run it; do not retype it. An elided block marks the gap with `…`, and the
  lines above it must be consecutive.
- Code comments say what is not in the code: a decision, a measurement, a bug that motivated the
  shape. Never what the next line plainly does.

## What not to flag

Long is not a fault; unearned is. A paragraph carrying a measurement, a bug number, or a reason a
reader could not derive is doing work — leave it. Cut the sentence that only sounds like it means
something.
