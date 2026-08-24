-- The shared skill vocabulary. This is what makes `Postgres` and `PostgreSQL`
-- one thing on both sides of a match.

CREATE TABLE skills (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    canonical    citext   NOT NULL UNIQUE,
    display_name text     NOT NULL,
    category     text     NOT NULL,
    aliases      citext[] NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX skills_aliases_idx ON skills USING gin (aliases);

-- Adjacency is curated, never derived from embeddings. Embedding similarity
-- places "Java" and "JavaScript" close together, which is exactly the failure
-- a candidate would find unforgivable. See ADR-0006.
CREATE TABLE skill_adjacency (
    skill_id    bigint NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    related_id  bigint NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    -- Fraction of credit a match on related_id earns toward skill_id.
    credit      real   NOT NULL CHECK (credit > 0 AND credit <= 1),
    PRIMARY KEY (skill_id, related_id),
    CHECK (skill_id <> related_id)
);

CREATE TYPE skill_requirement AS ENUM ('must_have', 'nice_to_have', 'mentioned');

CREATE TABLE posting_skills (
    posting_id  bigint            NOT NULL REFERENCES job_postings (id) ON DELETE CASCADE,
    skill_id    bigint            NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    requirement skill_requirement NOT NULL,
    confidence  real              NOT NULL DEFAULT 1.0,
    PRIMARY KEY (posting_id, skill_id)
);

CREATE INDEX posting_skills_skill_idx ON posting_skills (skill_id, requirement);
