# Architecture decision records

An ADR records **a decision, the alternatives considered, and the consequences accepted** — at the
moment it was made, with the information available then. ADRs are immutable once decided. To change a
decision, write a new ADR that supersedes the old one; do not edit history.

The point is not ceremony. It is that six months from now someone will look at a choice that seems
obviously wrong, and the ADR will tell them whether it was a mistake or a trade they have forgotten
the other half of.

## Index

| # | Decision | Status |
|---|---|---|
| [0001](0001-go-for-backend.md) | Go, standard library first, `pgx` + `sqlc`, no ORM, no framework | DECIDED |
| [0002](0002-frontend-framework.md) | Frontend framework — **SvelteKit 2 / Svelte 5**, chosen over Next.js and Nuxt | DECIDED |
| [0003](0003-postgres-single-datastore.md) | PostgreSQL as the only datastore | DECIDED |
| [0004](0004-source-acquisition-policy.md) | Public first-party ATS feeds and JSON-LD only; no board scraping | DECIDED |
| [0005](0005-river-background-jobs.md) | River for background jobs over asynq/Temporal/broker | DECIDED |
| [0006](0006-hybrid-retrieval-and-scoring.md) | Hybrid lexical + vector retrieval, RRF fusion, explainable weighted scoring | DECIDED |
| [0007](0007-resume-parsing-local-first.md) | Deterministic local parsing; LLM as opt-in enrichment only | DECIDED |
| [0008](0008-service-decomposition.md) | **Modular monolith, six deployment units, one extracted service** | DECIDED |

**If you read only one:** [ADR-0008](0008-service-decomposition.md). It answers the
microservices-vs-monolith question with the actual numbers and sets the extraction triggers.

## Status values

- **PROPOSED** — written, not agreed. Do not build on it.
- **OPEN** — a real decision is required from a human before work proceeds.
- **DECIDED** — binding. Overturning requires a superseding ADR.
- **SUPERSEDED** — kept for history, with a link forward.

## Template

```markdown
# ADR-NNNN — <short imperative title>

- **Status:** PROPOSED | OPEN | DECIDED | SUPERSEDED by ADR-MMMM
- **Date:** YYYY-MM-DD
- **Decision drivers:** <the 3-5 things that actually decided it>

## Context
What is true that forces a choice. Include numbers.

## Options
A table or section per option. Every option gets a fair hearing, including
the one we rejected — a strawman here is a trap for a future reader.

## Decision
What we are doing, stated so someone could implement it.

## Consequences
### Good
### Bad, and accepted
The second heading is mandatory. An ADR with no accepted downsides has not
been thought about.

## Revisit
The condition under which this should be reopened.
```
