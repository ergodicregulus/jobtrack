# ADR-0015 — Hand-written SQL behind named store methods; sqlc is not adopted

- **Status:** DECIDED
- **Date:** 2026-08-25
- **Decision drivers:** ADR-0001 chose `sqlc` and it was never adopted; pgx v5 removes the main
  benefit without codegen; the real defect is SQL's *location*, not its authorship
- **Amends:** [ADR-0001](0001-go-for-backend.md), whose `sqlc` clause this supersedes. The rest of
  ADR-0001 — Go, standard library first, `pgx`, no ORM, no framework — stands unchanged.

## Context

ADR-0001 decided "`sqlc` to generate type-safe Go from hand-written SQL. **No ORM.**" That decision
was recorded and never implemented. There is no `sqlc.yaml`, no `.sql` query directory, and no
generated code; `make generate` emits TypeScript types only.

Meanwhile the code drifted the other way. Measured 2026-08-25:

| Package | SQL literals |
|---|---|
| `internal/api` (HTTP handlers) | **36** |
| `internal/jobs` (River workers) | 19 |
| `internal/store` (the data layer) | **8** |

The HTTP layer holds 4.5× more SQL than the data layer. `internal/api/dashboard.go` builds a
`pgx.Batch` of seven queries inline and scans them positionally, and seven of nine files in the
package import `pgx` directly.

The consequences are concrete, not stylistic:

- A query cannot be reused by the ingestor or the matcher without an HTTP request.
- "Where do we read applications from" has no answer a reader can grep for.
- The five longest handlers in the repository are long **because** they carry the query, the batch
  plumbing and the positional scanning: `handleDashboard` is 164 lines, `handleResumeUpload` 109,
  `parseFeedFilter` 108, `handleUpdateSaved` 105, `handleResumeApply` 103.

That last point is why this is one problem and not two. The function-length tail and the layering
violation have the same cause, and extracting the data layer collapses most of both.

## Options

**A. Adopt `sqlc` as ADR-0001 said.** Compile-time checking of column names and types against a real
schema — genuinely the strongest safety of the three. Costs: queries move to `.sql` files, a codegen
step joins the build, `make generate` must run before the code compiles, and a stale regeneration
becomes a new class of confusing failure. Also a migration of ~63 existing statements before anything
improves.

**B. Hand-written `pgx` with positional `Scan`.** What exists. No tooling. Positional scanning is a
live hazard: adding a column to a `SELECT` and forgetting the matching `Scan` argument is a runtime
error that tests only catch if they cover that row shape.

**C. Hand-written `pgx` with `RowToStructByName` + `CollectRows`.** pgx v5 maps result columns onto
struct fields by name or `db:` tag. A column added to the `SELECT` but not the struct fails loudly
and immediately, which is the specific bug class option B leaks and option A was chosen to prevent.
No codegen, no build step, no new module — v5.10.0 is already in `go.mod`.

**The framing question this project requires:** *what is the simplest thing that gives us type-safe,
locatable, reusable database access for ~63 statements maintained by one developer?* Option A buys
the last increment of safety — checking SQL against the live schema — at the cost of a codegen step
in every build. `make drift-check` already compares the live schema against the migrations, which
covers the neighbouring risk.

## Decision

**Option C.**

1. **All SQL lives in `internal/store`**, or in the migration packages (`internal/migrate`,
   `internal/datamigrations`, `internal/seed`). Enforced by
   `scripts/arch/check_sql_location.py`.
2. **Every query is a named method** on a store type, taking a context and typed arguments and
   returning domain types — never `pgx.Rows`, never a `*pgxpool.Pool`, to its caller.
3. **Row scanning uses `pgx.RowToStructByName` / `pgx.CollectRows`** where a struct is the natural
   result. Positional `Scan` is acceptable for a single scalar.
4. **`internal/api` must not import `pgx`.** Enforced by `scripts/arch/check_layering.py`.
5. **`sqlc` is not adopted.** The ADR-0001 clause is superseded.

Existing violations are ratcheted under ADR-0014 rather than fixed in one commit: 9 import edges and
61 misplaced statements, recorded and only allowed to shrink.

## Consequences

### Good

- One place to look for any query, and one place to optimise one.
- Queries become reusable by the workers, which is what the ingestor needed and worked around.
- Handlers shrink to HTTP concerns, which drains the function-length baseline as a side effect.
- The store becomes testable against a real Postgres without an HTTP layer — `internal/store`
  currently has no tests at all, and this is what makes writing them reasonable.
- Zero new dependencies.

### Bad, and accepted

- **No compile-time check that a query matches the schema.** `sqlc` would give that. We accept the
  gap and cover its neighbourhood with `make drift-check` and store-level integration tests.
- **`RowToStructByName` is reflection at runtime**, so a mismatch fails on the first execution rather
  than at build time. Loud and immediate, but still runtime.
- **A store package can grow into a junk drawer.** Split by aggregate — `feed.go`, `postings.go`,
  `applications.go`, `scores.go` — not by "everything else".
- **The extraction is a large diff** touching every handler, and it is being done incrementally,
  which means the codebase is inconsistent while it happens.

## Revisit

- If store methods exceed ~150, or two schema-mismatch bugs reach a deployed environment, the
  compile-time guarantee is worth the codegen step: adopt `sqlc` and supersede this.
- If `RowToStructByName` shows up in profiles as a measurable cost on the hot feed query, drop to
  explicit scanning **for that query only**, with a comment saying why.
