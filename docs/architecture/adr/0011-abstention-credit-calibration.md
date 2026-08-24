# ADR-0011 — The abstention credit is calibrated to the corpus, not to the scale

- **Status:** DECIDED
- **Date:** 2026-08-16
- **Decision drivers:** honesty of the ranking, the product's central claim
- **Amends [ADR-0009](0009-score-granularity-and-skill-coverage.md). Supersedes nothing.**

## Context

When a posting states no requirements we can read, the skills component — 40 of
100 points, the largest — has nothing to score. ADR-0009 established that
*removing* it from the denominator is wrong, because it lets location, years and
freshness carry an unread posting to near-certainty. A marketing role scored 98
for a backend engineer that way.

The fix was to credit the unstated weight at **the midpoint, 0.5**, and to cap
the *band* below `strong` whenever evidence was partial. That was shipped as
scorer version `2026.08.2`.

**The band cap worked. The ranking fix did not, and we did not check.**

## What we missed

A band is a label. The feed sorts on the **number**. Capping the band stopped an
unreadable posting *claiming* a strong match; it did nothing to stop it
*outranking* real ones, and the sort order is what a user actually experiences.

Measured on 2026-08-16 across 5,858 live scores for one profile
([A-00c](../../research/evidence-ledger.md#a-00c)):

| Population | Count | Min | Median | p90 | Max |
|---|---|---|---|---|---|
| Requirements readable | 4,918 | 11.5 | **39.2** | 56.5 | 84.6 |
| Requirements unreadable | 940 | **42.3** | 54.7 | 65.1 | 74.3 |

**The floor of the unreadable population sits above the median of the readable
one.** A posting we understood nothing about was *guaranteed* to outrank half
the postings we understood. 42 of the top 100 matches were roles we could not
read — for a software engineer, that meant page one of the feed filled with
Counsel, Vendor Manager and Account Executive.

The mechanism is a single number. Across readable postings the skills component
earns a mean of **0.236** of its weight (median 0.250, p90 0.375). We credited
unreadable ones at **0.500** — more than double what reading one is worth.

## Decision

**The abstention credit is the readable population's mean, not the midpoint of
the scale.** `Config.AbstentionCredit`, default **0.25**, applied in *both*
places the question arises:

- a posting stating **nothing** we can read (full abstention), and
- the unstated remainder of a posting stating **almost** nothing (partial
  evidence).

The second was hard-coded `0.5` in `scoreSkills` while the first was calibrated,
so one concept carried two numbers and a posting stating a single requirement
scored more generously than one stating none. That is scorer `2026.08.7`.

The error was conceptual, not arithmetic. "No information" was translated as
"half marks", which is the midpoint of the *scale*. The no-information
expectation is what a posting drawn at random from the population actually
earns — the midpoint of the *distribution*. Those are different numbers, and
using the first in place of the second does not express ignorance. It expresses
optimism, and it systematically promotes exactly the postings we know least
about.

Rounded to 0.250 rather than 0.236: the third decimal is noise from one corpus
on one day, and carrying it would claim a precision the measurement does not
have.

### What this deliberately does not do

- **It does not remove unreadable postings from the feed.** They remain fully
  browsable and filterable. Ranking them at the population's expectation is
  honest; hiding a job because our parser is weak is our failure charged to the
  user.
- **It does not push the credit to zero.** Abstaining is not evidence of a *bad*
  match either. An unread posting must sit between "matched most requirements"
  and "matched none of them", and there is a test for both bounds.
- **It does not touch the band cap or the confidence damping.** Those were
  correct and remain.

### The consequence worth stating plainly

A posting matching exactly the average fraction of its requirements now **ties**
with a blank. That tie is the calibration working, not a defect: an average
match is, by definition, worth what no information is worth. Only an
above-average match should win, and only a below-average one should lose.

## Measured result

Measured after the sweep completed, for the same profile and corpus:

| | Before | After |
|---|---|---|
| Readable population — min / median | 11.5 / **39.2** | 1.1 / **40.6** |
| Unreadable population — min / median | **42.3** / 54.7 | **13.9** / 44.1 |
| Unreadable postings in the top 100 | **42** | **11** |
| Top of feed | Counsel, Vendor Manager, Account Executive | 7 of the first 8 are engineering roles |

The pathology is gone: the unreadable floor (13.9) now sits well below the
readable median (40.6), where before it sat above it. The two medians remain
close — 44.1 against 40.6 — and that is the design, not a residual bug: the
credit is calibrated so an unread posting lands *at* the readable population's
expectation, neither above nor below it.

11 of the top 100 is inside this ADR's own successor threshold of 20%, so the
condition has not fired.

*An earlier draft of this section reported 0, taken mid-sweep before the corpus
had finished re-rendering. 11 is the settled figure.*

### The `skillsFullEvidence` hypothesis, measured and rejected

This section previously blamed the remaining false positives on
`skillsFullEvidence = 2` — the idea being that a posting stating two
requirements the reader happens to hold earns full face value it has not
earned. That was measured on 2026-08-17 through the scorer's own view of
requirement depth (which must account for `mentioned` skills being promoted to
nice-to-haves when a posting has no requirements section; querying
`posting_skills` directly undercounts by more than half).

**The hypothesis is wrong.** Median score is flat across depth — 43.8 for thin
postings, 41.7 at the threshold, 43.3 deep, 45.6 very deep — so there is no
score inflation left to fix; ADR-0011 removed it. The `strong` band does
concentrate at the threshold (3.3% of postings there, against 1.1% deep), but
inspecting those results shows why: **11 of the top 12 are genuine software
engineering roles the reader matches well** — "Software Engineer, Expansion",
"Postgres Product Engineer", "Staff Software Engineer, DevPlatform". Exactly one
is wrong.

Raising the threshold would delete eleven correct strong matches to remove one
incorrect one. That is a bad trade and the change is **not** being made.

### What the remaining false positive actually shows

"Product Support Specialist" scores 84.6 on *2 of 2 must-haves* because it
genuinely names two technologies the reader genuinely has. No amount of evidence
weighting fixes that, because the evidence is real — the posting is simply **not
in the reader's field**, and nothing in the model asks that question. `users`
carries `current_title` and `target_title` and the scorer does not use either.

A field or seniority component is a real gap and a significant scoring change:
it needs its own weight, its own abstention behaviour, and its own measurement
against a corpus, exactly as the skills component did. Deliberately not bolted
on here. Recorded so the next person starts from the measurement rather than
from the same wrong hypothesis.

## Consequences

- Scorer version → `2026.08.7`. The `rescore_stale` sweep re-renders the corpus;
  no migration, no backfill script.
- `AbstentionCredit` is **configuration**, because it is a population estimate
  that drifts with the vocabulary and the source mix. It is not a constant and
  must not be tidied into one.
- Two tests lock the contract: `TestIgnoranceDoesNotOutrankAMeasuredMatch` for
  both bounds, and `TestAbstentionCreditIsCalibratedNotChosen`, which fails with
  an explanation if anyone rounds it back to 0.5.

## The general lesson, since this is the second time

ADR-0009 found that an abstaining component distorted the score. This ADR finds
that the *fix* for it distorted the ranking. Both were invisible to every test we
had, and both were found the same way: by measuring the live corpus rather than
reasoning about the code.

**A component that abstains needs its replacement value measured, not chosen.**
Any future default that stands in for missing evidence — parse confidence,
inferred seniority, estimated compensation — must state which population it is
the expectation of, and cite the measurement. A plausible-looking constant is
how both of these shipped.

## Successor condition

Re-measure with `make coverage` after any extraction change. Revisit this ADR if
the readable population's mean skills fraction moves outside **0.18–0.32**, or if
abstaining postings exceed **20%** of the top 100 for a representative profile.

Current: mean 0.236, abstaining share of top 100 now **11** (was 42).
