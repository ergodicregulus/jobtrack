# ADR-0022 — Retrieval is lexical; posting features are computed at ingest, scores at read

- **Status:** DECIDED
- **Date:** 2026-09-30
- **Supersedes:** the *retrieval* half of [ADR-0006](0006-hybrid-retrieval-and-scoring.md). Its
  scoring half stands unchanged and is what the product runs on.
- **Decision drivers:** a DECIDED retrieval design with no implementation and no host to run it; a
  public README presenting it as shipped; the open question of doing "matching while fetching"

## Context

ADR-0006 decided hybrid lexical + vector retrieval fused with RRF, with 384-dimensional embeddings
"generated locally in `matcher`". None of the retrieval half was built. A note added to ADR-0006 on
2026-08-25 says so, and that note is accurate. Measured on the tree, 2026-09-30:

| | |
|---|---|
| Vector queries (`<=>`, `<->`, `<#>`) in any Go source | **0** |
| RRF, or any rank fusion | **0** |
| Code that writes or reads `posting_embeddings` / `resume_embeddings` | **0** |
| The service ADR-0006 assigned embedding generation to | **deleted** by [ADR-0016](0016-scores-are-computed-not-materialised.md) |
| Code that reads the `skill_adjacency` table | **0** — adjacency is `matching.DefaultAdjacency()`, in Go |

So the design is not merely unbuilt; the component that was to build it no longer exists. Meanwhile
the README headlined "Postgres 17 + pgvector" and CLAUDE.md listed "similarity via pgvector" among
the ways Postgres replaces other stores. In a repository linked from a résumé, a capability an
interviewer can ask about and not find is worse than one never claimed.

Retrieval as built: `tsvector` with `setweight` (title A, body B), `websearch_to_tsquery`, ranked by
`ts_rank` when a query is present, over structured filters — country, work mode, field, experience
band, compensation normalised to USD, vendor, recency.

## Options

| Option | Verdict |
|---|---|
| **Build ADR-0006's vector retrieval** | Rejected *for now*. Embeddings need a model: an external API is a new dependency, a per-query cost, and résumé text leaving the process; a local model is a new heavyweight container and exactly the service ADR-0016 removed. Against that, there is no measurement showing lexical retrieval misses relevant postings at this corpus size — building it would be spending against an assumed problem. |
| **Keep the schema as a placeholder** | Rejected. Two tables with HNSW indexes and one curated-adjacency table, all empty, are what made pgvector look shipped in the first place. `check_schema_usage.py` already flags them. |
| **Lexical retrieval, drop the unused schema, re-open on evidence** | **Chosen.** |

## Decision

1. **Retrieval is lexical** — full-text search plus structured filters, as described above. Nothing
   in the product claims semantic search.
2. **Migration 0029 drops `posting_embeddings`, `resume_embeddings` and `skill_adjacency`.** No
   release has ever read or written them, so the contract step needs no expand step before it.
   `watchlists` is not dropped: it belongs to a separate, unbuilt product feature (F8), not to this
   decision, and stays in the schema-usage baseline where its status is tracked.
3. **Skill adjacency stays curated, in code.** ADR-0006's reasoning here was right and is
   implemented — embedding similarity puts "Java" beside "JavaScript" — but the curation lives in
   `matching.DefaultAdjacency()`, reviewed in diffs and versioned with the scorer that uses it, not in
   table rows nothing reads.
4. **The `vector` extension stays installed, and the `pgvector/pgvector:pg17` image stays.** Not
   out of inertia: migrations 0003 and 0005 create `vector` columns, migrations are forward-only, and a
   fresh database has to replay that history before 0029 can drop them. Nothing reads the extension,
   and no document claims otherwise.

### Where match-time work happens

This is the answer to "should matching happen while fetching?" — it already does, for every part
that can.

| Work | When | Why there |
|---|---|---|
| Title and seniority, location, years of experience, work mode, compensation, skills, field | **Ingest**, once per posting, in `store.PostingFromRaw` | Depends only on the posting. Doing it per request would repeat the same text parsing for every viewer. |
| Profile × posting score | **Read**, per result shown | Depends on the user. Precomputing it is materialising users × postings — measured by ADR-0016 at 1,952 MB, 78.4% of the database, for 244 users. |

`matching.Scorer.Score` does arithmetic on precomputed structured fields and parses no text, so the
per-request half is already the cheap half. There is no further split available that is not the
one ADR-0016 measured and reversed.

## Consequences

### Good

- The README, CLAUDE.md and the schema now describe the same system.
- Three dead tables and two HNSW indexes leave the schema, and the schema-usage baseline shrinks from
  four entries to one.
- "Matching while fetching" has a recorded answer instead of an open question.

### Bad, and accepted

- **Synonym and paraphrase queries miss.** A search for "backend" does not match a posting that says
  only "server-side". That is the real cost of lexical retrieval, and it is accepted until measured.
- **Reviving vector retrieval is real work:** re-create the tables, choose where embeddings are
  generated, and index a corpus. It would need a superseding ADR, which is the point.

## Revisit

When a labelled query set — queries paired with the postings a human judges relevant — shows lexical
search missing more than one relevant posting in five. Measure it before building anything: if the
misses are vocabulary rather than meaning, extending the skill vocabulary and its synonyms is cheaper
than embeddings and keeps every result explainable.
