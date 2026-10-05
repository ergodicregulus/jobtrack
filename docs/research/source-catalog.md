# Source catalog

> Status: **LIVING DOCUMENT**. Every quirk listed here must have a corresponding golden-file fixture
> in `internal/source/<vendor>/testdata/`. A documented quirk with no fixture is a regression waiting
> to happen.

Policy basis: [ADR-0004](../architecture/adr/0004-source-acquisition-policy.md). Pipeline:
[ingestion-pipeline.md](../architecture/ingestion-pipeline.md).

## Tiers

| Tier | Definition | v1 |
|---|---|---|
| **1 — Public ATS API** | Unauthenticated JSON/XML the vendor publishes so employers can embed listings. Stable because the vendor's paying customer depends on it | ✅ Built |
| **2 — JSON-LD career pages** | `schema.org/JobPosting` the publisher embeds specifically for machine consumption | ✅ Built |
| **3 — Licensed feeds** | A commercial agreement | Future, on merit |
| **4 — Board scraping** | LinkedIn, Indeed, Naukri, Glassdoor | ❌ **Prohibited** |

---

## Tier 1 — Public ATS APIs

### Greenhouse

> **Verified 2026-08-15** against the official schema
> ([grnhse/greenhouse-api-docs](https://github.com/grnhse/greenhouse-api-docs/blob/master/source/includes/job-board/_jobs.md)).
> This section previously understated the available fields; see
> [verification-log](verification-log.md#v1--greenhouse-field-coverage-was-materially-understated). `[A-23]`

```
GET https://boards-api.greenhouse.io/v1/boards/{board_token}/jobs?content=true
GET https://boards-api.greenhouse.io/v1/boards/{board_token}/jobs/{job_id}?pay_transparency=true
```

| Property | Value |
|---|---|
| Auth | None |
| Pagination | **None** — entire board in one response, even at 500+ roles. Response carries `meta.total` |
| List fields | `id`, `internal_job_id`, `title`, `updated_at`, `requisition_id`, `location.name`, `absolute_url`, `language`, `metadata`, and `content` when `content=true` |
| Detail-only fields | `first_published`, `company_name`, `application_deadline`, `pay_input_ranges`, `data_compliance`, `questions`, `include_ai_disclaimer`, `ai_disclaimer`, `ai_opt_out_request_url` |
| Compensation | **Two paths — see below** |
| Stable ID | ✅ `id` |

**Quirks and design consequences:**

- ⚠️ **`content` is HTML that is itself HTML-escaped.** The official example literally shows
  `&amp;lt;p&amp;gt;` — a double decode. This is exactly where sanitisation bugs hide; see
  [security §2](../operations/security-and-privacy.md#2-threat-model).

- ⚠️ **Compensation has no structured field on the list endpoint.** Two options, and we use both:
  - `?content=true` (or `full_content=true`, which appends intro/pay-transparency/conclusion blocks)
    embeds pay ranges **inside the description HTML** — text extraction, `comp_source='parsed_text'`.
  - The **per-job** endpoint with `pay_transparency=true` returns structured `pay_input_ranges[]`
    with `min_cents`, `max_cents`, `currency_type` — `comp_source='structured'`.

  So Greenhouse is an **N+1 vendor for structured comp**, like SmartRecruiters is for descriptions.
  We fetch detail **only for new or changed postings that the text parse did not resolve**, which
  keeps the cost proportional to churn rather than to board size.

- ⚠️ **`first_published` exists only on the detail endpoint**, and it is the correct field for posting
  age. `updated_at` moves on any edit, so using it would make an edited 40-day-old posting look fresh
  — directly corrupting the signal the product is built on
  ([P2](../product/principles.md#p2--freshness-is-the-product)). Where we have only `updated_at`, age
  is marked as an upper bound rather than reported as fact.

- ❌ **`requisition_id` is NOT a dedup key.** It is employer free text. Measured 2026-10-05: Stripe
  sets "See Opening ID" on every posting, Airbnb "ONE" on 137 unrelated roles, Brex one id on twenty
  different product roles. Treating it as a key hid 2,293 postings still listed on their boards
  ([ingestion-pipeline §5](../architecture/ingestion-pipeline.md#5-deduplication)).

- ✅ **`include_ai_disclaimer` / `ai_disclaimer` / `ai_opt_out_request_url`** — Greenhouse now exposes
  whether the employer runs AI talent matching, **and an opt-out URL**. This is a product feature, not
  a schema note; see [feature-spec §F10](../product/feature-spec.md#f10--ai-screening-disclosure).

- `data_compliance[]` carries GDPR consent and retention requirements per posting. Not used in v1,
  but recorded because it is relevant to how long we keep application context.
- Dates are ISO-8601 with offsets — the best-behaved vendor on this point.
- No search or filtering on the public feed; we build our own index.
- The single large response makes `ETag` / content-hash comparison unusually valuable.

**Fixtures:** `full-board.json`, `escaped-html.json`, `empty-board.json`, `detail-pay-ranges.json`,
`detail-ai-disclaimer.json`

### Lever — **DROPPED FROM SCOPE**

> **Verified 2026-08-15: this endpoint is not usable.** Every token tried
> returned `{"ok":false,"error":"Document not found"}` or an empty array,
> including sites whose careers pages are visibly Lever-hosted. The public
> `v0/postings` route appears to have been retired or gated.
>
> The schema notes below are kept because they were correct when written and
> because the epoch-milliseconds quirk is the kind of thing worth remembering if
> the endpoint returns. **No adapter is implemented, and none should be written
> until a live board can be demonstrated.**

```
GET https://api.lever.co/v0/postings/{site}?mode=json
```

| Property | Value |
|---|---|
| Auth | None |
| Pagination | `skip` / `limit` |
| Fields | text, categories{commitment, location, team}, hostedUrl, applyUrl, salaryRange |
| Compensation | Sometimes, structured |
| Stable ID | ✅ `id` |

**Quirks:**
- ⚠️ **`createdAt` is epoch milliseconds, not ISO-8601.** The only vendor that does this, and it
  produces plausible-looking dates in 1970 if mishandled.
- Flat JSON array with no wrapper — the cleanest shape of any vendor.
- `workplaceType` enum (`remote`/`hybrid`/`onsite`) is present but **not trusted**; we cross-check it
  against the description.

**Fixtures:** `full-board.json`, `epoch-millis.json`, `with-salary.json`

### Ashby

```
GET https://api.ashbyhq.com/posting-api/job-board/{board}?includeCompensation=true
```

| Property | Value |
|---|---|
| Auth | None |
| Pagination | None |
| Fields | id, title, location, department, workplaceType, jobUrl, applyUrl, compensation |
| Compensation | ✅ **Best of any vendor** — structured min/max/currency/interval |
| Stable ID | ✅ `id` |

**Quirks:**
- ⚠️ Compensation nests inconsistently: usually in `summaryComponents`, sometimes top-level,
  sometimes inside `compensationTiers[]`. All three shapes need handling.
- `includeCompensation=true` is required, and omitting it silently returns no salary rather than an
  error — a quiet failure worth a test.

**Fixtures:** `full-board.json`, `comp-summary.json`, `comp-tiers.json`, `comp-toplevel.json`

### SmartRecruiters

```
GET https://api.smartrecruiters.com/v1/companies/{company}/postings?limit=100&offset=0
GET https://api.smartrecruiters.com/v1/companies/{company}/postings/{id}      # for the description
```

| Property | Value |
|---|---|
| Auth | None |
| Pagination | `limit`/`offset`, **max 100** |
| Compensation | Rare |
| Stable ID | ✅ `id` |

**Status: IMPLEMENTED** (`internal/source/smartrecruiters`), verified live 2026-08-17.

**Quirks:**
- ⚠️ **Descriptions are absent from the list response.** One additional request per posting, returned
  as separate sections that must be reassembled in order.
- This is by far the most expensive vendor per posting. The adapter caps the second phase at **250
  details per poll**, and skips it entirely when the list hashes identically to the previous poll —
  so a steady-state board costs one request, and a 4,800-posting board backfills over several polls
  rather than arriving as a burst against someone else's API.
- ⚠️ **The company identifier is case-sensitive and is not the domain.** `bosch` returns an empty
  board; `BoschGroup` returns 4,803 postings. Five candidate boards initially looked dead for this
  reason alone. Always verify against `totalFound` before adding a board.
- `releasedDate` is a genuine **first-published** timestamp, not an updated-at. Unusual and valuable:
  freshness from this vendor is real rather than an upper bound.
- `refNumber` is the employer's own requisition code. Free text, so not a dedup key — see Greenhouse's `requisition_id`.
- ⚠️ **Field types vary by tenant.** `Ubisoft2` sends `department.id` as a number where every other
  tenant sends a string. The adapter decoded that unused id, every Ubisoft detail document failed to
  decode, and 0 of 347 postings had a description until 2026-10-05 — with no error, because a failed
  body is skipped by design. Decode only fields the adapter reads; the ingestor now warns when a poll
  fills fewer than half its detail requests.

**Why it earns its place — measured, not assumed** ([A-00f](evidence-ledger.md#a-00f)):

| Vendor | Extracted skills classified as must/nice |
|---|---|
| Greenhouse | 26.0% |
| Ashby | 24.2% |
| **SmartRecruiters** | **54.1%** |

The posting is split into named sections and one is `qualifications`, which the extractor already
treats as a requirements heading. Every other source hands us one blob and depends on the employer
happening to write a heading we recognise. This is the difference between a posting that produces
real must-haves and one whose skills component abstains.

`companyDescription` is assembled **last**, deliberately: it is boilerplate naming technologies used
across the whole group, and above the qualifications it would let a company-wide name-drop read as a
requirement for the specific role.

**Boards, verified live 2026-08-17:** `BoschGroup` (4,803 — 527 in India), `Wise` (438), `Ubisoft2`
(273), `Swiggy` (46, all India). `Visa` and `Bytedance` return 2 each and were not added.

**Fixtures:** `list.json`, `detail.json` (captured from BoschGroup)

### Workday — **Tier 1b**

> **Verified by direct call 2026-08-25** against NVIDIA
> (`wd5/nvidia/nvidiaexternalcareersite`). No published schema exists; every
> field below was observed, not documented.

```
POST https://{tenant}.{dc}.myworkdayjobs.com/wday/cxs/{tenant}/{site}/jobs
GET  https://{tenant}.{dc}.myworkdayjobs.com/wday/cxs/{tenant}/{site}{externalPath}
```

| Property | Value |
|---|---|
| Auth | **None** |
| Tier | **1b** — undocumented internal API. ADR-0004's durability argument holds (the endpoint *is* the customer's careers page) but it can change without notice |
| Board token | `{datacentre}/{tenant}/{site}`, e.g. `wd5/nvidia/nvidiaexternalcareersite`. None of the three is derivable from the others |
| Pagination | POST body `{limit, offset}`. `limit` above 20 is silently ignored |
| **`total` is CAPPED at 2,000 and lies** | Measured: NVIDIA reports `total: 2000` while its facet counts sum to **2,630**. Coverage must be derived from facets, never from `total` |
| List fields | `title`, `externalPath`, `locationsText`, `postedOn`, `bulletFields[0]` (requisition id) |
| Detail fields | `jobDescription`, `externalUrl`, `jobReqId`, `startDate`, `country`, `location`, `timeType` |
| Dates | **`postedOn` is unusable** — relative, localised, and ceilinged (`"Posted 30+ Days Ago"`). `startDate` on the detail endpoint is a real ISO date and is the only one we store |
| Descriptions | Detail only. Two-phase, bounded at 40 per poll and resumed from `DetailCursor` |

**Why it matters.** Workday is the enterprise default and what most large Indian
IT-services employers and captive centres run on, which makes it the single
largest available increase in the thinnest part of the corpus.

**Two traps, both hit during implementation and both now tested:**

1. **The vendor's cap.** `total` saturates at 2,000. The adapter walks the
   facet with the most countable values and merges the slices.
   `TestTruncationIsDetected` fails on a response whose facets exceed its
   `total`.
2. **Our own cap.** The first working version returned 2,037 against a true
   total of 2,630: it beat the vendor's cap and then truncated on
   `maxPagesPerSlice`, which was 60 pages (1,200 rows) against a largest slice
   of 1,794. Any per-slice bound must exceed the vendor cap divided by the page
   size, or it silently reintroduces the bug it exists to prevent.

**Not verified:** Accenture's tenant returned HTTP 422 for the site name tried,
so the token is wrong rather than the endpoint. Tenants are confirmed by call,
never guessed.

### Recruitee

```
GET https://{company}.recruitee.com/api/offers/
```

| Property | Value |
|---|---|
| Auth | None |
| Pagination | None — the whole board in one response |
| Fields | id, title, slug, description, requirements, careers_url, careers_apply_url, created_at, published_at, salary{min,max,period,currency}, remote/hybrid/on_site, country_code, city, state_name, department, employment_type_code, status |
| Compensation | **Structured, with a period — 12 of 15 disclosed on channable** |
| Stable ID | ✅ `id` |

**Verified live 2026-08-25** against channable (15), nmbrs (4) and hotelchamp (1).

**This is the richest list endpoint of any vendor we support.** Full description, structured salary
*with a period*, and explicit workplace flags, all without a detail fetch — so a Recruitee adapter
has no detail phase at all. An earlier version of this page called its compensation "Rare"; the
measurement says 80% on the board we checked, which is better than every vendor except Ashby.
Boards are small because Recruitee sells to European SMEs, so these are worth more per posting than
per board.

**Quirks:**
- ⚠️ **Per-company subdomain**, so an invalid slug fails as **DNS resolution, not HTTP 404** — a
  completely different error path, and the adapter must classify it or the circuit breaker misfires.
- ⚠️ **`api.recruitee.com/c/{company}/offers` is a different endpoint and returns 401 on every
  tenant.** That is the authenticated admin API, and it is out of scope under
  [ADR-0004](../architecture/adr/0004-source-acquisition-policy.md). The public one is the
  careers-site path above.
- Dates are `YYYY-MM-DD HH:MM:SS UTC` — a space rather than a `T`, and a zone abbreviation rather
  than an offset. Neither ISO-8601 nor RFC 3339. Parsing it with a Go `MST` layout silently yields a
  zero offset for anything but UTC, so the suffix is required and checked.
- Salary figures are **strings**, and the object is present with four nulls when undisclosed —
  "salary present" and "salary disclosed" are different questions.
- `remote`, `hybrid` and `on_site` are **not mutually exclusive**; hotelchamp returns hybrid and
  on_site together. Read them in priority order.
- `description` and `requirements` are two separate HTML bodies. The second is where
  years-of-experience and skills live, so dropping it loses most of the scoring signal.
- `published_at` ≠ `created_at`, sometimes by years: channable's "Open application" evergreen was
  created in 2019 and published in 2021.

**Fixtures:** `board-full.json` (one offer per salary/workplace variant)

### Workable

```
GET https://apply.workable.com/api/v1/widget/accounts/{account}?details=true
```

| Property | Value |
|---|---|
| Auth | None |
| Pagination | None |
| Fields | title, shortcode, code, department, employment_type, telecommuting, url, application_url, published_on, created_at, country/city/state, locations[], description (with `details=true`) |
| Compensation | Not exposed |
| Stable ID | ✅ `shortcode` |

**Verified live 2026-08-25** against blueground (26), skroutz (9), epignosis (5), persado (3).

An earlier version of this page gave the endpoint as `www.workable.com/api/accounts/{subdomain}`
and said locations and departments **come from separate endpoints and must be joined**. Neither is
true of the endpoint above: `details=true` returns descriptions and a structured `locations` array
in the same response, which is what makes this a one-request adapter rather than one request per
posting.

**Quirks:**
- ⚠️ **`published_on` is a DATE with no time of day.** Parsed as midnight UTC, which makes a posting
  look up to 24 hours *older* than it is. That direction is the only safe one — the opposite
  rounding manufactures freshness — and every Workable posting therefore carries
  `PostedAtIsEstimate`. It also means Workable cannot contribute to the ingest-latency measurement.
- ⚠️ **Without `details=true` there is no description at all.** Omitting it returns a board that
  parses cleanly and is useless.
- An account with no live vacancies returns **200 with an empty `jobs` array**, identical to a board
  that just closed every role.
- `employment_type` is often `""`. It stays empty rather than being guessed at.
- `code` is the employer's own requisition reference. Free text, so not a dedup key — see Greenhouse's `requisition_id`.
- The flat `country`/`city` pair only ever holds the FIRST location; the `locations` array is
  authoritative.

**Fixtures:** `board-full.json`

### Personio

```
GET https://{company}.jobs.personio.de/xml
```

| Property | Value |
|---|---|
| Auth | None |
| Pagination | None |
| Fields | id, name, subcompany, office, additionalOffices, department, jobDescriptions[{name,value}], employmentType, seniority, schedule, yearsOfExperience, keywords, occupationCategory, createdAt |
| Compensation | Not exposed |
| Stable ID | ✅ `id` |

**Verified live 2026-08-25** against urbansportsclub (38), orderbird (5), personio (1).

**The only vendor publishing seniority and a years-of-experience range as structured fields**, and
one of only three with a real time of day on the posting date. Both are scoring inputs we otherwise
infer from prose, which makes a Personio board unusually valuable per posting despite small tenants.

**Quirks:**
- ⚠️ **XML, not JSON** — the only such vendor.
- ⚠️ **A non-existent tenant does not 404.** It 307s to personio.com, which sits behind a bot check,
  so a wrong token yields HTML rather than an error. The guard is declaring `XMLName` on the root
  struct: with it `xml.Unmarshal` rejects a mismatched root instead of returning an empty feed that
  downstream would read as "this company closed every role".
- ⚠️ **The feed contains no links of any kind** — not to the posting, not to the application form.
  Both URLs are constructed from the tenant and the id, which makes them *our* claim rather than the
  vendor's.
- `?language=en` **does nothing**: orderbird returns German section names either way. An earlier
  version of this page listed the parameter as though it selected a language.
- Both `.de` and `.com` resolve; `.de` is canonical for every tenant including non-German ones,
  because Personio is a German company and never moved it.
- `yearsOfExperience` is a **range string** — "2-5", "lt-1" — not a number.
- `employmentType` is the contract ("permanent", "working_student") and `schedule` is the hours
  ("full-time"). They answer different questions and the more specific one wins: orderbird's
  Werkstudent role is `part-time` + `working_student`, and calling it merely part-time discards the
  fact that it is a student position.
- Descriptions arrive as named `{name, value}` sections in the employer's own language — often the
  only structure the body has, so the names are kept as headings.
- German-language descriptions are a feature for the corpus: they exercise normalisation paths that
  an all-English corpus never reaches.
- ⚠️ **Some tenants publish `<jobDescriptions />` — present and empty.** Verified 2026-09-01:
  orderbird returns four sections per position, urbansportsclub returns none for any of its 38. It is
  not a parse failure and there is no second source for the text: the job page is a JS-rendered
  Next.js app, so the description exists only after client-side hydration. Those postings are stored
  and score as an honest abstention. **Expect a Personio board's description coverage to be all or
  nothing**, and do not read a low figure as an adapter bug without checking the feed first.

**Fixtures:** `board-full.xml`

### Verified 2026-09-01 — Keka and BambooHR are open; Darwinbox and iCIMS are not

Four vendors probed live. Findings, not plans.

| Vendor | Endpoint | Result | Verdict |
|---|---|---|---|
| **Keka** | `{tenant}.keka.com/careers/api/embedjobs/{portal}/active/{orgId}` | 200 JSON on 3 tenants (spyneai, softprodigy, awfis) | ✅ **Built 2026-09-01** |
| **BambooHR** | `{company}.bamboohr.com/careers/list` + `/careers/{id}/detail` | 200 JSON on 3 tenants (flyio, posthog, palantir) | ✅ **Built 2026-09-01** |
| **Darwinbox** | `{tenant}.darwinbox.in/ms/candidateapi/job` | 403 Cloudflare bot challenge on 3/3 tenants | ❌ Out of scope |
| **iCIMS** | `{tenant}.icims.com/jobs` | 405 AWS WAF CAPTCHA on 3/3; documented feed is OAuth2 partner-gated | ❌ Out of scope |

**Two integer enums are NOT decoded, and the adapter refuses to guess them.**
`jobType` was 2 for all 16 postings on the sampled board — an intern, a product
manager, an analyst and an engineer alike — so it does not mean employment type
on this evidence. `salaryRange.salaryPeriod` took the values 0 and 4 over figures
of the SAME magnitude (1,800,000 INR at 0, 900,000 INR at 4), so it cannot be
read as year/month either. **Keka therefore emits no structured compensation**,
despite having the richest salary object of any vendor: publishing a monthly
figure as annual is the Ashby interval bug that put "$30 – $45 per year" in front
of users for an hourly contract. The description still goes through text
extraction, which has the plausibility guard. Both enums are recorded here so the
next person decodes them from a wider sample rather than rediscovering the
ambiguity.

**Keka matters most.** It is India-first, and India is 7.1% of a corpus for a
product that names India as its primary market. Its payload is unusually rich —
`title, description, jobLocations, jobType, experience, salaryRange, skillNames`
— which is structured compensation AND structured skills in the list response.
Caveat: the org identifier is a tenant-specific GUID that has to be read once
from the careers page, so board tokens are two-part and cannot be guessed.

**BambooHR is two-phase, and needs a browser User-Agent.** The list carries a
title, a department and a location and nothing else — the description, the
posting date and the seniority all live on `/careers/{id}/detail`. Shipping the
list alone would add postings that can never be scored on skills.

**The arrangement is `locationType`, not `isRemote`.** `isRemote` is null on every flyio posting.
`locationType` is the string `"0"`, `"1"` or `"2"` — on-site, remote, hybrid, per BambooHR's own
[API reference](https://documentation.bamboohr.com/reference/create-job-opening) (verified
2026-10-05). The bound on detail fetches (60 per poll) bounds the detail phase only; it once sliced
the list itself, which would have closed every posting past the 60th as absent.

Both endpoints answer **403 to a bare HTTP client**. That is bot-shaping rather
than authentication: no key, no session, no challenge, and the same public data
the careers page shows. Sending a normal User-Agent is how a normal client
identifies itself. Contrast Darwinbox below, which sits behind an actual
Cloudflare challenge — the difference is whether there is a control to defeat.

**Darwinbox is a policy decision, not a technical one.** The JSON endpoint
exists and returns data to a browser; it sits behind Cloudflare bot mitigation
that a plain HTTP client cannot pass. Getting through it would mean impersonating
a browser well enough to defeat a control the vendor deliberately put there,
which is not what [ADR-0004](../architecture/adr/0004-source-acquisition-policy.md)
means by a public first-party feed. **Excluded, and not to be revisited by
trying harder.**

---

## Tier 2 — JSON-LD career pages

> ⚠️ **Measured 2026-09-01: not viable as designed. Do not build this without
> re-testing first.**
>
> Fifteen URLs across eleven hosts were fetched and searched for
> `<script type="application/ld+json">` blocks containing a schema.org
> `JobPosting`. **Every one returned zero.** That included individual posting
> pages on job-boards.greenhouse.io (vercel, gitlab, anthropic), a company's own
> careers site (hellofresh), self-hosted pages (atlassian, google), and the
> aggregators (wellfound, weworkremotely, remoteok).
>
> The cause is that career pages are now client-rendered. Where JSON-LD exists at
> all it is injected after hydration, so an HTTP client sees a shell. Rendering
> the page to recover it is a materially different activity from reading a feed
> an employer published — it is executing someone's application to extract data
> their server chose not to send — and that is not what
> [ADR-0004](../architecture/adr/0004-source-acquisition-policy.md) permits.
>
> The tier is not rejected on principle; the principle is fine, and a static
> career page with JSON-LD would be squarely in scope. It is that we could not
> find one. **Re-test before building: this is a fact about the web in 2026, and
> it is the kind of fact that changes.**

## Tier 4 — Prohibited, and why

Recorded so the reasoning survives; each is a *hard* no, not a backlog item.

| Source | Why not |
|---|---|
| **LinkedIn** | ToS prohibits scraping; aggressive anti-automation; accounts get restricted. Easy Apply data would not be actionable anyway. *hiQ* lost on breach of contract precisely here `[A-11]` |
| **Indeed** | ToS; anti-bot. Delivery is unreliable by design — Apply Sync is opt-in `[A-10]` |
| **Naukri** | ToS. Structurally it is a **resume database, not an application system**, so scraping it would misrepresent what the listings are |
| **Glassdoor** | ToS; anti-bot |
| **Workday** (`*.myworkdayjobs.com`) | ⚠️ **Note the nuance:** Workday is a legitimate ATS whose apply URLs we happily *link to*, but its job search is a POST-based internal API, not a published feed. Consuming it would be reverse-engineering an undocumented interface — Tier 4 behaviour on a Tier 1 vendor. We ingest Workday postings only where a company also publishes JSON-LD |

**The boundary that decides all of these:** is the endpoint *published for machine consumption*? A
documented public board API is. An internal API discovered in a browser's network tab is not, however
technically accessible it may be.

---

## Discovery — finding boards to poll

The unglamorous problem nobody writes about: knowing that `acme` has a Greenhouse board at all.

| Method | Yield | Notes |
|---|---|---|
| Public ATS token lists | High | Community-maintained lists of board tokens |
| Career-page JSON-LD crawl of a curated company list | Medium | Also discovers the Tier-2 source |
| Career-page redirect inspection | High | `careers.acme.com` → `boards.greenhouse.io/acme` reveals both the vendor and the token |
| User submission | Low volume, **high value** | A user asking for a company is the strongest possible relevance signal, and it promotes to tier A immediately |
| Funding/news monitoring | Medium | Companies that just raised are hiring |

**Validation before adding:** the endpoint must return ≥ 1 posting, parse cleanly, and map to a
company we can identify. Failures are logged for manual review rather than retried indefinitely.

### Tokens are verified, never guessed

The starter list in `internal/seed/boards.go` carries an `Openings` count taken
at verification time, and every token was confirmed live before it was added.

This is a rule learnt the hard way. An earlier revision inferred tokens from
company names — `boards.greenhouse.io/razorpay`, `/swiggy`, `/zomato` — which
looked entirely plausible and were wrong. Nine of twelve did not exist. The
postings seeded from them carried apply URLs that 404ed, which is the single
worst failure this product can have: the whole claim is that the link lands in a
real requisition queue.

Razorpay's actual token, for the record, is
`razorpaysoftwareprivatelimited`. It is not guessable.

Recording the opening count also makes drift visible. A board that silently
drops to zero has usually **migrated ATS vendors**, not stopped hiring, and the
two need very different responses.

**Current coverage: 61 boards, ~6,700 live postings.** 34 Greenhouse, 27 Ashby,
across US, UK, EU and India.

### India coverage is thin, and honestly so

Four Indian boards, all Greenhouse. This is not a curation preference: most
Indian employers run Lever (see above), Darwinbox, Keka or an in-house portal,
and the large product companies that do use Greenhouse often gate the board
behind their own careers site rather than publishing the JSON endpoint. Closing
this gap needs a Darwinbox or Keka adapter, not a longer token list.

---

## Per-vendor operational notes

| Vendor | Priority | Typical board size | Poll cost | Notes |
|---|---|---:|---|---|
| Greenhouse | 1 | 20–500 | Low list, **N+1 for structured comp** | Widest coverage among startups and product companies. Detail fetch only on change |
| Lever | — | — | — | **Dropped**: public endpoint returns "Document not found" |
| Ashby | 1 | 5–100 | Low | **Best compensation data** — worth over-weighting in discovery |
| SmartRecruiters | 2 | 50–1000 | **High** (N+1) | Fetch descriptions only for changed postings |
| Recruitee | 3 | 5–50 | Low | Mostly European |
| Workable | 3 | 5–100 | Medium (3 endpoints) | |
| Personio | 4 | 5–50 | Low | XML; European |
| JSON-LD | 2 | 1–200 | Medium | Broadest reach, most variable quality |

**Discovery priority follows compensation quality, not board count.** Ashby boards are smaller but
carry structured salary data, which is one of the four ghost-job integrity correlates `[A-06]` and one
of the most-used filters. A hundred Ashby boards are worth more to users than a hundred Personio ones.

---

## Adding a vendor — checklist

- [ ] Endpoint documented here, with auth, pagination and rate limits
- [ ] Every quirk listed, each with a golden-file fixture
- [ ] Adapter implements `source.Adapter`
- [ ] Golden tests cover: full board, empty board, malformed response, and every listed quirk
- [ ] **Empty-board test asserts the board is NOT mass-closed** — the guard that has embarrassed every
      aggregator that skipped it
- [ ] Stable ID strategy documented (vendor ID, or the derivation rule)
- [ ] Compensation mapping, including the "not disclosed" case
- [ ] Date parsing, including the vendor's specific format
- [ ] Added to seed data so it appears in local development
- [ ] Rate limit and circuit-breaker thresholds set
