-- The contract half of ADR-0022: drop the schema of a retrieval design that was
-- never built.
--
-- posting_embeddings and resume_embeddings were created for ADR-0006's vector
-- retrieval, with HNSW indexes, and have held zero rows since. Their embeddings
-- were to be generated in `matcher`, which ADR-0016 deleted. skill_adjacency was
-- to hold ADR-0006's curated adjacency; the curation is real but lives in Go
-- (matching.DefaultAdjacency), versioned with the scorer that reads it.
--
-- No release has ever read or written any of the three, so there is no previous
-- release for this to be incompatible with, and no expand step owed first. The
-- HNSW indexes from migration 0007 go with their tables.
--
-- watchlists is deliberately NOT dropped. It belongs to an unbuilt product
-- feature (F8), not to ADR-0022, and stays in the schema-usage baseline.
DROP TABLE IF EXISTS posting_embeddings;
DROP TABLE IF EXISTS resume_embeddings;
DROP TABLE IF EXISTS skill_adjacency;
