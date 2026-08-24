# ADR-0013 — The freshness weighting is not the problem; a thin profile is

- **Status:** DECIDED
- **Date:** 2026-08-17
- **Decision drivers:** honesty of the ranking, not changing scores without evidence
- **Successor to [ADR-0009](0009-score-granularity-and-skill-coverage.md), which required one. Amended by nothing.**

## Context

[ADR-0009](0009-score-granularity-and-skill-coverage.md) set itself an explicit
test:

> *"if freshness still outranks skills on a corpus we can actually read, the
> weighting is wrong."*

When it was written, abstention had fallen to 20.3% and freshness still
outranked skills — **0.274 against 0.184**. The condition fired, and ADR-0009
deferred the weighting question to a successor rather than tacking it onto a
change about coverage. This is that successor.

**It was written to change the weighting. The measurement says not to.**

## What changed in between

[ADR-0011](0011-abstention-credit-calibration.md) recalibrated the abstention
credit from 0.5 to 0.25. That was a change about *inputs*, not weights — but the
correlations ADR-0009 was watching are a function of both.

Re-measured 2026-08-17, **within a single profile**, which is how ADR-0009
measured and is the only comparison that means anything (pooling users with
different profiles dilutes every component toward zero):

| Profile | Skills | Correlation with skills | Correlation with freshness | Verdict |
|---|---|---|---|---|
| 11 skills, YoE set, 2 countries | full | **0.428** | 0.241 | condition **un-fires** |
| 3 skills, YoE set, no countries | thin | 0.227 | **0.272** | condition **still fires** |

For a reader who has filled their profile in, skills now outranks freshness
comfortably and the ADR-0009 condition is satisfied — the fix was to the inputs,
and it worked. Abstention is 15.3%, down from 80.5% at the start of this
sequence.

## Decision

**Do not change the freshness weight.**

The naive reading of ADR-0009 — "freshness has too much leverage, reduce it" —
would be wrong for the majority case, and would be applied on the strength of a
number that is now stale. Ten points of freshness behaves correctly against a
profile that says enough for the other components to have something to score.

## What the split actually shows

The problem is not symmetrical to what ADR-0009 assumed. Freshness does not have
too much leverage in general; it has too much leverage **when the profile is
thin**, because everything else in the score has abstained or is being credited
at the no-information level. A user with three skills is ranked substantially by
recency.

That is the same shape as the defect ADR-0011 fixed, with the sides swapped:

| | Thin **posting** | Thin **profile** |
|---|---|---|
| What is missing | the employer stated little | the user entered little |
| Effect | remaining components carry the score | remaining components carry the score |
| Status | fixed — credit calibrated, band capped, confidence damped | **not handled at all** |

And it lands hardest on exactly the people least able to do anything about it: a
new account, before onboarding has been completed, whose first impression of the
product is a ranking driven by dates.

## Why the symmetric fix is not being made here

The machinery already exists — `Confidence`, the band cap, an evidence factor —
and applying it to profile completeness is a small change. **The constant is the
problem.** Choosing "damp below N skills" without measuring what N is would
repeat precisely the mistake ADR-0011 documents at length: a plausible-looking
number standing in for missing evidence, which is how both of the previous
scoring defects shipped.

Measuring it needs a corpus of **real** profiles of varying completeness. What
exists today is one real profile and several hundred synthetic ones created by
the E2E suite, all with identical onboarding answers — which is why the two rows
in the table above are two profiles rather than twenty. Fitting a constant to
that would be fitting it to a test fixture.

## Consequences

### Good

- No scores change. Nothing is re-rendered, no sweep runs, and the ranking that
  is now demonstrably working for complete profiles is left alone.
- ADR-0009's condition is formally answered with a measurement rather than left
  open indefinitely.

### Bad, and accepted

- **Thin profiles are still ranked substantially by recency**, and that is a
  real and un-fixed defect for new users. Accepted for now because the
  alternative is guessing the constant, and a wrong constant would degrade every
  score rather than only the thin ones.
- The dashboard's "Sharpen your matches" panel is the current mitigation: it
  names what a missing field CHANGES, which at least tells a thin-profile user
  why their results look the way they do.

## Revisit

Revisit when there are **at least 20 real profiles** spanning 0–20 skills.
The measurement to run is the one in the table above, per profile, plotted
against skill count: the crossover point where freshness stops outranking skills
is the constant, and it should be read off the data rather than chosen.

Revisit sooner if the full-profile correlation with skills falls below **0.35**,
or freshness rises above **0.30**, for a representative complete profile.

Current, for the 11-skill profile: skills **0.428**, freshness **0.241**.
