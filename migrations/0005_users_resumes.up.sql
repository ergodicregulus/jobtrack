-- Users, sessions, resumes and precomputed scores.

CREATE TABLE users (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email             citext NOT NULL UNIQUE,
    -- NULL for OAuth-only accounts.
    password_hash     text,
    email_verified_at timestamptz,

    -- Search preferences are denormalised onto the user because every feed
    -- query reads them and they are small.
    pref_countries char(2)[]      NOT NULL DEFAULT '{}',
    pref_modes     work_mode[]    NOT NULL DEFAULT '{}',
    pref_comp_min  numeric(14, 2),
    pref_currency  char(3),
    total_yoe      real,

    -- Per-user data key, wrapped by the KMS key. Resume PII is encrypted with
    -- this, so a table dump is not a resume dump — and destroying this key
    -- makes deletion meaningful even inside backups we cannot edit.
    dek_wrapped bytea,

    -- Drives the active-user bound on score fan-out. Scoring dormant users is
    -- the difference between ~4 and ~12 sustained cores at year-2 volume.
    last_active_at timestamptz NOT NULL DEFAULT now(),

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE INDEX users_active_idx ON users (last_active_at) WHERE deleted_at IS NULL;

-- Server-side sessions rather than stateless JWTs, because revocation must be
-- immediate: a user signing out on a shared machine cannot be told to wait for
-- a token to expire.
CREATE TABLE sessions (
    -- SHA-256 of the token. The plaintext token exists only in the cookie, so
    -- a database dump cannot be replayed as a session.
    id         bytea PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    user_agent text,
    -- Hashed, for anomaly detection only. We never store a raw IP.
    ip_hash    bytea
);

CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expiry_idx ON sessions (expires_at);

CREATE TABLE resumes (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint  NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    label      text    NOT NULL,
    is_default boolean NOT NULL DEFAULT false,

    blob_key  text NOT NULL,
    mime_type text NOT NULL,
    byte_size int  NOT NULL CHECK (byte_size > 0),

    -- Encrypted with the user's DEK.
    parsed_text_enc bytea,
    parsed_json_enc bytea,

    -- Fraction of expected sections successfully extracted. Surfaced to the
    -- user as the parse diagnostic and used to gate scoring confidence.
    parse_confidence real,
    parser_version   text,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX resumes_one_default_per_user ON resumes (user_id) WHERE is_default;
CREATE INDEX resumes_user_idx ON resumes (user_id);

CREATE TABLE resume_skills (
    resume_id   bigint  NOT NULL REFERENCES resumes (id) ON DELETE CASCADE,
    skill_id    bigint  NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    years       real,
    -- User edits always win over re-parsing. Without this flag a parser upgrade
    -- would silently discard corrections the user made.
    user_edited boolean NOT NULL DEFAULT false,
    PRIMARY KEY (resume_id, skill_id)
);

CREATE TABLE resume_embeddings (
    resume_id  bigint PRIMARY KEY REFERENCES resumes (id) ON DELETE CASCADE,
    embedding  vector(384) NOT NULL,
    model      text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Precomputed. The feed reads this; it never scores at request time.
CREATE TABLE user_job_scores (
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    posting_id bigint NOT NULL REFERENCES job_postings (id) ON DELETE CASCADE,
    resume_id  bigint NOT NULL REFERENCES resumes (id) ON DELETE CASCADE,

    score real NOT NULL CHECK (score >= 0 AND score <= 100),
    band  text NOT NULL CHECK (band IN ('strong', 'plausible', 'stretch', 'unlikely')),
    -- Per-component contributions, rendered directly in the UI as the
    -- explanation. jsonb rather than columns because the scoring profile is
    -- configurable and components can be added without a migration; the shape
    -- is pinned by profile_version.
    components jsonb NOT NULL,
    confidence real  NOT NULL CHECK (confidence >= 0 AND confidence <= 1),

    profile_version text        NOT NULL,
    computed_at     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, posting_id)
);

CREATE INDEX ujs_feed_idx ON user_job_scores (user_id, score DESC, posting_id DESC);
