-- Saved searches: the product's memory.
--
-- Filter state is already the URL query string, so a saved search is a stored
-- query string with a name. No new filter model, no second copy of the parsing,
-- and replaying one is a redirect. That is the whole reason this table is four
-- interesting columns rather than a mirror of FeedFilter — a schema that
-- duplicated the filters would have to be migrated every time a filter is
-- added.
--
-- Bounded per user by a partial unique index on the name, and by the
-- application. This is the one table whose row count grows with user COUNT
-- rather than with corpus size, which is what ADR-0016 says a materialised
-- per-user thing must be.
CREATE TABLE IF NOT EXISTS saved_searches (
    id      bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- Auto-generated from the active facets and then editable. eBay's pattern:
    -- the generated name is right most of the time and removes a naming step
    -- from the moment someone is trying to save something, not name it.
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),

    -- The URL query string, exactly as the feed produced it. Stored whole
    -- rather than parsed: the feed is the only thing that knows how to read it,
    -- and a second parser would drift from the first.
    query text NOT NULL CHECK (length(query) <= 2000),

    -- One search per user may be the default, applied on landing at /jobs.
    -- Enforced by a partial unique index below rather than by application code.
    is_default boolean NOT NULL DEFAULT false,

    created_at timestamptz NOT NULL DEFAULT now(),
    last_run_at timestamptz,

    -- The high-water mark for "new since you last ran this".
    --
    -- posted_at, not a timestamp of when we looked: first_seen_at moves when WE
    -- fetched something, so after a backfill every posting would look new. This
    -- is the same mistake migration 0014 documents for "new since your last
    -- visit", and it is the same fix.
    last_seen_max_posted_at timestamptz,

    UNIQUE (user_id, name)
);

CREATE INDEX IF NOT EXISTS saved_searches_user_idx
    ON saved_searches (user_id, created_at DESC);

-- At most one default per user. A constraint, not a convention: two defaults
-- means the landing page picks one arbitrarily and the user cannot tell why.
CREATE UNIQUE INDEX IF NOT EXISTS saved_searches_one_default_idx
    ON saved_searches (user_id) WHERE is_default;

COMMENT ON TABLE saved_searches IS
  'A named URL query string per user. See docs/engineering/phase-5-production-readiness.md §6.5.';
