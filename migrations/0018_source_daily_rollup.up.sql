-- Daily rollup of ingest activity per source.
--
-- The first instance of the pattern ADR-0017 sets: a global widget — one with no
-- user_id to filter by — reads a rollup, never the operational tables. The
-- homepage chart of jobs seen per source over time cannot be answered by today's
-- schema at all. `sources` keeps only scalars (last_polled_at, last_changed_at),
-- so there is no history, and `posting_observations` grows with every poll of
-- every board forever.
--
-- Bounded by entities x days rather than by traffic: 65 sources x 365 days is
-- 23,725 rows a year, and a thousand sources over a decade is 3.65M — still a
-- table you can scan.
--
-- EVERY COLUMN HERE IS DERIVABLE from job_postings by one statement, which is
-- ADR-0017's rule and not a stylistic preference: a rollup that cannot be
-- rebuilt has quietly become a system of record, and then a bug in it needs a
-- migration instead of a recompute.
--
-- That rule is why there are no poll counters. Charting polls, 304s and errors
-- per day would be genuinely useful — a source that is erroring is not a source
-- with no jobs — but nothing records a poll as an event, so those columns could
-- only ever be accumulated, never rebuilt. They need an operational record
-- first; that is a later expand, not a column added here on the promise of one.
CREATE TABLE IF NOT EXISTS source_daily (
    source_id bigint NOT NULL REFERENCES sources (id) ON DELETE CASCADE,
    day       date   NOT NULL,

    -- Postings first seen from this source on this day.
    postings_new integer NOT NULL DEFAULT 0,
    -- Postings from this source that stopped being live on this day.
    postings_closed integer NOT NULL DEFAULT 0,
    -- Postings from this source still live at the end of this day. The level,
    -- where the two columns above are the flow; a chart of the level is what
    -- answers "is this board growing or shrinking".
    postings_live integer NOT NULL DEFAULT 0,

    computed_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (source_id, day)
);

-- The chart's access pattern is a date range across every source. The primary
-- key leads with source_id and cannot serve it.
CREATE INDEX IF NOT EXISTS source_daily_day_idx ON source_daily (day);

COMMENT ON TABLE source_daily IS
  'Daily ingest rollup per source. Derived from job_postings, rebuildable in full. See ADR-0017.';
