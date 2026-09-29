# Development environment

> Status: **DECIDED**. **Fully containerised.** Revised 2026-08-15 — the previous version ran Go
> processes on the host, which was wrong. Reasoning in §2.
>
> Target: a new contributor — or an AI agent — has the full system running with realistic data in
> **one command**, with **no host toolchain beyond Docker and git**.

## 1. Zero to running

```bash
git clone <repo> && cd jobtrack
make dev
```

That is the whole thing. No Go install, no Node install, no `sqlc`, no `air`, no version managers.
Open http://localhost:5173 — signed in as `dev@jobtrack.local`, with a parsed sample resume and
~2,000 seeded postings across five ATS vendors.

**Prerequisites: Docker Desktop (or Docker Engine + Compose v2.22+) and git.** That is the complete
list, and keeping it that short is the point.

### VS Code / Cursor / JetBrains users

`.devcontainer/devcontainer.json` is checked in. **Reopen in Container** puts your editor *inside*
the toolchain container — `gopls`, `golangci-lint`, `sqlc` and the debugger all resolve, with no host
install. Everyone gets the same language-server version, which quietly eliminates a whole class of
"works on my machine" report.

---

## 2. Why fully containerised

The previous design ran stateful dependencies in Docker and Go processes on the host, for hot-reload
speed. That trade is no longer necessary and was never worth its cost.

**What it cost:**

| Problem | Consequence |
|---|---|
| Host Go version drift | "Works on my machine" on a `go.mod` toolchain bump |
| Host `sqlc`/`buf`/`golangci-lint` versions | Generated code differs between contributors → spurious diffs |
| Different libc on macOS vs Linux | CGO and PDF-parsing behaviour differs between dev and prod |
| Onboarding | Install Go, Node, six tools, hope versions match |
| AI agents / CI runners | Must replicate a host setup that is documented in prose |

**What removed the trade:** Docker Compose Watch reached GA and gives **sub-500 ms sync** versus the
2–4 second delays of traditional bind mounts, with three actions — `sync` for source, `rebuild` for
dependency changes, and `sync+restart` for config `[B-33]`. For compiled languages like Go it
triggers the compile step defined in the Dockerfile.

So containerised development is now **as fast as host development and strictly more reproducible.**
There is no longer a reason to choose otherwise.

**The rule that follows:** the toolchain exists in exactly one place — the `tools` image. Nothing is
installed on a host, ever. If a command needs a tool, it runs in a container.

---

## 3. What `make dev` starts

```mermaid
flowchart TB
    subgraph deps["Stateful — containers"]
        PG[("postgres:17 + pgvector\n:5432")]
        JAEGER["jaeger — traces\n:16686"]
    end
    subgraph app["Application — containers, compose watch"]
        API["api :8080"]
        ING["ingestor"]
        SCH["scheduler"]
        RP["resume-parser :9090"]
        WEB["web :5173"]
    end
    TOOLS["tools container\ngo · sqlc · buf · lefthook\nmigrate · golangci-lint"]

    WEB --> API --> PG
    API --> RP
    ING & SCH --> PG
    API & ING & RP --> JAEGER
    TOOLS -.->|make targets exec here| PG

    style TOOLS fill:#0f4c5c,stroke:#22a3c3,color:#fff
```

### Compose Watch configuration

```yaml
services:
  api:
    build: { context: ., dockerfile: deploy/compose/Dockerfile.dev, target: api }
    develop:
      watch:
        # Go source changes: sync into the container, air rebuilds inside it.
        - action: sync+restart
          path: ./internal
          target: /src/internal
        - action: sync+restart
          path: ./cmd/api
          target: /src/cmd/api
        # Dependency changes need a real image rebuild, not a sync.
        - action: rebuild
          path: go.mod
        # Scoring config is read at runtime — sync only, no restart needed.
        - action: sync
          path: ./config/scoring
          target: /src/config/scoring

  web:
    develop:
      watch:
        - action: sync
          path: ./web/src
          target: /src/web/src        # Vite HMR handles it from here
        - action: rebuild
          path: ./web/package.json
```

Note the three distinct actions doing three distinct jobs. `sync` alone for anything read at runtime,
`sync+restart` for compiled Go, `rebuild` only for dependency manifests. Using `rebuild` everywhere —
the common mistake — is what makes people believe container development is slow.

### Development images are not production images

| | Dev | Production |
|---|---|---|
| Base | `golang:1.25` (full toolchain, `air`, `dlv`) | **`gcr.io/distroless/static`** |
| Size | ~900 MB | ~20 MB |
| Shell | yes | **no** |
| User | root (dev convenience) | `nonroot` (65532), read-only rootfs |

Production uses **distroless static** rather than scratch. Scratch has zero base-image CVEs by
construction, but distroless is **regularly rebuilt and patched**, so a vulnerability in the base
libraries is fixed by pulling the latest tag — and it still ships no shell, so an attacker with code
execution cannot pivot to arbitrary commands `[B-34]`. For a Go static binary that trade favours
distroless. Alpine is rejected: it carries BusyBox and a package manager we have no use for.

`resume-parser` gets the hardest variant — distroless static, non-root, read-only rootfs, no network
egress, seccomp — because it is the only component processing untrusted binary input
([service-topology §3](../architecture/service-topology.md#resume-parser--the-one-true-service)).

---

## 4. Make targets

Every target runs inside a container. `make` is the only host command.

```makefile
make dev              # full stack, watching
make dev-logs         # tail all services
make down             # stop; keeps volumes
make clean            # stop and destroy volumes

make check            # everything CI runs. THE definition of done
make test             # unit + integration (testcontainers)
make test-unit        # fast, no DB — the one you run constantly
make test-golden      # source adapters; run after touching ANY adapter
make test-scoring     # golden corpus of resume/JD pairs
make test-e2e         # Playwright against a seeded stack

make lint             # golangci-lint + eslint + sqlc vet + import-graph
make generate         # sqlc + openapi + buf; CI fails if this dirties the tree

make migrate-new NAME=add_foo
make migrate-up
make migrate-verify   # runs migrations against the PREVIOUS release's schema
make drift-check      # atlas: live schema vs. migration history

make seed             # ~2,000 realistic postings
make seed-large       # 200k postings for query-plan work
make db-reset         # drop, migrate, seed
make psql             # psql inside the network

make shell            # shell in the tools container
make bench-budget     # frontend budgets on the reference low-end profile
make capture-source VENDOR=ashby BOARD=example-co
```

Under the hood every one is `docker compose run --rm tools <cmd>`. A contributor never learns that,
and never needs to.

---

## 5. Seed data

```bash
make seed              # register the real boards and create demo users
docker compose run --rm --no-deps tools "go run ./cmd/seed -reset -ingest"
```

`-ingest` fetches every registered board synchronously and returns only once the
queue has drained, so "the data is there" is true when the command exits. It
takes four to five minutes: the per-host limiter allows one concurrent request
per vendor by design.

| Data | Volume | Source |
|---|---|---|
| Companies | 61 | **Real**, verified live |
| Sources | 61 | 34 Greenhouse · 27 Ashby |
| Job postings | **~6,700** | **Fetched live from the vendors' own APIs** |
| Users | 3 | Junior IN · senior IN/GB · new-grad US/GB |
| Scores | — | Computed on the read path, never stored (ADR-0016) |

### The seed does not fabricate postings

It registers real boards and lets the ingestor fetch them. Every posting
therefore carries the employer's genuine apply URL, and clicking Apply lands in
that company's real requisition queue.

This replaced a generator that produced synthetic postings with plausible-looking
URLs. It was fine for exercising the pipeline offline and wrong the moment
anyone clicked one: nine of the twelve board tokens were inferred from company
names and did not exist, so the links 404ed. In a product whose entire premise is
that the apply link works, seed data that lies about it is worse than no seed
data. See [source-catalog](../research/source-catalog.md#tokens-are-verified-never-guessed).

### Demo accounts

All three share the password `dev-password-please` and are already onboarded, so
the dashboard has something to show on first sign-in.

| Email | Who | Profile |
|---|---|---|
| `dev@jobtrack.local` | Arjun Mehta | 2 yrs · Python/Postgres/Docker/AWS · India · any mode |
| `senior@jobtrack.local` | Priya Sharma | 7 yrs · Go/K8s/Kafka/Terraform · India + UK · remote only |
| `grad@jobtrack.local` | Sam Okafor | 0 yrs · TypeScript/React · US + UK · remote or hybrid |

Three profiles rather than one because the scoring bands only mean anything in
contrast: the same posting should be a strong match for one of these people, a
stretch for another, and irrelevant to the third. A single demo user makes every
score look reasonable.

An empty dev database is a dev environment where nobody can see whether their
change worked. Seeding is a correctness tool, not a convenience.

---

## 6. Configuration

`.env.example` is complete and checked in; `make dev` copies it on first run. Config is validated **at
startup, reporting every problem at once** — a service that starts fine and fails an hour later on a
missing value is a service that pages someone at 3 a.m.

```bash
DATABASE_URL=postgres://jobtrack:dev@postgres:5432/jobtrack?sslmode=disable
SESSION_SECRET=dev-only-not-a-real-secret-min-32-chars
RESUME_ENCRYPTION_KEY=dev-only-resume-key-also-min-32-chars
RESUME_PARSER_URL=http://resume-parser:9090
OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4318
LOG_LEVEL=debug
LOG_JSON=false                                  # true in production

INGEST_MODE=fixture                             # see §7
INGEST_TIER_A_INTERVAL=2h
LLM_ENRICHMENT_ENABLED=false                    # opt-in, off everywhere by default
```

Hostnames are **service names, not `localhost`** — the containers share a network. This is the one
thing that surprises people moving from a host setup.

---

## 7. Working on ingestion without hammering anyone

```bash
INGEST_MODE=fixture    # DEFAULT — replay golden files. No network at all.
INGEST_MODE=recorded   # replay a captured session with realistic timing
INGEST_MODE=live       # real requests. Requires INGEST_LIVE_ALLOWLIST.
```

`live` **refuses to start without an explicit allowlist of source IDs.** Politeness is a property of
the system, not of the environment
([ADR-0004](../architecture/adr/0004-source-acquisition-policy.md)) — the guard rail belongs in code,
not in a wiki page nobody reads.

```bash
make capture-source VENDOR=ashby BOARD=example-co
# → internal/source/ashby/testdata/example-co.json
# Review before committing: strip anything resembling personal data.
```

---

## 8. Debugging

**Traces** — Jaeger at http://localhost:16686. Every request and job is traced; `trace_id` appears in
every log line and every error response. Start there rather than grepping.

**Debugger** — `dlv` runs in the dev image with its port exposed; `.vscode/launch.json` attaches. This
works identically inside a devcontainer.

**SQL** — `LOG_SQL=true` logs every query with timing and args. Noisy by design.

**Queue** — River jobs are rows, so `make psql` is the debugger:

```sql
SELECT kind, state, count(*) FROM river_job GROUP BY 1,2 ORDER BY 3 DESC;
SELECT * FROM river_job WHERE state = 'discarded' ORDER BY finalized_at DESC LIMIT 10;
```

**Scoring** — `make explain-score USER=1 POSTING=42` prints the component breakdown the UI shows.

---

## 9. Common problems

| Symptom | Cause | Fix |
|---|---|---|
| Port 5432 in use | Host Postgres running | `make dev PG_PORT=5433` |
| Changes not appearing | Watch not running | `make dev` uses `--watch`; check the path is covered in `develop.watch` |
| Rebuild on every save | Path matched a `rebuild` rule | It should be `sync` or `sync+restart` |
| `pgvector` missing | Wrong image | Must be `pgvector/pgvector:pg17` |
| Generated diffs after pull | A `.sql` or the OpenAPI spec changed | `make generate` |
| Golden tests fail after adapter change | Expected — the tests working | Review the diff, then `make test-golden UPDATE=1` |
| Slow on macOS | Bind-mount I/O | Enable VirtioFS; Compose Watch avoids most of this already |
| Scores all zero | No default resume, or scoring profile failed to load | `make explain-score` says which |
| `connection refused` to `localhost` | Using `localhost` instead of a service name | Use `postgres`, `api`, `resume-parser` |

---

## 10. CI uses the same containers

CI runs `make check` against the same compose stack, **with no cloud credentials present**. That is
both a reproducibility property and the test of the vendor-neutrality claim: a dependency that cannot
run in this environment cannot merge
([service-topology §8](../architecture/service-topology.md#8-vendor-neutrality--the-portability-contract)).

Portability that is never exercised is portability that has already been lost.

---

## 11. First contribution path

Ordered so each step teaches what the next one needs:

1. `make dev`, open the app, apply a filter. **See it work.**
2. Read [principles.md](../product/principles.md) — especially the anti-features.
3. Add a skill alias in `internal/domain/skill/` and watch matching change. *Smallest change with a
   visible effect.*
4. Add a scoring component behind a config flag. Teaches the config-driven model.
5. Add an ATS adapter with golden files. Teaches the hardest part of the system.

Anyone who has done step 5 can work on anything here.

Before every commit: `make check`. It is the same thing CI runs, and it is the definition of done —
see [consistency-and-drift](consistency-and-drift.md).
