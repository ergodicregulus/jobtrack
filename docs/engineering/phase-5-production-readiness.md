# Phase 5 — Production readiness, and giving the product a voice

> **Status:** PLAN, written 2026-08-25. Part I is verified against the running
> stack. Part II is the proposal, and every recommendation in it carries either a
> measurement from our own system or an external source with its date.
>
> Where a widely-believed claim turned out to be wrong, it is corrected in place
> rather than quietly dropped — including one the author of this plan was asked
> to build on. See [§11](#11-scale-and-resource-efficiency).

---

# Part I — Where we actually are

## 1. The system, in numbers

Queried from the live database on 2026-08-25, not estimated.

| | Value |
|---|---|
| Live postings | **13,445** |
| Companies / sources polled | **65 / 65**, 0 erroring |
| Postings in India | **1,032** |
| Scores computed | n/a — computed per request since [ADR-0016](../architecture/adr/0016-scores-are-computed-not-materialised.md), not stored |
| Skill vocabulary | **113** canonical terms |
| Postings with a disclosed salary | **1,969** (14.6%) |
| Schema migrations | **17**, all applied, no drift (335 catalogue facts) |
| Go source files / Svelte components | **76 / 22** |
| End-to-end tests | **43**, three consecutive clean runs |
| First-load JS (gzipped, `/jobs`) | **75.1 KB** of a 100 KB budget |
| CSS (gzipped) | **10.5 KB** of a 20 KB budget |
| Time to score one posting (62 users) | **0.49 s** |
| Deployment units | **5** (api, ingestor, scheduler, migrate, resume-parser) — matcher removed by [ADR-0016](../architecture/adr/0016-scores-are-computed-not-materialised.md) |

**Gates that exist and pass today:** `make check` (fmt, vet, lint, race tests,
build, frontend tests, performance budget), `make test-e2e`, `make ui-audit`,
`make drift-check`, `make test-golden`, `make coverage`.

## 2. Backend — the decisions and where they came from

| Decision | Source | Consequence we can measure |
|---|---|---|
| **Go, stdlib first** — `net/http` `ServeMux`, no router framework | [ADR-0001](../architecture/adr/0001-go-for-backend.md) | Six binaries, one `go.mod`. Every dependency has a written justification |
| **Postgres as the only datastore** — FTS via `tsvector`, queue via River, pub/sub via `LISTEN/NOTIFY`, cache in-process LRU. (`pgvector` is installed and its tables exist, but **nothing writes to them** — see the implementation note on [ADR-0006](../architecture/adr/0006-hybrid-retrieval-and-scoring.md)) | [ADR-0003](../architecture/adr/0003-postgres-single-datastore.md) | No Redis, no Elasticsearch, no broker. Every alternative has a **numeric trigger** in [caching-and-storage](../architecture/caching-and-storage.md); none has fired |
| **River for background jobs**, transactional enqueue | [ADR-0005](../architecture/adr/0005-river-background-jobs.md) | A job is enqueued in the same transaction as the write it depends on. Separate queues: `score` (10 workers), `score_bulk` (2), `maintenance` |
| **Public first-party ATS feeds and JSON-LD only.** No accounts, no auth bypass, no scraping | [ADR-0004](../architecture/adr/0004-source-acquisition-policy.md) | Legal posture is contractual-exposure-free by construction. See [§7](#7-sources--the-honest-ceiling-and-how-to-raise-it) |
| **Explainable weighted scoring**, not a black box | [ADR-0006](../architecture/adr/0006-hybrid-retrieval-and-scoring.md) | Every score replays the five components that produced it, from what the scorer stored |
| **Abstention credit measured, not chosen** — 0.25, the readable population's mean | [ADR-0011](../architecture/adr/0011-abstention-credit-calibration.md) | Unreadable postings in the top 100 fell from 42 to 11 |
| **Deterministic local resume parsing**, isolated service, no egress | [ADR-0007](../architecture/adr/0007-resume-parsing-local-first.md), [ADR-0012](../architecture/adr/0012-pdf-extraction-via-subprocess.md) | PDF/DOCX/text with zero new `go.mod` entries. Parser holds no DB credentials and is default-deny egress |
| **Expand/contract migrations**, `CREATE INDEX CONCURRENTLY` always | [deployment-zdt](../operations/deployment-zdt.md) | 17 migrations, every one backward-compatible with the previous release |

## 3. Frontend — the decisions and where they came from

| Decision | Source | Consequence |
|---|---|---|
| **SvelteKit 2 / Svelte 5 runes**, server-rendered | [ADR-0002](../architecture/adr/0002-frontend-framework.md) | 75.1 KB first-load JS. Chosen on bundle-size evidence over the original Next.js preference |
| **Works with JavaScript off** | [frontend-architecture](../architecture/frontend-architecture.md) | Filters are links, forms are forms. The filter sheet is `<details>` — no JS, no focus trap |
| **State in the URL** | same | Every filtered view is shareable and cmd-clickable |
| **Performance budgets enforced in CI** | [CLAUDE.md](../../CLAUDE.md) | 100 KB JS / 20 KB CSS / INP p75 ≤ 200 ms / `GET /v1/jobs` p95 ≤ 120 ms |
| **One density**, removed as a setting | design session 4 | Its documented accessibility justification was false — it changed padding only, never type or target size |

## 4. UI/UX — the decisions and where they came from

Six design sessions produced the current interface. The rules that survived:

- **Colour encodes direction, never decoration.** `--grow`/`--shrink` are named
  for what they mean, not what they look like.
- **Bands lead, numbers follow** — `91 Strong fit`, never a bare figure.
- **`null` is not `0`.** Undisclosed salary says undisclosed; an unscored posting
  shows `—`.
- **Matches quiet, gaps loud** — then capped at two gap chips plus "+N more",
  because once extraction improved, five gaps per card made a wall of red.
- **Gap chips carry a `−` prefix**, so a gap reads without colour at all.
- **Contrast is computed, never eyeballed.** 33 assertions over the token set in
  both themes, with alpha compositing — a ratio measured against a translucent
  colour answers a question nobody is asking.
- **Targets ≥24px** (WCAG 2.2 SC 2.5.8), 44px on `@media (pointer: coarse)`.
- **`make ui-audit`** loads every page at 390/834/1440 and checks the invariants
  mechanically. It found 148 issues on its first run, ten of them real.

Three of the design export's own colour values failed AA and were taken one step
darker: `--grow-ink` `#0d9488`→`#0f766e`, `--uncertain-ink` `#b45309`→`#92400e`,
`--fg-subtle` `#78716c`→`#6b645f`.

## 5. What is measurably weak

Stated plainly, because the plan in Part II is mostly about these.

| Weakness | The measurement |
|---|---|
| **The interface has no voice** | The accent is `#4f46e5` — Tailwind indigo-600 — on `ui-sans-serif, system-ui`. Those are the two most-repeated choices in machine-generated UI, and no amount of correct spacing compensates. [§6](#6-design--the-homepage-and-the-filters) |
| **Nothing is remembered between visits** | A user rebuilds their filters every session. No saved searches, no alerts, no digest |
| **Coverage is narrow** | 3 ATS vendors, 65 companies, 13,445 postings. Workday alone would multiply this |
| **A third of the corpus has no description yet** | SmartRecruiters bodies fill at 250/poll; Bosch is ~26% through its first real sweep |
| **The model has no notion of field or seniority** | `current_title` and `target_title` exist; the scorer uses neither. "Product Support Specialist" scores 84.6 for a backend engineer |
| **Only 14.6% of postings disclose salary** | 1,969 of 13,445. Compensation filters are therefore weak by data, not by design |
| ~~No CD, no signed artefacts, no SBOM~~ | **Corrected 2026-08-25.** `release.yml` generates an SBOM (`anchore/sbom-action`), scans (`anchore/scan-action`), signs keylessly (`cosign`) and attaches SLSA provenance; `deploy.yml` verifies the signature by identity before rollout |
| **No load testing** | Still true. `make ui-audit` is **no longer** local-only — CI runs it in the `accessibility` job and uploads screenshots on failure. Server-side latency budgets are still checked by hand |

---

# Part II — The plan

## 6. Design — the homepage and the filters

### 6.1 The diagnosis is specific, not a matter of taste

"It looks AI-made" is a real and diagnosable property, and our tokens confirm it.
The pattern the design press converged on through 2026 is *"purple gradient,
Inter font, four cards in a grid"* — models trained on the average of the web
reproduce the average of the web ([925 Studios](https://www.925studios.co/blog/ai-slop-web-design-guide),
[Pimp my Type](https://pimpmytype.com/about-ai-design/)).

We have two of the three:

| Ours today | Why it reads as generic |
|---|---|
| `--accent: #4f46e5` | Tailwind's default `indigo-600`, the single most-used accent on the web |
| `--font: ui-sans-serif, system-ui, …` | No personality by definition — it renders as whatever the OS ships |
| Landing hero + four cards + footer | The exact layout skeleton named in the critique |

**The warm stone base is not the problem** — `#fbfaf9` ground and `#1c1b19` ink
are a considered choice and should stay. The accent and the type are the problem,
and they are the two cheapest things to change.

The prescription in the sources is consistent: *"typography is the single fastest
way to escape AI slop"*, and the named exemplars all commissioned or modified
type — Linear (custom-modified), Stripe (bespoke serif headline + clean sans
body), Vercel (Geist). We cannot commission a typeface; we can make a
**deliberate, specific pairing** instead, which achieves most of the distance.

### 6.2 Typography — a real pairing, not a system stack

Proposal, all open-licence and self-hostable (no CDN — our CSP forbids it, and
self-hosting removes a third-party request from every page load):

| Role | Face | Why this one |
|---|---|---|
| Headlines, hero, section heads | **Instrument Serif** or **Fraunces** (variable, optical size axis) | A serif headline against a sans body is the Stripe move: it reads as *edited*, not generated. Fraunces' `SOFT`/`WONK` axes give a voice no default stack has |
| Body, UI, controls | **Inter Tight** or **Public Sans** | Tighter than Inter's defaults; Public Sans is a US federal design-system face, which is a real accessibility pedigree rather than a fashion |
| Numbers — scores, salary, counts | Body face with `font-variant-numeric: tabular-nums` | Already set (`"tnum" 1`). Scores must not jitter between rows |

**Budget check.** Two variable fonts, subset to Latin, `woff2`, ~28–40 KB
combined. Our CSS budget has 9.5 KB of headroom and fonts are not counted in the
JS budget — but they *are* on the critical path, so: `font-display: swap`,
`preload` the two files actually used above the fold, and a metric-compatible
fallback so the swap does not reflow. If the pair cannot land under **45 KB
total**, use one face with two optical sizes instead.

### 6.3 Colour — a palette that is ours, and provably accessible

Move the palette generation to **OKLCH**. It is perceptually uniform: equal
numeric changes produce equal perceived changes, so a ramp built at fixed
lightness steps has predictable contrast across every hue — unlike HSL, where the
same lightness value in two hues yields very different luminance
([ColorArchive](https://colorarchive.org/guides/oklch-color-space-guide/),
[Atmos](https://atmos.style/glossary/oklch-color-space)). Native in CSS since
2023 and supported in all current browsers.

What this buys us concretely: **our contrast test currently verifies pairings
after the fact.** With an OKLCH ramp the lightness step *is* the contrast
guarantee, and the test becomes a regression check rather than a discovery
mechanism.

The accent should move off indigo. Selection criteria, in order:

1. **It must not collide with the semantic colours.** `--grow` teal and
   `--shrink` rose already carry meaning; the accent must be distinguishable from
   both for the most common colour-vision deficiencies.
2. **It must hold 4.5:1 as text on `#fbfaf9` and on `#131211`**, in both themes,
   composited.
3. **It should be warm-adjacent**, because the ground is warm stone. An
   ink-blue or a deep aubergine sits with `#1c1b19` in a way indigo does not.

Candidate directions to test against the corpus in situ, not in a swatch grid:
a deep **ink/navy** (authoritative, reads as "instrument"), a dark
**terracotta/rust** (warm, editorial, distinctly not-SaaS), or a muted
**forest** — with the caveat that forest crowds `--grow` teal and is likely out
on criterion 1.

**Deliverable:** an OKLCH ramp generator in `web/src/lib/palette.ts` that emits
the token block, plus the existing contrast suite extended to assert the ramp's
own invariants. Never hand-pick a hex again.

### 6.4 The homepage — soul from the corpus, not from stock

The instruction "it needs quotes and images" is right about the symptom and we
must be careful with the cure. **We have no users, so we have no testimonials,
and inventing one would be the single worst thing this product could do** — it is
a fabricated claim on the page whose entire pitch is that we do not fabricate.
Stock photography of smiling people at laptops is the same lie in another
medium.

The honest and more distinctive answer: **the corpus is the content.**

| Section | What it shows | Where the content comes from |
|---|---|---|
| **Hero** | A real score breakdown for a real posting, including an abstaining row | `GET /v1/market` + one live posting. Already built; keep it and give it the new type |
| **"What we can see today"** | 13,445 live postings · 65 companies · 3 ATS vendors · updated N minutes ago | Live query. A number that moves while you watch is proof of life no illustration provides |
| **"What we cannot see"** | LinkedIn, Naukri, Indeed — named, with the reason, linked to ADR-0004 | The anti-feature list. **This is the page's most distinctive asset** and no competitor will copy it |
| **"How a score is built"** | The five weights, as a static diagram with real numbers | `matching-and-scoring.md`. An SVG we draw, not a stock graphic |
| **Quotes** | Cited findings, attributed, with grades — *"48.7% of live postings yielded zero extracted skills"* | [evidence-ledger](../research/evidence-ledger.md). Real quotes from real measurements, which is what a quote is for |
| **Imagery** | Editorial illustration or generated texture, used sparingly, `<img>` with real `alt` | Must be original or licence-clean. SVG preferred: it scales, it themes, and it costs nothing against the budget |

Rules for the imagery, so this does not undo the budget work:

- **SVG first.** Raster only where a photograph is genuinely the right answer.
- If raster: **AVIF with WebP fallback**, `srcset` at 1x/2x, explicit
  `width`/`height` to reserve layout, `loading="lazy"` below the fold,
  `fetchpriority="high"` on the one hero asset if any.
- **Images are not in the JS budget but they are in the user's data plan.** Add a
  separate CI budget: **≤ 150 KB of images above the fold**, measured the same
  way `check-budget.js` measures JS.
- Every decorative image `alt=""`; every informative one described.

### 6.5 Filters and preferences — the thing that makes it a tool

Today the filter rail is genuinely good — links, live counts, URL state,
JS-off — and it forgets everything the moment you leave. The UX literature is
unambiguous that saved filter combinations are the retention mechanic for a
search product, and that users need **explicit confirmation** that a save
happened, since a silent save leaves them unsure it worked
([LogRocket](https://blog.logrocket.com/ux-design/filtering-ux-ui-design-patterns-best-practices/),
[UX Design World](https://uxdworld.com/how-to-improve-advanced-search-ux/)).

**The architecture already supports this for free.** Filter state is the URL. A
saved search is therefore a stored query string with a name — no new filter
model, no duplication of logic.

| Feature | Design | Why this shape |
|---|---|---|
| **Saved searches** | `saved_searches(id, user_id, name, query, created_at, last_run_at, last_seen_max_posted_at)` | `query` is the URL query string. Replaying a saved search is a redirect |
| **Auto-named, user-editable** | "Backend · India · ≥₹25L · Remote" generated from the active facets | eBay's pattern; the auto-name is right ~90% of the time and removes a naming step |
| **Explicit confirmation** | Inline "Saved" state on the button, not a toast | A toast that disappears is not confirmation for anyone using a screen reader or reading slowly |
| **New-since-last-run count** | Compare `max(posted_at)` against `last_seen_max_posted_at` | We already built exactly this mechanic for "new since your last visit" (migration 0014). Same idea, per search |
| **Digest email** | Opt-in, daily or weekly, one message per user not per search | Requires an email sender — the first genuinely new piece of infrastructure this plan proposes. See [§8.4](#84-the-one-new-dependency-email) |
| **Default search** | One saved search marked default, applied on landing at `/jobs` | This is the "self-sufficient environment" the brief asks for: open the app, see your feed |

**Preferences that should persist and currently do not:** results-per-page,
default sort, whether the salary filter is expanded, dismissed postings ("not
interested"), and the currency to display compensation in. All belong on
`/v1/me/preferences`, which already exists and is under-used.

**One thing to resist.** Do not add a "recommended for you" feed that hides the
ranking. The product's whole claim is that the score shows its working; a
personalised stream that cannot be explained is the anti-feature this project
exists to avoid, and it is squarely prohibited by
[principles](../product/principles.md).

## 7. Sources — the honest ceiling, and how to raise it

### 7.1 LinkedIn: the answer is no, and the reason is not timidity

The brief asks for LinkedIn because *"LinkedIn releases a lot in a day"*. That is
true and it does not change the answer, for two independent reasons.

**First, there is no legitimate technical route.** LinkedIn's Job Posting API
exists for *distribution partners* — ATS and HR-tech vendors pushing jobs **into**
LinkedIn. It permits creating, updating, renewing and closing listings. It does
**not** provide access to LinkedIn's job search index. Access additionally
requires Partner Program approval, an active job-slot agreement, and
authorisation from a client's Recruiter contract
([Phyllo](https://www.getphyllo.com/post/linkedin-api-access-in-2026-partner-program-approval-timeline-alternatives),
[LinkedIn Developer Network](https://developer.linkedin.com/docs/v1/job-posting/linkedin-job-credits-partner-program)).
There is no tier of that programme that gives us what we would want.

**Second, [ADR-0004](../architecture/adr/0004-source-acquisition-policy.md)
already settled the scraping route with the case law.** Scraping public data is
not a CFAA violation after *hiQ v. LinkedIn* — but **hiQ still lost**, on breach
of the User Agreement it accepted by creating accounts, ending in a consent
judgment. The binding rule that follows is rule 1 of that ADR: *no account is
ever created on any source*. That is what keeps us outside contractual terms
entirely, and it is worth more than any amount of inventory.

Overturning this needs a superseding ADR, not a PR.

**What is permitted and worth building:** a user pastes a LinkedIn job URL and we
**resolve it to the underlying first-party requisition**. User-initiated,
resolves *toward* the source, already blessed by ADR-0004. It converts LinkedIn
from an inventory source into a lookup, which is the honest use of it.

### 7.2 What we can add — verified live on 2026-08-25

Every endpoint below was called from this machine today. Findings, not claims:

| Vendor | Endpoint | Auth | Verified result | Status |
|---|---|---|---|---|
| **Workday** | `POST {tenant}.myworkdayjobs.com/wday/cxs/{tenant}/{site}/jobs` | **None** | NVIDIA `total: 2000`; Accenture `total: 2000` | ⭐ **Biggest single win** |
| **Personio** | `{company}.jobs.personio.de/xml` | None | Valid XML, positions returned | Ready |
| **Recruitee** | `{company}.recruitee.com/api/offers/` | None | Documented; slug needed | Ready |
| **Workable** | `apply.workable.com/api/v1/widget/accounts/{account}` | None | Account resolved; needs a live-vacancy slug to confirm shape | Ready |
| **BambooHR** | `{company}.bamboohr.com/careers/list` | None | Empty for the slug tried; re-verify before building | Verify first |
| **iCIMS** | public customer feeds | None reported | Not independently verified by us | Verify first |
| **Teamtailor** | requires an API key per customer | **Key** | — | ❌ Out of scope (ADR-0004) |
| **JazzHR** | XML feed and REST both opt-in, per-customer key | **Key** | — | ❌ Out of scope |

Endpoint patterns cross-checked against
[a public survey of ATS feeds](https://dev.to/agenticemail/every-major-ats-has-a-public-job-feed-here-is-how-to-read-them-all-3k10).

**Workday deserves its own note.** It is the enterprise default and it is heavily
used in India — Accenture, and most large IT services and captive centres. Its
careers site is a single-page app talking to an unauthenticated JSON endpoint.

Two cautions, both of which we have already learned the hard way:

1. **It is an undocumented internal API**, not a published contract. That is a
   different durability class from Greenhouse's documented boards API, and the
   adapter must be written expecting it to change. It also sits at the edge of
   ADR-0004's spirit — the ADR's durability argument ("the vendor's paying
   customer depends on it") holds, since this endpoint *is* the customer's
   careers page; but it should be recorded as **Tier 1b**, not Tier 1.
2. **Both tenants returned exactly `total: 2000`.** That is a cap, not a
   coincidence — and it is precisely the trap that truncated BoschGroup to 41% of
   its board for a week ([§5.2 of the roadmap](roadmap-to-completion.md)). A
   Workday adapter must walk facets (location, category, date) to get beneath the
   cap, and must have a test that fails when a board exceeds what one query
   returns.

### 7.3 Company career pages — Tier 2, already designed and not yet built

[ADR-0004](../architecture/adr/0004-source-acquisition-policy.md) permits
`schema.org/JobPosting` JSON-LD, and the source catalogue documents it. This is
how we reach companies that post directly on their own site with no ATS we
recognise. `title` and `datePosted` appear in ~99% of JSON-LD postings;
`baseSalary` and `employmentType` in ~80%.

It needs: `robots.txt` fetched, cached 24 h and honoured including `Crawl-delay`;
an identifying User-Agent with a contact URL; per-source parse confidence, since
quality is uneven.

### 7.4 India specifically

The thinnest market in the corpus and the one this project's own user is in.

- **Workday** is the largest available win here by a wide margin — it is what the
  Indian enterprise and IT-services market runs on.
- **Darwinbox** (Indian enterprise, 1,000+ employee conglomerates), **Keka** and
  **Zoho Recruit** (mid-market, startups, IT services, BPO) cover most of the
  rest. Zoho notably has **no single endpoint listing all jobs** — per-job
  detail only on legacy sites — so it needs verification before any commitment.
  Darwinbox and Keka public-feed availability is **unverified** and must be
  checked the way we checked Workday: call it and look.
- **Naukri stays out**, per ADR-0004, and the framing there still holds: Naukri's
  real channel is inbound recruiter profile-search, not outbound applications, so
  a JobTrack that ignores it is less incomplete than it first appears.

### 7.5 Sequencing, by expected value

| Order | Work | Why first |
|---|---|---|
| 1 | **Finish the SmartRecruiters body sweep** | Already fixed and running; costs nothing but patience, and unlocks re-measuring [A-00f](../research/evidence-ledger.md#a-00f) |
| 2 | ~~**Workday adapter**~~ | ✅ **Built 2026-08-25.** Facet-walking recovers **2,631** postings from NVIDIA against a reported `total` of 2,000 — 31% more than a naive read. Tier 1b in the catalogue |
| 3 | **Personio + Recruitee + Workable** | Three small, documented, unauthenticated adapters sharing one shape. Mostly EU inventory |
| 4 | **JSON-LD career-page tier** | Unlocks companies with no recognised ATS; more machinery (robots.txt, crawl scheduling, confidence) |
| 5 | **Verify Darwinbox / Keka / iCIMS / BambooHR** | Call them and look before planning anything |
| 6 | **LinkedIn URL resolution** | Small, user-facing, permitted, and turns the biggest gap into a feature |

## 8. Deployment — to any environment

### 8.1 The target shape

Six deployment units ([ADR-0008](../architecture/adr/0008-service-decomposition.md)),
one image per binary, configuration by environment variable, no state in any
container. That shape already deploys anywhere; what is missing is the manifests
and the pipeline, not the architecture.

| Environment | What runs it | Status |
|---|---|---|
| **Local dev** | `docker compose` + `air` hot reload | ✅ Built, 9 services |
| **Single VM / homelab** | `docker compose -f docker-compose.prod.yml` | ✅ **Built.** Pinned images, healthchecks, no bind mounts, Caddy for TLS. One replica each, so a rollout is a brief outage — stated in the file |
| **Kubernetes** | manifests in `deploy/k8s/` | ✅ **Complete.** All five units, plus ConfigMap, Secret keys, Ingress, HPA, PDBs and a default-deny NetworkPolicy set |
| **Docker Swarm** | `docker stack deploy` | ❌ **Not shipped, deliberately.** §8.2's own evidence says do not start new projects on it; adding a deployment path nobody asked for and nobody maintains is the opposite of minimal |
| **Managed PaaS** (Fly, Render, Railway) | one process group per service | ⚠️ Works today with a Procfile-equivalent; no manifests |

### 8.2 Docker Swarm — supported, and not recommended

Stated with the evidence because the brief asks for it explicitly.

Mirantis has **committed to supporting Swarm through 2030**, so it is not
abandoned in the sense of disappearing. But it has **received no meaningful new
features since 2019**, has no published roadmap, and the consensus across
independent 2026 reviews is *"do not start new projects on Docker Swarm"*
([Mirantis](https://www.mirantis.com/blog/mirantis-guarantees-long-term-support-for-swarm/),
[Virtualization Howto](https://www.virtualizationhowto.com/2026/03/is-docker-swarm-still-safe-in-2026/),
[Coding Protocols](https://codingprotocols.com/blog/docker-swarm-vs-kubernetes-vs-nomad)).

**Our recommendation:** ship a `docker-stack.yml` because it is ~40 lines and
makes the "any environment" claim true, but document it as the *small-deployment*
path and make Kubernetes the reference target. Swarm cannot express the two
things our zero-downtime story depends on — `PodDisruptionBudget` and ordered
`initContainer` migration gating — so a Swarm deployment accepts a weaker
guarantee, and that must be written down rather than discovered.

**Nomad** is the credible middle path if Kubernetes proves too heavy, but adding a
third orchestrator to support is a cost with no current trigger.

### 8.3 What to build

1. **Complete the Kubernetes manifest set** — `ingestor`, `scheduler`,
   `web`, plus `ConfigMap`, `Secret` (external-secrets or SOPS,
   never committed), `Ingress`, `HorizontalPodAutoscaler` for `api`, and `NetworkPolicy` for every service, not just `resume-parser`.
2. **A Helm chart or Kustomize overlays** — `base/` plus `overlays/{dev,staging,prod}`.
   Kustomize is the lighter answer and needs no new dependency.
3. **`docker-compose.prod.yml`** — no bind mounts, no `air`, pinned image
   digests, healthchecks, `restart: unless-stopped`, and Caddy or Traefik for TLS.
4. **`docker-stack.yml`** for Swarm, with its limitations documented.
5. **The migration gate.** `migrate` must run to completion before any new
   application pod serves traffic. In Kubernetes that is a `Job` plus an
   `initContainer` that blocks on `migrate-verify`; in Compose it is
   `depends_on: {condition: service_completed_successfully}`, which we already
   use in dev.

### 8.4 The one new dependency: email

Saved-search digests need outbound email. This is the only genuinely new piece of
infrastructure in this plan, and per the dependency rule it needs its
justification stated: **what it replaces** — nothing, it is new capability; **what
breaks if abandoned** — digests stop, the product still works.

Keep it at arm's length: one `Notifier` interface, one SMTP implementation
(stdlib `net/smtp`, zero dependencies), and the provider behind configuration so
swapping SES for Postmark is an env var. Do **not** take an SDK.

## 9. Pipelines — what exists, and the guardrails to add

### 9.1 What we already have

`.github/workflows/ci.yml`, 281 lines, seven jobs: **static** (fmt, vet, build,
tidy, doc links), **unit** (race), **integration** (real Postgres, migrations
apply from empty and are idempotent), **images** (build all, assert minimal and
no shell), **frontend** (type-check, vitest, build, performance budget),
**e2e** (Playwright against a real stack), **security** (govulncheck, dependency
budget).

That is a stronger baseline than most projects have. The gaps are supply chain,
delivery, and the checks we currently run by hand.

### 9.2 The guardrails to add, in priority order

| Guardrail | Tool | Why it earns its place |
|---|---|---|
| **SBOM per image** | `syft` → SPDX/CycloneDX, attached to the release | You cannot answer "are we affected by this CVE" without an inventory. It is also increasingly a procurement requirement |
| **Image vulnerability scan** | `grype` or `trivy`, failing on High/Critical with an expiring allowlist | An allowlist without an expiry date is a permanent exception |
| **Keyless signing** | `cosign` with GitHub OIDC | No key to store or rotate. Proves an image came from our repo and our workflow |
| **Build provenance (SLSA 3)** | `slsa-github-generator` / GitHub attestations | Provenance generated by an isolated builder is non-falsifiable, which is the property that makes SLSA 3 mean anything ([GitHub](https://github.blog/security/supply-chain-security/slsa-3-compliance-with-github-actions/), [slsa.dev](https://slsa.dev/blog/2023/02/slsa-github-workflows-container-ga)) |
| **Static analysis** | CodeQL (Go + JS) on PR and schedule | Catches classes `go vet` does not |
| **Secret scanning** | `gitleaks` in CI + GitHub push protection | The cheapest catastrophic-failure prevention available |
| **Accessibility gate** | `make ui-audit` in CI | It is already written and already green. Leaving it out of CI is the only reason it can regress |
| **Migration backward-compat** | `make migrate-verify` against the previous release tag | The expand/contract rule is currently enforced by review. It should be enforced by CI |
| **Load test** | `k6` smoke on PR, full on merge, soak nightly | The three-tier model the practitioner literature converges on ([ARDURA](https://ardura.consulting/blog/load-testing-complete-guide-2026/)) |
| **Pinned actions** | SHA-pinned `uses:` + Dependabot | A tag is mutable; a SHA is not |

The tooling maturity note is worth repeating because it changes the estimate:
GitHub's built-in attestations plus `cosign` get most projects to **SLSA Level 2
in an afternoon**, with Level 3 a further step via the isolated generator.

### 9.3 The pipeline set

- **`ci.yml`** (exists) — extended with CodeQL, gitleaks, `ui-audit`,
  `migrate-verify`, and a k6 smoke test.
- **`release.yml`** (new) — on tag: build multi-arch images, generate SBOM, scan,
  sign with cosign, attach SLSA provenance, push to GHCR by digest.
- **`deploy.yml`** (new) — on release published or manual dispatch: verify the
  signature and provenance **before** deploying, run migrations as a gated Job,
  roll out, smoke-test, and roll back automatically on failure.
- **`nightly.yml`** (new) — soak test, full `grype` re-scan of published images
  against today's vulnerability database (an image that was clean at build time
  is not clean forever), and `drift-check` against staging.

Skeletons for `release.yml` and `deploy.yml` are written and committed alongside
this plan.

## 10. Benchmarks

We have budgets and no benchmark harness. The budgets in
[CLAUDE.md](../../CLAUDE.md) and
[backend-performance](../architecture/backend-performance.md) are:

| Budget | Limit | Enforced today? |
|---|---|---|
| First-load JS (gzipped, `/jobs`) | ≤ 100 KB | ✅ CI |
| CSS (gzipped) | ≤ 20 KB | ✅ CI |
| INP p75, 4× CPU throttle | ≤ 200 ms | ✅ By hand (`make inp`) — **measured 72 ms**, 2026-08-25 |
| `GET /v1/jobs` p95 server time | ≤ 120 ms | ✅ CI (`make load-test`) — **measured 75.6 ms**, 2026-08-25 |
| `GET /v1/me/dashboard` p95 | ≤ 400 ms | ✅ CI (`make load-test`) — **measured 178 ms**, 2026-08-25 |
| Ingest → visible, tier A source | ≤ 90 min median | ✅ By hand (`make ingest-latency`) — **measured 56–65 min p50**, 2026-08-25 |

**What to build:**

- **k6 scenarios** using `ramping-arrival-rate`, not `ramping-vus`. The
  distinction matters: arrival-rate fires a fixed request rate regardless of how
  the app is performing, so a regression shows up as latency rather than being
  masked by the load generator slowing down with the app.
- **Pass criteria as thresholds in the script**, so k6 exits non-zero itself:
  p95 under budget, error rate under the budget, and no threshold crossed for the
  duration.
- ~~**Lighthouse CI**~~ — rejected, and INP measured directly instead
  (`make inp`, **72 ms p75**, budget 200). Lighthouse cannot measure INP: it is a
  field metric needing real interactions, so a lab run reports Total Blocking
  Time as a stand-in. TBT is main-thread busyness during load; INP is the delay
  a person feels on click. Playwright is already here, drives the real
  interactions, and reads the browser's own Event Timing entries — the metric
  the budget actually names, with nothing added to `package.json`. See
  [A-32](../research/evidence-ledger.md#a-32).
- ~~**An ingest-latency probe**~~ — done, `make ingest-latency`. Tier A is **within
  budget at 56–65 min p50**, and the interesting part is what it took to get a
  number that means anything: the query has four exclusions and dropping any one
  of the first three changes the answer by between one and two orders of
  magnitude. See [A-31](../research/evidence-ledger.md#a-31). The naive version
  reports 72 days and is measuring backfill.

**A caution from this month.** The dashboard's slow query measured 68 ms warm and
13,687 ms under write churn — the same statement, the same data. A benchmark run
against a freshly-vacuumed idle database would have reported the 68 ms and told
us nothing. **Benchmarks must run against a system under representative
background load**, or they measure the benchmark.

## 11. Scale and resource efficiency

### 11.1 The Pokémon example, corrected

The brief invokes the story of Nintendo fitting Kanto into Pokémon Gold/Silver
through Iwata's compression. **That story is not true**, and this document would
be a poor place to repeat it.

Did You Know Gaming's code-level research established that Iwata's algorithm for
Gold/Silver was about **speed, not size** — it saved a fraction of a second at
battle start and similar transitions, adapted from HAL Laboratory's EarthBound
code. **Kanto fit because Game Freak used a 1 MB cartridge instead of the
previous 512 KB** — a hardware change, not a compression miracle
([Nintendo Everything](https://nintendoeverything.com/satoru-iwatas-work-on-pokemon-gold-and-silver-clarified/),
[Nintendo Life](https://www.nintendolife.com/news/2023/10/new-details-emerge-on-satoru-iwatas-work-on-pokemon-gold-and-silver)).

The real story is the better engineering lesson, and it is one we just lived:

- **Iwata optimised the thing that was actually slow**, measured — battle
  transitions — rather than the thing that sounded impressive. We spent this
  month discovering that scoring was 65× slower than it needed to be *in network
  round trips*, not in computation. Same shape.
- **Sometimes the correct answer is the bigger cartridge.** Game Freak did not
  contort the game to fit 512 KB; they used the part that had become available.
  Raising `maxPages` from 20 to 60 tripled our corpus for one constant. Not every
  constraint deserves cleverness; some deserve to be lifted.

### 11.2 What efficiency means here, with our numbers

| Property | Today | Why it holds |
|---|---|---|
| **Datastore count** | 1 | No Redis, no Elasticsearch, no broker. Every alternative has a numeric trigger; none has fired |
| **Vector store trigger** | n/a — nothing writes embeddings at all, see the note on [ADR-0006](../architecture/adr/0006-hybrid-retrieval-and-scoring.md) | — |
| **PgBouncer trigger** | 20 connections in use vs 400 sustained | 5% of the trigger |
| **First-load JS** | 75.1 KB | Smaller than most single React vendor chunks |
| **Score write cost** | none — nothing is written | ADR-0016: scoring is a read-path computation at 1.84 µs |
| **Ingest at steady state** | ~1 request per board per poll | Content-hash short-circuit; ~90% of polls are 304 or identical |
| **Resume parser** | 512 Mi cap, in-memory `/tmp`, no egress | A hostile PDF kills a child process, not the service |

### 11.3 The scaling work that is actually next

1. **Score storage is the growth curve — and the trigger below has already
   fired.** Superseded by [ADR-0016](../architecture/adr/0016-scores-are-computed-not-materialised.md).

   The trigger was "**50M rows** or **25% of database size**". Measured
   2026-08-25: 1.38M rows — nowhere near — but **78.4% of the database**, three
   times over the size clause. A two-clause trigger is only as good as the
   clause somebody reads, and only the row count was ever checked.

   The resolution is not partitioning. Scoring measures **1,837 ns**, so the
   entire live corpus can be ranked at read time in 22 ms against a 120 ms
   budget: 1,952 MB exists to avoid 22 ms of CPU. ADR-0016 stops materialising
   the cross-product.
2. ~~**The fan-out job is the CPU curve.**~~ **Wrong, corrected 2026-08-25.**
   At 1M active users and 1,000 new postings a day the fan-out is 11,574
   scores/second, which at the measured 1.84 µs is **2.1% of one core**. CPU was
   never the constraint.

   The constraint is write amplification: the same work writes **1.5 TB of rows
   per day**, onto a table already tuned to `autovacuum_scale_factor 0.02`
   because its churn drives dashboard latency. The curve is I/O and storage. The
   distinction matters because the mitigations are opposite — more cores do
   nothing for it.
3. **Autovacuum is now load-bearing** for dashboard latency and is tuned to 0.02
   on `user_job_scores`. It needs a monitor, not just a setting: alert when dead
   tuples exceed **5%** of live on that table.
4. **Read replicas before caching.** Postgres streaming replication is one config
   change and no new datastore; a cache is a new consistency problem. **Trigger:**
   primary CPU sustained above **60%** with read queries dominating.

## 12. Compliance — ranked by whether it actually applies to us

The brief asks for VPAT, VAPT and FedRAMP. All three are covered below, but they
are **not equally applicable**, and treating them as equal would burn a large
budget on the one that does not apply while missing the one with a deadline three
months away.

### 12.1 Applies now, with a hard deadline: India's DPDP

We store CVs. That is personal data of the most sensitive employment kind, and
our own user is in India.

The DPDP Rules were **notified 14 November 2025**. The phased timeline
([India Briefing](https://www.india-briefing.com/news/india-dpdp-compliance-timeline-enforcement-2026-27-44740.html/),
[Fisher Phillips](https://www.fisherphillips.com/en/insights/insights/indias-new-data-privacy-rules-are-here)):

| Date | What becomes true |
|---|---|
| 13 Nov 2025 | Definitions apply; Data Protection Board constituted |
| **13 Nov 2026** | **The Board can inquire and levy penalties.** ~3 months from today |
| 13 May 2027 | Notice, consent, data-principal rights, retention, transfer and breach obligations apply in full |

**What we already do right**, by accident of good design: the original CV file is
never stored ([ADR-0007](../architecture/adr/0007-resume-parsing-local-first.md));
extracted text is encrypted at rest with AES-256-GCM under a key separate from
the session secret; the parser holds no database credentials and has no egress;
we store only job-posting data from sources, never recruiter personal data.

**Built 2026-08-25.** Consent records (`user_consents`, append-only, versioned
against `store.NoticeVersion` and written in the same transaction as the account);
a stated retention period with automated deletion (24 months for CV text, a
7-day grace for erasure, both enforced by a daily `retention_sweep` job with
integration tests); and the rights path as real endpoints — `GET /v1/me/export`
for access and portability, `PATCH /v1/me/profile` for correction,
`POST /v1/me/erasure` for erasure, `DELETE /v1/me/consents` to withdraw.

[privacy-notice](../operations/privacy-notice.md) and
[breach-runbook](../operations/breach-runbook.md) are written.

**Still missing:** a named incident responder, 180-day log retention in Indian
jurisdiction, and an NTP assertion — all §12.2, all listed in the runbook's own
"what is genuinely missing today" table rather than discovered mid-incident.

### 12.2 Applies now, no size threshold: CERT-In

The CERT-In Directions (effective 27 June 2022) have **no minimum company size or
revenue threshold** — a ten-person startup carries the same obligation as an
enterprise ([Bachao.AI](https://www.bachao.ai/blog/cert-in-2022-directions-cybersecurity-compliance-india),
[AMLEGALS](https://amlegals.com/cert-in-compliance-guide-2025/)). Non-compliance
is punishable under s.70B(7) of the IT Act.

| Obligation | Our status |
|---|---|
| **Report covered incidents within 6 hours** | ❌ No runbook, no named responder |
| **Retain logs 180 days, within Indian jurisdiction** | ❌ Logs go to stdout; no retention, no residency guarantee |
| **NTP synchronisation of all ICT systems** | ⚠️ Inherited from the host; not asserted or monitored |
| **VAPT** | ❌ Never done. This is the "VAPT" in the brief, and this is where it belongs |

**VAPT is a scheduled activity, not a certificate.** The practical version for us:
an annual third-party penetration test by a CERT-In empanelled auditor once there
is a production deployment worth testing, plus continuous automated scanning
(§9.2) in the meantime. Doing the automated half first is what makes the paid
test worth its fee — you do not pay a human to find what `grype` finds free.

### 12.3 Applies when we have EU users: EAA / EN 301 549 / VPAT

The European Accessibility Act's enforcement date was **28 June 2025**. New
services placed on the market after it must comply; existing services have until
**28 June 2030** ([Level Access](https://www.levelaccess.com/compliance-overview/european-accessibility-act-eaa/),
[Accessibility.works](https://www.accessibility.works/blog/saas-eaa-compliance-european-accessibility-act-en-301-549-requirements/)).

**The good news is that we are already ahead of the standard.** EN 301 549 — the
harmonised standard the EAA references — incorporates **WCAG 2.1 AA**; WCAG 2.2
is not yet in the harmonised standard. We build to **WCAG 2.2 AA**, including
2.4.11 Focus Not Obscured and 2.5.8 Target Size, which are 2.2 additions.

So the compliance work is mostly **documentation of what we already do**:

- Produce a **VPAT 2.5 EU Edition** (reports against EN 301 549) and an
  **INT Edition** if we want one document covering 508, EN 301 549 and WCAG.
  VPAT 2.5 was published September 2023 specifically to align with WCAG 2.2
  ([Accessibility.works](https://www.accessibility.works/blog/vpat-25-wcag-ada-508-reporting/)).
- Publish an **accessibility statement** with a feedback route.
- **Put `make ui-audit` in CI** so the claims in the VPAT stay true. A VPAT is a
  legal representation; one that drifts from the product is worse than none.
- Commission a **third-party accessibility audit** before publishing the VPAT.
  Self-attestation is permitted and is worth much less to an enterprise buyer.

Note that our audit checks the *mechanical* subset — target size, headings,
labels, overflow, contrast. It does not check screen-reader flow, focus order
through a modal, or whether an error message is actually announced. Those need a
human with assistive technology.

### 12.4 Does not currently apply: FedRAMP

**FedRAMP is not required for commercial products.** It is the gate for selling
to US federal agencies — no federal buyer, no requirement
([Convox](https://www.convox.com/blog/fedramp-authorization-2026-guide-saas-companies),
[Knox Systems](https://knoxsystems.com/resources/fedramp-20x)).

JobTrack is a consumer job-search tool with no federal customer. Pursuing FedRAMP
now would cost **$100k–$300k even under the new 20x path** (traditional Rev5 Low
is $250k–$500k initial plus $100k–$200k annually) and would consume the entire
engineering budget for a market we are not in.

**The honest recommendation: do not pursue FedRAMP.** Revisit if and only if a
US federal or federal-contractor buyer appears. The 20x path is genuinely better
if that day comes — it removes the agency-sponsor requirement, which was the
barrier that made this impossible for small teams, and compresses the timeline
from 12–18 months toward 3–6.

**What to do instead, which serves the same purpose at 5% of the cost:** the
controls FedRAMP verifies overlap heavily with **SOC 2 Type II** and
**ISO 27001**, and those are what commercial buyers actually ask for. Neither is
needed before there are customers asking.

### 12.5 The order to do this in

| Priority | Item | Driver |
|---|---|---|
| ~~**1**~~ | ~~DPDP: consent record, retention policy, rights endpoints, breach runbook~~ | ✅ **Done 2026-08-25.** Penalties live 13 Nov 2026 |
| **2** | CERT-In: 6-hour runbook, 180-day log retention in-region, NTP assertion | No threshold; applies on day one of production |
| **3** | `ui-audit` + `govulncheck` + `grype` in CI | Free, and it is what keeps every later claim true |
| **4** | Accessibility statement + VPAT 2.5 (self-attested), then third-party audit | EAA applies the moment an EU user signs up |
| **5** | Third-party VAPT by a CERT-In empanelled auditor | After there is a production deployment |
| **6** | SOC 2 Type II / ISO 27001 | When a buyer asks |
| **—** | FedRAMP | **Not planned.** Revisit only with a federal buyer |

---

## 13. Sequencing

Ordered so each block ships on its own and nothing is built twice.

### Block A — Make the gates tell the truth (days, not weeks)

`ui-audit`, `migrate-verify`, CodeQL, gitleaks and `grype` into CI; SBOM, cosign
signing and SLSA provenance in a new `release.yml`; actions pinned by SHA. This
is first because **every later claim in this plan depends on the gates being
real**, and because it is the cheapest work here.

### Block B — Coverage (the largest user-visible win)

Workday adapter with facet-walking beneath the 2,000 cap and a test that fails on
truncation: **built**. **Personio, Recruitee and Workable: built 2026-08-25**,
all three verified live before a line was written and again after, ingesting 106
postings across 10 boards with zero errors. See
[A-34](../research/evidence-ledger.md#a-34).

Three of the endpoint facts recorded in
[source-catalog](../research/source-catalog.md) turned out to be wrong and are
corrected there: Workable's URL and its supposed need to join separate
locations/departments endpoints, Recruitee's compensation described as "Rare"
when it is the second-best of any vendor, and Personio's `?language=en` which
does nothing at all.

Still open: the JSON-LD career-page tier (§7.3), and verifying
Darwinbox/Keka/iCIMS/BambooHR. Re-measure
[A-00f](../research/evidence-ledger.md#a-00f) once the SmartRecruiters sweep
completes.

### Block C — The product's memory

**Saved searches and the new-since-last-run count: built 2026-08-25.** A saved
search is the feed's URL query string with a name, so there is no second filter
model — replaying one is a redirect. Auto-named from the active facets,
confirmed inline rather than by a toast, and one may be marked default and
applied to a bare `/jobs`.

**Dismissed postings, results-per-page and default-sort: built 2026-08-25.**
Hiding collapses a card to an undo strip rather than removing it — a row that
vanishes under the cursor makes the list jump and leaves nothing to click if the
click was wrong. Page size and sort live in the existing preferences jsonb,
since nothing filters or joins on them, and precedence is URL → stored
preference → the product's opinion.

Still open: the digest email, the only item here needing new infrastructure
(§8.4).

### Block D — The voice

Typography pairing, OKLCH palette generator, accent off indigo, homepage rebuilt
from corpus content. Ships behind the existing contrast suite and the image
budget.

### Block E — Deployment and proof

Complete the Kubernetes manifest set, Kustomize overlays, `deploy.yml` with
signature verification before rollout, `docker-compose.prod.yml`,
`docker-stack.yml` with its limitations documented. Then k6, Lighthouse CI and
the ingest-latency probe, so the budgets stop being aspirations.

### Block F — Compliance

DPDP first, CERT-In second, per §12.5.

### What this plan deliberately does not do

- **No LinkedIn or Naukri scraping.** [ADR-0004](../architecture/adr/0004-source-acquisition-policy.md);
  overturning it needs a superseding ADR.
- **No FedRAMP.** §12.4.
- **No Redis, no Elasticsearch, no vector store, no broker.** No trigger has
  fired; the closest is 5% of its threshold.
- **No "recommended for you" feed** that hides its reasoning.
- **No field/seniority scoring component yet.** It is the largest known modelling
  gap and it needs a corpus of at least 20 real profiles first — choosing the
  constant without that would repeat the mistake
  [ADR-0011](../architecture/adr/0011-abstention-credit-calibration.md)
  documents at length.
- **No invented testimonials, stock smiling-laptop photography, or a fabricated
  user count.** §6.4.

## 14. Sources

Every external claim in this document, with the date it was read (2026-08-25).

**Sources and ATS access**
- [Every major ATS has a public job feed](https://dev.to/agenticemail/every-major-ats-has-a-public-job-feed-here-is-how-to-read-them-all-3k10) — endpoint patterns, cross-checked by direct call
- [Workday job boards have a JSON API too](https://dev.to/udaninn/workday-job-boards-have-a-json-api-too-its-just-better-hidden-23fl)
- [LinkedIn API access in 2026 — partner programme](https://www.getphyllo.com/post/linkedin-api-access-in-2026-partner-program-approval-timeline-alternatives)
- [LinkedIn Job Credits Partner Program](https://developer.linkedin.com/docs/v1/job-posting/linkedin-job-credits-partner-program)

**Design**
- [AI slop web design guide](https://www.925studios.co/blog/ai-slop-web-design-guide)
- [AI design has no soul, but typography makes it whole](https://pimpmytype.com/about-ai-design/)
- [OKLCH colour space guide](https://colorarchive.org/guides/oklch-color-space-guide/) · [Atmos glossary](https://atmos.style/glossary/oklch-color-space)
- [Filtering UX/UI patterns and best practices](https://blog.logrocket.com/ux-design/filtering-ux-ui-design-patterns-best-practices/)
- [How to improve advanced search UX](https://uxdworld.com/how-to-improve-advanced-search-ux/)

**Deployment and pipelines**
- [Mirantis guarantees long-term support for Swarm](https://www.mirantis.com/blog/mirantis-guarantees-long-term-support-for-swarm/)
- [Is Docker Swarm still safe in 2026?](https://www.virtualizationhowto.com/2026/03/is-docker-swarm-still-safe-in-2026/)
- [Kubernetes vs Docker Swarm vs Nomad 2026](https://codingprotocols.com/blog/docker-swarm-vs-kubernetes-vs-nomad)
- [SLSA 3 compliance with GitHub Actions and Sigstore](https://github.blog/security/supply-chain-security/slsa-3-compliance-with-github-actions/)
- [SLSA 3 container generator GA](https://slsa.dev/blog/2023/02/slsa-github-workflows-container-ga)
- [Load testing complete guide 2026](https://ardura.consulting/blog/load-testing-complete-guide-2026/)

**Compliance**
- [India DPDP compliance timeline 2026–27](https://www.india-briefing.com/news/india-dpdp-compliance-timeline-enforcement-2026-27-44740.html/)
- [India's new data privacy rules — 8 steps](https://www.fisherphillips.com/en/insights/insights/indias-new-data-privacy-rules-are-here)
- [CERT-In 2022 Directions compliance guide](https://www.bachao.ai/blog/cert-in-2022-directions-cybersecurity-compliance-india) · [AMLEGALS guide](https://amlegals.com/cert-in-compliance-guide-2025/)
- [European Accessibility Act compliance guide](https://www.levelaccess.com/compliance-overview/european-accessibility-act-eaa/)
- [SaaS EAA compliance and EN 301 549](https://www.accessibility.works/blog/saas-eaa-compliance-european-accessibility-act-en-301-549-requirements/)
- [VPAT 2.5 reporting](https://www.accessibility.works/blog/vpat-25-wcag-ada-508-reporting/)
- [FedRAMP authorization in 2026](https://www.convox.com/blog/fedramp-authorization-2026-guide-saas-companies) · [FedRAMP 20x](https://knoxsystems.com/resources/fedramp-20x) · [cost breakdown](https://secureframe.com/hub/fedramp/costs)

**The Pokémon correction**
- [Satoru Iwata's work on Pokémon Gold and Silver clarified](https://nintendoeverything.com/satoru-iwatas-work-on-pokemon-gold-and-silver-clarified/)
- [New details emerge on Iwata's work](https://www.nintendolife.com/news/2023/10/new-details-emerge-on-satoru-iwatas-work-on-pokemon-gold-and-silver)
