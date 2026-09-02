-- When a saved search last produced a digest.
--
-- On the search rather than the user, because the cadence is per search: a
-- reader with three saved searches gets three independent digests, and one that
-- has been quiet for a month must not suppress one that is busy.
--
-- Nullable with no default, and NULL means "never sent" — which is a real state
-- distinct from "sent long ago", and the one that decides whether a newly
-- created search is eligible immediately.
ALTER TABLE saved_searches
    ADD COLUMN IF NOT EXISTS last_digest_at timestamptz;

COMMENT ON COLUMN saved_searches.last_digest_at IS
  'When a digest was last sent for this search. NULL means never. See ADR-0019.';
