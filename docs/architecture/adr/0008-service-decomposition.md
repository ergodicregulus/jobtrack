# ADR-0008 — Service decomposition: modular monolith, multi-process deployment

- **Status:** DECIDED
- **Date:** 2026-08-15
- **Supersedes:** the informal "four binaries" note in the first draft of
  [system-architecture.md](../system-architecture.md)
- **Decision drivers:** scale-readiness, operational cost, security isolation, team size, vendor
  neutrality

---

## The question

Microservices or monolith for JobTrack?

This ADR exists because the question was asked directly and deserves a real evaluation rather than a
default. The answer turns out to be neither of the two options as usually posed, and the reasoning
matters more than the label.

---

## Part 1 — Measure the actual workload first

Architecture arguments go wrong when they start from patterns instead of numbers. Ours:

### Traffic

| Scenario | MAU | Avg RPS | Peak RPS | Notes |
|---|---:|---:|---:|---|
| v1 launch | 1,000 | 0.4 | 5 | |
| Target year 1 | 10,000 | 4 | 40 | 50 sessions/user/mo × 20 req/session |
| Optimistic year 2 | 100,000 | 40 | 400 | |
| Aggressive | 1,000,000 | 400 | 4,000 | Well beyond any near-term plan |

**A single Go process serving cached, indexed queries handles 4,000 RPS.** At every scenario above
including the last, request throughput is not a reason to distribute anything. Any argument for
splitting must come from somewhere other than RPS — and this is the single most common place these
decisions go wrong.

### Workload heterogeneity — this *is* a real argument

| Workload | Shape | Latency tolerance | Resource profile | Failure impact |
|---|---|---|---|---|
| Serve API | Spiky, sub-second | **None** | Low CPU, high concurrency | User-visible outage |
| Ingest sources | Bursty, I/O-bound | Hours | Many idle sockets, low CPU | Feed goes stale |
| Score + embed | Batch | Hours | **CPU-saturating** | Feed loses ranking |
| Parse resumes | Unpredictable | Seconds | **CPU + memory spikes, untrusted input** | Upload fails |
| Schedule | Steady, tiny | Minutes | Negligible, **must be singleton** | Polling stalls |

Two rows here carry actual weight:

- **Scoring saturates CPU.** If embedding 10,000 postings competes with API request handling in the
  same process, p99 latency moves. That is a genuine isolation requirement.
- **Resume parsing consumes untrusted binary input.** PDF parsers are a well-known source of
  crashes, unbounded memory growth, and parser-level exploits. This is a **security** isolation
  requirement, and it is the strongest single argument for a real network boundary in this system.

### Data coupling — this is the argument *against*

The feed query, which is the product's hot path, joins:

```
job_postings ⋈ user_job_scores ⋈ companies ⋈ posting_skills ⋈ watched_companies
```

Under a database-per-service rule this becomes either a distributed join at request time or
continuous denormalisation across four services. Both are strictly worse than one indexed query in
one Postgres.

**Prime Video's finding names this cost exactly.** Their video-quality service moved from distributed
serverless components orchestrated by Step Functions back to a single process, achieving a **90% cost
reduction** `[A-14]`. The bottleneck was not compute — it was **orchestration overhead and moving
data between components** via S3 as intermediate storage. Removing the network hops and passing data
in memory removed the cost. Our feed query has the same property: the expensive thing about splitting
it would be the data movement, not the computation.

### Team size — the decisive factor almost nobody weighs

JobTrack has **one developer**, plausibly growing to three to five.

The evidence here is unusually consistent: microservices benefits appear at team sizes **above 10–15
developers**, and below that threshold teams experience **net productivity loss** from coordination
overhead and infrastructure complexity `[B-12]`. The commonly cited triggers — deployment
coordination becoming a bottleneck, teams blocking each other on releases — are *organisational*
problems. With one team there is nothing to decouple.

Uber is the cautionary case at the other extreme: having reached roughly **2,200 critical
microservices**, they spent two years building DOMA specifically to *reduce* the complexity that
proliferation created, while retaining its benefits `[B-13]`. The complexity is real and the
remediation is expensive.

---

## Part 2 — The options, scored

| | **A. Single process** | **B. Modular monolith, multi-process** | **C. Full microservices** |
|---|---|---|---|
| Codebases / modules | 1 | **1** | 8–12 |
| Deployment units | 1 | **5–6** | 8–12 |
| Databases | 1 | **1** | 8–12 |
| Inter-component transport | function call | **function call + Postgres queue** | gRPC / message bus |
| Independent scaling | ✗ | **✓** | ✓ |
| CPU isolation (scoring vs API) | ✗ | **✓** | ✓ |
| Security isolation for untrusted input | ✗ | **✓ (one extracted service)** | ✓ |
| Distributed transactions needed | ✗ | **✗** | ✓ (saga + outbox) |
| Service mesh needed | ✗ | **✗** | ✓ |
| Ops surface for 1–5 devs | minimal | **low** | **prohibitive** |
| Refactor cost across boundaries | trivial | **low** | high |
| Local dev "run everything" | trivial | **trivial** | painful |
| Debuggability of a request | trivial | **easy** | needs distributed tracing to be usable at all |
| Path to 1M MAU | vertical only | **✓ documented** | ✓ |

**Option A** fails on CPU isolation and on running untrusted PDF parsing in the API process. Rejected.

**Option C** buys independent scaling and isolation — both of which Option B also provides — at the
cost of distributed transactions, a service mesh, a schema registry, per-service CI/CD, and an
operational burden that a one-to-five person team cannot carry while also building the product. It
solves an organisational problem we do not have. Rejected, with documented conditions under which
parts of it become correct.

---

## Decision

**A modular monolith in the codebase; multiple independently-scalable deployment units at runtime;
split along workload-isolation lines rather than domain lines; with exactly one genuinely
network-isolated service where security demands it.**

Concretely:

1. **One Go module, one release version, one database, one deploy pipeline.**
2. **Module boundaries enforced mechanically**, not by convention — an import-graph linter fails CI
   when `internal/matching` imports `internal/http`. This is the Shopify lesson: they run one of the
   largest Rails codebases in existence as a modular monolith, handling Black Friday peaks measured
   in **tens of terabytes per minute**, with boundaries enforced by static analysis (Packwerk)
   `[B-14]`. Boundaries that are enforced by a tool are real; boundaries maintained by discipline
   decay.
3. **Six binaries compiled from that one module**: `gateway-config`, `api`, `ingestor`, `matcher`,
   `scheduler`, `migrate`. Same code, different entrypoints, deployed and scaled independently.
4. **The deployment units do not call each other over the network.** They coordinate through Postgres
   — River for durable work, `LISTEN/NOTIFY` for wakeups. This is what removes the entire distributed
   systems tax: no saga, no outbox-to-broker, no service mesh, no circuit breakers between our own
   components, no distributed transaction ever.
5. **`resume-parser` is a real network-isolated service** with its own container, no network egress,
   read-only filesystem, seccomp profile, and hard memory limits. It is extracted for **security**,
   not scale — it is the only component that processes untrusted binary input.
6. **Module boundaries are designed as service boundaries.** Each module exposes a Go interface that
   is shaped like an RPC surface — value in, value out, no shared mutable state, no ambient
   transaction. Extraction to a network service is then a transport adapter plus a deployment change,
   not a rewrite.

This is Uber's DOMA idea applied at 1/1000th the scale: **domain boundaries expressed as clean public
interfaces with owned data**, minus the network hops we have no reason to pay for yet.

---

## Why this is genuinely "microservice-ready" and not a hedge

The property that makes microservices valuable is **independent deployability and scaling of
components with different characteristics**. We have that:

- `api` scales on request latency (HPA), `ingestor` and `matcher` scale on queue depth (KEDA), all
  independently, to zero if idle.
- A CPU-saturated `matcher` cannot affect `api` p99 — they are separate pods with separate limits.
- Any one can be rolled, rolled back, or scaled without touching the others.

The property that makes microservices *expensive* is **network boundaries between components that
share data**. We do not have that, and we do not want it.

The test for whether this is a real architecture or a compromise: **can we extract a service without
a rewrite?** Yes — the interface already exists, the data ownership is already declared, and the
extraction procedure is written down in
[service-topology.md §7](../service-topology.md#7-extraction-path). We have done it once already,
for `resume-parser`, which proves the seam works.

---

## Extraction triggers

Each is a measurable condition, not a feeling. When one fires, extract that module into a network
service; until then, do not.

| Module | Extract when | Why that number |
|---|---|---|
| `resume-parser` | **Already extracted** (v1) | Untrusted binary input; security, not scale |
| `matcher` | Embedding moves to GPU, **or** scoring backlog p95 > 30 min at max replicas | GPU nodes are a different node pool with different cost; scheduling them separately is the point |
| `ingestor` | Tracked sources > 50,000, **or** per-vendor blast radius becomes a recurring incident | Below this, per-source circuit breakers give the same isolation for free |
| `api` read path | Feed p95 > 120 ms with the DB below 50% CPU | Means the bottleneck moved into the app; split read/write before adding a read replica |
| Anything, for org reasons | Team exceeds **10 engineers** across ≥ 3 independent workstreams | The threshold at which the evidence says the coordination benefit turns positive `[B-12]` |

**Anti-trigger, stated explicitly:** "we might need to scale it differently someday" is not a
trigger. Neither is "microservices are more professional." A PR proposing an extraction must cite the
metric that fired.

---

## Consequences

**Good**

- One `docker compose up` runs the entire system locally. New contributor productive in under 15
  minutes ([dev-environment.md](../../engineering/dev-environment.md)).
- One database transaction spans any business operation. Application state transitions are atomic
  with their audit events, with no saga.
- A request is debuggable by reading one stack trace.
- Refactoring across module boundaries is a compiler-checked rename, not a coordinated multi-repo
  release.
- Infrastructure cost at 10k MAU is roughly 4 vCPU of app tier plus one Postgres
  ([scaling-and-capacity.md](../../operations/scaling-and-capacity.md#6-cost-model)).

**Bad, and accepted**

- One database is a shared failure domain. Mitigated by managed Postgres with sync standby, PITR, and
  per-component connection pool limits so a runaway `matcher` cannot exhaust connections needed by
  `api`. Pool sizing is in [service-topology.md §5](../service-topology.md#5-resource-and-connection-budgets).
- A single `go.mod` means a dependency upgrade affects everything at once. Mitigated by the
  dependency budget in [principles.md §P6](../../product/principles.md#p6--every-dependency-is-a-liability)
  — with ≤ 12 direct dependencies, this is a small surface.
- Module boundaries can rot. Mitigated by the import linter in CI, which is non-negotiable and has no
  exemption mechanism.
- We will be told this is "not real microservices." Correct, and deliberate.

---

## Revisit

Re-open this ADR when the team exceeds 10 engineers, when any extraction trigger fires twice in a
quarter, or when a required capability cannot be built without a network boundary. Not before, and
not because of a conference talk.

## Sources

`[A-14]` Prime Video serverless→monolith, 90% cost reduction ·
`[B-12]` team-size threshold for microservice benefit ·
`[B-13]` Uber DOMA, ~2,200 services and the two-year complexity reduction ·
`[B-14]` Shopify modular monolith, Packwerk-enforced boundaries at Black Friday scale.
Full citations: [evidence-ledger.md](../../research/evidence-ledger.md).
