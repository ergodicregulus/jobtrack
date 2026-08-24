# ADR-0006 — Hybrid retrieval with RRF, explainable weighted scoring

- **Status:** DECIDED
- **Date:** 2026-08-15
- **Decision drivers:** explainability, ranking quality, cost, testability

## Context

Two related but distinct problems, routinely conflated:

1. **Retrieval** — from 200k live postings, get the ~200 that could plausibly be relevant.
2. **Scoring** — of those, rank them against *this user's* resume, with reasons.

They need different machinery. Retrieval must be fast and high-recall; scoring must be precise and
explainable.

## Options for retrieval

| | Lexical only (`tsvector`) | Vector only (`pgvector`) | **Hybrid + RRF** |
|---|---|---|---|
| Exact terms ("Kubernetes", "Bengaluru") | **Excellent** | Poor — "Java"≈"JavaScript" | **Excellent** |
| Paraphrase ("distributed systems" ≈ "large-scale backend") | Poor | **Excellent** | **Excellent** |
| Cost per query | Lowest | Medium | Medium |
| Explainability | High | Low | Medium |
| New infrastructure | None | None | None |

Neither arm alone is adequate. Lexical search misses semantic equivalence; vector search makes
errors that are indefensible to a user ("Java" and "JavaScript" are near neighbours in embedding
space, and a candidate shown JavaScript roles because their resume says Java would be right to be
angry).

**Fusing them requires care.** `ts_rank` scores and cosine distances live on incomparable scales, so
adding or weighting them requires a normalisation that is itself arbitrary and drifts as the corpus
changes.

**Reciprocal Rank Fusion sidesteps this entirely** by discarding raw scores and using only rank
positions `[B-19]`:

```
RRF(d) = Σ over lists L:  1 / (k + rank_L(d))          k = 60
```

A document ranking highly in both lists rises; one ranking highly in only one still surfaces. No
normalisation, no tuning beyond `k`, and identical in shape to what you would write against
Elasticsearch — without adding Elasticsearch.

## Options for scoring

| | Weighted components | Learned ranker (LTR) | LLM scoring |
|---|---|---|---|
| Explainable as text | **✓** | ✗ | partially |
| Testable against a golden corpus | **✓** | ✓ | ✗ (non-deterministic) |
| Cost per score | **~0** | low | **high** |
| Latency | **µs** | ms | seconds |
| Needs training data | **✗** | ✓ (we have none) | ✗ |
| Config-tunable without deploy | **✓** | ✗ | partially |

**Learned ranking is rejected for v1 on a simple fact: we have no training data**, and the only
signal we could bootstrap from — user clicks and applications — is confounded by our own ranking.
Training on it would launder our initial guesses into an unexplainable model.

**LLM scoring on the hot path is rejected** on cost, latency, non-determinism, and — decisively — that
it cannot be regression-tested. A scoring function whose output changes between runs cannot have a
golden corpus, and a scoring function without a golden corpus drifts.

## Decision

**Retrieval:** hybrid lexical + vector, fused with RRF (`k = 60`), all inside Postgres.

**Scoring:** a configuration-driven weighted sum over independent, individually-explainable
components. Full model: [matching-and-scoring.md](../matching-and-scoring.md).

**Embeddings:** 384-dimensional, sentence-transformer class, generated locally in `matcher`. Chosen
over 768/1536 because at 200k postings the HNSW index fits comfortably in RAM and recall for this
task is indistinguishable — the larger models cost 2–4× the memory to buy nothing measurable here.

**Filtered vector search always sets `hnsw.iterative_scan`.** Before pgvector 0.8, a vector search
combined with a `WHERE` clause could over-filter and silently return too few rows; iterative scan
keeps scanning until enough post-filter results are found `[A-13]`. Our queries are *always* filtered
(location, YoE, freshness), so this is not optional. A lint rule asserts it on every vector query.

**Skill adjacency comes from a curated table, not from embeddings.** This is the specific place
embeddings fail hardest, and the Fraunhofer duplicate-detection work found the same in a neighbouring
problem: **curated weighted lookup lists for specific skills, combined with string comparison and
embeddings, significantly outperformed any single technique** `[A-20]`.

## Consequences

### Good

- Every ranking is explainable as text, satisfying
  [P7](../../product/principles.md#p7--explainability-is-a-feature-not-a-debug-tool).
- Weights are configuration, so tuning per market is a data change with a version stamp, not a
  deploy.
- Testable: a golden corpus of ~200 labelled pairs, asserted against *bands* rather than exact
  values.
- No new infrastructure; no training pipeline; no inference API bill.

### Bad, and accepted

- **Weights are hand-tuned and therefore initially wrong.** Mitigated by making them config,
  monitoring the score distribution, and treating a drift in band proportions as a defect signal.
- **`ts_rank` is not true BM25.** Real, and worse on long queries. Partly compensated by the vector
  arm; upgrade path is `pg_search` inside the same database if a measured evaluation says lexical
  ranking is the binding constraint.
- **The curated adjacency table is manual work** that grows with the skill vocabulary. Accepted — it
  is exactly the work that prevents the failure mode users would find unforgivable.
- **RRF discards score magnitude.** A document ranked #1 by a huge margin is treated the same as one
  ranked #1 narrowly. Acceptable for candidate generation, since the weighted scorer re-ranks
  afterwards and it is the scorer's output the user sees.

## Revisit

Once there is a genuine outcome dataset — real interview outcomes across enough users and channels —
revisit a **learned re-ranker layered on top of** the explainable base score, capped in how far it may
move a result so the explanation stays true. Never as a replacement for it.
