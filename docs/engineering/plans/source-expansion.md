# Source expansion

**Status:** Keka and BambooHR built. JSON-LD measured and rejected. 18 engineering boards added. What remains is the India ceiling, which is not an engineering problem. **Done when:** India is a share of the corpus that
matches the product's stated focus, and the Tier 2 career-page route is either
built or rejected in an ADR.

## The problem, measured

India is **1,006 of 13,484 live postings — 7.5%** — in a product whose
[phase-5 §7.4](../phase-5-production-readiness.md) names India as the primary
market. Every vendor added so far has been Western: Greenhouse, Ashby and
SmartRecruiters are US-first, and Personio, Recruitee and Workable are European.
They were the right adapters to build and they do not move this number.

## Steps

1. **[sonnet]** ~~Verify the four unverified vendors from §7.2~~ — done 2026-09-01.
   Keka and BambooHR are open and now BUILT; Darwinbox is behind a Cloudflare
   challenge and iCIMS behind an AWS WAF CAPTCHA, both excluded under ADR-0004.
   Original brief: Darwinbox, Keka, iCIMS, BambooHR. Record status, auth requirement, response
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

## The India ceiling, measured

Four of the eighteen boards added on 2026-09-02 were chosen for India presence
and the share moved 7.3% → 7.5%. That is within noise, and the reason is not
effort: **roughly 140 candidate tokens for India-native companies were tried
against Greenhouse and Ashby and every one 404'd.** Hasura, Freshworks,
Chargebee, BrowserStack, Whatfix, Zoho, Meesho, CRED and Zepto are not on public
boards for either vendor.

So this route has a ceiling, and it is low. The vendors that would move it are
the India-first ATSs, and of the two verified only Keka is reachable — Darwinbox
is bot-walled. **The honest next step is more Keka tenants**, which is a
discovery problem (each needs its org GUID read from a careers page) rather than
an adapter one, and that is bounded, mechanical work.
