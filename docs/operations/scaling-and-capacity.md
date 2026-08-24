# Scaling and capacity

> Status: **DECIDED**. All figures are engineering estimates from first principles; the assumptions
> are stated so they can be corrected against real measurements.

The point of this document is to know **in advance** what breaks first, at what number, and what the
fix is — so that scaling is a scheduled task rather than an incident.

## 1. Assumptions

| Parameter | v1 | Year 1 | Year 2 |
|---|---:|---:|---:|
| Tracked company boards | 500 | 5,000 | 20,000 |
| Live postings | 20,000 | 200,000 | 1,000,000 |
| Monthly active users | 1,000 | 10,000 | 100,000 |
| Sessions/user/month | 40 | 50 | 50 |
| Requests/session | 20 | 20 | 20 |
| Resumes per user | 1.5 | 2 | 2 |

## 2. Ingestion load

The number that surprises people: **ingestion is nearly free.**

At year 1 (5,000 sources: 500 tier A, 1,500 tier B, 3,000 tier C):

```
Tier A:   500 × 12 polls/day  =  6,000
Tier B: 1,500 ×  4 polls/day  =  6,000
Tier C: 3,000 ×  1 poll/day   =  3,000
                                ───────
                                 15,000 requests/day = 0.17 req/s
```

Spread across ~5,000 hosts: **three requests per host per day.** Gentler than a search crawler.

**~90% return 304 or an unchanged content hash**, so real parsing work is ~1,500 payloads/day. At an
average 200 KB per changed payload that is 300 MB/day of ingress.

Normalisation and dedup run at roughly 500 postings/s per core, so the daily work is on the order of
**a couple of CPU-minutes**.

> **Conclusion:** ingestion never becomes the bottleneck at these scales. `ingestor` scales to zero
> between cycles and the cost is rounding error. Conditional requests are what make this true — which
> is why `source_poll_304_ratio` is an alerting metric
> ([observability §5](../engineering/observability.md#5-alerts)).

At year 2 (20,000 sources) it is 60,000 requests/day ≈ 0.7 req/s. Still trivial.

## 3. Scoring load

**This is the first real pressure point**, and the fan-out ratio is the variable that decides it.

Naive: every new posting × every user = `10,000 postings/day × 10,000 users = 100M scores/day`.
Impossible, and unnecessary — most users do not care about most postings. So a posting is scored only
for users whose stored preferences (geography, work mode, YoE band) make it **plausibly relevant**.

### Deriving the fan-out ratio

> ⚠️ **Corrected 2026-08-15.** This was previously stated as "~3%", which was a placeholder that had
> become a capacity model without ever being derived. The derivation below puts it near **13%** —
> roughly 4× higher — with a tail scenario materially worse than that. Full working:
> [verification-log V11](../research/verification-log.md#v11--fan-out-the-assumption-was-wrong-and-the-tail-is-worse-than-the-mean).

Fan-out is the probability that a random user's coarse filter admits a random new posting.

**Geography does most of the filtering.** Using verified market composition — Bengaluru **34%** of
tracked Indian tech listings, Hyderabad 19%, Pune 13%, tier-1 cities **88–90%** of demand `[B-37]` —
and noting that the user base concentrates the same way the market does:

```
posting city      posting share   × users accepting   = contribution
Bengaluru              0.34       ×      ~0.50        =   0.17
Hyderabad              0.19       ×      ~0.25        =   0.048
Pune                   0.13       ×      ~0.20        =   0.026
other tier-1           0.22       ×      ~0.15        =   0.033
tier-2/3               0.12       ×      ~0.08        =   0.010
                                         geography term ≈ 0.29
```

**Experience band** contributes roughly 0.40–0.50 — roles requiring ≤3 years are **28%** of postings,
down from 43% in 2018 `[B-38]`, against a junior-skewed user base and a stretch band that widens
matching upward.

```
fan-out ≈ 0.29 × 0.45 ≈ 0.13
```

### The tail risk exceeds the error in the mean

**A remote posting matches every user regardless of geography** — the term doing all the filtering
collapses to 1.0. Fan-out is therefore acutely sensitive to remote share, and **that data conflicts
irreconcilably** `[C-09]`:

| Population | Remote / hybrid |
|---|---|
| All job postings, Q2 2026 | 3–4% remote, ~10% hybrid, **87% on-site** |
| **Software engineering**, LinkedIn, late 2025 | **67% remote or hybrid** |

Different populations, different dates, and the tech sector genuinely diverges from the wider
return-to-office trend. **We build for the population where the higher figure applies**, so the
pessimistic branch is the one to plan against.

### Sensitivity

| Scenario | Fan-out | Year-1 scores/day | Year-2 scores/day | Sustained cores @ 0.5 ms |
|---|---:|---:|---:|---:|
| Old assumption (wrong) | 3% | 3 M | 150 M | ~1 |
| **Derived estimate** | **13%** | **13 M** | **650 M** | **~4** |
| High-remote scenario | 40% | 40 M | 2.0 B | ~12 |

Twelve sustained cores in the worst case is real but tractable: `matcher` scales on queue depth to 30
replicas on its own node pool, which exists precisely so this cannot reach API latency
([service-topology §3](../architecture/service-topology.md#matcher)).

### Two bounds that make the uncertainty survivable

**1. Score only for users active in the last 30 days.** Dormant users are scored lazily on their next
session. At typical retention this is a **~2.5× reduction**, and it is free — nobody waits on a score
they will never look at. This is what turns the 40% scenario from ~12 cores into ~5.

**2. `score_fanout_ratio` is instrumented from day one**, with an alert above 20%. The 13% figure is a
derivation, not a measurement, and it stops being a derivation the moment real traffic exists.

Embedding generation is unaffected by fan-out — it is per-posting, not per-user-per-posting. At ~20 ms
per posting for a 384-dim model, 10,000/day is **200 CPU-seconds/day**. Negligible in every scenario.

### Storage is insensitive to fan-out — and that is not luck

`user_job_scores` is the fastest-growing table, but **the top-N cap binds before fan-out does**:

```
Year 1:  10,000 users × 2,000 capped  =   20M rows  ≈  4 GB
Year 2: 100,000 users × 2,000 capped  =  200M rows  ≈ 40 GB
```

**These figures hold in all three fan-out scenarios**, including the 40% one. We store at most ~2,000
scores per user regardless of how many postings qualify; the rest are computed on demand.

That cap was originally written down as a routine mitigation. The fan-out correction revealed it is
actually **the thing that makes an incorrect fan-out assumption survivable** — a better argument for
it than the one first given, and worth remembering if anyone proposes relaxing it.

Mitigations, in the order we apply them:

1. **Delete scores when a posting closes.** ~70% of postings are closed at steady state, so this is
   the largest single win and it is free.
2. **Cap stored scores per user** at the top ~2,000 by rank; compute the rest on demand. **Load-bearing
   — see above.**
3. **Partition by `user_id` hash** if it exceeds ~100M rows.

## 4. Query load

| | Avg RPS | Peak RPS (10×) |
|---|---:|---:|
| Year 1 | 4 | 40 |
| Year 2 | 40 | 400 |

The feed query against `user_job_scores` with a keyset cursor and partial indexes is **~5–15 ms** at
200k live postings. At 400 RPS that is ~6 concurrent queries — a fraction of one Postgres core.

**HNSW vector search** on 200k × 384-dim: index ~300 MB, resident in shared buffers, ~2–5 ms per query
with `ef_search = 40`. At 1M vectors the index is ~1.5 GB and still comfortably resident on a 16 GB
instance.

pgvector 0.8 matters here: parallel index builds cut build time 30–50% on multi-core machines, and
iterative index scans are what make our always-filtered vector queries return correct result counts
`[A-13]`.

## 5. Bottleneck order

The most useful table in this document. **This is the order things break, and we know each fix in
advance.**

| # | Bottleneck | Fires at | Fix | Cost |
|---|---|---|---|---|
| 1 | **Postgres connections** | ~400 pooled | **PgBouncer**, transaction mode | Hours. Expected, not an emergency |
| 2 | `user_job_scores` size | ~100M rows | Retention + top-N cap + partitioning | Days |
| 3 | Postgres write throughput | ~5k writes/s | Batch upserts (`COPY`), reduce score churn | Days |
| 4 | Postgres read CPU | > 60% sustained | **Read replica** for feed queries | Days — but introduces replication lag bugs, so not before |
| 5 | Feed query latency | p95 > 120 ms with DB < 50% CPU | Bottleneck moved to the app: split the read path | Weeks |
| 6 | HNSW build time | > 20M vectors | Dedicated vector store | Weeks |
| 7 | Single-primary writes | ~20k writes/s | Shard by user, or CQRS | Months |

**We are at #1 around 100k MAU.** Everything past #4 is beyond any credible near-term plan and is
listed only so nobody has to rediscover the order under pressure.

**Read replicas are deliberately #4, not #1.** They introduce a genuine correctness hazard — a user
applies, then reads a stale replica and sees no application — for a capacity problem PgBouncer solves
more cheaply. Adding one before the trigger fires trades a real bug class for an imaginary benefit.

## 6. Cost model

Provider-neutral, in generic units. Actual prices vary; the **ratios** are the point.

### Year 1 — 10k MAU

| Component | Spec | Est. monthly |
|---|---|---:|
| `api` | 2–4 pods × 0.25 vCPU | $30 |
| `web` | 2 pods × 0.25 vCPU | $20 |
| `ingestor` | 0–4 pods, mostly zero | $10 |
| `matcher` | 0–4 pods, bursty | $25 |
| `scheduler` + `resume-parser` | 1 + 0–2 pods | $15 |
| Gateway | 2 pods | $20 |
| **Postgres** | **4 vCPU / 16 GB / 200 GB + standby** | **$180** |
| Object storage | 50 GB + egress | $10 |
| Telemetry | self-hosted or small hosted tier | $40 |
| **Total** | | **≈ $350/mo** |

**Postgres is over half the bill**, which is the expected shape of a
[single-datastore architecture](../architecture/adr/0003-postgres-single-datastore.md) and is exactly
the trade we chose: one well-provisioned database instead of five under-provisioned systems, each
with its own operational overhead.

### Year 2 — 100k MAU

| Component | Est. monthly |
|---|---:|
| Application tier (all units) | $250 |
| **Postgres** — 8 vCPU / 32 GB / 1 TB + standby | **$600** |
| PgBouncer | $20 |
| Object storage | $60 |
| Telemetry | $150 |
| **Total** | **≈ $1,080/mo** |

**10× the users for ~3× the cost.** The sublinear scaling comes from three places: ingestion cost is
flat in users, scoring fan-out is bounded, and workers scale to zero when idle.

### Cost per user

`$350 / 10,000 = $0.035/user/month` → `$1,080 / 100,000 = $0.011/user/month`.

Both are comfortably inside any plausible business model, which is the actual purpose of this section:
it tells us the architecture does not need to change for the product to be viable.

## 7. What we do not need to scale

Worth stating, since these are where over-engineering usually happens:

| Not a problem | Why |
|---|---|
| Ingestion throughput | 0.17 req/s at year 1 |
| Embedding generation | 200 CPU-seconds/day |
| Resume parsing | Bounded by upload rate: ~500/day at 10k MAU |
| Static assets | Content-hashed, immutable, cached forever |
| Search index size | 200k documents is small for Postgres FTS |

## 8. Load testing

Run in CI against a fixed dataset, so a regression fails the build rather than being discovered in
production:

| Scenario | Assertion |
|---|---|
| Feed query, 200k postings, 3 filters | p95 ≤ 120 ms |
| Feed query, 1M postings | p95 ≤ 200 ms (year-2 headroom check) |
| 400 concurrent feed requests | No error, p99 ≤ 500 ms |
| Ingest 5,000 sources | Complete within one tier-A cycle |
| Score fan-out, 10k changed postings | Queue drains within 10 min |

The 1M-posting case is deliberately beyond current scale. Load tests that only cover today's traffic
tell you nothing about the headroom you are relying on.
