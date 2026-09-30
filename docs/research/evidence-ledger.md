# Evidence ledger

> Status: **LIVING DOCUMENT**. Every load-bearing claim in this repository traces here.

## Why this exists

Roughly a third of what circulates as job-search "fact" is content marketing published by companies
selling resume services. A product built on those numbers inherits their bias, and a product that
*quotes* them in its UI is indistinguishable from them.

So every claim carries a grade, and the grade is visible wherever the claim is used — including in
the product itself.

## Grading

| Grade | Meaning | May be used to… |
|---|---|---|
| **A** | Primary source, reproducible dataset, or first-party technical documentation | Justify an architectural decision or appear as a stated fact in the UI |
| **B** | Credible secondary source, or a survey with stated methodology | Justify a decision when combined with reasoning. Directionally trustworthy, numerically soft |
| **C** | Contested, vendor-published, anecdotal, or methodologically opaque | Form a hypothesis. **Never** justify a decision or appear unlabelled in the UI |

**Current distribution: 26 A, 37 B (+1 superseded), 9 C.** That ratio is itself the honest summary —
the load-bearing facts (postings indices, payroll data, application volume, the resume RCT, vendor
documentation, measured bundle sizes) are A. What is soft is precisely the material that circulates
most: ghost-job percentages, layoff totals, cold-email reply rates, remote-work share, and framework
pass-rate comparisons.

**Verification history:** [verification-log.md](verification-log.md) records what has been re-checked
against primary sources, what was wrong, and what changed.

- **Round 1** — six corrections, including `hnsw.iterative_scan` written as a boolean when it is an
  enum: a configuration that **would not have worked**.
- **Round 2** — the two remaining soft claims closed. Framework bundle sizes were split into two
  verifiable facts ([A-24](#a-24), [A-25](#a-25)) plus one correlational comparison held at arm's
  length ([B-36](#b-36)), and [B-22](#b-22) was **retired**. The fan-out assumption was found to be
  **wrong by ~4×**, with a tail scenario worse still.

**One entry is deliberately retired rather than deleted** ([B-22](#b-22)). Deleting a wrong claim
hides that it was ever believed; the citation trail matters more than a tidy list.

---

## A — Load-bearing

### Our own system

These are measurements of JobTrack itself, taken against the live corpus. They
grade A because anyone with the stack running can reproduce them with the SQL
recorded in the ADR — which is a higher bar than most external sources clear.

<a id="a-00a"></a>**A-00a — 48.7% of live postings yielded zero extracted skills (6,677 postings, 2026-08-16, vocabulary of 47 terms).**
Average 1.71 skills per posting; only 9.8% had any must-have. This is the
measured ceiling on how often the product can deliver its central claim —
"which of their requirements you meet" — and it is the reason the vocabulary was
expanded to ~120 terms.
*Used in:* [ADR-0009](../architecture/adr/0009-score-granularity-and-skill-coverage.md)

<a id="a-00b"></a>**A-00b — Match score correlated more strongly with posting age (r = 0.256) than with skills fit (r = 0.173).**
Measured over 5,554 scores for one profile. The mechanism is compositional: an
abstaining skills component removes 40 of 100 points from the denominator,
dropping the average available weight to 48.4, which raises freshness from a
nominal 10% of the score to 23.7% on average and up to 40%.
**Any user-facing claim about match quality must respect this ceiling** until it
is re-measured after the vocabulary expansion.
*Used in:* [ADR-0009](../architecture/adr/0009-score-granularity-and-skill-coverage.md)

<a id="a-00c"></a>**A-00c — Postings whose requirements we can read earn a mean 0.236 of the skills weight; postings we cannot read were credited 0.500.**
Measured 2026-08-16 over 5,858 live scores for one profile (4,918 readable, 940
abstaining), after the ADR-0009 vocabulary expansion and the `mentioned`-skills
fallback. Mean 0.236, median 0.250, p90 0.375. The consequence is the finding:
crediting abstention at half marks put the **floor** of the unreadable
population (42.3) above the **median** of the readable one (39.2), so a posting
we understood nothing about was guaranteed to outrank half the postings we
understood — and 42 of the top 100 matches were roles we could not read.
*Caveat:* one profile, one corpus, one day. The credit is a population estimate
and drifts with the vocabulary and the source mix, so it is configuration, not a
constant. Re-measure with `make coverage` after any extraction change.
*Used in:* [ADR-0011](../architecture/adr/0011-abstention-credit-calibration.md)

<a id="a-00d"></a>**A-00d — Match score does not vary with how much a posting states; the `strong` band does.**
Measured 2026-08-17 across 5,184 live scores for one profile, bucketing postings
by stated requirement depth through the scorer's own view (must-haves + half of
nice-to-haves, falling back to half of `mentioned` when a posting has no
requirements section — the direct `posting_skills` query undercounts by more
than half).

| Stated depth | Postings | Median score | `strong` |
|---|---|---|---|
| thin (<2) | 3,172 | 43.8 | 0 |
| at threshold (2–3.5) | 1,176 | 41.7 | 39 (3.3%) |
| deep (4–7.5) | 722 | 43.3 | 8 (1.1%) |
| very deep (8+) | 114 | 45.6 | 1 (0.9%) |

Flat medians mean the ADR-0011 calibration removed the score inflation. The
band concentration at the threshold looked like a defect and is not: 11 of the
top 12 are correct strong matches, so tightening it would cost eleven right
answers to remove one wrong one.
*Caveat:* one profile, one corpus, one day; `strong` counts are small enough
(39 / 8 / 1) that the ratio is indicative rather than precise.
*Used in:* [ADR-0011](../architecture/adr/0011-abstention-credit-calibration.md)

<a id="a-00e"></a>**A-00e — Freshness outranks skills for a thin profile and not for a full one.**
Measured 2026-08-17, within a single profile (pooling profiles dilutes every
component toward zero and is not a valid comparison):

| Profile | Corr. with skills | Corr. with freshness |
|---|---|---|
| 11 skills, YoE set, 2 countries | **0.428** | 0.241 |
| 3 skills, YoE set, no countries | 0.227 | **0.272** |

Supersedes the 0.184 / 0.274 pair in [A-00b](#a-00b), which predates the
ADR-0011 abstention recalibration. Answers ADR-0009's successor condition: the
weighting is correct for a complete profile, so the freshness weight was **not**
changed.
*Caveat:* two profiles, one real and one synthetic, one corpus, one day. The
crossover point between them is not measurable from two samples, which is
exactly why ADR-0013 declines to pick a constant.
*Used in:* [ADR-0013](../architecture/adr/0013-freshness-weighting-re-measured.md)

<a id="a-00f"></a>**A-00f — A vendor with a structural requirements section classifies 54.1% of extracted skills, against ~25% for vendors that supply one undifferentiated blob.**
Measured 2026-08-17 across 6,091 live postings carrying at least one extracted
skill.

| Vendor | Postings | must_have | nice_to_have | mentioned | classified |
|---|---|---|---|---|---|
| Greenhouse | 3,809 | 2,277 | 2,228 | 12,798 | 26.0% |
| Ashby | 1,938 | 1,014 | 706 | 5,384 | 24.2% |
| **SmartRecruiters** | 344 | 572 | 95 | 567 | **54.1%** |

The mechanism is structural, not linguistic: SmartRecruiters splits a posting
into named sections and one is `qualifications`, which the extractor already
recognises as a requirements heading. Vendors supplying one blob depend on the
employer happening to write a heading the extractor knows, and most do not —
which is [A-00a](#a-00a)'s finding from the other direction.
*Caveat:* the SmartRecruiters sample is 344 postings against thousands for the
others, and is drawn from four boards; the gap is large enough to survive that,
the exact figure is not. Re-measure once detail backfill completes.
*Used in:* source-catalog.md, [roadmap Phase 4](../engineering/roadmap-to-completion.md)

<a id="a-00g"></a>**A-00g — 1,861 of 2,393 live SmartRecruiters postings (77.8%) held no description, because the bounded detail phase had no memory and re-read the same 250 every poll.**
Measured 2026-08-19 against the live corpus.

| Vendor | Live postings | Empty description | Zero extracted skills |
|---|---|---|---|
| Greenhouse | 4,723 | 0 | 10.5% |
| Ashby | 2,493 | 0 | 19.9% |
| **SmartRecruiters** | 2,393 | **1,861** | **84.0%** |

SmartRecruiters serves bodies separately from the list, capped at 250 fetches
per poll. The cap was implemented as `jobs[0:250]` with nowhere to record
progress, so the window never moved: a 4,770-posting board would never have
finished, at any polling frequency, ever. Fixed by `sources.detail_cursor`
(migration 0015); the cursor was observed advancing 0 → 250 → 500 → 750 against
the live board.
*Caveat:* the per-vendor zero-skill figures conflate two causes — no body to
read, and a body with nothing in it. [A-00h](#a-00h) separates them.
*Used in:* [roadmap Phase 4](../engineering/roadmap-to-completion.md)

<a id="a-00h"></a>**A-00h — The coverage instrument reported 13.7% zero-skill extraction while the live corpus stood at 32.3%.**
Measured 2026-08-19. `make coverage` sampled with
`WHERE length(description_text) > 400 ORDER BY id LIMIT 1500`, which excluded
every posting with no body — the exact population [A-00g](#a-00g) describes —
and then took the oldest 1,500 rows rather than a sample, which meant the first
board ever ingested and no vendor added since.

| | Old instrument | Corrected |
|---|---|---|
| Population | oldest 1,500 with a body | whole corpus, then 1,500 hashed by id |
| Zero-skill reported | 13.7% | **44.6%** projected corpus-wide |
| Postings with no body | invisible | reported as its own line |

The number was not wrong about what it measured; it was measuring a population
chosen so the failure could not appear in it. Both halves are now reported
separately, because "we could not read it" and "we read it and found nothing"
have different fixes.
*Used in:* [roadmap Phase 4](../engineering/roadmap-to-completion.md), ADR-0009

<a id="a-00i"></a>**A-00i — Scoring one posting took 32.09s for 62 users; batching the writes took it to 0.49s.**
Measured 2026-08-24 from River's own `average_run_duration`, before and after,
on the same corpus and worker count.

| | Before | After |
|---|---|---|
| Mean run duration, one posting | **32.09 s** | **0.49 s** |
| Score jobs completed per minute | ~19 | **~1,806** |
| `GET /v1/me/dashboard` under backlog | 500, deadline exceeded at 10 s | served |
| End-to-end suite | 13 of 43 failing, 8.5 min | **43 passing, 1.9 min** |

The scorer was never the cost. `upsertScore` ran one `Exec` per user inside the
fan-out loop, so scoring a posting was linear in *network round trips* rather
than in computation — in the one workload whose whole shape is fan-out. A
backlog of 14,700 jobs on the interactive queue followed, and the dashboard
timed out behind it. Both fan-out directions (one posting × every user, one user
× the live corpus) now write through a single batched statement.
*Used in:* [roadmap Phase 4](../engineering/roadmap-to-completion.md),
[backend-performance](../architecture/backend-performance.md)

<a id="a-00j"></a>**A-00j — Three live Ashby postings presented an hourly rate as an annual salary.**
Measured 2026-08-19. The clearest case is one company publishing the same band
twice: $30–45 **per hour** where we parsed it from the text, and $30–45 **per
year** where we read the vendor's structured field.

| Posting | Vendor's `interval` | Stored, before | Stored, after |
|---|---|---|---|
| Part-Time Recruiting Coordinator | `1 HOUR` | year | **hour** |
| IT Support Specialist | `1 HOUR` | year | **hour** |
| Administrative Business Partner | `1 YEAR` | year | year (correct) |

Two stacked defects: the JSON key `interval` was bound to a field named
`InterviewType`, while the field named `Interval` read `compensationInterval` —
a key Ashby does not send — and `normaliseInterval` then defaulted anything
unrecognised to `"year"`. The default is what made it invisible: every wrong
answer looked like an ordinary salary. Unrecognised periods now return empty,
which stores as NULL.
*Caveat:* three postings is the count that was *visibly* wrong because the
figures were small. Any Ashby monthly or weekly band was equally mislabelled and
is not separable retrospectively.
*Used in:* [principles — never fabricate a number](../product/principles.md)

<a id="a-00k"></a>**A-00k — The dashboard's match counts read 15,000 rows per page load to report four numbers, and returned 500 under churn.**
Measured 2026-08-24 at 15,379 live postings and 1.26M scores.

| | Fused query | Split, per part |
|---|---|---|
| Strong / plausible counts | — | **26 ms** (719 rows scanned, not 15,000) |
| New in 24 hours | — | **655 ms** |
| Total scored | — | **7 ms** |
| Whole thing | **3,565 ms** clean / **13,687 ms** under churn | **~690 ms** |

Four `count(*) FILTER (...)` aggregates over one join read every one of a user's
score rows to evaluate filters matching a few hundred of them. A covering index
made it an index-only scan and took it to 68 ms — then it decayed back to 12,149
heap fetches, because `user_job_scores` is rewritten wholesale on every rescore
and its visibility map is never clean.

**The dead-tuple fraction was the multiplier, not the query.** The same count
measured 1,598 ms at 283,138 dead tuples and 285 ms immediately after a vacuum.
Postgres' default `autovacuum_vacuum_scale_factor` of 0.2 lets a 1.2M-row table
accumulate 240,000 dead tuples before it starts; this table now runs at 0.02.
*Caveat:* the 655 ms "new in 24 hours" figure is inflated by our own backfill —
`COALESCE(posted_at, first_seen_at)` makes every newly-ingested posting look new,
which is the effect `marketSummarySQL` already carries a comment about.
*Used in:* [roadmap §5.7](../engineering/roadmap-to-completion.md),
[backend-performance](../architecture/backend-performance.md)

### Market

<a id="a-01"></a>**A-01 — Software engineer job postings at 51 (Feb 2020 = 100).**
Indeed Hiring Lab postings index; the underlying FRED series `IHLIDXUSTPSOFTDEVE` is downloadable as
CSV, which is what makes this grade A rather than B — anyone can reproduce it.
*Used in:* [problem-statement §1](../product/problem-statement.md#1-the-market-rotated-it-did-not-simply-shrink)

<a id="a-02"></a>**A-02 — ML engineer job postings at 159 on the same index and baseline.**
Same source. The 108-point gap *within one profession* is the finding, and it is what "rotation, not
contraction" means.

<a id="a-03"></a>**A-03 — 71% of the software posting recovery, May 2025 → May 2026, came from senior roles.**
Indeed Hiring Lab. Directly establishes that the recovery bypassed junior engineers.

<a id="a-04"></a>**A-04 — Software developers aged 22–25 down ~20% from their late-2022 peak, across 33 consecutive months; older developers at the same firms grew.**
Stanford Digital Economy Lab, from ADP payroll records covering 3.5–5M workers monthly. The strongest
evidence in this body of research, because it observes **actual hiring decisions** rather than
postings or self-reports.
⚠️ **Caveats, both material:** (1) the headline appears variously as 13%, 16% and "nearly 20%" —
different cuts of one dataset, not contradictions, but not interchangeable either; (2) **causation is
contested** — see [C-02](#c-02). We use the shape, not the explanation.

<a id="a-05"></a>**A-05 — Applications per hire exceed 300, roughly 3× the 2021 figure.**
Ashby, 2026, from their own ATS data. First-party operational data from a large sample.

<a id="a-06"></a>**A-06 — 18–22% of online postings are "ghost jobs"; four properties correlate with a posting being real.**
Greenhouse, 2025. The correlates — posted within two weeks, present on the company's own site, salary
disclosed, named team or hiring manager — are directly implementable and are built into the data model.
Contrast with [C-01](#c-01).

<a id="a-07"></a>**A-07 — The genuine auto-reject is the knockout questionnaire; where AI ranking exists it determines order, not rejection.**
Recruiter accounts of operating iCIMS, Lever, Taleo and Greenhouse, corroborated by ATS vendor
documentation. A low rank ends an application with no rejection ever sent — which is why `ghosted` is
a distinct state in our schema.

<a id="a-08"></a>**A-08 — Fewer than half of organisations will use AI in HR at all in 2026.**
SHRM research. Skews heavily toward large employers and high-volume roles. Directly contradicts the
universality assumed by most "beat the ATS" advice.

<a id="a-09"></a>**A-09 — AI *editing* of human-written resume prose raised hires by 7.8%.**
Randomised controlled trial, ~481,000 jobseekers. An RCT at this scale is the strongest evidence type
available in this domain. Note precisely what it measures: **editing**, not generation. Pair with
[B-03](#b-03).

<a id="a-10"></a>**A-10 — Application delivery depends on the submission domain; Indeed's Apply Sync is opt-in and LinkedIn Easy Apply forwarding depends on the poster's configuration.**
Indeed's and LinkedIn's own documentation, plus a LinkedIn engineer's public statement. First-party,
and the single most under-documented mechanic in job search. Drives
[ADR-0004](../architecture/adr/0004-source-acquisition-policy.md).

<a id="a-11"></a>**A-11 — Scraping public data is not a CFAA violation (*hiQ v. LinkedIn*, 9th Cir. 2022, applying *Van Buren*) — but hiQ lost on breach of contract, having accepted LinkedIn's terms by creating accounts.**
Court records. Both halves matter, and citing only the first is the common error. The operative lesson
is that exposure is **contractual and attaches when you accept terms** — which is why we never create
an account on a source.

### Technical

<a id="a-12"></a>**A-12 — `title` and `datePosted` appear in ~99% of JSON-LD job postings; `baseSalary` and `employmentType` in ~80%.**
Web Data Commons analysis of crawled structured data. Determines that compensation filtering must
distinguish "below your floor" from "not disclosed".

<a id="a-13"></a>**A-13 — pgvector 0.8 added iterative index scans (fixing over-filtering on `WHERE`-constrained vector queries), parallel HNSW builds (30–50% faster), and up to 5.7× query improvement over 0.7.4.**
pgvector release notes and AWS Aurora benchmarking. Load-bearing for
[ADR-0003](../architecture/adr/0003-postgres-single-datastore.md) — our vector queries are *always*
filtered, so iterative scan is not optional.

<a id="a-14"></a>**A-14 — Amazon Prime Video reduced infrastructure cost ~90% by moving a video-quality monitoring service from distributed serverless components to a single process.**
Amazon's own engineering write-up. The bottleneck was orchestration overhead and passing data between
components through S3. ⚠️ **Scope it correctly:** this was *one service* being right-sized, not Amazon
abandoning microservices. Cited in [ADR-0008](../architecture/adr/0008-service-decomposition.md) for
the specific point that inter-component data movement dominates cost.

<a id="a-15"></a>**A-15 — ingress-nginx was retired; end of life March 2026, no further releases, bugfixes or security patches.**
Kubernetes Steering and Security Response Committee announcements (Nov 2025, Jan 2026). It was
critical infrastructure in roughly half of cloud-native environments. Decides the gateway choice.

<a id="a-16"></a>**A-16 — Kubernetes Gateway API core resources reached GA and replaced Ingress as the de facto traffic standard.**
Kubernetes project documentation. `GatewayClass`, `Gateway`, `HTTPRoute`, `GRPCRoute`, `ReferenceGrant`.

<a id="a-17"></a>**A-17 — River supports enqueueing a job inside an existing pgx transaction.**
River documentation and source. This is what eliminates the dual-write problem and therefore the need
for a transactional outbox — see [B-16](#b-16) and
[ADR-0005](../architecture/adr/0005-river-background-jobs.md).

<a id="a-18"></a>**A-18 — `buf breaking` detects wire- and JSON-incompatible protobuf changes against a previous snapshot.**
Buf documentation. Protobuf is forward/backward compatible provided field numbers are preserved and
tags never reused; the tool enforces exactly that.

<a id="a-19"></a>**A-19 — Public, unauthenticated job-board APIs and their per-vendor quirks.**
Vendor documentation plus independent comparison analyses, cross-checked. Endpoints, pagination, date
formats, and compensation representation per vendor. Full detail:
[source-catalog.md](source-catalog.md).

<a id="a-20"></a>**A-20 — Job-posting duplicate detection: no single technique performs acceptably alone; combining string comparison, text embeddings and curated weighted skill lookups significantly outperforms any one.**
Fraunhofer IAO / University of Stuttgart, arXiv 2406.06257. Peer-reviewed and deployed in production
with reported real-world validation. Drives both the three-stage dedup pipeline and the decision to
use a **curated** skill-adjacency table rather than embedding similarity.

<a id="a-21"></a>**A-21 — OWASP baseline for Argon2id is m = 19 MiB, t = 2, p = 1, producing ~100 ms on a modern server core; memory cost defeats GPU/ASIC attackers more effectively than time cost.**
OWASP Password Storage Cheat Sheet. An alternative balance of 46 MiB / t=1 / p=1 is given as
equivalent. Corrected an earlier unsourced claim — see
[verification-log V3](verification-log.md#v3--argon2id-parameters-were-asserted-not-sourced).

<a id="a-22"></a>**A-22 — Go 1.22+ `net/http.ServeMux` supports method matching and path wildcards, exposes `Request.PathValue`, returns 405 automatically, and detects conflicting patterns at registration time. Wildcards must occupy a whole path segment, and there are no route groups or middleware chaining.**
Go project blog and `net/http` documentation. Both the capabilities and the limits are load-bearing
for [ADR-0001](../architecture/adr/0001-go-for-backend.md)'s no-router-framework position.

<a id="a-24"></a>**A-24 — Runtime library size, minified + gzipped: React + ReactDOM ~48 KB, Vue ~33 KB, Svelte ~1.6 KB.**
Published package sizes, independently verifiable from the registry. Svelte's figure reflects that it
is a **compiler, not a runtime** — the framework largely disappears at build time. The ~46 KB
React-to-Svelte gap is a fact, not an estimate.

<a id="a-25"></a>**A-25 — Next.js App Router "First Load JS shared by all" measures 80.4 KB and 102 KB in public build outputs; one documented Pages→App Router migration went 141 KB → 200 KB (+42%).**
Real `next build` output. The shared baseline is inherited by every route. Community guidance derived
from the same measurements: < 130 KB good, 170–200 KB warning, 200 KB+ investigate. Decisive for the
budget arithmetic in [ADR-0002](../architecture/adr/0002-frontend-framework.md).

<a id="a-26"></a>**A-26 — HTTP Archive's Core Web Vitals Technology Report states explicitly that correlation does not equal causation, and that a technology correlating with good or poor performance does not indicate it caused that performance.**
HTTP Archive methodology documentation. The reason [B-36](#b-36) is weak supporting evidence rather
than a decision input — and worth quoting whenever framework pass-rate comparisons are cited at us.

<a id="a-23"></a>**A-23 — Greenhouse Job Board API schema: list endpoint returns `meta.total` and no pagination; `requisition_id` is exposed; `first_published`, `pay_input_ranges` (`min_cents`/`max_cents`/`currency_type`) and the AI-disclaimer fields are available only on the per-job endpoint with `pay_transparency=true`; `content` is double-HTML-escaped.**
Official schema, [`grnhse/greenhouse-api-docs`](https://github.com/grnhse/greenhouse-api-docs/blob/master/source/includes/job-board/_jobs.md).
Drove a design change (N+1 for structured comp), a correctness fix (`first_published` over
`updated_at` for posting age), a dedup improvement (`requisition_id`), and a new product feature
([F10](../product/feature-spec.md#f10--ai-screening-disclosure)).

---


<a id="a-31"></a>**A-31 — Ingest → visible latency, tier A: 56–65 min median.**
Measured 2026-08-25 by `make ingest-latency` over 407 postings that were published after we had
already begun polling their board and whose entire wait fell inside a period of continuous
ingestion: Greenhouse p50 56.3 min / p90 106.9, SmartRecruiters p50 64.5 / p90 113.1. Grade A — it
is our own instrumentation over our own rows, reproducible by one command.

*Caveat, and it is the whole story of this number.* Three of the four exclusions in the query are
load-bearing, and the naive version of it is off by a factor of thirty. Counting every posting gives
a SmartRecruiters median of **72 days**, which is not latency but backfill: adding a source ingests
its entire board, including roles posted two years ago, and stamps them all with today's
`first_seen_at`. Excluding those still gives **26 hours**, which is not latency either but downtime
— the ingest history has a 4-day-19-hour hole in it, because this corpus is built on a laptop that
sleeps. Only after discarding postings whose wait spans an outage does the figure become about
polling.

The result then agrees with theory to within a few minutes, which is why we believe it: a 2-hour
poll interval sampling uniformly-arriving postings should give a median of 60 min and a p90 of 108,
and Greenhouse measured 56.3 and 106.9. **The agreement is the evidence, not the number.** Two
figures derived independently — one from the config, one from the rows — landing on top of each
other is a much stronger claim than either alone, and it is what distinguishes this from the two
earlier versions that were also computed correctly and also meaningless.

Ashby is excluded from the headline: 52 qualifying rows is too few, and the probe says so rather
than reporting a median it cannot support.

<a id="a-32"></a>**A-32 — INP p75 at 4× CPU throttle: 72 ms.**
Measured 2026-08-25 by `make inp` over eight visits to `/jobs`, driving filter chips, per-keystroke
search and the sort control, reading the browser's own Event Timing entries: p75 72 ms, worst 80 ms,
all eight visits producing a sample. Budget is 200 ms. Grade A — the browser's own instrumentation
of the metric the budget names.

*Caveat.* Measured against the Vite **dev** server, so the JavaScript is unbundled and unminified
and every module is a separate request. For interaction cost that biases the figure PESSIMISTIC —
production ships less code through the same handlers — which is the safe direction for a budget
check, but it means 72 ms is a ceiling rather than the number a user gets. Chromium only: the Event
Timing API does not exist in Firefox or Safari, so this is a claim about Chromium and the browsers
that share its engine.

The tightest number here is the worst case, not the p75: 80 ms across eight visits means no single
interaction came close to the budget, which is a stronger statement than a quantile computed from
eight samples can make on its own. p75 of n=8 is the 6th value; treat it as an order of magnitude,
and the max as the real result.

*Gated in CI since 2026-09-30* (the `e2e` job). Its first run, on a GitHub-hosted runner against the
same dev server: p75 64 ms, worst 72 ms, n=8/8 — agreeing with the local figure within the
visit-to-visit spread.

<a id="a-33"></a>**A-33 — `GET /v1/me/dashboard` p95: 178 ms.**
Measured 2026-08-25 by `make load-test` (smoke profile, 30 s) against a seeded account carrying
~8,300 scores, with the dashboard scenario running CONCURRENTLY with the feed scenario rather than
alone: avg 124 ms, p90 156, p95 178, max 264, zero failures over 405 requests. Budget 400 ms.
Grade A — k6's own timings, and the threshold now lives in the script so the run exits non-zero on a
regression instead of printing a number nobody reads.

*Caveat, and it is the same one that makes this endpoint interesting.* This ran without deliberate
write churn. The same statement on the same data has measured 68 ms warm and **13,687 ms** under
ingest load — a 200× spread that no idle load test can see. 178 ms is therefore a floor with load
generated against it, not a worst case, and the honest reading is "the query shape is fine" rather
than "the endpoint is fast". The number that would actually settle it has to be taken while the
ingestor is writing, which is why `make load-test` says so in its own recipe.

A fresh account was rejected as the test subject: no resume means no scores, an empty dashboard, and
single-digit milliseconds that would pass the budget while measuring nothing.

<a id="a-34"></a>**A-34 — Personio, Recruitee and Workable: 106 postings across 10 boards, 0 errors.**
Measured 2026-08-25 after building all three adapters: Personio 3 boards / 44 postings,
Recruitee 3 / 20, Workable 4 / 42. Every board token was called from this machine before the adapter
was written and the resulting count re-checked against the database afterwards; the two agree.
Recruitee disclosed structured salary on 12 of 20. Grade A — our rows, our boards, one command to
reproduce.

*Caveat.* Ten boards is a thin sample and the tenants are European SMEs, so this says nothing about
how these vendors behave at Greenhouse scale. Two of the three add little to the India-first corpus.
The value is not volume: Recruitee is the only vendor besides Ashby with structured salary **and a
period**, and Personio is the only one publishing seniority and a years-of-experience range as
fields rather than prose.

**The number that nearly went in here was zero.** Personio's first live poll produced 44 postings
and stored none of them: `RawPosting.Raw` was marshalled back to XML, which the jsonb column rejects
on every row. Golden tests could not have caught it — they never touch a database — and the count
this entry would have reported without the live check was three boards, zero postings, zero errors.
`TestParse_RawIsValidJSON` now exists in all three packages.

<a id="a-35"></a>**A-35 — What the corpus knows about itself: pay 15.0%, skills 50.4%, experience 54.5%, work mode 55.9%.**
Measured 2026-08-31 from the `source_daily` rollup over 14,036 live postings, and rendered live on the
landing page by the absence field. Grade A — our own instrumentation over our own rows, recomputed
hourly and reproducible with one query.

**Pay is the outlier and it is not close.** We hold a figure for 2,110 of 14,036 roles; the other
three facts are known for roughly half. A reader's most-used filter is the one we can answer least
often, and the honest response is to show that rather than to fill the gap with an estimate. It also
corroborates [design-law's](../../.claude/skills/design-law/SKILL.md) 14.6% from an independent
recount five days later.

*Caveat.* These are counts of what **we** hold, not of what employers disclosed. Work mode in
particular is usually our own parse of the prose rather than a field anyone filled in, so a rise in
any of these numbers may mean our extraction improved rather than that the market got more open. The
column names say `known_*` for exactly this reason. Treat them as a measure of our coverage, and only
weakly as a measure of employer behaviour.

**A 38% overcount was found and fixed in the same work.** `source_daily.postings_live` tested
liveness as `closed_at IS NULL`, which counted all 5,371 *superseded* postings — duplicates that
dedup merged, and which therefore never get a `closed_at` because they were never closed. The
homepage was rendering "18,102 live now" on the corpus chart directly beneath a hero reading "13k
live postings" from `/v1/market`: same page, same word, two numbers 38% apart. The feed had always
filtered `status = 'live'`, so no reader could ever browse the roles the chart was counting. Both
figures now read 14,036.

<a id="a-36"></a>**A-36 — 40% of live postings are over 60 days old, and that is real, not a defect.**
Measured 2026-09-01 after closing every posting its board had stopped listing: 4,442 of 10,998 live
postings (40.4%) were posted more than 60 days ago, and 1,535 (14.0%) more than 180. Grade A — our
rows, one query.

The interesting part is what did NOT change. Before the repair, 42% were over 60 days old and the
obvious explanation was that dead postings were never being removed — which was independently true,
and which the repair fixed by closing 1,920 of them. The share moved to 40.4%. **The staleness is
therefore a property of these employers, not an artefact of our ingestion**: large boards genuinely
carry roles for months, and Bosch in particular keeps requisitions open far longer than a startup
does. A product that treats age as a ghost-job signal has to say which of the two it is measuring,
because on this corpus it is mostly the former.

*Caveat.* `posted_at` is the employer's own stated date and 42 postings carry an estimate flag. A
requisition that is edited and re-published may reset it, which would make this an undercount; one
that is never touched keeps its original date whether or not anyone is still hiring, which would make
it an overcount. The two errors run in opposite directions and we have not measured either.

**The related repair, worth its own line.** `ReconcileAbsent` bumped `missing_count` in a
data-modifying CTE and then updated the same rows again in the outer statement. PostgreSQL applies
one modification per row per command, so `status='closed'` never took effect, while the function
returned a closure count read from the command tag — which matched. It reported work it had not done
for its entire life, and no posting had EVER been closed: 1,920 were absent from their boards, one
for 29 consecutive polls, every one still served, scored and shown as applicable. Live postings fell
from 12,918 to 10,998 when the backfill ran.

<a id="a-37"></a>**A-37 — Country coverage: 64.5% → 90.8% of live postings, from one table.**
Measured 2026-09-01. Before: 4,005 of 11,276 live postings (35.5%) had no country. After widening
the country-name table and resolving unambiguous state codes: 1,036 of 11,266 (9.2%). Grade A — our
rows, one backfill, reproducible.

**The cause was nine entries.** `countryNames` knew India, the US, the UK, Germany, the Netherlands,
Singapore, Ireland, Canada and Australia. A 150-posting sample of the failures showed 71% named their
country in plain English in the last comma-segment — "Shanghai, Shanghai, China", "Budapest, ,
Hungary", "Tokyo, Japan" — and the lookup had no entry for it. SmartRecruiters posts a clean
City / Region / Country triple every time, and 58% of its live postings were landing with a null
country purely because of this map. A further ~5% carried a US or Canadian state code that the parser
computed into `Region` and then discarded, because a result with no city and no country was rejected.

*Caveat, and it is the reason the number is not higher.* The remaining 9.2% is mostly strings with no
country in them to find — "Remote", "Hybrid", "In-Office" were 6.7% of the sample — plus cities the
curated table does not know. Those are honestly unresolvable from `location_raw` alone, and inventing
a country for a posting that says only "Remote" would be worse than leaving it null.

**One pre-existing bug fell out of writing the tests.** `countryNames` mapped the bare code `"in"` to
India, so "Springfield, IN" resolved to India rather than Indiana. Removed: Indian postings spell
their city and reach IN through the city table, so the bare code bought nothing and cost a US state.
The new region table deliberately omits every code that collides with a country — CA, DE, IN, ID, PA
and the rest — because a wrong country on a filter people rely on is worse than an absent one.

<a id="a-38"></a>**A-38 — The field classifier is precise and low-recall: 93.5% vs 44.4%.**
Measured 2026-09-01 over 11,213 live postings, using an engineering-title regex as an independent
check on the classifier. Of postings it labels `software`, **93.5% carry an engineering title**. The
default feed, which hides only `other`, is **44.4%**. The whole corpus is 34.3%. Grade A — our rows,
two independent signals.

**Precision is high and recall is low, and the shape matters more than either number.** When the
classifier commits, it is nearly always right; it simply declines to commit on 43% of the corpus,
because one employer posts in German, Chinese and Hungarian and a token table cannot follow. Those
land in `unknown`, stay visible under the default, and are why Bosch is still 33.2% of the default
feed after classification.

This is the ADR-0018 trade working as designed rather than failing: hiding what we cannot name would
hide software jobs with it. But it also showed the design was one chip short — the most precise view
in the product existed and no reader could reach it. The rail now offers Software / + unsorted /
Everything.

*Caveat.* The 93.5% is measured against a title regex, which is itself imperfect — it misses
"Engineering Manager" and matches "Sales Engineer". It is an independent signal rather than ground
truth, and the right reading is "these two disagree rarely", not "the classifier is 93.5% accurate".
A labelled sample is still the honest way to claim accuracy, and has not been built.

<a id="a-39"></a>**A-39 — Adding 18 engineering-dense boards moved the corpus 34.3% → 35.4% engineering-titled.**
Measured 2026-09-02 after adding 18 verified Greenhouse and Ashby boards (1,182 live postings, zero
errors): whole corpus 13,484 postings at 35.4% engineering-titled, up from 34.3%. Grade A — our rows.

**The corpus-level number barely moved, and that is the finding.** One employer's board grew from
4,698 to 34.8% of the corpus over the same period — it posts faster than eighteen curated boards
could dilute it. **Board curation cannot outrun a single 4,700-posting conglomerate board**, which
settles a question [ADR-0018](../architecture/adr/0018-classify-and-expose-not-filter-at-ingest.md)
left open: curation was kept as a live alternative if classification underdelivered, and it is now
measured as the weaker lever, not the stronger one.

**What DID move is the view a reader actually uses.** `field=software` grew from 2,596 to 3,188
postings while holding 94.0% precision. That is 592 more genuinely-software roles at no cost to
quality, which is the entire point of the exercise even though the headline percentage is flat.

Description coverage also reached 80.9% (from 77.5%) as the SmartRecruiters sweeps continued.

*Caveat.* India moved 7.3% → 7.5%. Four of the eighteen boards were chosen for India presence and
the effect is within noise; most large India-native SaaS companies do not publish on Greenhouse or
Ashby at all — roughly 140 candidate tokens were tried and 404'd.

**The classifier cannot be improved by re-running it.** Of the 5,940 postings it labels `unknown`,
ZERO have six or more recognised skills and the bucket averages 0.90. Re-classifying now that
descriptions exist would change nothing, and the low skill count is itself evidence that most of that
bucket genuinely is not software work. Inferring `other` from an absence of recognised skills was
considered and rejected: it is the direction that hides a real job, which is the trade ADR-0018 is
built to refuse.

## B — Directionally trustworthy, numerically soft

### Screening and channels

<a id="b-01"></a>**B-01 — An ATS functions as a filing cabinet: applications are not automatically filtered or weighted; a human decides who advances.**
Recruiter accounts across iCIMS, Lever, Taleo, Greenhouse. Consistent across many independent
sources; B rather than A because it is practitioner testimony, not documentation.

<a id="b-02"></a>**B-02 — Parsing failures and knockout questions eliminate more applications than keyword gaps; hidden text and keyword stuffing are actively detected and flag an application as manipulative.**
ATS vendor guidance and recruiter accounts.

<a id="b-03"></a>**B-03 — 49% of US hiring managers report auto-dismissing resumes they suspect are AI-generated; 62% reject AI resumes lacking personalisation.**
Survey data; methodology partially disclosed. Pair with [A-09](#a-09) — the reconciliation is that
editing and generating are different operations.

<a id="b-04"></a>**B-04 — Postings collect hundreds of applications within hours; being in the first 24–48 hours matters more than resume polish.**
Recruiter accounts and aggregated ATS timing data. The mechanism is uncontested; the magnitude varies
by role and market. **This is the single claim most of the architecture is built on** — it is why
adaptive tiered polling exists and why ranking carries a freshness decay.

<a id="b-05"></a>**B-05 — Referrals are ~7% of applicants but 30–50% of hires.**
Direction consistent across many sources; specific numbers vary widely and the most-quoted "4×/10×"
figures are vendor-published. Use the direction, not the multiplier.

<a id="b-06"></a>**B-06 — Only ~6% of employees say they refer for the bonus.**
Survey data. A referral is a reputation stake, which is why generic "please refer me" fails.

<a id="b-07"></a>**B-07 — Hiring-manager outreach produces one to two orders of magnitude more responses than applying online alone.**
interviewing.io, from their own user data. Large effect, imprecise measurement.

<a id="b-08"></a>**B-08 — Cold email: 5–15% reply for highly targeted outreach, under 1% for mass sends; B2B baseline ~3.4%.**
Aggregated across outreach-tooling sources. ⚠️ Several are vendors selling outreach tools; treat the
targeted-vs-mass *gap* as the finding, not the specific percentages.

<a id="b-09"></a>**B-09 — Cold referrals rank net negative — worse than applying online.**
interviewing.io survey, ~500 respondents. **The most counter-intuitive finding in this body of
research and the one that most changes product design.** Mechanism: companies split "true referrals"
from "leads", and a stranger's referral lands in the lead pile. Reconciles with [C-06](#c-06) —
willingness is real, outcome is nil. Drives the entire design of
[roadmap v3](../product/roadmap.md#v3--referral-graph).

<a id="b-10"></a>**B-10 — Recruiters are measured on response rates rather than on hires, so sourcing rules exclude junior candidates by design.**
Recruiting practitioner accounts. Important because it is **not contested and not about AI** — this
mechanism would close the inbound channel to juniors regardless, and it is the one candidates can
route around by reaching hiring managers.

<a id="b-11"></a>**B-11 — `JobFunnel` was archived by its author: built when boards served static HTML, and boards moved to aggressive anti-automation, making the approach unmaintainable.**
The repository's own archival notice. The best available evidence on the durability of
scraping-based aggregation, and it is *architectural* evidence rather than opinion. Foundational to
[ADR-0004](../architecture/adr/0004-source-acquisition-policy.md).

<a id="b-26"></a>**B-26 — Naukri JobSpeak reports 0–3 year hiring up 11–17% in India.**
Naukri's own index. ⚠️ **Directly conflicts with [C-07](#c-07).** We display both, labelled, and do
not average them. The likely reconciliation is that "0–3 years" and "entry level" are different
populations — which puts a 1-YOE engineer exactly on the seam.

### Engineering

<a id="b-12"></a>**B-12 — Microservices benefits appear above ~10–15 developers; smaller teams experience net productivity loss from coordination and infrastructure overhead.**
Consistent across multiple engineering-practice analyses. Decisive for
[ADR-0008](../architecture/adr/0008-service-decomposition.md).

<a id="b-13"></a>**B-13 — Uber reached ~2,200 critical microservices and spent two years building DOMA to reduce the resulting complexity while retaining the benefits.**
Uber Engineering blog. Establishes both that proliferation is a real failure mode and that
domain-oriented boundaries with clean public interfaces are the remedy.

<a id="b-14"></a>**B-14 — Shopify runs one of the largest Rails codebases in existence as a modular monolith with boundaries enforced by static analysis (Packwerk), handling Black Friday peaks in the tens of TB/min.**
Shopify Engineering. The existence proof that mechanically-enforced module boundaries scale.

<a id="b-15"></a>**B-15 — For queue-driven workloads, backlog is the true scaling signal; infrastructure utilisation is a lagging proxy.**
KEDA documentation and CNCF practitioner write-ups. An I/O-blocked worker shows near-zero CPU while
its backlog grows, so a CPU-based HPA scales backwards.

<a id="b-16"></a>**B-16 — The transactional outbox pattern is required whenever a service must write to its database and publish to a separate broker (the dual-write problem); high-throughput implementations replace polling with CDC.**
microservices.io, AWS Prescriptive Guidance. Cited to explain why our architecture **does not need
it** — see [A-17](#a-17).

<a id="b-17"></a>**B-17 — PodDisruptionBudgets limit unavailability during voluntary disruptions; zero downtime requires replicas, spread, readiness probes, rollout settings, headroom, graceful termination and a PDB all cooperating.**
Kubernetes documentation and practitioner guides.

<a id="b-18"></a>**B-18 — Polite crawlers adapt delay to observed server response time and revisit at a rate proportional to observed change rate.**
Crawler-design literature and practitioner write-ups.

<a id="b-19"></a>**B-19 — Reciprocal Rank Fusion combines ranked lists using rank positions only, avoiding score normalisation across incomparable scales.**
Information-retrieval literature and multiple production implementations.

<a id="b-20"></a>**B-20 — `pgx` is the de facto Postgres package for Go since `lib/pq` entered maintenance; `sqlc` generates pgx-compatible code.**
sqlc and pgx documentation.

<a id="b-21"></a>**B-21 — `otelslog` adds under 1% overhead; `slog` is a zero-allocation structured logger in the standard library.**
OpenTelemetry Go documentation and benchmarks.

<a id="b-22"></a>**B-22 — ~~SvelteKit ships materially less JavaScript than Next.js or Nuxt (~12–20 KB vs ~70–90 KB)~~ — SUPERSEDED.**
⚠️ **Retired 2026-08-15.** The claim was directionally right but sourced from blog benchmarks with
unstated methodology, and the "65% smaller" / "1,200 vs 850 RPS" figures attached to it have no
traceable provenance and are **not used anywhere in this repository**. Replaced by
[A-24](#a-24) (runtime library sizes) and [A-25](#a-25) (measured framework baselines), which are
independently verifiable, plus [B-36](#b-36) for the correlational real-user comparison. See
[verification-log V10](verification-log.md#v10--frontend-bundle-sizes-resolved-with-the-soft-part-isolated).

<a id="b-36"></a>**B-36 — Real-user Core Web Vitals pass rates by framework (CrUX + HTTP Archive, homepages only): all-websites baseline 40.5%, Astro > 50%, SvelteKit above baseline, Next.js ~25%, Nuxt ~20%.**
Astro's 2023 Web Framework Performance Report. ⚠️ **Four caveats, all material:** (1) published by
**Astro**, a direct competitor to Next.js and Nuxt; (2) the authors themselves name version bias —
older frameworks carry a long tail of legacy sites on outdated versions — plus homepages-only scope
and unexamined framework-age effects; (3) HTTP Archive's methodology explicitly warns that
**correlation is not causation** here ([A-26](#a-26)); (4) it is a **2023** report and Next.js 16 /
mature RSC postdate it. Much of the spread is plausibly *who chooses each framework and for what*.
Treated as weak supporting evidence only — [ADR-0002](../architecture/adr/0002-frontend-framework.md)
does not rest on it.

<a id="b-37"></a>**B-37 — Bengaluru is 34% of tracked Indian tech listings, Hyderabad 19%, Pune 13%; tier-1 cities are 88–90% of IT demand.**
HireDoor, July 2026, from 18,400+ listings. The geographic concentration that drives the fan-out
derivation in [scaling-and-capacity §3](../operations/scaling-and-capacity.md#3-scoring-load).

<a id="b-38"></a>**B-38 — The share of tech jobs requiring three years of experience or less fell from 43% (2018) to 28% (2024).**
IEEE Spectrum. Independently corroborates the junior-market contraction in
[A-03](#a-03)/[A-04](#a-04), and supplies the experience-band term in the fan-out derivation.

<a id="b-23"></a>**B-23 — Layout-aware parsing that converts documents into an indexed linear text sequence before extraction significantly improves resume field extraction accuracy.**
Resume information-extraction literature (arXiv 2510.09722 and related).

<a id="b-24"></a>**B-24 — `golang-standards/project-layout` is not an official Go standard and the Go team does not endorse a fixed project structure.**
Go project documentation ("Organizing a Go module") and Go team statements.

<a id="b-25"></a>**B-25 — In OpenTelemetry Go, traces and metrics are covered by stability guarantees; the logs signal is usable in production but its API is not yet frozen.**
OpenTelemetry project status. Why we log through `log/slog` with a bridge.

<a id="b-27"></a>**B-27 — Redis delivers ~1.2 ms p50 vs PostgreSQL's ~8.5 ms on read-heavy cache workloads (3–12× throughput), but most applications never exceed Postgres's caching capacity, and the hidden costs of a second stateful system routinely exceed the benefit.**
2026 comparative benchmarking. Sets the prior for
[caching-and-storage](../architecture/caching-and-storage.md): the question is not which is faster but
at what point speed outweighs operational cost — hence numeric triggers rather than a preference.

<a id="b-28"></a>**B-28 — `bytea` values above ~2 KB move to TOAST with 2–10× query degradation; files beyond a few megabytes belong in object storage. Large `bytea` inflates backups, ships in full through WAL on every update, and requires full decompression to read.**
PostgreSQL documentation and practitioner analyses. Decisive for resume blobs, and the reason
description HTML (8–15 KB) is a genuinely marginal case with its own trigger.

<a id="b-29"></a>**B-29 — Shared caches store one response and serve it to many users, so personalised content must never enter one; `stale-while-revalidate` explicitly trades freshness for latency.**
MDN and CDN provider documentation. Why every authenticated response is `private` and the feed is
never cached at the edge.

<a id="b-30"></a>**B-30 — `golang.org/x/sync/singleflight` collapses concurrent identical calls, but is per-process only and causes head-of-line blocking through `Do`; `DoChan` with a timeout is the production form.**
Go documentation and production write-ups. Both limits shape the design: per-process is why 4 replicas
give 4 queries not 400 (and why Redis is not yet needed), and head-of-line blocking is why every
coalesced call carries a deadline.

<a id="b-31"></a>**B-31 — Lefthook is a single Go binary running hooks in parallel, roughly 10× faster than Husky (which runs sequentially through bash) on large projects.**
Git-hook tooling comparisons. What makes a < 5 s pre-commit budget achievable — and a hook slower than
that gets bypassed with `--no-verify`, protecting nothing.

<a id="b-32"></a>**B-32 — Atlas detects schema drift by comparing the target database against the expected state at the latest applied revision, and blocks `migrate apply` when they differ.**
Atlas documentation. Turns drift from something discovered after the fact into a synchronous
pre-apply control.

<a id="b-33"></a>**B-33 — Docker Compose Watch (GA) delivers sub-500 ms sync versus 2–4 s for traditional bind mounts, with distinct `sync`, `rebuild` and `sync+restart` actions; for compiled languages it triggers the Dockerfile's compile step.**
Docker documentation and 2026 practitioner reports. Removed the only real argument for running
processes on the host — see
[verification-log V5](verification-log.md#v5--the-dev-environment-was-not-containerised).

<a id="b-34"></a>**B-34 — Distroless images are regularly rebuilt and patched, so base-library vulnerabilities are fixed by pulling the latest tag; scratch has zero base CVEs but no maintenance stream. Neither ships a shell, which blocks most post-exploitation tooling.**
Container security comparisons. For a Go static binary the maintenance property decides it. Alpine
rejected on carrying BusyBox and a package manager.

<a id="b-35"></a>**B-35 — WCAG 2.2 adds nine success criteria (two A, four AA, three AAA), including 2.4.11 Focus Not Obscured (Minimum), 2.5.8 Target Size (Minimum) and 3.3.8 Accessible Authentication (Minimum) at AA.**
W3C WAI. Directly shapes the interaction design in
[frontend-architecture §5](../architecture/frontend-architecture.md#5-accessibility).

---

## C — Hypothesis only. Never a justification.

<a id="c-01"></a>**C-01 — "1 in 3 job postings are fake."**
Combines incompatible methodologies (employer-admission surveys vs. ATS data). The US Congressional
Research Service acknowledged in April 2025 that **no official ghost-job statistics exist**. We use
[A-06](#a-06) instead and never repeat this figure.

<a id="c-02"></a>**C-02 — AI caused the junior-developer employment decline.**
The Stanford authors attribute the divergence in [A-04](#a-04) to AI exposure; economists at Google
and Apollo's Torsten Slok attribute it to interest rates and the post-ZIRP correction. **Genuinely
unresolved.** We use the shape of the data and take no position on cause — the product implications
are identical either way.

<a id="c-03"></a>**C-03 — "75% of resumes are rejected by ATS bots before a human sees them."**
Widely repeated and **false**. Sources repeating it are generally selling resume-optimisation
services. Contradicted by [B-01](#b-01) and [A-08](#a-08). Recorded here specifically so nobody
reintroduces it.

<a id="c-04"></a>**C-04 — Hiring managers receive 30–50 cold emails daily and delete recognisably AI-generated ones on sight.**
Single practitioner account. Not a measurement — but a useful behavioural constraint, and it yields a
concrete rule: *if a sentence would work unchanged for a different company, delete it.*

<a id="c-05"></a>**C-05 — Recruiters report receiving eight applications from one candidate within two minutes and flagging it as spam.**
Anecdotal. Illustrative of why [AF1](../product/principles.md#af1--automated-application-submission)
exists; the anti-feature rests primarily on [B-11](#b-11), not on this.

<a id="c-06"></a>**C-06 — Engineers report willingly referring strangers who cold-contact them.**
Forum accounts with vote counts. Vote counts are not evidence. Reconciled with [B-09](#b-09):
willingness is genuine, outcome is nil, because the referral enters the lead pile.

<a id="c-07"></a>**C-07 — Indian entry-level openings down 44% (July 2026).**
Single analysis, methodology undisclosed. ⚠️ Conflicts with [B-26](#b-26). Shown labelled alongside
it, never averaged.

<a id="c-09"></a>**C-09 — Remote/hybrid share of job postings.**
All postings, Q2 2026: **3–4% fully remote, ~10% hybrid, 87% on-site.** Software engineering
specifically, LinkedIn late 2025: **67% offered remote or hybrid.** ⚠️ **Irreconcilable by averaging**
— different populations (all occupations vs. software), different dates, and the tech sector genuinely
diverges from the wider return-to-office trend. **This conflict matters more than most**, because a
remote posting matches every user regardless of geography, so fan-out is acutely sensitive to it:
13% under the low figure, potentially 35–45% under the high one. We build for the population where
the higher figure applies and plan capacity against the pessimistic branch. See
[scaling-and-capacity §3](../operations/scaling-and-capacity.md#the-tail-risk-exceeds-the-error-in-the-mean).

<a id="c-08"></a>**C-08 — 2025 tech layoff totals.**
Layoffs.fyi reports ~123K; TrueUp reports ~246K. **A 2× disagreement on the same year.** Different
inclusion criteria, neither authoritative. Not used anywhere in the product.
---

## Rules for using this ledger

1. **Any user-visible statistic must have an entry here, with its grade shown in the UI.**
2. **A C-grade claim may never justify a decision.** It may motivate an experiment.
3. **When two sources conflict, show both, labelled.** Do not average, do not pick the convenient one.
   [B-26](#b-26)/[C-07](#c-07) is the worked example.
4. **When a claim is contested on causation but not on shape** — [A-04](#a-04)/[C-02](#c-02) — use
   the shape and state the dispute.
5. **Adding a claim requires a grade and a caveat line.** An entry with no caveat has not been read
   critically.
6. **Re-grade on new evidence.** Grades move down as often as up; a downgrade is not a failure.
