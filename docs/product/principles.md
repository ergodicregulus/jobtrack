# Design principles and anti-features

> Status: **DECIDED**. The anti-feature list is binding on code review.

Principles that do not reject anything are decoration. Each of these has a concrete consequence and,
where relevant, names the feature it forbids.

---

## P1 — Ship data, not JavaScript

*The Game Boy principle.*

Pokémon Red ran on a 4 MHz CPU with 8 KB of work RAM and fit in about a megabyte of ROM, and it felt
instant. It achieved that by being **data-driven rather than code-driven**: creatures, moves, maps and
encounters were tables interpreted by a small fixed engine, not bespoke logic per case. The engine was
budgeted; the content grew.

Translated here:

- **The server does the work.** Filtering, sorting, scoring and pagination happen in Postgres and Go.
  The client receives a page of ~25 already-decided results and renders them.
- **The client is a small fixed engine.** Interaction budget is spent on rendering and navigation, not
  on shipping a query engine to the browser.
- **Budgets are fixed in advance and enforced in CI.** ≤ 100 KB gzipped first-load JS on `/jobs`;
  INP p75 ≤ 200 ms under 4× CPU throttle. See
  [frontend-architecture.md](../architecture/frontend-architecture.md#2-performance-budget--enforced-in-ci).

**What this forbids:** client-side filtering over a large in-memory job array, a charting library that
costs more than the charts are worth, and any state-management dependency added before a concrete
problem demands it.

## P2 — Freshness is the product

If a posting reaches the user after 300 people have applied, everything else we do is decoration.

- Ingest latency is a **product SLO**, not an infrastructure metric: median source→visible ≤ 90
  minutes for tier-A companies, ≤ 8 hours for tier C.
- Posting age is displayed on **every card**, never hidden behind a filter.
- A posting that disappears from its source feed is marked closed within one poll cycle. We do not
  guess at intent; observed feed behaviour is the ground truth.

**What this forbids:** batch nightly ingestion, and "we have 2 million listings" as a metric of
success. Inventory size is a board's vanity metric (see
[personas §4](personas-and-perspectives.md#part-4--the-aggregator--board)).

## P3 — Never claim precision we do not have

A match score of "87%" implies a calibrated probability. We do not have one, and neither does any
competitor displaying such a number.

Instead:

- Scores are shown as **bands** — `Strong fit` / `Plausible` / `Stretch` / `Unlikely` — with the
  numeric value available on expansion for users who want it.
- Every score carries a **component breakdown** naming what matched and what did not: *"7 of 9
  must-haves. Missing: Kafka, Terraform."*
- Every score carries a **parse confidence**. If we extracted only 40% of a job description because
  the source returned HTML-escaped soup, the UI says so rather than scoring on fragments.
- Where a statistic appears in the UI, its **evidence grade** appears with it.

This is the evidence-grading discipline from the market research, turned inward on our own output.

**What this forbids:** a headline "ATS score", a percentage without a breakdown, and any ranking whose
reasoning we cannot render as text.

## P4 — Refuse to show numbers the user will misread

Distinct from P3, and rarer in practice.

At individual job-search volumes, per-channel interview rates are statistically meaningless for the
first two to three months. Three interviews from ten referrals is not a 30% rate. A product that
displays "30%" is not merely imprecise — it will cause a user to change strategy based on noise.

**Therefore:** channel analytics suppress the rate entirely below a minimum sample size (n = 25 per
channel) and display `not enough data — 15 more for a reading`. The legitimate signal at low volume is
a channel producing **zero over 25+ attempts**, and that is what we surface.

**What this forbids:** a dashboard tile showing conversion percentages from week one, however
tempting.

## P5 — Assist editing, never authorship

The evidence splits cleanly. AI **editing** of human-written prose raised hires by 7.8% in an RCT of
~481,000 jobseekers `[A-09]`. Meanwhile 49% of hiring managers auto-dismiss resumes they suspect are
AI-generated, and 62% reject AI resumes lacking personalisation `[B-03]`.

So JobTrack's language features are: **gap analysis**, **compression** ("you supply the number, we
tighten the phrasing"), and **parse diagnostics**. Never generation.

The same rule applies to outreach, and there it is sharper. A useful test surfaced in the research:
*if a sentence in your message would work unchanged for a different company, delete it.* JobTrack can
assemble the research — recent launches, engineering-blog themes, the team's stated problems — but the
message is the user's to write.

**What this forbids:** a "Generate cover letter" button, and template-filling outreach.

## P6 — Every dependency is a liability

Stated as a budget, not a vibe. The target for v1:

| Layer | Direct dependency budget |
|---|---|
| Go (`go.mod`, excluding `golang.org/x/*` and OTel) | ≤ 12 |
| Frontend runtime (`dependencies`) | ≤ 6 |
| Infrastructure services in production | 2 — Postgres and the app itself |

Postgres does full-text (`tsvector`), vectors (`pgvector`), queues (River), pub/sub (`LISTEN/NOTIFY`)
and scheduling. That is one operational surface to secure, back up, monitor and upgrade instead of
five. Go 1.22+'s `net/http.ServeMux` handles method and path-parameter routing, so no router framework.

Full reasoning in [ADR-0003](../architecture/adr/0003-postgres-single-datastore.md).

**What this forbids:** adding Redis for caching before proving an in-process LRU is insufficient, and
adding Elasticsearch before proving Postgres FTS is insufficient at our data size (see
[scaling-and-capacity.md](../operations/scaling-and-capacity.md)).

## P7 — Explainability is a feature, not a debug tool

If the system ranked a job at position 3, the user can ask why and get an answer in the UI. If it
marked a posting stale, the user can see the last time it was observed in its source feed. If it
deduplicated two listings, the user can see both originals.

This is not altruism — it is how we earn trust from a persona that has been marketed at by
resume-optimisation vendors for two years and is correctly cynical.

**What this forbids:** an opaque learned ranker as the primary ordering. Learned re-ranking may sit
*on top of* an explainable base score, but the base must remain legible. See
[ADR-0006](../architecture/adr/0006-hybrid-retrieval-and-scoring.md).

## P8 — The user's data is theirs, and it is the most sensitive thing we hold

A resume is a dense PII package: name, contact details, employment history, education, sometimes
address and date of birth. Under India's DPDP Act 2023 and the GDPR it is personal data with real
obligations.

- Resume text and extracted entities are **encrypted at rest** with a per-user data key.
- Full export and hard delete are **v1 features**, not compliance debt.
- No third-party analytics that receive user content. Telemetry is self-hosted and carries no PII.
- If any LLM enrichment is enabled, it is **opt-in per user**, disclosed at the point of use, and the
  configuration records exactly which fields leave the system.

See [security-and-privacy.md](../operations/security-and-privacy.md).

## P9 — Design for the next three products, build only this one

The stated long-term shape is a single platform for engineers seeking work: referral graph, resume
reviewer with human-standard critique, interview preparation, compensation intelligence.

That is a reason to get **boundaries** right now — a `domain` package that does not import a database
driver, an ingestion pipeline that does not know about scoring, a scoring engine that is configuration
driven — and **not** a reason to build abstractions for features that do not exist. The test for any
"we'll need this later" argument: name the ADR that will use it. If you cannot, do not build it.

---

## Anti-features

**These are binding.** A pull request implementing any of the following is closed on sight, regardless
of implementation quality. Each has an evidentiary basis, not just a preference.

### AF1 — Automated application submission

Discovery, scoring, and drafting can be automated. **Submission cannot.**

- Recruiters report receiving eight applications from the same candidate within two minutes and
  flagging it as spam `[C-05]`.
- LinkedIn Easy Apply bots violate the platform's terms and get accounts restricted.
- The strongest evidence is architectural: `JobFunnel` was **archived by its own author** because
  boards moved to aggressive anti-automation and the approach became unmaintainable `[B-11]`. Tools
  built on automated submission have a short half-life by construction.
- It also inverts the finding that matters most: applications that convert in 2026 are tailored, not
  fast.

**The line:** JobTrack may pre-fill a structured profile the user can copy or export. It may never
press submit.

### AF2 — Keyword stuffing, hidden text, or "ATS beating"

Hidden text and keyword stuffing are actively detected and flag an application as manipulative `[B-02]`.
Beyond being ineffective, selling this is the exact behaviour of the vendors whose credibility our
persona has already written off.

### AF3 — Fabricated match percentages

See P3. A number without a breakdown is forbidden.

### AF4 — Scraping behind authentication or anti-bot defences

No logged-in scraping, no CAPTCHA solving, no residential-proxy rotation, no fingerprint evasion.
Public first-party feeds and publisher-intended structured data only.

The legal position we operate under: scraping public, non-personal data without bypassing technical
barriers is not a CFAA violation after *hiQ v. LinkedIn* applying *Van Buren* — **but hiQ still lost**,
on breach of contract, because it had accepted LinkedIn's user agreement by creating accounts `[A-11]`.
The lesson is precise: the exposure is contractual, and it attaches when you accept terms. We never
create accounts on sources, so we never accept their terms.

### AF5 — Engagement mechanics built on anxiety

No streaks, no "you haven't applied in 3 days", no leaderboards, no comparison to other users' volume.
Persona P3 (the returner) vetoes this category, and P2 (the passive switcher) is actively repelled by
it. The product's job is to reduce the number of hours a job search consumes, which is incompatible
with maximising daily active use.

### AF6 — Dark-pattern retention

Export and delete are one click, always available, and never behind a retention flow. If a user got a
job, the correct experience is congratulations and a clean export.

---

## Using this document in review

A reviewer should be able to reject a change by citing a principle number, and the author should be
able to appeal by writing an ADR. That loop is the whole mechanism — principles are only real if
overriding them costs something.
