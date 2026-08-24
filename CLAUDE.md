# Working agreement for JobTrack

Read by AI coding agents and by humans skimming for the house rules. Deliberately short.

**Everything here is enforced by a command.** That is not a boast, it is a scar: four rules in the
previous version of this file were documented and unenforced, and all four had drifted — `sqlc` was
never adopted, handlers bypassed the store layer, a promised citation gate was never written, and the
ADR index sat four records stale. Every rule that had a script was still true. So: **if you add a rule
here, add its check to `scripts/arch/`, or expect it to become false.**

## The one-paragraph summary

JobTrack is a Go + PostgreSQL + SvelteKit job-search instrument for software engineers. It ingests
job postings from **public first-party ATS feeds only**, normalises and deduplicates them, scores
them against a parsed resume with an explainable model, and tracks applications with per-channel
attribution. It does **not** auto-apply, does not scrape behind auth, and does not claim precision it
cannot justify.

## Before you write code

1. Read [docs/product/principles.md](docs/product/principles.md). The anti-features section is
   binding — a PR that adds auto-apply, keyword stuffing, or a fabricated match percentage gets
   closed regardless of quality.
2. Check [docs/architecture/adr/](docs/architecture/adr/) for the decision covering your area. To go
   against an ADR, write a superseding ADR first; don't argue it in a PR thread. The `/adr` skill
   walks the workflow.
3. Any user-visible statistic must trace to an entry in
   [docs/research/evidence-ledger.md](docs/research/evidence-ledger.md) with its grade shown.

## Non-negotiables

**Dependencies are a liability.** Every new module in `go.mod` or `package.json` needs a one-line
justification in the PR description: what does this replace, and what breaks if it is abandoned? The
standard library is the default. `net/http`'s `ServeMux` (Go 1.22+) routes fine; we do not need a
router framework.

**Postgres is the only datastore** — plus S3-compatible object storage for blobs, which is not a
database. No Redis, no Elasticsearch, no separate vector DB, no broker. Full-text via `tsvector`,
similarity via `pgvector`, queues via River, pub/sub via `LISTEN/NOTIFY`, cache via in-process LRU.

This is not dogma: every alternative has a **numeric trigger** in
[caching-and-storage.md](docs/architecture/caching-and-storage.md). Proposing Redis is fine — cite the
metric that fired.

**Layers point one way.** `domain/` imports nothing from `internal/`. SQL lives in `internal/store`
and the migration packages, not in HTTP handlers. Adapters return `RawPosting` and never persist.
The scorer never touches a database. → `make arch-check`

**Every migration is backward-compatible with the previous release.** Expand/contract, always.
`CREATE INDEX CONCURRENTLY`, never a bare `CREATE INDEX` on a populated table. Details in
[docs/operations/deployment-zdt.md](docs/operations/deployment-zdt.md).

**Never fabricate a number.** Not in the UI, not in a log line, not in a test fixture that looks like
production data. If confidence is low, the type system should carry that — see the
`Confidence` type in [docs/architecture/matching-and-scoring.md](docs/architecture/matching-and-scoring.md).

## Comments

The rule, because it gets asked: **comments explain why, code explains what.** A comment that
restates the line below it is noise and will be flagged in review.

Write a comment when one of these is true:

- The code encodes a decision that a reader would otherwise second-guess
  (`// Lever returns createdAt as epoch millis, not ISO-8601 — see source-catalog.md#lever`)
- There is a non-obvious constraint (`// Must run before the contract migration in v1.4`)
- The straightforward implementation is wrong for a reason worth recording
- It is a doc comment on an exported identifier — those are required, and start with the identifier's
  name per Go convention

Do not write a comment for: section banners, restating a function signature, `// increment i`, or
commented-out code. Delete commented-out code; git remembers.

Full standard: [docs/engineering/coding-standards.md](docs/engineering/coding-standards.md).

## Common commands

**Everything runs in containers**, except `make arch-check`, which is stdlib Python on the host so it
stays usable before the stack has ever been started. The only host prerequisites are Docker, git and
python3.

```bash
make dev          # full stack with compose watch: postgres, minio, jaeger, all services
make check        # everything CI runs. THE definition of done
make arch-check   # architecture invariants. Under a second, no toolchain
make test         # Go tests
make test-golden  # source adapters — run after touching ANY adapter, then READ THE DIFF
make generate     # TypeScript API types from api/openapi.yaml
make migrate-new NAME=add_foo
make migrate-verify   # migrations against the PREVIOUS release's schema
make drift-check      # live schema vs. migration history
make psql         # psql inside the network
```

Details and troubleshooting: [docs/engineering/dev-environment.md](docs/engineering/dev-environment.md).

## Before you commit

`make check` is the definition of done — not "it compiles", not "my test passes".

| Before you… | Do this | Enforced by |
|---|---|---|
| Change package boundaries or move SQL | `make arch-check` | `scripts/arch/check_layering.py`, `check_sql_location.py` |
| Write a function longer than 80 lines | Split it, or record why | `scripts/arch/check_func_length.py` |
| Change `api/openapi.yaml` | `make generate` — spec first, handlers after | `make check-generated` |
| Add or change an ADR | Update the index table | `scripts/arch/check_adr_index.py` |
| Cite a statistic | Add a ledger entry with its grade | `scripts/arch/check_citations.py` |
| Change River job args | Read [deployment-zdt §4](docs/operations/deployment-zdt.md#4-queue-compatibility) | review |
| Add a dependency | Say what it replaces and what breaks if abandoned | CI dependency budget |
| Touch a source adapter | `make test-golden`, then **read the diff** | `make test-golden` |
| Propose new infrastructure | Cite the metric that fired | [caching-and-storage](docs/architecture/caching-and-storage.md) |

Full rules: [docs/engineering/consistency-and-drift.md](docs/engineering/consistency-and-drift.md).

## The invariant baselines are a ratchet

`scripts/arch/baseline/*.txt` records violations that exist today. **New entries fail. Entries that
stop violating also fail** — so the file has to shrink as debt is paid, and a stale baseline can't
hide the next regression behind known debt.

```bash
make arch-check-update   # rewrite the baselines, then READ THE DIFF
```

A `-` line is debt paid off. A `+` line is new debt and needs a sentence in the commit message.

## Repository shape

```
cmd/            one main.go per binary: api, ingestor, matcher, scheduler, migrate, resume-parser, seed
internal/
  domain/       entities + business rules; imports nothing from internal/
  source/       one adapter per ATS vendor, each with golden-file tests
  store/        hand-written SQL behind named repository methods
  matching/     scoring engine and profiles; no database
  normalise/    skill vocabulary, compensation and location parsing
  resume/       parsing pipeline
  jobs/         River workers: ingest, score, maintenance
  api/          HTTP handlers, middleware, OpenAPI-generated types
  httpx/        middleware, problem+json, rate limiting
web/            SvelteKit app
docs/           this documentation set
migrations/     numbered, forward-only, expand/contract
scripts/arch/   the invariant checks that keep this list true
```

No ORM and no `sqlc`: queries are hand-written against `pgx`, which ADR-0001 chose deliberately.
Rationale for the layout (and why there is no `pkg/`):
[docs/engineering/repository-structure.md](docs/engineering/repository-structure.md).

## Known gaps — real, recorded, being paid down

Named here rather than hidden, because a document that describes an aspiration as a fact is the
failure mode this file exists to prevent. Live counts: `make arch-check`.

| Gap | Size | Status |
|---|---|---|
| ~~SQL outside `internal/store`~~ | 0 | **Closed.** Every query is in the store or the migration packages |
| ~~Database calls outside the data layer~~ | 0 | **Closed** |
| ~~Package layering violations~~ | 0 | **Closed** |
| Functions over 80 lines | 12 | Ratcheted; target is 50. Longest is 117 |
| `internal/jobs`, `internal/httpx`, `internal/app` have no tests | 3 packages | Open. `internal/store` now has 7 integration tests |
| INP and ingest-latency budgets | — | Declared but never measured. See phase-5 §10 |

## Testing expectations

- **Source adapters**: golden-file tests against captured real responses. Never hit a live ATS in CI.
- **Scoring**: a fixed corpus of resume/JD pairs with expected score *bands*, not exact values.
- **Migrations**: every migration runs forward against a snapshot of the previous release's schema.
- **HTTP**: contract tests generated from the OpenAPI spec; the spec is the source of truth.

[docs/engineering/testing-strategy.md](docs/engineering/testing-strategy.md).

## Performance budgets — enforced in CI

| Budget | Limit | Enforced |
|---|---|---|
| First-load JS (gzipped, `/jobs`) | ≤ 100 KB | `make bench-budget` |
| CSS (gzipped) | ≤ 20 KB | `make bench-budget` |
| INP p75, 4× CPU throttle | ≤ 200 ms | not yet measured |
| `GET /v1/jobs` p95 server time | ≤ 120 ms | by hand |
| Ingest → visible, tier A source | ≤ 90 min median | not yet measured |

A PR that regresses an enforced budget fails CI. Raising a budget requires a note in the PR
explaining the trade — see [docs/architecture/frontend-architecture.md](docs/architecture/frontend-architecture.md).
