-- The central table.

CREATE TYPE work_mode       AS ENUM ('onsite', 'hybrid', 'remote', 'unknown');
CREATE TYPE employment_type AS ENUM ('full_time', 'part_time', 'contract', 'intern', 'temporary', 'other');
CREATE TYPE posting_status  AS ENUM ('live', 'closed', 'superseded');
CREATE TYPE comp_source     AS ENUM ('structured', 'parsed_text');

CREATE TABLE job_postings (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_id  bigint NOT NULL REFERENCES sources (id) ON DELETE CASCADE,
    company_id bigint NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- The vendor's stable identifier where one exists, otherwise a derived
    -- hash. The derivation rule is documented per vendor in
    -- docs/research/source-catalog.md.
    external_id text NOT NULL,
    -- Greenhouse exposes requisition_id; two postings from one company sharing
    -- it are the same role, which lets dedup skip similarity computation
    -- entirely for that case.
    requisition_id text,

    title            text NOT NULL,
    title_normalised text NOT NULL,
    description_html text,
    description_text text,

    apply_url   text NOT NULL,
    posting_url text,

    -- Location is kept raw *and* structured: the structured form drives
    -- filters, the raw form is shown to the user because our parse is not
    -- always right and hiding that would be dishonest.
    location_raw text,
    country      char(2),
    region       text,
    city         text,
    mode         work_mode NOT NULL DEFAULT 'unknown',

    employment employment_type NOT NULL DEFAULT 'full_time',

    -- NULL means "not disclosed" and is distinct from zero. The filter must be
    -- able to tell those apart; collapsing them loses ~20% of the market.
    comp_min      numeric(14, 2),
    comp_max      numeric(14, 2),
    comp_currency char(3),
    comp_period   text,
    comp_src      comp_source,

    yoe_min smallint,
    yoe_max smallint,
    -- 0..1. Filters treat a low-confidence value as unknown rather than
    -- excluding the posting: our parse failure must never cost the user a role.
    yoe_confidence real,

    -- posted_at prefers the vendor's first-published timestamp over its
    -- updated_at, because updated_at moves on any edit and would make an
    -- edited 40-day-old posting look fresh.
    posted_at     timestamptz,
    posted_at_is_estimate boolean NOT NULL DEFAULT false,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    closed_at     timestamptz,
    status        posting_status NOT NULL DEFAULT 'live',
    -- Consecutive full polls in which this posting was absent. Closure needs
    -- two: a truncated vendor response would otherwise close an entire board.
    missing_count int NOT NULL DEFAULT 0,

    -- Greenhouse exposes whether the employer runs AI talent matching, and an
    -- opt-out URL. Surfaced to the user (feature-spec F10). NULL means
    -- "not disclosed", never "no AI".
    ai_screening_disclosed boolean,
    ai_disclaimer          text,
    ai_opt_out_url         text,

    -- Generated so the feed can filter on ghost-job integrity signals cheaply.
    has_disclosed_comp boolean GENERATED ALWAYS AS (comp_min IS NOT NULL) STORED,

    -- Points at the survivor when status = 'superseded'. Nothing is deleted:
    -- the user can see both originals, and a wrong merge is recoverable.
    canonical_id bigint REFERENCES job_postings (id) ON DELETE SET NULL,

    search_tsv tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(description_text, '')), 'B')
    ) STORED,

    parse_confidence real  NOT NULL DEFAULT 1.0,
    raw              jsonb,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (source_id, external_id)
);

-- Every index below is partial on status = 'live'. At steady state ~70% of rows
-- are closed, so these are ~3x smaller and stay resident in shared buffers.
-- The cost: a query that forgets the predicate silently loses the index, which
-- is why `sqlc vet` has a rule requiring it.
CREATE INDEX jp_live_recent_idx ON job_postings (posted_at DESC NULLS LAST, id DESC)
    WHERE status = 'live';
CREATE INDEX jp_geo_idx ON job_postings (country, region, mode)
    WHERE status = 'live';
CREATE INDEX jp_yoe_idx ON job_postings (yoe_min, yoe_max)
    WHERE status = 'live';
CREATE INDEX jp_comp_idx ON job_postings (comp_currency, comp_min)
    WHERE status = 'live' AND comp_min IS NOT NULL;
CREATE INDEX jp_company_idx ON job_postings (company_id, posted_at DESC)
    WHERE status = 'live';
CREATE INDEX jp_source_idx ON job_postings (source_id) WHERE status = 'live';
CREATE INDEX jp_fts_idx ON job_postings USING gin (search_tsv);
CREATE INDEX jp_title_trgm_idx ON job_postings USING gin (title_normalised gin_trgm_ops);
CREATE INDEX jp_requisition_idx ON job_postings (company_id, requisition_id)
    WHERE requisition_id IS NOT NULL AND status = 'live';

-- Append-only observation log. "Is this posting still open?" is answered by
-- observed feed behaviour, not by guessing employer intent.
-- Partitioned monthly so retention is a DROP TABLE rather than a DELETE that
-- would bloat the heap and stall autovacuum.
CREATE TABLE posting_observations (
    posting_id   bigint      NOT NULL,
    observed_at  timestamptz NOT NULL DEFAULT now(),
    content_hash bytea       NOT NULL,
    PRIMARY KEY (posting_id, observed_at)
) PARTITION BY RANGE (observed_at);

-- A default partition guarantees inserts never fail because maintenance fell
-- behind. Rows landing here are a signal, not a loss (runbook R6).
CREATE TABLE posting_observations_default PARTITION OF posting_observations DEFAULT;

CREATE TABLE posting_embeddings (
    posting_id bigint PRIMARY KEY REFERENCES job_postings (id) ON DELETE CASCADE,
    -- 384 dimensions: at 200k postings the HNSW index fits comfortably in RAM
    -- and recall for this task is indistinguishable from 768/1536.
    embedding  vector(384) NOT NULL,
    model      text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
