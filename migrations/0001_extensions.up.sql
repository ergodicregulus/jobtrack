-- Extensions the whole schema depends on.
--
-- pgvector    : embedding similarity (ADR-0003)
-- pg_trgm     : fuzzy title matching, used by dedup blocking
-- citext      : case-insensitive uniqueness without lower() on every query
-- pgcrypto    : gen_random_bytes for session IDs and DEK generation

CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
