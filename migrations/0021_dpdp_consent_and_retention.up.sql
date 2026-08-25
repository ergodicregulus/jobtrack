-- Consent records and retention, for India's DPDP Act.
--
-- The Rules were notified 14 November 2025. The Data Protection Board can
-- inquire and levy penalties from 13 November 2026, and the full notice,
-- consent, rights, retention and breach obligations apply from 13 May 2027.
-- See docs/engineering/phase-5-production-readiness.md §12.1.
--
-- We store CVs. That is personal data of the most sensitive employment kind,
-- and this product's own user is in India, so this is not a hypothetical
-- obligation deferred until there is a compliance team.

-- One row per (person, purpose, version). Append-only.
--
-- Append-only is the point: consent is a claim about a moment, and the
-- defensible answer to "did they agree, to what, and when" is a row written at
-- that moment — not a boolean somebody later flipped. Withdrawal is a second
-- row's timestamp, never a delete, because deleting the record of consent
-- destroys the evidence that it was obtained.
CREATE TABLE IF NOT EXISTS user_consents (
    id      bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- What was agreed to. Separate purposes are separately withdrawable, which
    -- is the whole reason this is not one flag: consenting to have a CV parsed
    -- is not consenting to a marketing email.
    purpose text NOT NULL CHECK (purpose IN (
        'account',        -- create and hold an account at all
        'resume_parsing', -- extract text and skills from an uploaded CV
        'matching',       -- score postings against the stored profile
        'digest_email'    -- future: opt-in summaries
    )),

    -- The version of the notice that was shown. A consent record that cannot
    -- name what the person actually read is not a record of anything.
    notice_version text NOT NULL,

    granted_at   timestamptz NOT NULL DEFAULT now(),
    withdrawn_at timestamptz,

    -- Evidence, kept deliberately thin. The IP is HASHED, not stored: it proves
    -- two consents came from the same place without retaining a network
    -- identifier we would then have to justify holding.
    ip_hash    bytea,
    user_agent text
);

CREATE INDEX IF NOT EXISTS user_consents_user_idx
    ON user_consents (user_id, purpose, granted_at DESC);

-- Retention, stated on the row rather than in a document nobody can query.
--
-- A retention policy that lives only in prose is a policy with no enforcement
-- and no evidence. These columns let the sweep be a query, and let a regulator's
-- question be answered by one.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS deletion_requested_at timestamptz;

ALTER TABLE resumes
    ADD COLUMN IF NOT EXISTS retain_until timestamptz;

-- Existing resumes get the standard window from their creation, so the policy
-- applies to what is already stored rather than only to what arrives next.
UPDATE resumes
   SET retain_until = created_at + interval '24 months'
 WHERE retain_until IS NULL;

COMMENT ON TABLE user_consents IS
  'Append-only consent records for DPDP. Withdrawal sets withdrawn_at; rows are never deleted.';
COMMENT ON COLUMN users.deletion_requested_at IS
  'Set by the erasure endpoint. The sweep completes it after the grace window.';
COMMENT ON COLUMN resumes.retain_until IS
  'Automated deletion boundary. 24 months from upload unless the person shortens it.';
