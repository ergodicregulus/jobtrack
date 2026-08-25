# Runbooks

> **Amended 2026-08-25.** The `matcher` service described below no longer
> exists. [ADR-0016](../architecture/adr/0016-scores-are-computed-not-materialised.md)
> computes scores on the read path, which removed the scoring fan-out and the
> `score` / `score_bulk` queues — the matcher's only work. There are now **five**
> deployment units: api, ingestor, scheduler, resume-parser, migrate. Everything
> else on this page still holds.

> Status: **DECIDED**. Each runbook is written to be followed at 3 a.m. by someone who did not write
> the code. Symptom first, diagnosis second, fix third.

**General rule: stabilise before you diagnose.** Stop the bleeding, then find out why. A postmortem
with a gap in the timeline is better than an outage with a complete one.

---

## R1 — A source adapter is failing

**Symptom:** `sources_disabled{vendor="X"}` rising, or alert *"Vendor down: >30% of X's sources
failing"*.

### Diagnose

```sql
-- Which sources, and what error?
SELECT vendor, consecutive_errors, disabled_until, last_polled_at, board_token
FROM sources
WHERE consecutive_errors > 0
ORDER BY consecutive_errors DESC LIMIT 20;

-- Recent failures with their reasons
SELECT kind, errors->-1->>'error' AS last_error, count(*)
FROM river_job
WHERE kind = 'fetch_source' AND state = 'retryable'
GROUP BY 1,2 ORDER BY 3 DESC;
```

| Pattern | Likely cause | Action |
|---|---|---|
| One source failing, others fine | Board removed or renamed | Verify manually; disable the source |
| All of one vendor, HTTP 4xx | **Vendor changed their API** | → *Vendor schema change*, below |
| All of one vendor, HTTP 429 | We are being rate limited | → *Rate limited*, below |
| All of one vendor, timeouts | Vendor outage | Wait. The circuit breaker is doing its job |
| All vendors | **Our network or DNS** | Check egress, DNS, node health |

### Vendor schema change

1. Fetch the endpoint by hand and diff against the golden fixture:
   `curl -s "<endpoint>" | jq . > /tmp/new.json && diff <(jq . internal/source/X/testdata/board.json) /tmp/new.json`
2. If the shape changed: update the adapter, update the golden file, **read the diff carefully**, ship.
3. Meanwhile the circuit breaker has already isolated the vendor. Other vendors are unaffected — this
   is the design working, not an emergency.

### Rate limited

1. Confirm we are honouring `Retry-After` — check logs for `retry_after` values.
2. Reduce that vendor's tier-A interval temporarily:
   `UPDATE sources SET tier='b' WHERE vendor='X' AND tier='a';`
3. Check `source_poll_304_ratio{vendor="X"}`. **If it collapsed, conditional requests broke and we
   started sending full requests** — that is almost always the real cause, and it is the specific
   thing R2 covers.

### Re-enable after a fix

```sql
UPDATE sources SET consecutive_errors = 0, disabled_until = NULL, next_poll_at = now()
WHERE vendor = 'X';
```

---

## R2 — The 304 ratio collapsed

**Symptom:** `source_poll_304_ratio` dropped more than 30 points in an hour.

**Why this is urgent even though nothing is broken:** we are about to send full requests to every
source we track. Bandwidth rises ~10×, vendors start rate-limiting us, and freshness degrades as a
second-order effect. This alert exists to catch it *before* a vendor blocks us.

### Diagnose

1. **Did we deploy?** A change to how `ETag` / `If-Modified-Since` are stored or sent is the most
   common cause by a wide margin. Check the diff around `sources.etag` and the fetch code.
2. **Is it one vendor or all?** One vendor → they stopped returning validators. All → it is us.
3. **Are validators still stored?**
   ```sql
   SELECT vendor, count(*) FILTER (WHERE etag IS NOT NULL) AS with_etag, count(*)
   FROM sources GROUP BY 1;
   ```

### Fix

- Ours: roll back or patch. Conditional requests are load-bearing for the whole polling schedule
  ([ingestion-pipeline §2](../architecture/ingestion-pipeline.md#2-adaptive-scheduling)).
- Theirs: fall back to content-hash comparison (layer 2 of the change detection). Bandwidth still
  rises, but parsing and downstream work stay suppressed.

---

## R3 — Ingest freshness SLO breached

**Symptom:** `ingest_latency_seconds{tier="a"}` p50 > 90 min.

### Diagnose — in this order

```sql
-- 1. Is the queue backed up?
SELECT queue, state, count(*) FROM river_job GROUP BY 1,2;

-- 2. Is the scheduler enqueueing at all?
SELECT max(created_at) FROM river_job WHERE kind = 'fetch_source';

-- 3. Are sources due but not being picked up?
SELECT count(*) FROM sources WHERE next_poll_at < now() - interval '30 minutes';
```

| Finding | Cause | Fix |
|---|---|---|
| Queue deep, workers few | KEDA not scaling | Check the KEDA `ScaledObject` and its trigger query |
| Queue empty, nothing enqueued | **`scheduler` is down** | It is `replicas: 1` — check the pod. Jobs compute what is *due*, so a gap self-heals on restart |
| Queue deep, workers at max | Genuine capacity | Raise `maxReplicaCount`; check DB connection headroom first |
| Sources due, queue empty | Scheduler query bug, or `disabled_until` set en masse | Inspect the due-sources query |

### Mitigate immediately

```sql
-- Prioritise the sources users actually watch
UPDATE sources SET next_poll_at = now()
WHERE company_id IN (SELECT DISTINCT company_id FROM watched_companies);
```

---

## R4 — Scoring backlog

**Symptom:** `score_queue_depth` rising, alert at p95 > 30 min.

**User impact is bounded by design:** unscored postings still appear in the feed, marked `scoring…`
and ordered by recency ([P2](../product/principles.md#p2--freshness-is-the-product)). This is a
degradation, not an outage — do not treat it as a page-worthy emergency unless it persists.

### Diagnose

```sql
SELECT kind, state, count(*) FROM river_job
WHERE queue IN ('score','score_bulk','embed') GROUP BY 1,2;
```

| Finding | Cause | Fix |
|---|---|---|
| `score_bulk` huge | Someone triggered `rescore_all` | Expected. It is on a **low-priority queue with 5 workers** so it cannot starve live scoring. Let it drain |
| `score` huge, `score_bulk` empty | Fan-out too wide, or an ingestion spike | Check `postings_upserted_total`. Consider tightening the relevance predicate |
| `embed` huge | Embedding is the bottleneck | Scale `matcher`; check CPU limits |
| Workers at max, DB slow | Connection contention | Check `db_connections_in_use`; see R5 |

### Mitigate

Pause bulk work so live scoring recovers:

```sql
UPDATE river_job SET state = 'cancelled'
WHERE queue = 'score_bulk' AND state = 'available';
```

Safe: `rescore_all` is idempotent and re-runnable. Re-enqueue it once the backlog clears.

---

## R5 — Database connection exhaustion

**Symptom:** `db_connections_in_use` > 80% of pool; `FATAL: sorry, too many clients already`.

### Diagnose

```sql
SELECT application_name, state, count(*) FROM pg_stat_activity
GROUP BY 1,2 ORDER BY 3 DESC;

-- Long-running or idle-in-transaction sessions are the usual culprit
SELECT pid, application_name, state, now()-xact_start AS xact_age, left(query,80)
FROM pg_stat_activity
WHERE state <> 'idle' AND now()-xact_start > interval '30 seconds'
ORDER BY xact_age DESC;
```

| Finding | Cause | Fix |
|---|---|---|
| Many `idle in transaction` | A transaction held across a network call — **a coding-standards violation** | Terminate them; find and fix the caller |
| One service dominating | It scaled past its budget | Check replica count against [service-topology §5](../architecture/service-topology.md#5-resource-and-connection-budgets) |
| Evenly spread, all high | Genuine growth | **This is bottleneck #1 — deploy PgBouncer.** Expected at ~100k MAU, not an emergency |

### Emergency

```sql
-- Reclaim connections from stuck transactions. Workers retry; jobs are idempotent.
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE state = 'idle in transaction' AND now()-xact_start > interval '5 minutes';
```

Scaling workers **down** frees connections and is often the fastest stabilisation — the queue is
durable, so nothing is lost.

---

## R6 — Partition maintenance fell behind

**Symptom:** Inserts to `posting_observations` failing with *no partition of relation ... found*.

**Cause:** the scheduled partition-creation job did not run. It creates partitions one month ahead, so
this means it has been failing for over a month unnoticed — check why the alert did not fire.

### Fix

```sql
CREATE TABLE posting_observations_2026_09 PARTITION OF posting_observations
  FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
```

Then verify the job:

```sql
SELECT * FROM river_job WHERE kind = 'create_partitions'
ORDER BY created_at DESC LIMIT 5;
```

**Prevention:** the job creates **three** months ahead, not one, and alerts if the furthest existing
partition is less than 30 days out.

---

## R7 — Feed latency regression

**Symptom:** `GET /v1/jobs` p95 > 250 ms.

### Diagnose

```sql
-- Slowest statements
SELECT calls, mean_exec_time, left(query,120) FROM pg_stat_statements
ORDER BY mean_exec_time DESC LIMIT 10;
```

Then `EXPLAIN (ANALYZE, BUFFERS)` the feed query with realistic parameters.

| Finding | Cause | Fix |
|---|---|---|
| **Seq Scan on `job_postings`** | The `status='live'` predicate was dropped — every feed index is partial on it | Fix the query. There is a `sqlc vet` rule and an integration test asserting the plan; find out why both were bypassed |
| Index scan, many buffers | Bloat | `REINDEX CONCURRENTLY`; check autovacuum settings |
| Vector search slow | `ef_search` too high, or the index does not fit in RAM | Tune `ef_search`; check `shared_buffers` |
| Fast query, slow endpoint | The bottleneck is the app, not the DB | Check traces — this is bottleneck #5 |

---

## R8 — Bad deploy

**Symptom:** error rate or latency spikes immediately after a rollout.

### Act first

```bash
kubectl rollout undo deployment/api
```

Then diagnose. **Never debug a live regression in production while users are affected.**

| Situation | Rollback safety |
|---|---|
| Code only | ✅ Instant |
| After an **expand** migration | ✅ Safe by construction — the expanded schema still supports N |
| After a **contract** migration | ❌ **Requires a restore.** This is why contract steps ship alone, in their own release |
| Bad data written by N+1 | Roll back code, then repair with a migration. Never restore over live data |

If a feature flag is involved, turning it off is faster than any rollout — seconds rather than
minutes.

---

## R9 — Suspected data exposure

**SEV1. Escalate immediately; do not investigate alone.**

1. **Contain.** Revoke suspected credentials, invalidate all sessions if account compromise is
   plausible, block the source of access.
2. **Preserve evidence.** Snapshot logs and database state *before* remediating. This is routinely
   destroyed by well-meaning first responders.
3. **Assess scope.** Which users, which data, over what window.
4. **Notify.** Within 72 hours under GDPR; per DPDP timelines for Indian users. Legal review before
   any external communication.
5. **Remediate**, then a blameless postmortem with a written timeline and owned action items.

Note the mitigating structural fact: resume data is encrypted with per-user DEKs, so a database dump
alone does not expose resumes — the attacker also needs KMS access. State this accurately in any
assessment; do not overstate it either way.
Detail: [security-and-privacy §3](security-and-privacy.md#3-encryption).

---

## R10 — Postgres failover

**Symptom:** connection errors across all services; managed provider reports failover.

**Expected behaviour:** RPO ≤ 1 min, RTO ≤ 5 min. Services retry with backoff and recover
automatically.

### After recovery, verify in this order

1. All services reconnected — `db_connections_in_use` back to baseline.
2. **River jobs resumed.** In-flight jobs at failover are retried; they are idempotent, so this is
   safe.
3. **No duplicate postings** from interrupted ingestion:
   ```sql
   SELECT source_id, external_id, count(*) FROM job_postings
   GROUP BY 1,2 HAVING count(*) > 1;
   ```
   Should be empty — the unique constraint prevents it. If not, that is a real bug worth a postmortem.
4. Sequence continuity, and any partition created during the window.

---

## Escalation

| Severity | Definition | Response |
|---|---|---|
| **SEV1** | Data exposure, or total outage > 15 min | Page immediately; all hands |
| **SEV2** | Core feature broken (feed down, auth failing) | Page |
| **SEV3** | Degraded (stale feed, scoring behind) | Ticket, same business day |
| **SEV4** | Cosmetic or single-source failure | Backlog |

Every SEV1 and SEV2 gets a **blameless postmortem**: timeline, root cause, contributing factors, and
action items with named owners and dates. The output that matters is not the explanation — it is
whether the next occurrence is prevented or detected faster.
