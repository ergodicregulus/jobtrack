-- Scoring must work before a resume is ever uploaded.
--
-- Onboarding collects skills and years directly, which is what lets a user see
-- a personalised feed in their first session. Requiring a resume upload before
-- showing any value is the highest-friction possible first step, and the
-- original NOT NULL on resume_id encoded exactly that requirement.
--
-- Expand-only: dropping NOT NULL never breaks a reader.
ALTER TABLE user_job_scores ALTER COLUMN resume_id DROP NOT NULL;

COMMENT ON COLUMN user_job_scores.resume_id IS
    'The resume version this score used, or NULL when scored from the '
    'self-declared onboarding profile.';
