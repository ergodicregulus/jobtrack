-- +migrate no-transaction
--
-- CREATE INDEX CONCURRENTLY cannot run inside a transaction block, so this file
-- carries the no-transaction directive. The migrator marks the ledger row dirty
-- before running and clears it after, because the DDL and the ledger row cannot
-- commit atomically here — if the process dies mid-build, the next run refuses
-- to proceed rather than silently skipping or re-running a half-built index.
--
-- Indexes created after a table is populated must ALWAYS use CONCURRENTLY.
-- A bare CREATE INDEX takes ACCESS EXCLUSIVE for the whole build, which on
-- job_postings is a multi-minute total outage.
--
-- IF NOT EXISTS matters here too: a failed concurrent build leaves an INVALID
-- index behind, and this lets the retry succeed after it is dropped.

CREATE INDEX CONCURRENTLY IF NOT EXISTS jp_embedding_hnsw_idx
    ON posting_embeddings USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

CREATE INDEX CONCURRENTLY IF NOT EXISTS resume_embedding_hnsw_idx
    ON resume_embeddings USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

CREATE INDEX CONCURRENTLY IF NOT EXISTS companies_name_trgm_idx
    ON companies USING gin (name gin_trgm_ops);
