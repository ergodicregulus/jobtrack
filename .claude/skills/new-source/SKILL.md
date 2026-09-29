---
name: new-source
description: Use when adding or modifying a JobTrack ATS source adapter (Greenhouse, Ashby, SmartRecruiters, Workday, Personio, Recruitee, Workable, JSON-LD career pages). Covers the acquisition-policy gate, the pagination-cap trap, and golden-file discipline.
allowed-tools: Read, Grep, Glob, Bash, WebFetch, Edit, Write
---

# Adding a source adapter

## 1. The policy gate — answer this before writing any code

`docs/architecture/adr/0004-source-acquisition-policy.md` is binding. A source is
in scope only if **all** of these hold:

- It is a **public, first-party feed** — the vendor's own endpoint, or
  `schema.org/JobPosting` JSON-LD on the company's own careers page.
- **No account is ever created.** Not a free one, not a throwaway one.
- **No credential, API key or token** is required.
- No authentication is bypassed and no `robots.txt` is ignored.

If the vendor requires a per-customer key — Teamtailor, JazzHR — **stop. It is
out of scope**, and adding it needs a superseding ADR, not a pull request.

The account rule is the load-bearing one and it is not timidity. Scraping public
data is not a CFAA violation after *hiQ v. LinkedIn* — but **hiQ still lost**, on
breach of the User Agreement it accepted *by creating accounts*. Never holding an
account is what keeps this project outside contractual terms entirely.

## 2. Verify by calling it, not by reading about it

Blog posts about ATS endpoints are grade-C evidence. Call the endpoint and record
what actually came back, with today's date:

```bash
curl -sS -H 'User-Agent: JobTrackBot/1.0 (+https://github.com/ergodicregulus/jobtrack)' '<url>' | head -c 2000
```

Note the response shape, whether `ETag`/`Last-Modified` are served, whether
descriptions arrive in the list response or need a second call, and **the total
count**.

## 3. The trap: read the total count and believe it

**This has already cost us once.** BoschGroup was truncated to 41% of its board
for a week because pagination stopped early. Both Workday tenants tested returned
exactly `total: 2000` — that is a cap, not a coincidence.

Every adapter must:

- **Walk facets** (location, category, date) when one query cannot reach the
  whole board.
- **Have a fixture that trips the cap**, and a test that fails on truncation.
  `internal/source/greenhouse/testdata/board-truncated.json` is the pattern to
  copy.

A source that silently returns 41% of a board is worse than one that returns
nothing, because nothing is visibly broken.

## 4. Capture fixtures

```bash
make capture-source VENDOR=<vendor> BOARD=<board-token>
```

Add the vendor's URL to `CAPTURE_URL` in the `Makefile` first. Capture at least:
a full board, an empty board, a malformed body, and a truncated/capped response.
**CI never touches a live ATS** — fixtures are the only inputs.

## 5. Implement

One package per vendor: `internal/source/<vendor>/adapter.go`, satisfying
`internal/source.Adapter`:

```go
Vendor() Vendor
Fetch(ctx context.Context, src Source) (FetchResult, error)
Parse(body []byte) ([]RawPosting, error)
```

`Parse` is separate from `Fetch` precisely so golden tests can drive it directly.
Keep the split.

Rules that bite here:

- **Return `NotModified` honestly.** Carry both `ETag` and `Last-Modified` — vendors
  are inconsistent about which they honour. ~90% of polls are 304 or identical at
  steady state, and that is what makes 2-hourly polling affordable.
- **Never sanitise `DescriptionHTML`.** Adapters do not decide what is safe to
  render; that happens downstream.
- **Never import `internal/store`.** An adapter returns `RawPosting`; whether it
  gets persisted is the ingest job's decision. `make arch-check` enforces this.
- **Timestamps differ per vendor.** Lever returns epoch millis, not ISO-8601.
  Record the quirk in `docs/research/source-catalog.md`, not in a code comment
  nobody finds twice.

## 6. Test, and read the diff

```bash
make test-golden                # must pass
make test-golden UPDATE=1       # regenerate — then READ THE DIFF
```

**A blind `UPDATE=1` is the one way golden tests stop working as a technique.**
The diff is the test. If you cannot explain a changed line, do not commit it.

## 7. Record it

- `docs/research/source-catalog.md` — endpoint, auth posture, quirks, tier, and
  the date you verified it. An undocumented internal API (Workday) is **Tier 1b**,
  not Tier 1: it is the customer's own careers page, so the durability argument
  holds, but it is not a published contract.
- `make arch-check && make test-golden` before claiming done.
