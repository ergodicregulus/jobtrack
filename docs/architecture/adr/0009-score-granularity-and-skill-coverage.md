# ADR-0009 — Score granularity, and the skill-coverage problem it uncovered

- **Status:** DECIDED
- **Date:** 2026-08-16
- **Decision drivers:** honesty of the ranking, explainability, the product's central claim
- **Supersedes nothing. Amends the weighting rationale in [ADR-0006](0006-hybrid-retrieval-and-scoring.md).**

## Context

The design review argued that showing `91` and `88` implies a distinction the
model cannot support, and proposed grouping scores within about three points.

Rather than reason about it, we measured against the live corpus — 6,677
postings, ~96,000 scores. **The measurement answered a different and much more
important question than the one asked.**

## What the measurement found

For the senior demo profile, over 5,554 scored postings:

| Measure | Value |
|---|---|
| Average *available* points (non-abstaining weight) | **48.4 of 100** |
| Freshness as a share of the score it actually contributes to | **23.7% average, up to 40%** |
| Correlation of final score with the freshness component | **0.256** |
| Correlation of final score with the skills component | **0.173** |
| Postings where the skills component abstained | **80.5%** |

And over the whole live corpus:

| Measure | Value |
|---|---|
| Live postings with **zero** extracted skills | **3,253 of 6,677 — 48.7%** |
| Average extracted skills per posting | **1.71** |
| Postings with **any** must-have skills | **655 — 9.8%** |
| Canonical skills in the vocabulary | **47** |

### What that means

**The ranking currently correlates more strongly with how recently a job was
posted than with whether the reader can do it.**

The mechanism is not a bug in any single component. It is compositional:

1. The vocabulary holds 47 canonical skills. Real engineering postings draw on
   several hundred terms.
2. When no skills are extracted, the skills component abstains — correctly, per
   the fix in `2026.08.2`.
3. Abstention removes 40 of 100 points from the denominator. Compensation
   abstains often too. The average denominator falls to **48.4**.
4. Freshness is a fixed 10 points, so on a 48-point denominator it is worth
   ~21% of the score, and up to 40% when several components abstain.

So a posting that is fresh and unreadable outranks one that is older and a
genuine fit. The product's central claim — *"we tell you which of their
requirements you meet"* — is only fully deliverable for the **9.8%** of postings
where we extract any must-haves at all.

## Decisions

### 1. The visible score does not change — DECLINED

The score is the **sort key**. Hiding a number we are ordering by leaves a
ranked list with no visible reason for its rank, which is the failure this
product exists to correct. A band alone is a claim without evidence — the
"97% match" pattern with the number removed rather than the problem solved.

The arrangement stands: **band leads and makes the claim, number follows as
evidence, breakdown one click away.** No decimals.

The design review withdrew this objection when given the sort-key argument, and
we agree with its withdrawal.

### 2. Grouping near-ties — DEFERRED, and it is treating a symptom

Grouping rows within three points would smooth over an ordering problem whose
cause is that 40% of the weight is silently missing on four postings in five.
Fix the cause first, re-measure, and only then decide whether the residual
variance still warrants grouping.

### 3. Expand the skill vocabulary — ACCEPTED, and it is the real work

This is the highest-value change available to the product, and it is upstream of
everything else:

- Skills are the largest scoring component, so coverage sets the ceiling on
  match quality.
- Every abstention hands proportionally more weight to freshness.
- The gap chips — the product's most distinctive feature — cannot appear for a
  posting whose requirements we never read.

**Target: extraction on the substantial majority of engineering postings, not
48.7% at zero.** The vocabulary must keep its curated, ambiguity-aware character
— the `go` / `go to market` defect is exactly what a scraped list produces — so
growth means adding terms *with* their aliases and their ambiguity rules, not
importing a taxonomy wholesale.

### 4. Freshness weighting is revisited only after coverage improves

Freshness dominating is a *symptom* of abstention, not evidence that 10 points
is the wrong weight. Re-measure the correlations once coverage improves. If
freshness still outranks skills on a corpus we can actually read, the weighting
is wrong and this ADR gets a successor.

## Consequences

- The scorer's arithmetic is unchanged; no version bump is required for 1 and 2.
- Expanding the vocabulary **does** change scores, so it requires a
  `Scorer.Version` bump to trigger the stale-score sweep. Without that, existing
  rows keep their old numbers indefinitely.
- Golden tests over the extractor must grow with the vocabulary. A larger word
  list is a larger surface for the ambiguity class of bug — `go`, `c`, `r`,
  `rust`, `swift`, `spring` — and each addition of a common English word needs a
  context rule and a test.
- The evidence ledger should carry the coverage figure, since any user-facing
  claim about match quality now has a measured ceiling.

## Outcome, measured 2026-08-16 after implementation

Two changes shipped, not one. The vocabulary expansion alone did almost nothing,
and finding out why was the useful part.

| | Before | After vocabulary (47→113) | After `mentioned` fallback |
|---|---|---|---|
| Postings with zero skills | 48.7% | **16.1%** | 16.1% |
| Average skills per posting | 1.71 | **3.57** | 3.57 |
| Skills component abstaining | 80.5% | 73.2% | **20.3%** |
| Correlation with skills | 0.173 | 0.019 | **0.184** |
| Correlation with freshness | 0.256 | 0.242 | 0.274 |

### The vocabulary expansion nearly failed silently, twice

**First**, `posting_skills` is written with an INNER JOIN onto the `skills`
table, and that table was populated from a *separate* list in `internal/seed`.
Expanding the vocabulary changed extraction and every new skill was then dropped
at the join — no error, no log line. The vocabulary is now the single source of
truth (`Vocabulary.Canonicals()`), the ingestor reconciles the table at startup,
and two tests keep the lists from drifting again.

**Second**, extraction improved to 16.1% zero — but abstention only fell from
80.5% to 73.2%, because **73.7% of everything extracted is classified
`mentioned`**, and the scorer read only `must_have` and `nice_to_have`. The
must/nice split needs an explicit "Requirements" heading and most postings have
none, so 28,056 successfully-extracted skills were being discarded.

Where a posting has no parseable requirements section at all, merely-mentioned
skills are now used as nice-to-haves. "The posting talks about Kafka" is weaker
evidence than "the posting requires Kafka" and it is not nothing; the
evidence-weighting already damps a posting that states little. **Abstention fell
to 20.3%.**

> **Answered by [ADR-0013](0013-freshness-weighting-re-measured.md) (2026-08-17).**
> Re-measured after the ADR-0011 abstention recalibration, skills now outranks
> freshness (0.428 vs 0.241) for a complete profile and the condition below
> un-fires. It still fires for a THIN profile (0.227 vs 0.272), which is a
> different defect with a different fix. The freshness weight was not changed.

## This ADR now requires a successor

Its own test was: *"if freshness still outranks skills on a corpus we can
actually read, the weighting is wrong."*

We can now read the corpus — abstention is 20.3%, down from 80.5% — and
**freshness still outranks skills (0.274 vs 0.184)**. So the condition has
fired, and the weighting itself is the next thing to examine, not the inputs.

Deliberately **not** changed here. A weighting change alters every score in the
product and deserves its own decision with its own measurement, rather than
being tacked onto a change about coverage. The candidate questions for the
successor:

- Is 10 points of freshness on a variable denominator too much leverage? A fixed
  proportion of the *available* weight would behave more predictably.
- Should freshness be a tiebreaker rather than a component — sorting equal fits
  by recency instead of letting recency create the difference?
- The corpus is heavily skewed toward postings we fetched recently, which may
  inflate the freshness correlation. Re-measure once ingestion has been running
  long enough for `posted_at` to spread out.

`make coverage` is the instrument for re-measuring.
