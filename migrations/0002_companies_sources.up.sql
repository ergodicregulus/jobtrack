-- Companies and the source feeds they publish through.
--
-- A company may publish via several sources at once (a Greenhouse board *and*
-- JSON-LD on its careers page). Deduplication collapses the resulting duplicate
-- postings; see docs/architecture/ingestion-pipeline.md §5.

CREATE TABLE companies (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug           citext      NOT NULL UNIQUE,
    name           text        NOT NULL,
    website        text,
    -- Registrable domain. Used in v2 to link inbound recruiter email to a
    -- company without display-name matching, which is hopeless across locales.
    primary_domain citext,
    logo_url       text,
    hq_country     char(2),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX companies_primary_domain_key
    ON companies (primary_domain) WHERE primary_domain IS NOT NULL;

CREATE TYPE source_vendor AS ENUM (
    'greenhouse', 'lever', 'ashby', 'smartrecruiters',
    'recruitee', 'workable', 'personio', 'jsonld'
);

CREATE TYPE source_tier AS ENUM ('a', 'b', 'c', 'paused');

CREATE TABLE sources (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id bigint        NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    vendor     source_vendor NOT NULL,
    -- Vendor-specific board identifier: a Greenhouse board token, a Lever site
    -- slug, an Ashby job-board name, or a full URL for jsonld.
    board_token text         NOT NULL,
    tier       source_tier   NOT NULL DEFAULT 'c',

    -- Conditional-request state. Both validators are stored deliberately:
    -- vendors are inconsistent about which they honour, and a wrongly-assumed
    -- 304 costs freshness (the product) while an unnecessary 200 costs only
    -- bandwidth.
    etag          text,
    last_modified text,
    content_hash  bytea,

    last_polled_at     timestamptz,
    last_changed_at    timestamptz,
    next_poll_at       timestamptz NOT NULL DEFAULT now(),
    consecutive_errors int         NOT NULL DEFAULT 0,
    -- Circuit breaker. Set when consecutive_errors crosses the threshold;
    -- cleared by a successful poll or by an operator (runbook R1).
    disabled_until timestamptz,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (vendor, board_token)
);

-- The scheduler's only hot query: "which sources are due?"
CREATE INDEX sources_due_idx ON sources (next_poll_at)
    WHERE tier <> 'paused' AND disabled_until IS NULL;

CREATE INDEX sources_company_idx ON sources (company_id);
