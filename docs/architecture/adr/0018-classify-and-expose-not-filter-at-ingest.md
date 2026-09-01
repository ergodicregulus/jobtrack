# ADR-0018 — Postings are classified and labelled, never discarded at ingest

- **Status:** DECIDED
- **Date:** 2026-09-01
- **Decision drivers:** two-thirds of the live corpus is not the work this product
  claims to be for, and one employer is 31% of it; the classifier can name only
  part of the rest, so the default has to be built around what it does NOT know

## Context

JobTrack describes itself as a job-search instrument for software engineers.
Measured on 2026-09-01, **3,805 of 10,998 live postings (34.6%) had an
engineering title**, and a single employer — Bosch, a conglomerate whose board
carries HVAC sales in Bogotá and process engineering in Stuttgart — was **31% of
the corpus**. Two-thirds of what the product ingested was not the work it claims
to be for.

These are one problem, not two. The concentration is what makes the irrelevance
large.

The figures above are post-repair. Closing 1,920 postings that their boards had
already dropped moved the engineering share from 32.6% to 34.6%; the ordering
mattered so the decision was taken on a corpus that exists, but it did not change
the answer.

## Decision

**Store every posting an employer published. Label each with a field, a
confidence, and the reason. Default the feed to hiding `other` — and let the
reader turn that off.**

Concretely:

- `job_postings.field` is `software`, `other` or `unknown`, with
  `field_confidence` and `field_because` beside it (migration 0024).
- `normalise.ClassifyField` decides, from the title and the count of skills
  already extracted by the existing vocabulary. Deterministic, a table of tokens
  in a deliberate order, no model.
- The feed's default predicate is **`field <> 'other'`**, not
  `field = 'software'`.
- `?field=all` turns the filter off completely.

## Alternatives rejected

**Filter at ingest — store only what classifies as software.** Cheapest, and
rejected outright. Every false negative is a role an employer published, that the
user can never find, and that we keep no record of having discarded. It fails
silently, which is the one failure mode this repository is built to prevent —
and it fails silently in the direction of hiding a job from someone looking for
one.

**Curate the board list — drop the conglomerates.** Honest, needs no classifier,
and would work: the corpus falls to roughly a third and the irrelevance goes with
it. Rejected because it throws away the software roles Bosch genuinely does post
along with the rest, and because it makes the product's coverage a function of
which boards someone remembered to prune. Kept as a live option if the classifier
turns out not to earn its keep.

## Why the default is `<> 'other'` and not `= 'software'`

This is the part worth reading twice, because it looks like a detail and is the
whole design.

The classifier can name only part of the corpus with confidence. After widening
its vocabulary from real titles it reports **23.4% software, 33.4% other, 43.2%
unknown**, and the residual is a genuinely multilingual long tail — Chinese,
Hungarian and Spanish titles from one employer's global board. It is not going to
reach zero, and pretending otherwise by tuning against this corpus would be
overfitting to Bosch.

So the two ways of being wrong are priced, and they are not equal:

- A non-software posting left in the feed costs the reader **one row they can see
  is wrong**.
- A software posting excluded costs them **a job they will never know existed**.

Defaulting to `= 'software'` would hide all 43.2% we could not classify, software
included. Defaulting to `<> 'other'` hides only what we can positively name as
something else. The classifier is therefore built to abstain readily and to claim
`other` only on strong evidence, and `unknown` is a real value rather than a
default nobody chose.

This is the same reasoning as
[ADR-0011](0011-abstention-credit-calibration.md): a confident wrong answer is
worse than an admitted absence, and the scorer already pays for that lesson.

## Consequences

- The corpus keeps its size and its honesty; nothing is discarded.
- The default feed shows 7,386 of 11,095 live postings rather than all of them.
- `field_because` is shown to the reader, so a label can be argued with. A
  classification the user cannot see the grounds for is one they can only
  distrust.
- Re-classification happens on every poll, so improving the vocabulary reaches
  stored rows without a new backfill.
- The vocabulary will need widening as new employers arrive, and each addition
  must come from a title actually observed — not from imagination.
- **This ADR does not settle relevance.** It makes the corpus legible and gives
  the reader control. If the measured share of `other` stays near a third, board
  curation is still the honest next step, and that is a separate decision.
