-- +migrate no-transaction
-- Index the expression the feed actually filters and sorts on.
--
-- `GET /v1/jobs` measured 287ms median and 623ms p95 against a 120ms budget.
-- The plan showed a sequential scan over job_postings discarding 9,541 of
-- 10,198 rows.
--
-- The cause is that freshness is `COALESCE(posted_at, first_seen_at)`, not
-- `posted_at`. That coalesce is deliberate and correct — a posting whose vendor
-- publishes no date must still be orderable, and falling back to when we first
-- saw it is the honest approximation — but an index on `posted_at` cannot serve
-- a predicate on an expression over two columns. `jp_live_recent_idx` therefore
-- sat unused on the hottest query in the product.
--
-- One index serves both roles: the `>= $1` filter and the default newest-first
-- ordering, including the `(sort_key, id)` keyset tuple the cursor compares.
--
-- CONCURRENTLY, and therefore outside a transaction, per deployment-zdt: a bare
-- CREATE INDEX takes an ACCESS EXCLUSIVE lock and stalls every read on the
-- table for the duration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS jp_freshness_idx
	ON job_postings (COALESCE(posted_at, first_seen_at) DESC, id DESC)
	WHERE status = 'live';

-- Scoring joins the feed on (posting_id, user_id). The primary key is
-- (user_id, posting_id), which cannot serve a lookup keyed on posting_id
-- first — the leading column is missing from the predicate, so the planner
-- falls back to a scan of the user's entire score set per feed page.
CREATE INDEX CONCURRENTLY IF NOT EXISTS ujs_posting_idx
	ON user_job_scores (posting_id, user_id);
