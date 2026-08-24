<!-- The sqlc clause below was superseded by ADR-0015 on 2026-08-25: it was never
     adopted, and pgx v5's RowToStructByName covers the safety it was chosen for.
     Everything else in this record stands. -->

# ADR-0001 — Go for the backend, standard library first

- **Status:** DECIDED
- **Date:** 2026-08-15
- **Decision drivers:** low resource footprint, concurrency for I/O-bound ingestion, single-binary
  deployment, dependency minimisation, team familiarity

## Context

The backend has three workload shapes:

1. **HTTP serving** — thousands of small, latency-sensitive requests.
2. **Highly concurrent network I/O** — polling thousands of ATS feeds, mostly waiting on sockets.
3. **CPU-bound batch** — embedding generation and scoring.

Plus a stated product constraint: run efficiently on modest hardware, with the fewest dependencies we
can manage ([P6](../../product/principles.md#p6--every-dependency-is-a-liability)).

## Options

| | Go | Node/TypeScript | Python | Rust |
|---|---|---|---|---|
| Concurrent I/O at 5k sources | Goroutines — the canonical fit | Event loop, fine | asyncio, workable | Excellent |
| CPU-bound scoring | Good, trivially parallel | Poor (single-threaded) | Poor without native ext. | Excellent |
| Memory footprint | ~20–40 MB/process | ~80–150 MB | ~100–200 MB | ~10–20 MB |
| Deploy artefact | **Static binary, scratch image** | Runtime + node_modules | Runtime + venv | Static binary |
| Stdlib coverage for our needs | **Very high** — HTTP, TLS, JSON, crypto, templates | Moderate | High | Low; needs crates |
| Time to first working system | Fast | Fast | Fastest | Slowest |
| Ecosystem for Postgres | `pgx` — excellent | good | excellent | good |

**Rust** is the strongest technical option and is rejected on development velocity. For a one-to-five
person team building a product with an uncertain feature set, the borrow checker's cost during rapid
iteration exceeds its benefit. Nothing here needs Rust's guarantees.

**Python** was seriously considered because the founding developer's background is Django/DRF, which
would be the fastest path to a first version. Rejected on the concurrency shape: 5,000 concurrent
feed polls plus CPU-bound scoring in the same runtime is exactly where Python is weakest, and the
workarounds (separate async framework, native extensions, multiple process pools) reconstruct Go's
model badly.

**Node** is rejected on the CPU-bound half and memory footprint.

## Decision

**Go 1.25, standard library first.**

Specifically:

- **`net/http.ServeMux`** for routing. Since Go 1.22 it handles method matching and wildcards
  (`GET /v1/jobs/{id}`), exposes `Request.PathValue`, returns **405 automatically** when a path
  matches but the method does not, and **detects conflicting patterns at registration time** — i.e.
  at startup, not on the request that hits the ambiguity `[A-22]`. **No Chi, Gin, Echo, or Fiber.**

  Its real limits, stated so nobody rediscovers them mid-implementation: a wildcard must occupy a
  **whole path segment** (`/b_{bucket}` is not a valid pattern — parse inside the handler instead),
  and there are **no route groups or built-in middleware chaining**. We write a ~15-line `chain()`
  helper and register routes with an explicit prefix constant. That is the entire cost of not taking
  a router dependency, and it is smaller than one framework upgrade.
- **`pgx/v5`** as the driver — the de facto Postgres package since `lib/pq` entered maintenance, and
  its binary protocol and `CopyFrom` matter for bulk ingestion `[B-20]`.
- **`sqlc`** to generate type-safe Go from hand-written SQL. **No ORM.** SQL is the interface to the
  database; generating Go from it gives type safety without hiding the query, and the query is the
  thing we need to reason about for performance.
- **`log/slog`** for structured logging, bridged to OpenTelemetry via `otelslog` — which adds under
  1% overhead because it only extracts trace context and appends it `[B-21]`.
- **`encoding/json`**, `crypto/*`, `html/template` — stdlib.

**Direct dependency budget: ≤ 12**, excluding `golang.org/x/*` and OpenTelemetry.

## Consequences

### Good

- One static binary per deployment unit; `FROM scratch` images in the 15–25 MB range.
- Goroutine-per-source ingestion is the natural expression of the problem, not a workaround.
- The same module compiles into all six binaries ([ADR-0008](0008-service-decomposition.md)).
- Fast compilation keeps the test loop tight, which is what actually determines whether tests get run.
- Upgrades are low-drama: Go's compatibility promise plus a small dependency set means version bumps
  are usually uneventful.

### Bad, and accepted

- **More boilerplate than a framework.** Middleware chaining, request decoding and validation are
  hand-written. Accepted: it is perhaps 300 lines total, it is explicit, and it never surprises us.
- **Go's ML ecosystem is thin.** Embedding generation calls out to an ONNX runtime binding or a small
  sidecar rather than using a native library. Covered in
  [ADR-0007](0007-resume-parsing-local-first.md).
- **`sqlc` requires discipline.** Queries live in `.sql` files and regeneration is a build step; a
  developer who edits generated code will have a bad afternoon. Mitigated by a `make generate` check
  in CI.
- **Error handling is verbose.** Accepted; it is also why production failures are legible.

## Revisit

If embedding generation becomes the dominant cost and the ONNX binding proves unmaintainable, a
Python or Rust sidecar for that one workload is reasonable — the interface is already a queue, so it
is a contained change. That is not a reason to move the rest.
