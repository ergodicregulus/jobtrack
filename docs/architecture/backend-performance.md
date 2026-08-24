# Backend performance

> Status: **DECIDED**. Budgets here are enforced by load tests in CI.

"Snappy" on the backend is not one technique. It is four disciplines, in strict order of payoff —
and the order matters, because teams routinely reach for #4 while #1 is still broken.

```
1. Don't do the work at request time      →  precompute
2. Don't do the work twice                →  coalesce and batch
3. Do independent work concurrently       →  fan out
4. Do the remaining work faster           →  optimise
```

---

## 1. Precompute — the largest win, taken first

**Scoring never happens during a request.** `matcher` writes `user_job_scores` when a posting or a
resume changes; the feed reads it ([matching-and-scoring §6](matching-and-scoring.md#6-when-scoring-runs)).

The counterfactual is worth stating: scoring 25 postings inline at ~0.5 ms each plus embedding
comparison would put **12–40 ms of pure CPU** on every feed request, and it would scale with result
count rather than staying flat. Precomputation converts that into an indexed read.

The same principle, applied elsewhere:

| Precomputed | Instead of |
|---|---|
| `search_tsv` (generated column) | Building the vector per query |
| `has_disclosed_comp` (generated column) | `comp_min IS NOT NULL` in every filter |
| `title_normalised` | Normalising at query time |
| Company posture aggregates (nightly) | Aggregating 180 days of postings per request |
| Skill adjacency (in-memory table) | Joining a lookup per skill per posting |

**The rule:** if a value can be computed at write time and read many times, it is computed at write
time. Reads outnumber writes by roughly 100:1 here.

---

## 2. Coalesce and batch

### Request coalescing with `singleflight`

Under concurrency, N identical in-flight requests should cause **one** database query, not N.

```go
// Ten concurrent requests for the same facet counts collapse into one query.
// Without this, a cold cache under load is a stampede: every request misses,
// every request queries, and the database absorbs the full concurrency.
v, err, _ := facetGroup.Do(filterKey, func() (any, error) {
    return store.FacetCounts(ctx, filter)
})
```

`golang.org/x/sync/singleflight` is the battle-tested implementation and handles the edge cases
`[B-30]`. Two limitations we design around, because both are real:

- **It is per-process.** A `Group` only sees calls inside one replica, so it cannot coalesce across
  the fleet. With 4 replicas a stampede becomes 4 queries instead of 400 — which is the whole benefit
  we need, and is also why cross-replica coherence (Redis) is not yet worth it
  ([caching-and-storage §2](caching-and-storage.md#2-application-caching--why-not-redis-yet)).
- **Head-of-line blocking:** callers wait as long as the shared call takes, with no early bail-out
  through `Do`. So every coalesced call uses **`DoChan` with a context timeout**, never bare `Do` —
  one slow query must not stall every waiter past its deadline.

Applied to: facet counts, company metadata, skill taxonomy reloads, and embedding generation for the
same posting.

### Query batching with `pgx.SendBatch`

Multiple round trips over one connection become one round trip.

```go
// One network round trip instead of three. At 0.5ms RTT inside the cluster
// that is 1ms saved per request — which is 20% of a 5ms query budget.
batch := &pgx.Batch{}
batch.Queue(qFeedPage, args...)
batch.Queue(qFacetCounts, args...)
batch.Queue(qWatchedCompanies, userID)
br := tx.SendBatch(ctx, batch)
```

`CopyFrom` for bulk inserts during ingestion — a 400-posting board upsert is one `COPY` into a temp
table plus one `INSERT ... ON CONFLICT`, not 400 statements.

### N+1 elimination is structural, not vigilance

Two mechanisms, because relying on reviewers to spot N+1 does not work:

1. **`sqlc` generates from hand-written SQL**, so joins are visible in the query file. There is no ORM
   lazily loading a relation behind an innocuous field access.
2. **An integration test asserts query count per endpoint.** `GET /v1/jobs` must issue **≤ 3**
   queries regardless of result size. A change that introduces a per-row query fails the build rather
   than being noticed in production.

---

## 3. Fan out

### The ingestion case — where concurrency actually pays

Polling 5,000 sources is almost entirely socket wait. Sequential would take hours; concurrent takes
minutes.

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(maxConcurrentHosts)   // politeness is a hard limit, not a tuning knob

for _, src := range due {
    g.Go(func() error {
        // Per-host token bucket: never concurrent to the same host, and the
        // delay adapts to that host's observed response time. Global
        // concurrency is high; per-host concurrency is exactly 1.
        if err := limiter.Wait(ctx, src.Host); err != nil { return err }
        return fetch(ctx, src)
    })
}
return g.Wait()
```

The distinction that matters: **global concurrency high, per-host concurrency 1.** Fan-out is for our
throughput; the per-host limit is for their servers
([ingestion §politeness](ingestion-pipeline.md#politeness)).

### The request case — where it usually does not

A feed request needs the page, the facet counts, and the user's watched companies. These are
independent, so they *could* run concurrently — but they are also three cheap queries against one
database.

**We batch them rather than fanning out.** Three goroutines each taking a pooled connection would
triple connection pressure to save one round trip; `SendBatch` saves the round trips *and* uses one
connection. Concurrency inside a request is reserved for genuinely independent I/O against
*different* systems — e.g. `api` calling `resume-parser` while reading the database.

**The general rule:** fan out across systems, batch within one.

---

## 4. Optimise what remains

| Technique | Applied to |
|---|---|
| Partial indexes on `status='live'` | ~3× smaller, stays in shared buffers |
| Keyset pagination | Flat cost at any depth; `OFFSET` degrades linearly |
| Covering indexes | Index-only scans on the feed's ranking key |
| Generated columns | `search_tsv`, `has_disclosed_comp` |
| Binary protocol | pgx default — no text encode/decode |
| Prepared statements | pgx statement cache |
| `SET LOCAL` for vector GUCs | Correctness under transaction pooling |
| Streaming JSON encode | No full-response buffering |
| `sync.Pool` for hot buffers | Normalisation, which allocates heavily |

**Not doing:** micro-optimising Go allocations outside proven hot paths, or hand-rolling a JSON
encoder. Both cost readability for gains dominated by the database.

---

## 5. Latency budget

A budget only means something if it is apportioned. `GET /v1/jobs`, p95 ≤ 120 ms server time:

```
Gateway routing + TLS resumption          2 ms   ▏
Session lookup (indexed, cached rows)     3 ms   ▎
Authorisation                             1 ms   ▏
Feed query (keyset + partial index)      15 ms   █▌
Facet counts (LRU hit)                    0 ms
Facet counts (LRU miss, coalesced)       25 ms   ██▌   ← p95 driver
JSON encode (25 rows, streamed)           4 ms   ▍
────────────────────────────────────────────────
p50                                      ~25 ms
p95                                      ~50 ms
Budget                                   120 ms        ← 2.4× headroom
```

Headroom is deliberate. Budgets consumed at design time have nothing left for real-world variance —
a cold buffer cache, a noisy neighbour, an autovacuum pass.

### `GET /v1/me/dashboard`, p95 ≤ 400 ms server time

The first page every signed-in user loads, and it had **no budget at all** until 2026-08-24 — which is
why it spent an unknown period returning 500 at its ten-second deadline before anyone noticed
([A-00k](../research/evidence-ledger.md#a-00k)). An endpoint with no budget has no gate.

```
Session lookup                            3 ms   ▏
Band counts (range scan, ~700 rows)      26 ms   ██
New in 24h (freshness index → PK probe)  60 ms   ████▌   ← inflated during a backfill
Total scored (count over one user)        7 ms   ▌
Applications + attention + top matches   20 ms   █▌
Market summary                           40 ms   ███
────────────────────────────────────────────────
Measured, one batch, one round trip     ~160 ms
Budget                                   400 ms        ← 2.5× headroom
```

It is looser than the feed's 120 ms on purpose: this endpoint answers six questions rather than one,
and it is loaded once per session rather than on every scroll.

**An index-only scan is the wrong instrument for a continuously-rewritten table.** `user_job_scores`
is replaced wholesale for a user on every rescore, so its visibility map is never clean; a covering
index took the band counts from 9,500 ms to 68 ms and then decayed to 13,687 ms as dead tuples
accumulated. What fixed it was reading fewer rows — a range scan over the few hundred rows that match,
rather than a filter over all fifteen thousand — plus `autovacuum_vacuum_scale_factor = 0.02` on that
table, since the default lets a 1.2M-row table reach 240,000 dead tuples before it cleans.

**Every outbound call has a context deadline**, and the deadlines nest: a 120 ms request budget cannot
contain a 5 s database timeout. Timeouts are derived from the request budget, never set independently
per call site.

---

## 6. Ingestion throughput

| Stage | Target | Bound by |
|---|---|---|
| Fetch | 15,000 req/day (0.17 rps) | Deliberate politeness, not capacity |
| Parse + normalise | ≥ 500 postings/s/core | CPU |
| Dedup — blocking | ≥ 5,000 candidates/s | `pg_trgm` index |
| Dedup — embedding stage | ~50/s | Only the ambiguous band reaches it |
| Upsert | ≥ 2,000 postings/s | `COPY` + `ON CONFLICT` |
| Embedding | ~50/s/core | CPU (384-dim) |

The three-stage dedup ordering is itself a performance decision: blocking removes ~99% of pairs at
index cost, cheap string comparison resolves most of the rest, and embeddings run on the small
remainder `[A-20]`. Running embeddings first would be ~100× more expensive for the same answer.

---

## 7. Graceful degradation under load

When something saturates, **degrade visibly rather than failing or lying**:

| Pressure | Response |
|---|---|
| Scoring backlog | Feed falls back to recency order with a `scoring catching up` banner |
| Ingestion behind | Card age display makes staleness visible; a banner appears past the SLO |
| DB connections high | Workers shed first (queue is durable); `api` keeps its allocation |
| Facet query slow | Serve last-known counts, marked approximate |
| Rate limit hit | 429 with `Retry-After` — never a silent drop |

The ordering principle: **user-facing paths degrade last, and never silently.** A stale feed that says
it is stale is recoverable; one that pretends to be fresh destroys trust permanently.

---

## 8. Enforced in CI

| Test | Assertion |
|---|---|
| Feed query, 200k postings, 3 filters | p95 ≤ 120 ms |
| Feed query, 1M postings | p95 ≤ 200 ms (year-2 headroom) |
| **Query count, `GET /v1/jobs`** | **≤ 3, independent of result size** |
| 400 concurrent feed requests | No errors, p99 ≤ 500 ms |
| Ingest 5,000 sources | Completes within one tier-A cycle |
| Scoring fan-out, 10k postings | Queue drains ≤ 10 min |
| Allocation profile, feed handler | No regression > 20% |

The **query-count assertion** is the one that catches the most damaging class of regression, because
an N+1 looks fine at 25 rows in development and takes production down at scale.
