-- What the corpus knows, per source per day.
--
-- The homepage's signature graphic draws every live posting as a mark and
-- leaves a void where we could not read something. That needs four counts
-- beside the three source_daily already carries, and ADR-0017 decides where
-- they live: the query has no user_id in its WHERE clause, so it reads a
-- rollup. A `count(*) FILTER (...)` over every live posting on each render of
-- the most-served page in the product is not a slow query, it is a widget that
-- cannot ship — it gets worse for every reader as coverage grows.
--
-- Columns, not rows: the grain is unchanged, so this adds nothing to the
-- 75-rows-a-day the rollup already writes.
--
-- EVERY ONE IS REBUILDABLE by the same single statement that builds the rest —
-- ADR-0017's rule, and the reason there are still no poll counters here. Each
-- is a filtered count over job_postings on the day's live set, and the skills
-- one folds posting_skills through a CTE rather than a correlated subquery so
-- the statement stays one pass.
--
-- NAMED FOR WHAT WE KNOW, not for what the employer said. `known_mode` is true
-- when we could determine a work mode, which is often our own parse of the
-- prose rather than a field the employer filled in. Calling it `stated_mode`
-- would credit the employer with a disclosure they did not make, and this is
-- the one product where that distinction is the whole pitch.
ALTER TABLE source_daily
    ADD COLUMN IF NOT EXISTS known_comp   integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS known_yoe    integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS known_mode   integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS known_skills integer NOT NULL DEFAULT 0;

-- Defaults make this backward-compatible with the previous release: the running
-- version writes the three original columns and Postgres fills these with 0,
-- which reads as "nothing known" until the hourly rebuild replaces them with the
-- truth. A zero here is wrong for at most one hour and never unsafe.
COMMENT ON COLUMN source_daily.known_comp IS
  'Live postings that day for which we hold a pay figure, structured or extracted.';
COMMENT ON COLUMN source_daily.known_skills IS
  'Live postings that day from which we extracted at least one skill. Zero means we read none — the ADR-0011 abstention path.';
