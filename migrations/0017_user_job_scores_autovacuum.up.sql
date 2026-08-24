-- Vacuum user_job_scores on its own churn rate, not on the default.
--
-- Every rescore rewrites every row a user has, so this table turns over far
-- faster than the defaults assume. At 1.2M live rows the default
-- autovacuum_vacuum_scale_factor of 0.2 lets 240,000 dead tuples accumulate
-- before a vacuum starts, and the dashboard's per-user count measured
-- 1,598 ms at 283,138 dead tuples against 285 ms immediately after a vacuum —
-- a 5.6x swing driven entirely by how long ago the table was cleaned.
--
-- 0.02 keeps the dead fraction near 2%. The cost is more frequent vacuums on
-- one table, which is the trade this table's access pattern justifies: it is
-- written by background workers and read on the first page every signed-in
-- user loads.
--
-- Cheap and non-blocking: this is a catalogue update, not a rewrite.
ALTER TABLE user_job_scores SET (
    autovacuum_vacuum_scale_factor  = 0.02,
    autovacuum_analyze_scale_factor = 0.02
);
