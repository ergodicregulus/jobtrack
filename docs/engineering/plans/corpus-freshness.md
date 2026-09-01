# Corpus freshness

**Status:** not started. **Done when:** `closed_at IS NOT NULL` is non-zero and
tracks board removals within two polls, and the share of live postings older than
60 days is measured, explained, and either reduced or documented as real.

## The problem, measured

```sql
SELECT count(*) FILTER (WHERE closed_at IS NOT NULL)          AS ever_closed,
       count(*) FILTER (WHERE status='live' AND missing_count>0) AS live_but_absent,
       max(missing_count)                                     AS max_missing
  FROM job_postings;
--  0 | 2020 | 29
```

**No posting has ever been closed.** 2,020 are currently missing from their
board, one of them for 29 consecutive polls, and every one still reads as live.
42% of the corpus is over 60 days old and 14% is over 180 — which is the same
fact seen from the other end.

This is the ghost-job problem the product exists to reduce, and we have it
worse than the boards we read from, because they at least take postings down.

## Root cause

`store.ReconcileAbsent` (`internal/store/postings.go`) bumps the counter in a
data-modifying CTE and then updates the same rows again in the outer statement:

```sql
WITH bumped AS (
    UPDATE job_postings SET missing_count = missing_count + 1
     WHERE ... RETURNING id, missing_count
)
UPDATE job_postings p SET status='closed', closed_at=now()
  FROM bumped b WHERE p.id = b.id AND b.missing_count >= 2
```

PostgreSQL does not apply a second update to a row already modified by the same
command — "only one of the modifications takes place, and it is not reliably
possible to predict which one". The outer UPDATE reports rows affected and
changes nothing. `ReconcileAbsent` has been returning a closure count it did not
perform since it was written.

## Steps

1. **[opus]** Collapse it to one UPDATE per row, so no row is written twice:
   `missing_count = missing_count + 1`, with `status` and `closed_at` set by a
   `CASE` on `missing_count + 1 >= 2`. One statement, one write, no CTE.
2. **[opus]** Decide the threshold on evidence rather than keeping 2 because it
   is there. Two consecutive absences at a 2-hour tier is 4 hours; a board that
   paginates unstably could drop a posting from one page and restore it on the
   next. Check whether that happens before choosing.
3. **[sonnet]** Integration test in `internal/store`: insert a live posting,
   reconcile twice without it, assert `status='closed'` and `closed_at` set;
   reconcile once and assert it is still live. This is the test whose absence
   let a no-op ship.
4. **[opus]** Backfill. 2,020 postings are wrongly live, some for 29 polls.
   A data migration, not a schema one — expand/contract does not apply, and the
   rule that a data migration must be resumable does.
5. **[sonnet]** After the backfill settles, re-measure the age distribution and
   record it in the evidence ledger. If 42%-over-60-days survives the fix, it is
   a real property of these boards and the product should say so rather than
   quietly carrying it.

## Traps

- **`RowsAffected` lied for the whole life of this function.** Any new closure
  path must be verified by reading rows back, not by trusting the tag.
- A posting can legitimately vanish and return — a board reordering under
  pagination, or a vendor 500 mid-walk. The `ErrSuspiciousEmpty` guard covers a
  whole board disappearing; it does not cover one posting flickering.
- Closing is not deleting. `closed_at` is what the freshness signal and the
  `source_daily` rollup both read, and [ADR-0016](../../architecture/adr/0016-scores-are-computed-not-materialised.md)'s
  liveness test depends on it.
