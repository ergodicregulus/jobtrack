-- Track when a visit began, so "new since your last visit" can be true.
--
-- `last_active_at` existed and nothing ever wrote to it, so any feature built on
-- it would have compared against whatever the seed happened to set.
--
-- Two columns are needed, not one. A single "last seen" timestamp updated as the
-- user browses is always ~now, so nothing is ever new — the marker would erase
-- itself the moment it was read. `previous_visit_at` holds the value from
-- BEFORE the current visit started, which is the timestamp a person means when
-- they say "since I was last here".
--
-- A visit boundary is a gap of more than 30 minutes. Long enough that scrolling
-- the feed, opening a posting and coming back is one visit; short enough that a
-- lunchtime look and an evening look are two.
ALTER TABLE users ADD COLUMN IF NOT EXISTS previous_visit_at timestamptz;

-- Backfill so existing accounts do not see their entire history flagged as new
-- on the first load after this deploys. Their next visit sets it properly.
UPDATE users SET previous_visit_at = COALESCE(last_active_at, now())
 WHERE previous_visit_at IS NULL;
