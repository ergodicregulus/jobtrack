-- Remember how far a two-phase adapter got through a board.
--
-- SmartRecruiters serves the list and the bodies separately, and the body phase
-- is capped per poll so a first poll of a large board is not an attack. The cap
-- was implemented without anywhere to record progress, so every poll fetched
-- list positions 0..249 and no others: on 2026-08-19, 1,861 of 2,393 live
-- SmartRecruiters postings had no description at all, scored as abstentions,
-- and no number of further polls would have changed that.
--
-- Expand-only. The previous release never selects this column, and the default
-- means its INSERTs keep working — so both releases can run against this schema.
ALTER TABLE sources ADD COLUMN IF NOT EXISTS detail_cursor integer NOT NULL DEFAULT 0;

-- Existing SmartRecruiters rows start their sweep from the top. Vendors that
-- serve descriptions inline never read the column.
COMMENT ON COLUMN sources.detail_cursor IS
  'Index into the vendor board where the next detail fetch resumes; 0 for single-phase vendors.';
