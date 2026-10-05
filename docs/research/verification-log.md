# Verification log

> Status: **LIVING DOCUMENT**. A record of what was checked against primary sources, what was wrong,
> and what changed as a result.

## Why this exists

A design document that has never been checked against primary sources is a plausible story. This log
records the pass that turned this one into something verified, and it stays so that future
verification passes append rather than start over.

**Round 1 — 2026-08-15.** Load-bearing technical claims re-checked against vendor documentation and
project sources, deliberately different from those used when the claim was first written.

**Result: 6 corrections, 2 additions, 1 design change, 1 new product feature.** Nothing found was
catastrophic; several were the kind of imprecision that becomes a bug during implementation, which is
exactly what this pass is for.

---

## Corrections

### V1 — Greenhouse field coverage was materially understated

**Was:** "Fields: title, location, departments, offices, content, updated_at, absolute_url.
Compensation: rare."

**Checked against:** the official schema in
[`grnhse/greenhouse-api-docs`](https://github.com/grnhse/greenhouse-api-docs/blob/master/source/includes/job-board/_jobs.md).

**Actually:**

| Finding | Impact |
|---|---|
| The list response carries `meta.total` | Minor; useful as a sanity check against partial responses |
| `requisition_id` is exposed | ~~Dedup improvement~~ — **withdrawn 2026-10-05**: it is employer free text, reused across unrelated roles, and keying on it hid 2,293 postings still listed on their boards |
| `first_published` exists on the **detail** endpoint | **Correctness fix** — `updated_at` moves on any edit, so using it makes an edited 40-day-old posting look fresh. That corrupts the exact signal the product is built on |
| Structured pay is available via `?pay_transparency=true` on the **detail** endpoint (`pay_input_ranges[]` with `min_cents`, `max_cents`, `currency_type`) | **Design change** — Greenhouse is an N+1 vendor for structured comp, like SmartRecruiters is for descriptions |
| `full_content=true` returns intro + pay transparency + conclusion in `content` | Text-parse fallback when detail fetch is not warranted |
| Double HTML escaping **confirmed** — the official example literally shows `&amp;lt;p&amp;gt;` | Prior claim was right; now sourced rather than asserted |

**Changed:** [source-catalog §Greenhouse](source-catalog.md#greenhouse) rewritten;
[ingestion-pipeline](../architecture/ingestion-pipeline.md) poll-cost table updated; two new fixtures
required.

### V2 — `hnsw.iterative_scan` is an enum, not a boolean

**Was:** "Every filtered vector query runs with it enabled."

**Actually:** `off` | `strict_order` | `relaxed_order`. "Enabled" is not a valid value, so the
documented configuration **would not have worked**.

Also established, and more important than the syntax: filtering is applied *after* the index scan, so
at the default `hnsw.ef_search = 40` a predicate matching 10% of rows yields roughly **4 results**.
Our queries are always filtered, so the default silently returns short result sets.

**Changed:** exact syntax in
matching-and-scoring §7 (removed 2026-09-30: vector search was never built, see [ADR-0022](../architecture/adr/0022-retrieval-is-lexical.md)),
including `ef_search = 100`, `max_scan_tuples`, the choice of `relaxed_order` (the scorer re-ranks
anyway, so strict ordering buys nothing), and the
[pgvector#862](https://github.com/pgvector/pgvector/issues/862) planner caveat.

**Second-order finding:** these must be `SET LOCAL` inside a transaction, or transaction-mode
PgBouncer will drop them later. Now a `sqlc vet` rule
([consistency-and-drift §3.2](../engineering/consistency-and-drift.md#32-migrations-vs-the-queries))
so the correctness bug cannot be introduced.

### V3 — Argon2id parameters were asserted, not sourced

**Was:** "tuned to ~100 ms on production hardware (memory ≥ 64 MB)."

**Actually:** the OWASP baseline is **m = 19 MiB, t = 2, p = 1**, which is what produces ~100 ms on a
modern server core. Our figure was stricter than the standard but unsourced, and "≥ 64 MB" would have
been copied forward as if it were the recommendation.

**Changed:** [security §4](../operations/security-and-privacy.md#4-authentication) now cites the
baseline, notes that memory cost defeats GPU/ASIC attackers more than time cost, and adds that
parameters are stored **alongside each hash** so they can be raised with transparent rehash on login.

### V4 — `net/http.ServeMux` limitations were unstated

**Was:** "handles methods and path parameters, which covers everything we need."

**Actually true and better than claimed:** automatic **405** when a path matches but the method does
not, and **conflicting patterns are detected at registration time** — startup, not on the request that
hits the ambiguity.

**But two real limits were missing:** a wildcard must occupy a **whole path segment** (`/b_{bucket}`
is invalid), and there are **no route groups or middleware chaining**.

**Changed:** [ADR-0001](../architecture/adr/0001-go-for-backend.md) states both limits and the
mitigation (a ~15-line `chain()` helper, explicit prefix constants).

### V5 — The dev environment was not containerised

**Was:** "Backend services run locally, not in Docker, so hot reload is instant."

**Actually:** Docker Compose Watch reached GA and delivers **sub-500 ms sync**, versus 2–4 seconds for
traditional bind mounts. The performance argument for host processes no longer holds, while the
reproducibility cost — host Go version drift, differing tool versions producing spurious generated-code
diffs, macOS/Linux libc differences in PDF parsing — remained.

**Changed:** [dev-environment](../engineering/dev-environment.md) rewritten as fully containerised,
with a devcontainer, a `tools` image, and `develop.watch` rules distinguishing `sync`,
`sync+restart` and `rebuild`. Prerequisites are now **Docker and git**, nothing else.

*This correction was prompted by the user, not by the verification sweep — worth recording, because it
is the one a purely technical pass would have missed.*

### V6 — Container base image was unspecified

**Was:** "`FROM scratch` images in the 15–25 MB range."

**Actually:** scratch has zero base CVEs by construction, but **distroless is regularly rebuilt and
patched**, so a base-library vulnerability is fixed by pulling the latest tag. Both ship no shell.
For a Go static binary the maintenance property dominates.

**Changed:** distroless static, non-root (65532), read-only rootfs. Alpine explicitly rejected — it
carries BusyBox and a package manager we have no use for.

---

## Additions

### V7 — Caching and storage had no decision framework

The docs asserted "Postgres only, no Redis" without saying **at what point that stops being true.**
That is a preference, not a decision.

**Added:** [caching-and-storage.md](../architecture/caching-and-storage.md) — every concern with a
numeric trigger. Redis for facets at *>4 replicas AND >5% DB CPU*; Redis for sessions at *>15% DB
CPU*; PgBouncer at *>400 connections*; object storage offload for descriptions at *p95 row fetch >20
ms*; a dedicated vector store at *>20M vectors*.

Grounded in benchmark data showing Redis at ~1.2 ms p50 vs Postgres at 8.5 ms — real, and still not
worth a second stateful system until the trigger fires — and in TOAST guidance putting the practical
`bytea` threshold near 2 KB, which is what makes object storage correct for resume blobs **at any
scale**, not an optimisation.

### V8 — Backend concurrency was implied, never specified

**Added:** [backend-performance.md](../architecture/backend-performance.md) — the four disciplines in
payoff order, `singleflight` coalescing (including the head-of-line-blocking caveat, hence `DoChan`
with deadlines rather than bare `Do`), `pgx.SendBatch`, the fan-out-across-systems / batch-within-one
rule, an apportioned latency budget with 2.4× headroom, and a **query-count assertion in CI** (≤ 3 for
`GET /v1/jobs`, independent of result size) — which catches the N+1 class that looks fine at 25 rows
in development.

---

## Product change

### V9 — Greenhouse exposes AI screening disclosure, and we had missed it

Three fields per posting: `include_ai_disclaimer`, `ai_disclaimer` (the employer's own wording), and
**`ai_opt_out_request_url`**.

This is the most valuable single find of the pass. Our own research established that where AI ranking
is used it decides *order*, not rejection — a low rank ends an application with no rejection ever sent
`[A-07]`. A candidate who can see that a specific employer uses AI matching, read that employer's
disclosure, and follow a **real opt-out link** has information that is otherwise entirely invisible to
them.

**Added:** [feature-spec §F10](../product/feature-spec.md#f10--ai-screening-disclosure), with the
constraints that matter — we report without editorialising, we never follow the opt-out link on the
user's behalf, and absence means *unknown* rather than *no AI*, since other vendors do not expose it.

---

## Confirmed correct

Worth recording so a future pass does not redo the work:

| Claim | Source |
|---|---|
| Greenhouse returns the entire board in one response, no pagination | Official schema |
| Greenhouse `content` is double-HTML-escaped | Official example |
| Lever `createdAt` is epoch milliseconds | Vendor docs + comparison analyses |
| Ashby has the only consistently structured compensation on a **list** endpoint | Vendor docs |
| SmartRecruiters omits descriptions from list; max 100 per page | Vendor docs |
| Recruitee subdomains fail as DNS, not HTTP 404 | Comparison analyses |
| ingress-nginx EOL March 2026, no further security patches | Kubernetes Steering / SRC announcements |
| Gateway API core resources are GA | Kubernetes project docs |
| River supports enqueue inside an existing pgx transaction | River docs |
| `buf breaking` detects wire/JSON-incompatible changes | Buf docs |
| Queue depth, not CPU, is the correct scaling signal for workers | KEDA docs, CNCF write-ups |
| PDBs govern **voluntary** disruption only | Kubernetes docs |
| `golang-standards/project-layout` is not official | Go project docs |
| OTel logs signal is production-usable but not API-frozen | OpenTelemetry status |

---

---

# Round 2 — 2026-08-15

Closing the two items Round 1 left open, because "listed under still-unverified" is not the same as
resolved and both were load-bearing.

**Result: one claim upgraded and made precise; one assumption found to be wrong by roughly 4×, with a
worse tail risk than the point estimate suggests.**

---

## V10 — Frontend bundle sizes: resolved, with the soft part isolated

**Was:** "SvelteKit ~12–20 KB vs Next.js ~70–90 KB", direction B, magnitude C, sourced from blog
benchmarks with unstated methodology.

The claim was really three claims tangled together. Separated, two are now hard facts and only the
third remains correlational.

### (a) Runtime library size — now A-grade, independently verifiable

| Library | Minified + gzipped |
|---|---:|
| React + ReactDOM | **~48 KB** |
| Vue | **~33 KB** |
| Svelte runtime | **~1.6 KB** |

Svelte is ~1.6 KB because it is a **compiler, not a runtime** — the framework largely disappears at
build time. Anyone can verify these from the published packages, which is what makes them A-grade
rather than someone's benchmark `[A-24]`.

**The ~46 KB gap between React and Svelte is a fact, not an estimate.**

### (b) Framework baseline page weight — now A-grade, from real build output

Next.js App Router "First Load JS shared by all", measured from actual builds: **80.4 KB** in one
public example, **102 KB** in another. One documented migration measured **141 KB → 200 KB (+42%)**
moving from Pages Router to App Router — a shared-baseline increase every route inherits `[A-25]`.

Community guidance from the same measurements: **under ~130 KB is good, 170–200 KB is a warning
range, 200 KB+ warrants investigating dependencies and client boundaries.**

> **This validates our budgets rather than changing them.** ADR-0002 assumed a Next.js baseline of
> ~80 KB against a 100 KB budget, and a 160 KB budget for the Next.js fallback. Both survive contact
> with the measured data — 80 KB is the *low* end of the observed range, so if anything the fallback
> budget was slightly generous rather than tight.

### (c) Real-user Core Web Vitals by framework — remains B, and the caveat is now explicit

The best available methodology-stated comparison uses **CrUX real-user data + HTTP Archive**, homepages
only:

| | CWV pass rate |
|---|---:|
| All websites (baseline) | **40.5%** |
| Astro | **> 50%** |
| SvelteKit | above the 40.5% baseline |
| Next.js | **~25%** (roughly 1 in 4) |
| Nuxt | **~20%** (roughly 1 in 5) |

**Why this stays B and not A**, stated in full because the gap looks dramatic and would be easy to
over-read:

1. **The publisher is Astro** — a direct competitor to Next.js and Nuxt. Vendor interest.
2. **The authors name their own confounders**, to their credit: **version bias** (older frameworks
   carry a long tail of legacy sites on outdated versions, newer ones do not), homepages only, and
   unexamined framework-age effects.
3. **HTTP Archive's own methodology note is explicit:** *"correlation does not equal causation. A
   technology being highly correlated with good (or poor) performance does not necessarily indicate
   that technology is the cause of that performance"* `[A-26]`.
4. **It is a 2023 report**, and this is 2026. Next.js 16 and mature RSC adoption postdate it.

A large share of that 25%-vs-50% gap is plausibly *who chooses each framework and for what*, not the
framework itself. Marketing sites built on Astro are not comparable workloads to Next.js
applications.

### What changed

[ADR-0002](../architecture/adr/0002-frontend-framework.md) now cites (a) and (b) as measured facts
and presents (c) with the four caveats above rather than as a headline. The **"65% smaller" and
"1,200 vs 850 RPS" figures are struck entirely** — they have no traceable methodology and are not
repeated anywhere in this repository.

**The decision does not change.** It rests on (a) and (b), which are facts, and on the budget
arithmetic — not on the pass-rate comparison, which is the part we cannot stand behind.

---

## V11 — Fan-out: the assumption was wrong, and the tail is worse than the mean

**Was:** "a new posting is scored only for users whose preferences make it plausibly relevant —
typically **a few percent** of the user base."

That number was never derived. It was a placeholder that became a capacity model.

### Deriving it properly

Fan-out is the probability that a random user's coarse preference filter (geography, work mode, YoE
band) admits a random new posting. Using verified market composition:

**Geography.** Bengaluru is **34%** of tracked Indian tech listings, Hyderabad 19%, Pune 13%; tier-1
cities together are **88–90%** of IT demand `[B-37]`. Crucially, a user base serving this market
concentrates the *same way* — most Indian users will have Bengaluru in their preferred set, because
it is where the jobs are.

```
posting city      share of postings   × est. share of users accepting it   = contribution
Bengaluru               0.34          ×              ~0.50                 =    0.17
Hyderabad               0.19          ×              ~0.25                 =    0.048
Pune                    0.13          ×              ~0.20                 =    0.026
other tier-1            0.22          ×              ~0.15                 =    0.033
tier-2/3                0.12          ×              ~0.08                 =    0.010
                                                            geography term ≈  0.29
```

**Experience band.** Roles requiring ≤3 years fell from 43% (2018) to **28%** (2024) `[B-38]`. With a
user base skewed junior and a stretch band that widens matching upward, the YoE term is roughly
**0.40–0.50**.

```
fan-out ≈ 0.29 × 0.45 ≈ 0.13
```

### **≈ 13%, not 3%. The assumption was low by about 4×.**

### The tail risk is larger than the error in the mean

The genuinely dangerous finding is not the 4×. It is this:

**A remote posting matches every user regardless of geography.** The geography term — which does
almost all of the filtering — collapses to 1.0. So fan-out is acutely sensitive to remote share, and
**the remote-share data conflicts irreconcilably**:

| Source | Remote / hybrid share |
|---|---|
| All job postings, Q2 2026 | 3–4% fully remote, ~10% hybrid, **87% on-site** |
| **Software engineering** postings, LinkedIn, late 2025 | **67% offered remote or hybrid** |

These are not reconcilable by averaging — they measure different populations (all occupations vs.
software specifically) at different dates, and the tech sector genuinely diverges from the wider
return-to-office trend `[C-09]`. **We are building for the population where the higher figure
applies.**

If the SWE-specific figure is closer to right, geography stops filtering for most postings and
fan-out could reach **35–45%** — an order of magnitude above the original assumption.

### Sensitivity

| Scenario | Fan-out | Year-2 scores/day | Sustained cores @ 0.5 ms |
|---|---:|---:|---:|
| Original assumption | 3% | 150 M | ~1 |
| **Derived estimate** | **13%** | **650 M** | **~4** |
| High-remote scenario | 40% | 2.0 B | ~12 |

### What saves us, and it is worth naming

**Storage is unaffected**, because the **top-N cap binds before fan-out does.** We store at most ~2,000
scores per user regardless of how many postings are relevant, so `user_job_scores` stays at
100k × 2,000 = **200M rows** in every scenario.

That cap was written down as a mitigation. It turns out to be the thing that makes an incorrect
fan-out assumption survivable, which is a better argument for it than the one originally given.

**Compute is affected**, and ~12 cores in the worst case is real but tractable — `matcher` scales on
queue depth to 30 replicas and runs on its own node pool precisely so this cannot touch API latency.

### Two changes, so this cannot bite silently

1. **Score only for users active in the last 30 days**, not all registered users. Dormant users get
   scored lazily on their next session. At typical retention this is a **~2.5× reduction** and it is
   free — nobody is waiting on a score they will not look at.
2. **Instrument fan-out as a metric from the first day of production**
   (`score_fanout_ratio`), with an alert at > 20%. The corrected number is still a derivation, not a
   measurement, and it should stop being a derivation as soon as real data exists.

### What changed

[scaling-and-capacity §3](../operations/scaling-and-capacity.md#3-scoring-load) rewritten with the
derivation, the sensitivity table, the active-user bound and the metric.

---

## Still unverified — stated honestly

Both Round-1 entries have been closed by Round 2. What remains:

| Claim | Status | How it gets closed |
|---|---|---|
| **Our own** `/jobs` bundle size | The framework baselines are now measured ([A-25](evidence-ledger.md#a-25)), but **our application's** first-load figure does not exist until the page does | CI bundle budget, from the first `/jobs` build |
| Ingest latency SLO (90 min, tier A) | A **target derived from poll interval**, not a measurement | `ingest_latency_seconds` histogram, first production ingestion |
| Scoring component weights | **Initial guesses.** They are configuration precisely for this reason ([matching-and-scoring §1](../architecture/matching-and-scoring.md#1-the-model)) | Golden corpus + band-distribution monitoring |
| Fan-out ratio (now ~13%, was 3%) | **Derived, not measured.** Round 2 corrected the value and bounded the risk, but a derivation is still not data | `score_fanout_ratio` metric, alert > 20%, from day one |
| Remote/hybrid share ([C-09](evidence-ledger.md#c-09)) | **Genuinely conflicting sources** — 3–4% vs 67% for different populations. Not resolvable externally | Measured directly from **our own corpus** after first ingestion. We will have better data than either source, for our specific population |
| Cost model | Generic units. **Ratios are the point**, not the absolutes | Actual bills |

The last row is worth noting: several of these stop being estimates the moment the system runs,
because **we will be the best-positioned party to measure them.** Nobody publishes the remote share of
first-party ATS postings for Indian software roles. After one week of ingestion, we will know it.

---

## How to run the next pass

1. Take the claims tagged `[A-*]` in [evidence-ledger.md](evidence-ledger.md) — these justify
   architecture, so they are the ones that hurt when wrong.
2. Re-check each against a **primary source you did not use the first time**. Vendor documentation
   over comparison blog posts; project release notes over tutorials.
3. Record corrections here, with what changed as a result.
4. Downgrade any claim whose source turns out to be secondary. **A downgrade is a successful outcome,
   not a failure** — an A-grade claim resting on a blog post is more dangerous than a B-grade claim
   labelled as one.
