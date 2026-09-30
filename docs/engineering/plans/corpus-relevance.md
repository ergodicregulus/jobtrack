# Corpus relevance

**Status:** decided (ADR-0018), built, and exposed. Three chips ship. Curation was tried and measured as the weaker lever — see below. The done-condition is not yet met: the homepage shows only the tagline "Job search instrument · software engineering" and states no share. **Done when:** the share of live
postings that are software-engineering roles is stated on the homepage and is
high enough that the claim "a job-search instrument for software engineers" is
true without qualification.

## The problem, measured

```sql
SELECT count(*) FILTER (WHERE title ~* '(engineer|developer|programmer|architect
        |sre|devops|data scien|machine learning|software)') AS engineering,
       count(*) AS live
  FROM job_postings WHERE status='live';
--  3,805 of 10,998  (34.6%)   measured 2026-09-01, after the freshness repair
```

```sql
SELECT c.name, count(*), round(100.0*count(*)/sum(count(*)) OVER (),1) AS pct
  FROM job_postings p JOIN sources s ON s.id=p.source_id
  JOIN companies c ON c.id=s.company_id
 WHERE p.status='live' GROUP BY 1 ORDER BY 2 DESC LIMIT 3;
--  Bosch 3,456 (31.4%) | OpenAI 676 (6.1%) | Anthropic 468 (4.3%)
```

**Two-thirds of the corpus is not engineering work**, and one employer is 31% of
it.

These figures are post-repair. Closing 1,920 postings their boards had dropped
moved engineering from 32.6% to 34.6% and Bosch from 36.7% to 31.4% — real, and
nowhere near enough to dissolve the problem. It was worth doing first so this
decision is taken on a corpus that exists, but the answer did not change. These are the same fact: Bosch is a conglomerate whose board carries HVAC
sales in Bogotá and factory roles in Stuttgart alongside its software openings.
A user searching this product is mostly searching someone else's job board.

The title regex above is a rough instrument and will over- and under-count. It is
good enough to establish that the ratio is wrong; it is not good enough to filter
on, which is the first thing step 1 has to fix.

## The decision this is blocked on

Three routes, and they produce different products:

1. **Curate the board list.** Drop conglomerates, add engineering-dense
   employers. Honest, cheap, and reduces the corpus to roughly a third of its
   size — the counter on the homepage falls from 14,258 to about 4,600.
2. **Filter at ingest.** Keep the boards, store only what classifies as
   engineering. Keeps breadth, but means we discard postings an employer
   published, and every false negative is invisible.
3. **Classify and expose, discard nothing.** Store everything, label each posting
   with a field, and default the feed to engineering with the filter visible.
   Most work, most honest, and the only one where a mistake is recoverable by
   the user rather than silently ours.

Route 3 is the recommendation, because it is the only one consistent with a
product whose pitch is that it tells you what it does not know: a
misclassification the user can see and override is a different kind of error
from one that removed a role before they existed.

**This needs your call before any of it is built.**

## Steps, once decided

1. **[opus]** A field classifier that is honest about its confidence. Title plus
   description, deterministic, no model — the vocabulary in `internal/normalise`
   already does the harder half. Abstention is a real output, exactly as it is in
   scoring: `unknown` is a field value, not a default to engineering.
2. **[opus]** Migration adding `field` and `field_confidence`, expand-only.
   Backfill as a resumable data migration.
3. **[sonnet]** A labelled fixture set — 200 postings sampled across vendors,
   hand-checked — as the classifier's test corpus. Bands, never exact accuracy
   claims, per the testing strategy.
4. **[opus]** Feed default and filter chip, and the number on the homepage.
   Whatever it says has to be measured, not asserted.
5. **[opus]** ADR recording the route taken and what was rejected. This changes
   what the product is; a PR comment is not where that gets decided.

## Traps

- **A classifier that never abstains will be trusted more than it deserves.**
  ADR-0011 is the precedent: the scorer's abstention credit exists because a
  confident wrong answer outranked honest ones.
- Do not filter on the regex above. It matches "Sales Engineer" and misses
  "Backend, Payments".
- Dropping Bosch removes 37% of the corpus in one commit. Whatever the homepage
  says about corpus size that day has to be true the day after.

## Why the classifier stops here

Measured 2026-09-01 on the 5,703 postings it labels `unknown`:

| Skills extracted | Postings |
|---|---|
| 0 | 3,600 |
| 1–2 | 1,338 |
| 3–5 | 765 |
| 6+ | 0 — those are already `software` |

**There is no cheap recall win left.** 63% of the unknown bucket has no
recognised skill at all, because the posting has no readable body or is not in
English. The remaining 765 sit at three to five skills, and that is precisely the
band measured as unreliable: at three skills the population is only 17%
software-titled, and a finance director qualifies because the description named
three tools.

Lowering the threshold would trade the 93.5% precision recorded in
[A-38](../../research/evidence-ledger.md#a-38) for a handful of recovered
postings. The honest next lever is not the classifier — it is either board
curation, or descriptions for the postings that have none.

## Curation was tried, and it is the weaker lever

Measured 2026-09-02 ([A-39](../../research/evidence-ledger.md#a-39)). Eighteen
verified engineering-dense boards were added, contributing 1,182 postings with
zero errors. Corpus-level engineering share moved **34.3% → 35.4%**.

Over the same period the conglomerate board grew to **34.8% of the corpus**. It
posts faster than eighteen curated boards can dilute it.

ADR-0018 kept curation as the live alternative if classification underdelivered.
It is now measured, and it underdelivers harder: **the classifier's `software`
view grew 2,596 → 3,188 postings at 94.0% precision over the same change**, which
is the number a reader actually experiences.

**What is genuinely left is one decision, and it is not an engineering one.** The
conglomerate board is a third of the corpus and mostly not software. Dropping it
loses its real software roles along with the rest; keeping it means the
unfiltered corpus stays roughly a third irrelevant and the Software chip is the
answer for engineers. Both are defensible. Neither is a bug to fix.
