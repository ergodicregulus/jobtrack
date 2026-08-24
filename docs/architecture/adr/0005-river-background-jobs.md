# ADR-0005 — River for background jobs

- **Status:** DECIDED
- **Date:** 2026-08-15
- **Decision drivers:** transactional enqueue, no new infrastructure, type safety, operational
  simplicity

## Context

Nearly everything in this system is background work: source polling, normalisation, dedup, embedding,
scoring, the 21-day ghosting sweep, digest sends, partition maintenance, retention.

The requirement that decides it: **enqueueing a job must be atomic with the database change that
motivated it.** If a posting is upserted but its scoring job is lost, the posting sits unscored
forever with nothing to detect it.

## Options

| | **River** | asynq (Redis) | Temporal | Postgres table + hand-rolled | SQS/Pub-Sub |
|---|---|---|---|---|---|
| Backing store | **Postgres** | Redis | Own cluster / cloud | Postgres | Managed cloud |
| New infrastructure | **None** | Redis | Substantial | None | Cloud dependency |
| Transactional enqueue | **✓ native** | ✗ | ✗ | ✓ | ✗ |
| Type-safe args | **✓ generics** | ✗ (JSON) | ✓ | ✗ | ✗ |
| Retries, backoff, DLQ | ✓ | ✓ | ✓ | build it | ✓ |
| Periodic jobs | ✓ | ✓ | ✓ | build it | ✗ |
| Per-queue concurrency limits | ✓ | ✓ | ✓ | build it | partial |
| Vendor-neutral | ✓ | ✓ | ✓ | ✓ | **✗** |
| Operational burden | **Lowest** | +1 stateful system | Highest | Deceptively high | Lock-in |

**The transactional-enqueue row is the decisive one, and it is worth being explicit about why.**

Any queue whose storage is *not* our database creates the **dual-write problem**: update the database
and publish to the broker, either of which can fail independently. The standard remedy is the
**transactional outbox** — write the event into an outbox table in the same transaction, then relay it
— and for high throughput the relay is replaced by CDC (Debezium reading the Postgres WAL) `[B-16]`.
That is genuinely mandatory infrastructure *if you have a broker*.

River makes it unnecessary. It is built specifically around Postgres, Go and pgx, and supports
inserting a job inside an existing transaction `[A-17]`:

```go
tx, _ := pool.Begin(ctx)
defer tx.Rollback(ctx)

posting, err := q.UpsertPosting(ctx, tx, args)
_, err = riverClient.InsertTx(ctx, tx, ScorePostingArgs{PostingID: posting.ID}, nil)

return tx.Commit(ctx)   // both, or neither
```

No outbox table. No relay process. No CDC pipeline. No saga compensation. The problem is not
mitigated — it is **structurally absent**.

**Temporal** is rejected as wildly disproportionate: it solves long-running, multi-step,
compensating workflows across services. We have none of those, precisely because we have no
distributed transactions ([ADR-0008](0008-service-decomposition.md)).

**Hand-rolled** is rejected because the interesting parts — visibility timeouts, `SKIP LOCKED`
contention, backoff, poison-message handling, graceful shutdown — are exactly where a naive
implementation is subtly wrong under load, and River has already solved them.

## Decision

**River**, with these queues:

| Queue | Concurrency | Priority | Contents |
|---|---:|---|---|
| `ingest` | 20 | normal | `fetch_source`, `normalise_batch` |
| `embed` | 10 | normal | `embed_posting`, `embed_resume` |
| `score` | 30 | normal | `score_for_watchers`, `rescore_user` |
| `score_bulk` | 5 | **low** | `rescore_all` after a config change |
| `notify` | 5 | high | digests, instant alerts |
| `maintenance` | 2 | low | ghost sweep, partitions, retention |

Separate queues with separate concurrency limits are the mechanism that stops a 20-million-row
`rescore_all` from starving live scoring — a low-priority queue with 5 workers cannot crowd out the
30 on `score`.

**Conventions:**

- Every job is **idempotent**. Workers are killed by autoscalers and rolling deploys as a matter of
  course.
- Every job carries a **unique key** where duplicate enqueue is possible, so it collapses.
- **Job args are versioned and evolved additively.** A worker must handle args enqueued by the
  *previous* release — expand/contract applied to the queue, and the thing that makes rolling deploys
  safe ([deployment-zdt.md §4](../../operations/deployment-zdt.md#4-queue-compatibility)).
- Args carry no PII. `{UserID: 42}`, never `{Email: "..."}` — job tables are less protected than the
  encrypted columns.

## Consequences

### Good

- Atomic enqueue removes an entire class of "the event was lost" bugs, permanently.
- Zero new infrastructure ([ADR-0003](0003-postgres-single-datastore.md) holds).
- Queue depth is a **SQL query**, which makes KEDA autoscaling trivial and makes debugging a
  `SELECT` instead of a CLI tool.
- Generics give compile-time-checked args instead of JSON blobs.
- Jobs are visible in the same backup and PITR window as the data they refer to.

### Bad, and accepted

- **Queue throughput is bounded by Postgres.** River batches selects/updates and uses `COPY FROM` for
  bulk insert, and our peak is ~10k jobs/day — roughly four orders of magnitude below where this
  binds.
- **Queue load competes with query load** on the same database. Bounded by per-worker connection
  limits ([service-topology §5](../service-topology.md#5-resource-and-connection-budgets)) and
  monitored via lock waits.
- **Job tables need vacuum attention.** Completed rows are pruned on a retention schedule; this is a
  named runbook item, not a surprise.
- **Smaller community than Sidekiq/Celery.** Accepted; the project is actively maintained and the
  design is legible enough to debug from the SQL if needed.

## Revisit

If sustained job throughput exceeds ~1,000/s, or if an external system needs to consume our events as
a stream. Note that the second case **reintroduces the outbox pattern**, and any such proposal must
carry that cost explicitly.
