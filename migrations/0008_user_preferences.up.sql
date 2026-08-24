-- UI preferences (theme, density, and whatever comes next).
--
-- jsonb rather than columns, deliberately. The test is whether a field is
-- QUERIED, not whether it is important:
--
--   * Search preferences (pref_countries, pref_modes, total_yoe) are columns,
--     because the scoring fan-out filters on them on every posting.
--   * UI preferences are never in a WHERE clause. They are read once, with the
--     user row, on every page render. A column per setting would mean a
--     migration every time the design adds a toggle — expand/contract, backfill,
--     contract — for a value nothing filters on.
--
-- The cost of jsonb is that the database will not enforce the shape, so the
-- application validates on write and tolerates unknown keys on read. That is the
-- right trade here: an unknown key from a newer release must not break an older
-- one during a rolling deploy.
--
-- Storage note: this stays small (< 200 bytes), so it lives inline rather than
-- being TOASTed, and adds nothing measurable to the user row fetch.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS preferences jsonb NOT NULL DEFAULT '{}'::jsonb;

-- A constant default does not rewrite the table in PostgreSQL 11+, so this is
-- safe on a populated users table and needs no backfill.

COMMENT ON COLUMN users.preferences IS
    'Non-queryable UI preferences: theme (light|dark|system), density, etc. '
    'Validated by the application on write; unknown keys are preserved so a '
    'rolling deploy of two releases cannot lose settings.';
