# Repository structure

> Status: **DECIDED**. The import rules in §3 are enforced in CI and have no exemption mechanism.

## 1. Layout

```
jobtrack/
├── cmd/                       # one main.go per binary; thin wiring only
│   ├── api/                   # HTTP server
│   ├── ingestor/              # River workers: fetch, normalise, dedup
│   ├── matcher/               # River workers: embed, score
│   ├── scheduler/             # singleton; enqueues periodic work
│   ├── resume-parser/         # isolated gRPC service (untrusted input)
│   └── migrate/               # migration runner; runs to completion
│
├── internal/                  # all application code. Nothing importable externally
│   ├── domain/                # entities + business rules. Imports nothing from internal/
│   │   ├── job/               # JobPosting, staleness, integrity signals
│   │   ├── user/              # User, preferences
│   │   ├── resume/            # Resume, ParsedProfile, skill attribution
│   │   ├── application/       # Application, the state machine, channel attribution
│   │   └── skill/             # canonical vocabulary, aliases, adjacency
│   │
│   ├── source/                # one adapter per ATS vendor
│   │   ├── adapter.go         # the Adapter interface — the only vendor-aware surface
│   │   ├── greenhouse/        # each with testdata/ golden files
│   │   ├── lever/
│   │   ├── ashby/
│   │   ├── smartrecruiters/
│   │   ├── recruitee/
│   │   ├── jsonld/
│   │   ├── normalise/         # location, comp, YoE, skills, work mode
│   │   ├── dedup/             # the three-stage pipeline
│   │   └── schedule/          # tiering, politeness, circuit breaker
│   │
│   ├── matching/              # scoring engine + profile loading
│   │   ├── score.go
│   │   ├── components/        # one file per component, each independently testable
│   │   └── retrieval/         # hybrid query + RRF fusion
│   │
│   ├── resumeparse/           # the deterministic pipeline (used by cmd/resume-parser)
│   │   ├── extract/           # PDF/DOCX → positioned text
│   │   ├── layout/            # column detection, linearisation
│   │   └── sections/          # classification + field extraction
│   │
│   ├── store/                 # persistence
│   │   ├── queries/           # hand-written .sql — the real source of truth
│   │   ├── gen/               # sqlc output. NEVER edited by hand
│   │   └── *.go               # repository wrappers over generated code
│   │
│   ├── http/                  # transport
│   │   ├── handler/
│   │   ├── middleware/        # auth, ratelimit, otel, recovery, requestid
│   │   └── gen/               # OpenAPI-generated types. NEVER edited by hand
│   │
│   ├── jobs/                  # River job definitions + workers
│   ├── config/                # env + file config, validated at startup
│   └── telemetry/             # OTel setup, slog bridge
│
├── api/
│   ├── openapi.yaml           # source of truth for the REST contract
│   └── proto/                 # protobuf for api ↔ resume-parser
│
├── web/                       # frontend app (framework per ADR-0002)
├── migrations/                # numbered, forward-only, expand/contract
├── config/
│   └── scoring/               # scoring profiles (YAML) — hot-reloadable
├── deploy/
│   ├── compose/               # local dev
│   ├── k8s/                   # manifests: Gateway API, Deployments, KEDA, PDBs
│   └── tofu/                  # OpenTofu modules
├── docs/                      # this documentation set
├── scripts/
├── Makefile
└── go.mod                     # ONE module
```

## 2. Why this shape

### There is no `pkg/`

The widely-copied `golang-standards/project-layout` is **not an official Go standard, and the Go team
says so** `[B-24]`. Go's own guidance ("Organizing a Go module") describes a layout that grows with
the project — flat, then `internal/`, then `cmd/` — rather than one fixed structure imposed up front.

`pkg/` signals "intended for external consumption". We are not publishing a library. Everything goes
in `internal/`, which the compiler enforces as private. A `pkg/` directory here would be cargo cult.

### `cmd/` is thin

Each `main.go` does exactly three things: load config, wire dependencies, start. No business logic
lives in `cmd/`. If you are tempted to write a function there, it belongs in `internal/`.

This is what makes six binaries cheap: they are six different wirings of the same code
([ADR-0008](../architecture/adr/0008-service-decomposition.md)).

### `internal/domain` imports nothing from `internal/`

The load-bearing rule. `domain` holds entities and business rules and **does not import a database
driver, an HTTP package, or any vendor SDK**.

Three concrete payoffs:

1. **Scoring logic is tested with no database.** Those tests run in milliseconds, which is what
   determines whether they get run at all.
2. **Roadmap phases are cheap.** A referral graph or resume reviewer adds to `domain` and `store`
   without touching ingestion.
3. **It is the extraction seam.** A module whose interface takes and returns *values* — never database
   handles or ambient transactions — can become a network service via a transport adapter rather than
   a rewrite ([service-topology §7](../architecture/service-topology.md#7-extraction-path)).

### Generated code is never edited

`internal/store/gen/` (sqlc) and `internal/http/gen/` (OpenAPI) are build output. Editing them
produces a change that vanishes on the next `make generate`. CI runs `make generate` and fails if the
working tree is dirty, so this is caught rather than discovered later.

The real sources of truth are `internal/store/queries/*.sql` and `api/openapi.yaml`.

### Vendor adapters are isolated with golden files

Every ATS adapter lives in its own package with `testdata/` containing **captured real responses**.
When a vendor changes their schema, exactly one golden test fails and exactly one file changes. This
is the whole reason ingestion is maintainable across seven vendors with different formats
([ingestion-pipeline §4](../architecture/ingestion-pipeline.md#4-normalisation)).

Adapters never hit a live ATS in CI. Ever.

## 3. Import rules — enforced

```
domain/      → stdlib only (+ golang.org/x). No internal/ imports at all.
skill/       → domain
source/      → domain, skill
matching/    → domain, skill, store (read-only interfaces)
resumeparse/ → domain, skill
store/       → domain
http/        → domain, store, matching
jobs/        → anything except http
cmd/*        → anything
```

Enforced by an import-graph linter in CI (`go-arch-lint` or equivalent) with **no exemption
mechanism**. This is the point: boundaries maintained by convention decay, boundaries maintained by a
tool are real. It is the mechanism Shopify uses (Packwerk) to keep one of the largest Rails codebases
in existence modular at Black Friday scale `[B-14]`.

A cycle, or an import that crosses a forbidden edge, fails the build. The correct response is never
to add an exemption — it is to move the type into `domain` or invert the dependency with an interface.

## 4. Naming

| Thing | Convention | Example |
|---|---|---|
| Packages | Lowercase, singular, no underscores | `source`, `matching`, `job` |
| Files | `snake_case.go` | `posting_scorer.go` |
| Interfaces | Named for behaviour, not `IFoo` | `Adapter`, `PostingStore` |
| Tests | Alongside, `_test.go`; integration guarded by a build tag | `//go:build integration` |
| SQL | One file per domain area | `queries/postings.sql` |
| Migrations | `NNNN_snake_case.up.sql` | `0031_add_yoe_confidence.up.sql` |

**Avoid stutter.** `job.Posting`, not `job.JobPosting`. The package name is part of the identifier.

**Interfaces are defined by the consumer**, not the producer — `matching` declares the narrow
`PostingStore` interface it needs, and `store` happens to satisfy it. This is what keeps `matching`
testable without a database and is idiomatic Go rather than a Java habit transplanted.

## 5. Where to put a new thing

| I am adding… | It goes in |
|---|---|
| A new ATS vendor | `internal/source/<vendor>/` + a row in [source-catalog](../research/source-catalog.md) |
| A scoring component | `internal/matching/components/` + config entry + golden-corpus case |
| A new endpoint | `api/openapi.yaml` **first**, then `internal/http/handler/` |
| A background job | `internal/jobs/` + a queue assignment in [ADR-0005](../architecture/adr/0005-river-background-jobs.md) |
| A business rule | `internal/domain/<area>/` — and if it needs the database, the rule is in the wrong place |
| A DB query | `internal/store/queries/*.sql`, then `make generate` |
| A UI screen | `web/` per [frontend-architecture](../architecture/frontend-architecture.md) |
| A decision worth remembering | `docs/architecture/adr/` |

## 6. What is deliberately absent

| Absent | Why |
|---|---|
| `pkg/` | Not a library |
| `vendor/` | Modules + checksum DB is sufficient; `vendor/` bloats diffs |
| A DI framework | `main.go` wires by hand. Explicit, greppable, and only ~100 lines |
| A service layer per entity | Layers earn their place; most CRUD does not need one |
| Multiple `go.mod` files | One module, one version, one release ([ADR-0008](../architecture/adr/0008-service-decomposition.md)) |
| `utils/`, `helpers/`, `common/` | Packages named for what they contain, always. These become dumping grounds and then become cycles |
