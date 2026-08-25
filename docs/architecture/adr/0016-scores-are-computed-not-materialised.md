# ADR-0016 — Scores are computed at read time, not materialised per user

- **Status:** DECIDED
- **Date:** 2026-08-25
- **Decision drivers:** a stated goal of 1M users; storage that grows as `users × postings`;
  a measured scoring cost three orders of magnitude cheaper than assumed
- **Amends:** [ADR-0006](0006-hybrid-retrieval-and-scoring.md) (scoring model unchanged; its
  *storage* is what this replaces) and the trigger in
  [phase-5 §11.3](../../engineering/phase-5-production-readiness.md)

## Context

Every score is currently materialised: one row per (user, live posting), written by a fan-out job
whenever a posting arrives or a profile changes.

Measured on the live database, 2026-08-25:

| | |
|---|---|
| `user_job_scores` | 1,375,933 rows, **1,952 MB** |
| Bytes per row | **1,487** |
| `components` JSONB alone | **647 bytes — 44% of the row** |
| Share of the whole database | **78.4%** |
| Users actually holding scores | 244 |
| Live postings | 11,888 |

**A trigger this project set for itself has already fired and nobody noticed.** phase-5 §11.3 says to
act "when `user_job_scores` exceeds 50M rows **or the table exceeds 25% of database size**". It is at
78.4%, three times over. The row count is nowhere near 50M, which is why the size half went unread —
a two-clause trigger is only as good as the clause nobody checks.

### The curve

Storage is `users × live_postings × 1,487 bytes`. Projected, with the corpus at today's 11,888 and at
the 150,000 that [phase-5 Block B](../../engineering/phase-5-production-readiness.md) targets:

| Users | Today's corpus | After Workday |
|---|---|---|
| 1,000 | 16 GB | 208 GB |
| 10,000 | 165 GB | 2 TB |
| 100,000 | 2 TB | 20 TB |
| **1,000,000** | **16 TB** | **203 TB** |

Independent guidance puts a single unsharded Postgres comfortable below ~500 GB, with replicas and
partitioning extending into low terabytes ([VeloDB](https://www.velodb.io/glossary/ways-to-scale-postgresql),
[Technori](https://technori.com/2026/02/24537-how-to-scale-postgresql-to-terabytes/sebastian/)).
Postgres does reach far larger — OpenAI serves 800M users on it, Notion shards across 480 databases —
but not as one instance holding a materialised cross-product. **The goal is unreachable by tuning.**

### The measurement that decides it

`BenchmarkScoreOne`: **1,837 ns per score**, 872 B, 12 allocs. The scorer is a pure function of
profile and posting and touches no database — a property ADR-0006 chose deliberately and which turns
out to be worth more than the ranking design.

At that cost, scoring at read time:

| Candidates | CPU |
|---|---|
| 500 | 0.9 ms |
| 2,000 | 3.7 ms |
| 5,000 | 9.2 ms |
| **11,888 — the entire live corpus** | **22 ms** |
| 150,000 | 276 ms |

Against a `GET /v1/jobs` budget of 120 ms p95, measured today at 75.6 ms.

**So 1,952 MB — 78% of the database — exists to avoid 22 ms of CPU.**

### A correction to phase-5 §11.3

It states "**The fan-out job is the CPU curve.** One posting × every active user." That is wrong. At
1M active users and 1,000 new postings a day the fan-out is 11,574 scores/second, which at 1.84 µs is
**2.1% of one core**. CPU was never the constraint.

The real constraint is write amplification: the same work writes **1.5 TB of rows per day**, on a
table already tuned to `autovacuum_scale_factor 0.02` because its churn drives dashboard latency. The
curve is I/O and storage, not CPU, and the distinction matters because the mitigations are different.

## Options

**A. Keep materialising; partition by `user_id` and prune dormant users.** phase-5's suggestion.
Partitioning makes 16 TB manageable, not smaller, and pruning trades a storage problem for a
cold-start problem — a returning user waits for a full rescore. Postpones the wall.

**B. Drop `components`, keep the rest.** Removes 44% of every row. Cheap, and it is strictly correct
on its own terms: `components` is written for every (user, posting) pair and read by exactly one
endpoint, the detail page, for one posting at a time. But 9 TB instead of 16 TB is the same wall in a
different year — **the curve is unchanged**.

**C. Candidate generation, then score at read time.** Stop materialising the cross-product. The feed
already narrows aggressively before ranking — measured today: remote 2,007, US 4,117, last 7 days
1,028, remote-and-recent 186. Score that set on the way out.

This is the standard shape for personalised ranking, and the sources are consistent that the answer
is hybrid rather than pure push or pure pull — precompute where it is bounded, compute at serving
time where it is not ([Witty Coder](https://wittycoder.in/courses/news-feed/fan-out-strategies),
[System Design Sandbox](https://www.systemdesignsandbox.com/learn/fan-out-strategies),
[feed ranking: candidate generation then scoring](https://www.techinterview.org/post/3233466385/system-design-feed-ranking/)).

## Decision

**C, with a bounded materialised set for the cases that genuinely need one.**

1. **The feed scores at read time.** Filters produce the candidate set; the scorer ranks it. The
   default sort is `newest` and needs no score at all.
2. **The candidate set is bounded**, so the cost cannot grow with the corpus. Cap at **2,000**,
   ordered by recency before scoring.

   The first draft of this ADR said 20,000, derived from scoring cost alone (20,000 × 1.84 µs =
   37 ms). Implementation showed that was the wrong constraint. Reading the candidates dominates at
   roughly 21 µs per row — 500 → 15 ms, 2,000 → 45 ms, 5,000 → 108 ms, 12,000 → 263 ms — so the
   query, not the scorer, spends the budget. At 2,000 a ranked request is ~45 ms of candidates plus
   4 ms of scoring plus the page fetch; at 5,000 it is already over.

   Filters apply **before** the cap, which is what makes this acceptable: narrowing to remote, or
   India, or a skill produces a candidate set well under it and ranks the whole result. The cap only
   binds an unfiltered "rank everything", where the newest 2,000 is the part worth ranking anyway.
3. **Materialise only what is unbounded to recompute**: a saved search's "new since last run", and
   the digest. Both are `users × saved_searches`, which is flat in corpus size.
4. **`components` is never stored.** It is recomputed for the one posting being displayed, at 1.84 µs.
5. `user_job_scores` is retired once the feed no longer reads it. The contract migration comes a
   release after the expand, per [deployment-zdt](../../operations/deployment-zdt.md).

## Consequences

### Good

- Storage stops depending on the corpus. The 203 TB projection disappears; what remains is
  `users × saved_searches`, kilobytes each.
- The largest table, 78% of the database and the reason autovacuum is load-bearing, goes away — and
  with it the dashboard-latency problem that tuning was introduced to contain.
- A scoring change takes effect immediately. Today it needs a sweep that re-enqueues every posting,
  and a forgotten `Version` bump leaves stale scores serving silently — a failure ADR-0011 documents.
- `profile_version` and the whole stale-score sweep become unnecessary.

### Bad, and accepted

- **`sort=match` gets slower and gains a ceiling.** Ranking the whole corpus by score is no longer
  free, and above 20,000 candidates it is refused rather than served slowly. Sorting by score across
  a whole corpus is a query a user cannot act on anyway; the top 25 of a filter is.
- **Score history is lost.** Nothing will be able to answer "what did this score last week". Nothing
  asks today, and the evidence ledger's measurements are point-in-time queries that would need a
  deliberate snapshot instead.
- **CPU moves onto the request path**, where a slow scorer becomes a slow page rather than a slow
  job. 22 ms of headroom is not infinite, and `BenchmarkScoreOne` becomes a budget rather than a
  curiosity.
- **A large migration**, touching the feed, the dashboard and the scoring workers.

## What implementation changed

Three things were wrong or unforeseen in the plan above, recorded because the ADR is worth less if it
reads as though it went to plan.

**The cap was wrong by 10×**, for the reason given above: it was set from CPU when the constraint is
I/O.

**Postgres JIT cost more than the query.** The first working ranked request took 1,009 ms, of which
**517 ms was JIT** — 119 ms inlining, 257 ms optimising, 136 ms emitting — to run a plan that takes
300 ms without it. Postgres decides to JIT on estimated cost, and a query touching ten thousand rows
clears the default threshold while gaining nothing, because the work is I/O and not expression
evaluation. `jit = off` is now set on the pool.

**The matcher service has no work left**, and is deleted. Its only queues were `score` and
`score_bulk`, both of which existed to run the fan-out this ADR removes. Six deployment units become
five — see [ADR-0008](0008-service-decomposition.md).

## Measured outcome

| | Before | After |
|---|---|---|
| `GET /v1/jobs?sort=match` p95 | n/a — read from a table | **~80 ms** (budget 120 ms) |
| `GET /v1/jobs` (newest) | 75.6 ms | **~25 ms** |
| `GET /v1/me/dashboard` | — | **~85 ms** (budget 400 ms) |
| Deployment units | 6 | **5** |
| Storage growth | `users × live_postings` | flat in corpus size |

## Revisit

- If `BenchmarkScoreOne` exceeds **10 µs**, read-time scoring of 20,000 candidates crosses 200 ms and
  the trade inverts. The benchmark is the trigger and belongs in CI.
- If a widget genuinely needs score history — a "your match trend" chart — that is a **rollup**, not a
  restored cross-product. See [ADR-0017](0017-widget-data-comes-from-rollups.md).
- If candidate generation cannot get below 20,000 for a common filter, the fix is a better index or a
  narrower default, not materialisation.
