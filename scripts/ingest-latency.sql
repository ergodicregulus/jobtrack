-- Ingest latency: how long between an employer posting a role and it being
-- visible here. Budget: 90 min median, tier A. phase-5 §10.
--
-- This is first_seen_at − posted_at: the employer's own stated posting time to
-- the first poll in which we saw it. It therefore includes their publishing
-- delay as well as our polling interval, which is right — it is the gap a USER
-- experiences. It is not a measure of the scheduler alone.
--
-- THREE EXCLUSIONS, each of which changes the answer by orders of magnitude:
--
--   1. Postings that predate our first poll of their source. This one matters
--      most and is easy to miss: when a source is added we ingest its whole
--      board, including roles posted two years ago, and every one of them
--      records a first_seen_at of today. Left in, they are not slow ingestion,
--      they are backfill, and they dominate — the naive query returns a
--      SmartRecruiters median of 72 DAYS from rows that were fetched within
--      minutes of the source being added. Only a posting published after we
--      were already watching its board can measure how fast we noticed it.
--
--   2. Sources whose posted_at has no time of day. All three current vendors
--      carry one. Workday's startDate is a DATE, so its latency would round to
--      whole days; it will be excluded here rather than reported as a figure
--      that looks precise and is not.
--
--   3. Negative gaps — the employer's clock ahead of ours, or a stated future
--      start. Neither is our latency.
--
--   4. Postings whose wait spans an ingest OUTAGE. This exists because the
--      first honest run of this probe returned a tier A median of 26 hours and
--      the cause was not the scheduler: the ingest history has a 4-day-19-hour
--      hole in it, and a 2-day one, because this corpus is built on a laptop
--      that sleeps. Downtime reported as latency is worse than no measurement,
--      because it looks like a product defect and would send someone tuning a
--      scheduler that is behaving correctly. A posting only counts if we were
--      demonstrably polling for the whole of its wait.
--
--      In production this exclusion should discard almost nothing, and the
--      count it prints is itself the signal: if a large share of postings are
--      being dropped for spanning an outage, the availability problem is the
--      finding and the latency figure is beside the point.
--
-- What survives is a small sample by design. A number computed from 2,000 rows
-- that measure the thing beats one computed from 17,000 that do not.
WITH watch AS (
    -- When we first polled each source at all. Anything on the board before
    -- this instant was backfilled, not observed arriving.
    SELECT source_id, min(first_seen_at) AS started
      FROM job_postings
     GROUP BY source_id
),
-- An hour in which at least one posting arrived from any source. With tier A at
-- 2h, a gap of more than 3h across EVERY source is the worker being down, not a
-- quiet board.
ingest_hours AS (
    SELECT DISTINCT date_trunc('hour', first_seen_at) AS hr FROM job_postings
),
outages AS (
    SELECT prev AS starts, hr AS ends
      FROM (SELECT hr, lag(hr) OVER (ORDER BY hr) AS prev FROM ingest_hours) g
     WHERE hr - prev > interval '3 hours'
),
measured AS (
    SELECT s.vendor::text AS vendor,
           s.tier::text   AS tier,
           EXTRACT(EPOCH FROM (p.first_seen_at - p.posted_at)) / 60.0 AS minutes
      FROM job_postings p
      JOIN sources s ON s.id = p.source_id
      JOIN watch w   ON w.source_id = p.source_id
     WHERE p.posted_at IS NOT NULL
       AND p.posted_at > w.started                              -- exclusion 1
       AND date_trunc('day', p.posted_at) <> p.posted_at        -- exclusion 2
       AND p.first_seen_at >= p.posted_at                       -- exclusion 3
       AND NOT EXISTS (                                          -- exclusion 4
             SELECT 1 FROM outages o
              WHERE o.starts < p.first_seen_at AND o.ends > p.posted_at)
)
SELECT tier,
       vendor,
       count(*) AS postings,
       round(percentile_cont(0.50) WITHIN GROUP (ORDER BY minutes)::numeric, 1) AS p50_min,
       round(percentile_cont(0.90) WITHIN GROUP (ORDER BY minutes)::numeric, 1) AS p90_min,
       round(percentile_cont(0.99) WITHIN GROUP (ORDER BY minutes)::numeric, 1) AS p99_min,
       CASE
         WHEN count(*) < 100 THEN 'too few rows to call'
         WHEN tier <> 'a'    THEN '—'
         WHEN percentile_cont(0.50) WITHIN GROUP (ORDER BY minutes) > 90
           THEN 'OVER BUDGET (tier A median > 90 min)'
         ELSE 'within budget'
       END AS verdict
  FROM measured
 GROUP BY tier, vendor
 ORDER BY tier, vendor;
