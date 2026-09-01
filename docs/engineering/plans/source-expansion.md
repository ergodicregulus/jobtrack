# Source expansion

**Status:** Keka built and ingesting (28 postings, 3 boards). BambooHR verified and not built. JSON-LD tier not started. **Done when:** India is a share of the corpus that
matches the product's stated focus, and the Tier 2 career-page route is either
built or rejected in an ADR.

## The problem, measured

India is **815 of 11,131 live postings — 7.3%** — in a product whose
[phase-5 §7.4](../phase-5-production-readiness.md) names India as the primary
market. Every vendor added so far has been Western: Greenhouse, Ashby and
SmartRecruiters are US-first, and Personio, Recruitee and Workable are European.
They were the right adapters to build and they do not move this number.

## Steps

1. **[sonnet]** Verify the four unverified vendors from §7.2 by calling them:
   Darwinbox, Keka, iCIMS, BambooHR. Record status, auth requirement, response
   shape and one real tenant per vendor in
   [source-catalog](../../research/source-catalog.md). Findings only — no adapter,
   no judgement about whether to build.
   **Darwinbox and Keka matter most: both are India-first ATSs**, and if either
   has a public feed it is worth more to this product than the last three
   adapters combined.
2. **[opus]** Read the results against [ADR-0004](../../architecture/adr/0004-source-acquisition-policy.md).
   An endpoint that exists is not an endpoint we may use; the acquisition policy
   gate is a separate question from the technical one, and `/new-source` walks it.
3. **[opus]** Build whichever pass. The `source.Do` helper means a new
   single-request adapter is now about 120 lines, and `/new-source` carries the
   pagination-cap trap that truncated a board for a week.
4. **[opus]** ~~Tier 2, JSON-LD career pages~~ — **measured and not viable**, 2026-09-01.
   Fifteen URLs across eleven hosts, zero `JobPosting` blocks in server-rendered
   HTML: career pages are client-rendered and any JSON-LD is injected after
   hydration. Recovering it would mean executing someone's application to get
   data their server declined to send, which ADR-0004 does not permit. The
   reasoning is sound and the web has moved; see source-catalog for the evidence
   and re-test before reviving it.
5. **[sonnet]** Golden fixtures for whatever ships, captured from real tenants,
   trimmed to the branches the adapter takes.

## Traps

- **A vendor that returns 200 is not a vendor with a public feed.** Recruitee's
  documented endpoint returns 401 on every tenant; the careers-site path is the
  public one. Probe with a real tenant or the answer is meaningless.
- Board tokens are verified, never guessed — see source-catalog. A guessed slug
  that 404s is indistinguishable from a company that stopped hiring.
- Adding India-heavy boards will move the field-relevance ratio too, in whichever
  direction those boards lean. Coordinate with
  [corpus-relevance](corpus-relevance.md) rather than measuring both at once and
  attributing the change to the wrong cause.
