-- Application tracking with channel attribution.

CREATE TYPE application_status AS ENUM (
    'saved', 'applied', 'referred', 'recruiter_screen', 'hm_screen',
    'onsite', 'offer', 'rejected', 'ghosted', 'withdrawn'
);

CREATE TYPE application_channel AS ENUM (
    'careers_page', 'job_board', 'referral', 'cold_email', 'recruiter_inbound', 'other'
);

CREATE TABLE applications (
    id      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- Nullable on purpose: users track roles we never ingested. A tracker
    -- limited to our own inventory loses to a spreadsheet on day one.
    -- Note there is no ON DELETE CASCADE here — a user's history must survive
    -- a posting being purged.
    posting_id bigint REFERENCES job_postings (id) ON DELETE SET NULL,
    company_id bigint REFERENCES companies (id) ON DELETE SET NULL,

    -- Free-text fallbacks for manually-added applications.
    company_name text,
    role_title   text,

    status    application_status  NOT NULL DEFAULT 'saved',
    channel   application_channel,
    resume_id bigint REFERENCES resumes (id) ON DELETE SET NULL,

    applied_at       timestamptz,
    last_activity_at timestamptz NOT NULL DEFAULT now(),
    next_action      text,
    next_action_at   date,

    ats_vendor   source_vendor,
    apply_url    text,
    contact_name text,
    contact_note text,
    why_applied  text,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- Channel and resume version are the entire basis of channel analytics.
    -- Enforced in the database rather than only in the handler because if they
    -- can be NULL, they will be.
    CONSTRAINT applied_requires_attribution CHECK (
        status IN ('saved', 'withdrawn')
        OR (channel IS NOT NULL AND resume_id IS NOT NULL AND applied_at IS NOT NULL)
    ),
    -- Either a tracked posting or a hand-entered company name.
    CONSTRAINT has_subject CHECK (posting_id IS NOT NULL OR company_name IS NOT NULL)
);

CREATE INDEX app_user_idx ON applications (user_id, status, last_activity_at DESC);

-- Drives the 21-day ghosting sweep.
CREATE INDEX app_stale_idx ON applications (last_activity_at)
    WHERE status IN ('applied', 'recruiter_screen', 'hm_screen');

CREATE INDEX app_next_action_idx ON applications (user_id, next_action_at)
    WHERE next_action_at IS NOT NULL;

-- Append-only. Never updated, never deleted. This is what makes the automatic
-- 21-day ghost transition reversible: the sweep writes an event, a later reply
-- writes another, and no state is lost.
CREATE TABLE application_events (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id bigint             NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    from_status    application_status,
    to_status      application_status NOT NULL,
    -- 'user' | 'system:ghost_sweep' | 'system:email_classifier'
    actor       text        NOT NULL,
    note        text,
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX app_events_app_idx ON application_events (application_id, occurred_at DESC);

CREATE TABLE watchlists (
    id      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    label   text   NOT NULL,
    -- The saved filter set in the same shape the API accepts, so a new filter
    -- needs no migration.
    filters jsonb  NOT NULL,
    notify_mode text NOT NULL DEFAULT 'digest_daily'
        CHECK (notify_mode IN ('instant', 'digest_daily', 'digest_weekly')),
    last_notified_at timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX watchlists_user_idx ON watchlists (user_id);

-- Watching a company promotes its sources to ingestion tier A. User attention
-- directly drives crawl priority, which is both efficient and the correct
-- incentive.
CREATE TABLE watched_companies (
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    company_id bigint      NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, company_id)
);

CREATE INDEX watched_companies_company_idx ON watched_companies (company_id);
