# Caching and storage — what we need, and when

> Status: **DECIDED**. Every "add X" below has a **numeric trigger**. Adding infrastructure without
> the trigger having fired requires an ADR.

The question this document answers: **do we need Redis, S3, a CDN — or is Postgres enough?**

The honest answer differs per concern, so we take them one at a time with real numbers. The general
finding across 2026 benchmarking is worth stating up front because it sets the prior:

> Redis is genuinely faster — roughly **1.2 ms p50 vs Postgres at 8.5 ms** on read-heavy cache
> workloads, and 3–12× higher throughput. But most applications never exceed Postgres's caching
> capacity, and the hidden costs of specialisation — a second stateful system, network hops, cache
> invalidation, developer context-switching — routinely exceed the benefit `[B-27]`.

So the decision is not "which is faster". It is **at what point does the speed matter more than the
operational cost**. Below, that point is a number.

---

## 1. Decision summary

| Concern | v1 | Trigger to change | What we'd add |
|---|---|---|---|
| Facet counts | **In-process LRU** | > 4 `api` replicas **and** facet query > 5% of DB CPU | Redis |
| Feed results | **Not cached** | Never — freshness is the product | — |
| Session lookup | **Postgres** | Session reads > 15% of DB CPU | Redis |
| Rate limiting | **Gateway (in-memory) + Postgres** | Multi-region deployment | Redis |
| Resume blobs | **S3-compatible object storage from day one** | — | (already) |
| Job description HTML | **Postgres `text`** | p95 row fetch > 20 ms from TOAST | Move to object storage |
| Embeddings | **Postgres `vector(384)`** | > 20M vectors | Dedicated vector store |
| Static assets | **CDN from day one** | — | (already) |
| API responses at edge | **Never cached** | — | — |

**Two things are in from day one** (object storage for blobs, CDN for static assets) because they are
not optimisations — they are the correct place for that data at any scale. **Everything else waits**,
and the reasoning per row follows.

---

## 2. Application caching — why not Redis yet

### What we would actually cache

Being specific matters, because "add a cache" is usually a wish rather than a plan.

| Candidate | Size | Change rate | Shared across users? |
|---|---|---|---|
| **Facet counts** (`Remote (1,240)`) | ~50 KB total | Minutes | ✅ Yes, per filter prefix |
| Company metadata | ~5 MB total | Days | ✅ Yes |
| Skill taxonomy + adjacency | ~2 MB | Weeks | ✅ Yes |
| Feed results | Large | **Constantly** | ❌ Per user |
| `user_job_scores` | Large | On change | ❌ Per user |
| Session records | Small | Per request | ❌ Per user |

The three cacheable items total **under 10 MB, are identical across users, and change slowly.** That
is a description of an in-process LRU, not of Redis.

### The in-process design

```go
// Facet counts are small, slow-changing, and identical for every user with the
// same filter prefix. An in-process cache serves them in ~200ns with zero
// network hops and zero new infrastructure.
//
// Per-replica duplication is the trade: with 4 replicas we do 4x the refresh
// queries. At ~50KB and a 60s TTL that is 4 queries/minute, which is free.
// singleflight collapses the concurrent refreshes within each replica.
var facets = lru.New[FilterKey, Counts](1024, 60*time.Second)
```

**What Redis would buy:** cross-replica coherence — one refresh instead of N. **What it would cost:**
a second stateful system to run, secure, back up and monitor; a network hop on the hot path; and a
new failure mode (what happens when Redis is down?) that must be designed for.

At 4 replicas the saving is three redundant queries per minute. That is not worth an
[ADR-0003](adr/0003-postgres-single-datastore.md) exception.

### The trigger, stated numerically

Add Redis when **both** hold for a sustained week:

1. `api` replicas > 4, **and**
2. facet-refresh queries exceed **5% of Postgres CPU**

Below that, the cross-replica saving is smaller than the operational cost. This is the "avoid
premature caching" position, held with a number attached rather than as a preference.

### Sessions — the near-miss case

Session lookup happens on **every authenticated request**, which makes it the most tempting Redis
candidate. It stays in Postgres for v1 because:

- It is a **primary-key lookup on an indexed single row** — the case where Postgres is genuinely fast,
  consistently under 10 ms and usually far better from shared buffers.
- Server-side sessions exist so **revocation is immediate**
  ([security §4](../operations/security-and-privacy.md#4-authentication)). Redis with a TTL
  reintroduces exactly the staleness we chose sessions to avoid.
- At 400 RPS that is 400 indexed single-row reads per second — a small fraction of one core.

**Trigger:** session reads exceed 15% of database CPU. Expected around 100k MAU, after PgBouncer.

---

## 3. Why the feed is never cached

The most valuable line in this document, because it is where an experienced engineer's instinct is
wrong.

```
Feed request = f(user, resume version, 8 filters, sort, cursor, current time)
```

- **Cardinality is effectively unbounded.** Per-user × per-filter-combination means a near-zero hit
  rate; we would pay the cache write cost and get the database read anyway.
- **Freshness is the product.** A cached feed is a stale feed. Postings collect hundreds of
  applications within hours `[B-04]`, and the whole ingestion architecture exists to deliver a
  90-minute median. Caching the output would discard that at the last step
  ([P2](../product/principles.md#p2--freshness-is-the-product)).
- **It is already fast.** Scores are precomputed in `user_job_scores`; the request is an indexed
  keyset read of 25 rows at 5–15 ms.

**The correct optimisation was done upstream — precomputing scores — not downstream by caching
results.** Caching would be treating a symptom we do not have.

---

## 4. Object storage — needed from day one, and this one is not close

Resume files are PDFs and DOCX at up to 5 MB. The threshold guidance is unambiguous:

- `bytea` values over **~2 KB** are compressed and moved to TOAST, and query performance degrades
  **2–10×** for values past that point `[B-28]`.
- `bytea` is appropriate for small binary objects — hashes, signatures, certificates — up to perhaps
  10–20 MB **as an absolute ceiling**, not as a recommendation.
- Files of more than a few megabytes belong in object storage with a reference in the row `[B-28]`.

The operational consequences are what actually decide it:

| Storing 5 MB resumes in Postgres | Effect |
|---|---|
| Backups | 10k users × 2 resumes × 5 MB = **100 GB** added to every backup and PITR window |
| Replication | **Every update ships the full blob through WAL** to the standby |
| Memory | Fetching requires allocating and decompressing the whole value |
| Restore time | Grows with blob volume, not with row count |

100 GB of immutable binary data in a database whose *relational* footprint is ~5 GB would make every
backup, restore and failover twenty times slower — to store bytes that are never queried, only
fetched by key.

**Decision: S3-compatible object storage from day one.** Not a cloud vendor's SDK — the *API*, which
MinIO, Ceph and every provider implement, so it stays portable
([service-topology §8](service-topology.md#8-vendor-neutrality--the-portability-contract)). MinIO runs
in the local container stack, so development and production use identical code paths.

What stays in Postgres: `blob_key`, `mime_type`, `byte_size`, and the **encrypted parsed text and
structured profile** — those are small, queried, and must be transactionally consistent with the
user row.

### Job description HTML — the genuinely marginal case

Descriptions average 8–15 KB, which is above the TOAST threshold but far below the object-storage
threshold. At 200k live postings that is roughly **2–3 GB**.

**Stays in Postgres**, because unlike resume blobs the text is *queried* — it feeds `search_tsv`,
skill extraction and knockout parsing. Splitting it out would turn one row fetch into a row fetch plus
a network round trip, on the hot path.

**Trigger to reconsider:** p95 `job_postings` row fetch exceeds 20 ms attributable to TOAST
decompression. The fix at that point is to move `description_html` to a side table (keeping
`description_text` inline for search), which is cheaper than object storage and preserves
transactionality.

---

## 5. Connection pooling

Not caching, but the same category of question: when does the shared datastore need help?

**v1: pgx's built-in pool.** Per-component limits are a hard contract
([service-topology §5](service-topology.md#5-resource-and-connection-budgets)), totalling 422 against
a `max_connections` of 600.

**Trigger: PgBouncer in transaction mode at > 400 sustained pooled connections.** This is
[bottleneck #1](../operations/scaling-and-capacity.md#5-bottleneck-order) and is expected around 100k
MAU — a scheduled task, not an incident.

⚠️ **One thing must be fixed before PgBouncer lands**, and it is easy to miss: transaction-mode
pooling breaks session-level `SET`. Every `SET hnsw.iterative_scan` and `SET hnsw.ef_search` must
therefore be `SET LOCAL` inside an explicit transaction, or vector search silently reverts to defaults
and returns short result sets. We write it that way **from day one** so the migration is a
configuration change rather than a correctness bug hunt. A lint rule rejects a bare `SET` in
`internal/store/queries/`.

---

## 6. CDN

**Static assets: from day one.** Content-hashed filenames, `public, max-age=31536000, immutable`.
Free, uncontroversial, and provider-interchangeable.

**API responses: never at the edge.** Shared caches store one response and serve it to many users, so
personalised content must never enter one. Every authenticated response is `private`, and the feed
additionally carries `max-age=0, must-revalidate` `[B-29]`.

`stale-while-revalidate` is a good directive that is **wrong for our hot path** — it explicitly trades
freshness for latency, which inverts P2. It is used only for company metadata
(`public, max-age=300, stale-while-revalidate=60`), where staleness is harmless.

---

## 7. What Postgres is genuinely doing for us

Worth listing, because it is the reason the dependency count stays at two:

| Job | Mechanism | Alternative avoided |
|---|---|---|
| Relational store | tables | — |
| Full-text search | `tsvector` + GIN | Elasticsearch |
| Vector similarity | `pgvector` HNSW | Qdrant / Pinecone |
| Durable queue | River | Redis + asynq / RabbitMQ / SQS |
| Pub/sub wakeups | `LISTEN`/`NOTIFY` | Redis pub/sub / NATS |
| Scheduled jobs | River periodic | cron / Temporal |
| Fuzzy matching | `pg_trgm` | — |
| Leader election | advisory locks | etcd / Consul |

**Eight jobs, one system to back up, secure, monitor and upgrade.** And because the queue lives in the
same database as the data, enqueue is transactional — which is what removes the outbox pattern, saga
compensation and CDC from the architecture entirely
([ADR-0005](adr/0005-river-background-jobs.md)).

---

## 8. How we will know

Each trigger has a metric already defined in
[observability.md](../engineering/observability.md#4-metrics-and-slos):

| Trigger | Metric | Alert |
|---|---|---|
| Redis for facets | `db_query_duration_seconds{query="facet_counts"}` × rate | > 5% DB CPU, 7 days |
| Redis for sessions | `db_query_duration_seconds{query="session_lookup"}` × rate | > 15% DB CPU |
| PgBouncer | `db_connections_in_use` | > 400 sustained |
| Description offload | `db_query_duration_seconds{query="get_posting"}` p95 | > 20 ms |
| Vector store | `count(posting_embeddings)` | > 20M |

**No infrastructure is added without its metric having fired.** That rule is the point of this
document — it converts "should we add a cache?" from an argument into a query.
