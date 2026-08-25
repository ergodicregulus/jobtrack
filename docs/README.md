# JobTrack documentation

Thirty-odd documents is a lot. This page exists so you never have to read them all.

## Reading paths

**"I have 20 minutes and want to understand the product."**
[problem-statement](product/problem-statement.md) → [principles](product/principles.md) →
[feature-spec](product/feature-spec.md).

**"I'm going to write code this week."**
[principles](product/principles.md) → [system-architecture](architecture/system-architecture.md) →
[repository-structure](engineering/repository-structure.md) →
[dev-environment](engineering/dev-environment.md) → [coding-standards](engineering/coding-standards.md).

**"Is this microservices or a monolith, and can it scale?"**
[ADR-0008](architecture/adr/0008-service-decomposition.md) → [service-topology](architecture/service-topology.md) →
[scaling-and-capacity](operations/scaling-and-capacity.md).

**"I'm working on ingestion."**
[ingestion-pipeline](architecture/ingestion-pipeline.md) → [source-catalog](research/source-catalog.md) →
[ADR-0004](architecture/adr/0004-source-acquisition-policy.md) → [runbooks §source](operations/runbooks.md).

**"I'm working on matching."**
[matching-and-scoring](architecture/matching-and-scoring.md) →
[ADR-0006](architecture/adr/0006-hybrid-retrieval-and-scoring.md) →
[ADR-0007](architecture/adr/0007-resume-parsing-local-first.md) → [data-model §resume](architecture/data-model.md).

**"I'm on call."**
[runbooks](operations/runbooks.md) → [observability](engineering/observability.md) →
[deployment-zdt](operations/deployment-zdt.md).

**"Someone challenged a number in the UI."**
[evidence-ledger](research/evidence-ledger.md) → [verification-log](research/verification-log.md).

**"Do we need Redis / S3 / a CDN yet?"**
[caching-and-storage](architecture/caching-and-storage.md) — every answer has a numeric trigger, so
this is a query rather than an argument.

**"What actually works right now?"**

**"I'm an AI agent about to change something."**
[consistency-and-drift §10](engineering/consistency-and-drift.md#10-for-ai-agents-working-in-this-repo)
→ [principles](product/principles.md#anti-features). `make check` is the definition of done.

---

## Full index

### Product — *what and why*

| Document | What it settles |
|---|---|
| [problem-statement.md](product/problem-statement.md) | The market as of 2026, evidence-graded; the three mechanics that drive the design |
| [personas-and-perspectives.md](product/personas-and-perspectives.md) | Candidate, employer, ATS vendor and aggregator incentives — including whose interests we are *against* |
| [principles.md](product/principles.md) | Design principles, and the binding anti-feature list |
| [feature-spec.md](product/feature-spec.md) | v1 scope: screens, filters, states, acceptance criteria |
| [roadmap.md](product/roadmap.md) | v1 → v4, with the gate each phase must pass |

### Architecture — *how*

| Document | What it settles |
|---|---|
| [system-architecture.md](architecture/system-architecture.md) | Overview: containers, data flow, failure domains, module boundaries |
| [service-topology.md](architecture/service-topology.md) | Gateway tier, every deployment unit, scaling policies, contracts, connection budgets, vendor-neutrality contract |
| [data-model.md](architecture/data-model.md) | Full schema, ERD, indexes, partitioning, retention |
| [ingestion-pipeline.md](architecture/ingestion-pipeline.md) | Adaptive polling, conditional fetch, normalisation, 3-stage dedup, staleness |
| [matching-and-scoring.md](architecture/matching-and-scoring.md) | Config-driven scoring model, explainability, calibration, confidence |
| [api-design.md](architecture/api-design.md) | REST contract, pagination, errors, versioning, SSE |
| [caching-and-storage.md](architecture/caching-and-storage.md) | **When we need Redis / S3 / a CDN — and when Postgres is enough.** Every answer has a numeric trigger |
| [backend-performance.md](architecture/backend-performance.md) | Precompute, coalesce, batch, fan out; latency budget; degradation under load |
| [frontend-architecture.md](architecture/frontend-architecture.md) | Rendering strategy, performance budgets, design system, WCAG 2.2 AA |
| [adr/](architecture/adr/) | Eight decision records with alternatives and consequences. Start with [ADR-0008](architecture/adr/0008-service-decomposition.md) — monolith vs microservices, evaluated |

### Engineering — *working in the repo*

| Document | What it settles |
|---|---|
| [repository-structure.md](engineering/repository-structure.md) | Layout and the import rules that keep it honest |
| [dev-environment.md](engineering/dev-environment.md) | **Fully containerised** — `make dev` with only Docker and git on the host |
| [consistency-and-drift.md](engineering/consistency-and-drift.md) | **Precheck gates and drift detection** — how nothing goes stale. Includes the AI-agent checklist |
| [coding-standards.md](engineering/coding-standards.md) | Go and TS conventions, error handling, the comment policy |
| [testing-strategy.md](engineering/testing-strategy.md) | Test pyramid, golden files, contract tests, what we do not test |
| [observability.md](engineering/observability.md) | OTel setup, SLIs/SLOs, dashboards, alert design |
| [roadmap-to-completion.md](engineering/roadmap-to-completion.md) | The four build phases, resolved — and §5, the verification pass that found seven defects behind a green suite |
| [phase-5-production-readiness.md](engineering/phase-5-production-readiness.md) | **What we built and why, then what is next** — design voice, source coverage, deployment, pipelines, benchmarks, and which compliance regimes actually apply |

### Operations — *running it*

| Document | What it settles |
|---|---|
| [deployment-zdt.md](operations/deployment-zdt.md) | Expand/contract migrations, rollout, rollback, feature flags |
| [scaling-and-capacity.md](operations/scaling-and-capacity.md) | Napkin math to 100k MAU, bottleneck order, cost model |
| [security-and-privacy.md](operations/security-and-privacy.md) | Resume PII, threat model, India DPDP + GDPR posture |
| [runbooks.md](operations/runbooks.md) | Named failure modes and their fixes |

### Research — *the receipts*

| Document | What it settles |
|---|---|
| [evidence-ledger.md](research/evidence-ledger.md) | Every load-bearing claim, graded A/B/C, with source and caveat |
| [verification-log.md](research/verification-log.md) | **What has been re-checked against primary sources, what was wrong, what changed** |
| [source-catalog.md](research/source-catalog.md) | Every ATS/board: endpoint, fields, quirks, legal posture, tier |
| [competitive-landscape.md](research/competitive-landscape.md) | Existing tools, what each gets right, and where the gap is |

---

## Conventions used across these documents

**Evidence grades.** Claims carry `[A]`, `[B]` or `[C]` linking to the ledger.

- **A** — primary source, reproducible, or first-party documentation. Load-bearing; safe to design on.
- **B** — credible secondary source or a survey with stated methodology. Directionally trustworthy,
  numerically soft.
- **C** — contested, vendor-published, or anecdotal. Usable as a hypothesis, never as a justification.

This grading is carried over deliberately from the market research that preceded this project. It
exists because roughly a third of published job-search "fact" is content marketing by companies
selling resume services, and a design built on those numbers inherits their bias.

**Colour semantics** (used in diagrams and, later, the UI): teal = growing/good, rose =
shrinking/bad, ochre = contested/uncertain. Colour encodes direction, never decoration.

**Diagrams** are Mermaid, so they render in GitHub and stay diffable. If a diagram needs a
hand-drawn layout to be legible, that is usually a sign the design is too complicated.

**Status markers.** `DECIDED`, `PROPOSED`, `OPEN` at the top of any section that is not yet settled.
As of 2026-08-15 there are **no `OPEN` decisions** — ADR-0002 (frontend framework) was the last one
and is now settled on SvelteKit. Sections still marked `PROPOSED` are those that depend on data we
will only have after first ingestion, chiefly filter bucket boundaries and company hiring posture.
