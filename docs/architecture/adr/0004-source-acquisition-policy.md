# ADR-0004 — Public first-party ATS feeds and JSON-LD only

- **Status:** DECIDED
- **Date:** 2026-08-15
- **Decision drivers:** durability of the data source, legal exposure, delivery correctness,
  maintenance cost

## Context

We need job postings. There are three ways to get them, and the choice determines both our legal
posture and whether the product still works in two years.

## Options

### Option A — Public first-party ATS APIs

Greenhouse, Lever, Ashby, SmartRecruiters, Recruitee, Workable, Personio publish **unauthenticated
JSON (or XML) endpoints** for their customers' job boards `[A-19]`. No OAuth, no partner approval, no
per-employer setup.

The structural reason these are durable: they exist so employers can embed listings on their own
marketing sites. **The ATS vendor's paying customer depends on them working.** Withdrawing them would
break the vendor's own product.

### Option B — `schema.org/JobPosting` JSON-LD on company career pages

Structured data the publisher embeds *specifically so machines can read it*, largely to appear in
Google for Jobs. `title` and `datePosted` appear in ~99% of JSON-LD job postings; `baseSalary` and
`employmentType` in ~80% `[A-12]`. Coverage is broad but quality varies.

### Option C — Scraping job boards (LinkedIn, Indeed, Naukri, Glassdoor)

Largest inventory. Also:

- **The board's interest is directly opposed to ours.** Anti-automation is aimed at competitors and
  catches everyone.
- **The strongest evidence is archaeological.** `JobFunnel` — once a leading open-source job scraper —
  was **archived by its own author**, whose explanation was that it was built when boards served
  static HTML and boards have since moved to aggressive anti-automation, making a fast CLI approach
  too fragile to maintain `[B-11]`. That archival note is the best available data on how well
  scraping-based aggregation holds up over time.
- **It breaks delivery.** Board listings syndicate and go stale; the apply URL may not be the live
  requisition. That directly defeats the mechanic the product exists to fix
  ([problem-statement §4](../../product/problem-statement.md#4-delivery-is-not-guaranteed--and-this-is-the-most-under-documented-mechanic-in-job-search)).

### The legal position

Worth stating precisely, because it is usually stated wrong in both directions.

Scraping **publicly accessible** data is **not** a CFAA violation — the Ninth Circuit held so in
*hiQ v. LinkedIn* (2022), applying the Supreme Court's *Van Buren* logic that one "exceeds authorized
access" only by obtaining information from areas that are off-limits, not by using authorised access
for a disapproved purpose `[A-11]`.

**But hiQ still lost.** In November 2022 the court found it had **breached LinkedIn's User Agreement**,
which it had accepted by creating accounts, and the case ended in a consent judgment `[A-11]`.

The lesson is precise and directly actionable: **the exposure is contractual, and it attaches when
you accept terms.** Site operators can also pursue trespass to chattels, copyright, and state
unfair-competition claims, and privacy regimes (GDPR, CCPA, India's DPDP) apply independently to
personal data.

So the safe position in 2026: public, non-personal data, no technical barriers bypassed, **and no
account ever created on a source**.

## Decision

**Options A and B only. Option C is prohibited.**

Binding rules:

1. **No account is ever created on any source.** This is what keeps us outside contractual terms
   entirely, and it is the single most important line in this ADR.
2. **No authenticated fetching, no CAPTCHA solving, no residential proxies, no browser fingerprint
   evasion.** If a source requires evasion, it is not a source.
3. **`robots.txt` is fetched, cached 24 h, and honoured** for JSON-LD career-page sources, including
   `Crawl-delay`. ATS API endpoints are exempt only where documented as a public integration surface.
4. **Identifying User-Agent with a contact URL.** If we cause a problem we want to be told, not
   blocked.
5. **Only job-posting data is stored.** Never recruiter or employee personal data, which would move
   us into personal-data processing for people who never interacted with us.
6. **A source removal request is honoured within 48 hours**, no argument, with a documented process.

Boards may be used as a *hint* — a user pasting a LinkedIn URL and our resolving it to the underlying
ATS requisition is fine, because it is user-initiated and resolves *toward* the first-party source.
Systematic board crawling is not.

## Consequences

### Good

- **Smaller but higher-quality inventory.** Every posting is a live requisition with a working
  first-party apply URL. That is the product's entire value proposition, and it happens to be what
  the legal-safe path also gives us.
- **Durable.** These endpoints are stable because the vendor's customers depend on them, unlike
  scraping targets that actively defend themselves.
- **Cheap to run.** Conditional GETs against JSON endpoints; no headless browsers, no proxy budget.
- **Legally clean**, and — more usefully — *simple to explain* to a vendor who asks what we are doing.

### Bad, and accepted

- **We will not have LinkedIn's or Naukri's inventory.** For the Indian market specifically this is a
  real gap: Naukri carries recruiter traffic we cannot see. Accepted, and partly mitigated by the
  product framing — Naukri's real channel is inbound profile search, not applications, so a JobTrack
  that ignored it is not as incomplete as it first appears.
- **Coverage skews toward companies using modern ATS platforms** — which correlates with startups and
  product companies, and happens to align with the target persona. A fortunate accident, not a plan.
- **JSON-LD quality is uneven**, requiring per-source confidence scoring.
- **Company discovery is a separate problem.** We must find boards to poll. Seed via public ATS token
  lists, user submissions, and JSON-LD discovery from career-page crawls of a curated company list.

## Revisit

If an ATS vendor offers a formal partner API with better data, take it — that is more of the same
thing, not a change of principle. If a board offers a legitimate licensed feed, evaluate on merit;
licensing is a contract we would enter deliberately, which is categorically different from breaching
terms we never read.
