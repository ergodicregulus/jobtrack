-- Profile fields for onboarding, plus the saved-job list.
--
-- Onboarding exists because scoring is worthless without knowing who the user
-- is. Asking for a resume up front is the highest-friction possible first step;
-- asking for four fields is not. See docs/product/onboarding.md.

-- Names are columns, not preferences jsonb: they are displayed on every page
-- and will be queried (referral graph, v3). The jsonb rule is "is it queried?",
-- not "is it important?".
ALTER TABLE users ADD COLUMN IF NOT EXISTS first_name text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_name  text;

-- Current role and target help the dashboard say something specific rather
-- than generic. Nullable: onboarding must be skippable.
ALTER TABLE users ADD COLUMN IF NOT EXISTS current_title text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS target_title  text;

-- Marks onboarding complete. A separate timestamp rather than inferring from
-- "are the fields populated?" — a user who deliberately skipped every optional
-- field has still completed onboarding, and must not be asked again.
ALTER TABLE users ADD COLUMN IF NOT EXISTS onboarded_at timestamptz;

-- Skills the user claims, independent of any resume. This is what makes
-- scoring possible before a resume is ever uploaded.
CREATE TABLE IF NOT EXISTS user_skills (
    user_id  bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    skill_id bigint NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    years    real,
    -- Where it came from. A resume-parsed skill can be overwritten by the user;
    -- a user-stated one is never overwritten by a parse.
    origin   text NOT NULL DEFAULT 'user' CHECK (origin IN ('user', 'resume')),
    PRIMARY KEY (user_id, skill_id)
);

CREATE INDEX IF NOT EXISTS user_skills_skill_idx ON user_skills (skill_id);

-- Saved jobs: the lightweight step before an application exists.
--
-- Separate from `applications` deliberately. Saving is a low-commitment act
-- with no attribution to record, and forcing it through the applications table
-- would mean either nullable channel/resume columns (weakening the constraint
-- that makes channel analytics trustworthy) or a fake 'saved' status carrying
-- no data.
CREATE TABLE IF NOT EXISTS saved_jobs (
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    posting_id bigint      NOT NULL REFERENCES job_postings (id) ON DELETE CASCADE,
    note       text,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, posting_id)
);

CREATE INDEX IF NOT EXISTS saved_jobs_user_idx ON saved_jobs (user_id, created_at DESC);
