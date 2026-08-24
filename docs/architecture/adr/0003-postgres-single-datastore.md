# ADR-0003 — PostgreSQL as the only datastore

- **Status:** DECIDED
- **Date:** 2026-08-15
- **Decision drivers:** operational surface for a small team, dependency minimisation, transactional
  simplicity, adequacy at our data size

## Context

The system needs: relational storage, full-text search, vector similarity, a durable job queue,
pub/sub wakeups, caching, and scheduled work.

The conventional answer is Postgres + Redis + Elasticsearch + a vector DB + a broker. That is five
stateful systems to secure, monitor, back up, upgrade, and reason about during an incident — for a
team of one to five.

The relevant data sizes:

| Thing | v1 | Year 2 |
|---|---:|---:|
| Live postings | 200k | 1M |
| Total postings incl. closed | 700k | 5M |
| Embeddings (384-dim) | 200k ≈ 300 MB | 1M ≈ 1.5 GB |
| Users | 10k | 100k |
| `user_job_scores` | ~20M | ~200M |

**Every one of these is small for Postgres.** That is the whole argument — the conventional
multi-store answer solves problems that appear one to two orders of magnitude further out.

## Options

| Capability | Postgres does it via | Dedicated alternative | Verdict at our size |
|---|---|---|---|
| Full-text search | `tsvector` + GIN | Elasticsearch, Meilisearch | Postgres. `ts_rank` is a TF-IDF variant not true BM25 — a real limitation, acceptable at 1M docs, with an in-database upgrade path (`pg_search`) if it binds |
| Vector search | `pgvector` HNSW | Qdrant, Pinecone, Weaviate | Postgres. Benchmarked against dedicated stores at 100k–2M vectors; 0.8 brought parallel index builds and up to 5.7× query improvement over 0.7.4 `[A-13]` |
| Job queue | River | Redis+asynq, RabbitMQ, SQS | Postgres — see [ADR-0005](0005-river-background-jobs.md) |
| Pub/sub | `LISTEN`/`NOTIFY` | Redis pub/sub, NATS | Postgres. We need wakeups, not a streaming platform |
| Cache | In-process LRU | Redis | In-process. Facet counts are small, slow-changing, and identical across users |
| Scheduling | River periodic jobs | cron, Temporal | Postgres |

**The decisive property for vectors:** filtered vector search is our actual query shape (*"semantically
similar to this resume **and** in Bengaluru **and** 0–3 YoE"*). Before pgvector 0.8, combining a vector
index with a `WHERE` clause could over-filter and return too few results. Iterative index scans fixed
this by continuing to scan until enough post-filter results are found `[A-13]`. A separate vector
database makes this *harder*, not easier — the filter lives in Postgres and the vectors do not, so you
either over-fetch and post-filter or maintain a denormalised copy of the filterable columns.

## Decision

**PostgreSQL 17 is the only datastore.** Extensions: `pgvector`, `pg_trgm`, `citext`, `pgcrypto`.
Object storage (S3-compatible API) holds resume blobs — it is a blob store, not a database, and it is
provider-interchangeable.

**No Redis. No Elasticsearch. No vector database. No message broker.**

## Consequences

### Good

- **One transaction spans everything.** A posting upsert and the job that scores it commit together.
  This eliminates the dual-write problem outright, which is why the system needs no transactional
  outbox, no CDC pipeline, and no saga — see
  [service-topology §4](../service-topology.md#why-there-is-no-outbox-pattern-no-saga-and-no-broker).
  That single consequence is worth more than every performance argument on this page.
- One backup, one restore procedure, one PITR window, one set of credentials, one upgrade path.
- Local development is `docker compose up` with one service.
- `LISTEN/NOTIFY` gives sub-second worker wakeups without a polling loop.

### Bad, and accepted

- **Single failure domain.** Mitigated by sync standby, PITR, and hard per-component connection pool
  limits so one runaway worker cannot starve `api`
  ([service-topology §5](../service-topology.md#5-resource-and-connection-budgets)).
- **`ts_rank` is not BM25.** Ranking quality is measurably worse than Elasticsearch on long queries.
  Accepted because retrieval is hybrid and RRF-fused, so lexical weakness is partly compensated by
  the vector arm ([ADR-0006](0006-hybrid-retrieval-and-scoring.md)).
- **HNSW index builds are expensive.** A 1M-vector rebuild is minutes of heavy I/O. Mitigated by
  building concurrently and by rebuilding only on a model change.
- **Vacuum and bloat become real concerns** at `user_job_scores` volume. Addressed by partitioning
  and by deleting scores when postings close ([data-model.md](../data-model.md#user_job_scores)).

## Exit conditions

Specific, so that adding a store is an evidence-based decision rather than a preference. Each has a
metric and an alert defined in
[caching-and-storage §8](../caching-and-storage.md#8-how-we-will-know) — that document is the
operational form of this ADR, and it is where the reasoning per concern lives.

| Add | When |
|---|---|
| **PgBouncer** | Total pooled connections > 400 sustained. Expected first — a normal step, not a failure |
| **Read replica** | Read queries > 60% of primary CPU **and** the app tier is not the bottleneck |
| **BM25 in Postgres** (`pg_search`) | Search relevance complaints trace to lexical ranking in a measured evaluation |
| **Dedicated vector DB** | > 20M vectors **or** HNSW build time blocks a needed model iteration cadence |
| **Redis** | A measured cache need that an in-process LRU cannot serve — i.e. cross-instance coherence is genuinely required |
| **Broker (NATS/Kafka)** | An external consumer needs an event stream. Note this **reintroduces the outbox pattern**, and that cost must be in the proposal |

## Revisit

When any exit condition is met, with the measurement attached. Not before.
