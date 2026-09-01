-- +migrate no-transaction
--
-- What kind of work a posting is.
--
-- No transaction: CREATE INDEX CONCURRENTLY cannot run inside one, and a bare
-- CREATE INDEX on a table this size takes a write lock for the length of the
-- build. See deployment-zdt.
--
-- Two-thirds of the live corpus is not software engineering, and one employer's
-- board is 31% of it — carrying HVAC sales in Bogotá and process engineering in
-- Stuttgart beside its platform roles. A product that calls itself an instrument
-- for software engineers has to say which postings it believes are which.
--
-- CLASSIFY AND EXPOSE, not filter at ingest. Nothing is discarded: every posting
-- an employer published is stored and reachable. The feed defaults to hiding
-- `other`, and the reader can turn that off. The rejected alternative was to
-- drop non-software postings during ingest, which is cheaper and fails silently
-- — a role the employer published, that the user can never find, and that we
-- keep no record of having rejected. See ADR-0018.
--
-- TEXT with a CHECK, not an enum. The value set will change as the classifier
-- learns the corpus, and ALTER TYPE ... ADD VALUE cannot run inside a
-- transaction — migration 0019 is what that costs. A constraint is the cheaper
-- thing to widen.
ALTER TABLE job_postings
    ADD COLUMN IF NOT EXISTS field text NOT NULL DEFAULT 'unknown'
        CHECK (field IN ('software', 'other', 'unknown')),
    -- 0..1. Low is a real answer and is stored rather than rounded away: it is
    -- what stops a weak guess being read as a strong one.
    ADD COLUMN IF NOT EXISTS field_confidence real NOT NULL DEFAULT 0,
    -- Why the classifier said what it said, in the reader's words. A label with
    -- no grounds is one a user can only distrust.
    ADD COLUMN IF NOT EXISTS field_because text NOT NULL DEFAULT '';

-- The feed's default predicate is "field <> 'other'", so this index serves the
-- most-served query in the product. Partial, because `other` is the excluded
-- minority and indexing it would be paying for rows the default never reads.
CREATE INDEX CONCURRENTLY IF NOT EXISTS job_postings_field_live_idx
    ON job_postings (field)
    WHERE status = 'live' AND field <> 'other';

COMMENT ON COLUMN job_postings.field IS
  'software | other | unknown. Classified by normalise.ClassifyField; unknown is a real answer. See ADR-0018.';
