# Implementation status

> Status: **LIVING DOCUMENT**. Updated 2026-08-16 after the River wiring, the
> scoring engine, the full user flow and the premium frontend landed.

What exists, what is stubbed, and what has been verified running. Kept because
the alternative — design docs describing a system with no way to tell how much
of it is real — is how a codebase starts lying about itself.

## Verified working

Everything here was exercised against real containers, not reasoned about.

### Migrations and deployment

| Capability | Evidence |
|---|---|
| **Fresh install** | 8 migrations applied to an empty database; backfills recorded `skipped` — *"fresh database, no rows to backfill"* |
| **Upgrade** | Adding a migration produced `pending=1 already_applied=7 fresh_database=false`. **Only the new one ran** |
| Idempotent re-run | Second `up` is a no-op; one ledger row per migration |
| 5 concurrent migrators | Serialised on the advisory lock; no duplicates |
| **Edited-migration detection** | Caught a real conflict in development: a version number reused across two branches failed with `ErrChecksumMismatch` rather than silently diverging |
| Non-transactional migrations | `CREATE INDEX CONCURRENTLY` applies via the statement splitter |
| Failure → repair → retry | Dirty row blocks further runs; `migrate repair <v>` clears it |
| Schema gate at startup | Services call `migrate verify` and refuse to boot against a stale schema |

### Ingestion

| Capability | Evidence |
|---|---|
| **Greenhouse adapter** | 20 tests green against golden fixtures built to the official schema |
| Double-escaped HTML | `&amp;lt;p&amp;gt;` → `<p>`, and the decode is idempotent on clean HTML |
| Truncated-response guard | `meta.total=214` with zero jobs is `ErrSuspiciousEmpty`, **not** a mass closure |
| Structured compensation | `pay_input_ranges` takes the widest envelope across per-location bands |
| Date correctness | `first_published` is authoritative; list-endpoint dates are marked `posted_at_is_estimate` |
| AI screening disclosure | Captured with opt-out URL; absent stays `null` (unknown), never `false` |
| Conditional requests | `If-None-Match` / `If-Modified-Since` sent; 304 carries validators forward; content hash is the second line |
| **Normalisation** | `Gurgaon → Gurugram`, `₹28-42 LPA → 2 800 000–4 200 000 INR`, section-aware must/nice skill split |
| Seed from fixtures | Retired. The seed now registers **real boards** and lets the ingestor fetch them, because synthetic postings carried apply URLs that 404ed |
| **Live ingestion** | **6,672 live postings from 61 companies**, fetched from real Greenhouse and Ashby endpoints |
| **Apply links resolve** | Sampled apply URLs return HTTP 200 at the employer's ATS |
| **Ashby adapter** | Three compensation shapes handled; `isListed=false` filtered; `publishedAt` is authoritative, not an estimate |
| **River scheduling** | `schedule_sources` claims due sources with `FOR UPDATE SKIP LOCKED` + jitter; adaptive tiers retier hourly |
| **Per-host politeness** | Concurrency of exactly 1 per host, adaptive delay `max(base, observed*2)` capped at 30 s |
| **Deduplication** | Requisition-exact then trigram similarity >0.75 with location agreement — 75 duplicates collapsed on OpenAI's board alone |
| **Transactional enqueue** | Postings and their scoring jobs commit together; a crash cannot leave one without the other |

### Scoring

| Capability | Evidence |
|---|---|
| **Scoring engine** | 8 unit tests; 15,000+ scores computed across the live corpus |
| Component breakdown | Skills 40 / experience 20 / location 15 / compensation 15 / freshness 10, each with its own reasoning string |
| Abstention | Undisclosed salary abstains from BOTH numerator and denominator — verified not to change the band |
| **Skills abstention is different** | A posting whose requirements are unreadable is capped below `strong` and pulled toward the midpoint (see bug 15) |
| Asymmetric experience | Stretching upward costs less than being over-qualified |
| Adjacency | Curated, never inferred: exact > adjacent > unrelated, and unrelated scores exactly 0 |

### API

| Capability | Evidence |
|---|---|
| Auth flow | register → login → `/v1/me` → logout → 401 |
| User-enumeration resistance | Wrong password and unknown address return byte-identical responses |
| Feed endpoint | Keyset pagination, 8 filters, facet counts |
| Unknown query parameter | Rejected with 400 rather than silently ignored |
| Error shape | `application/problem+json` with `trace_id` on every error, including unmatched routes |
| Preferences | jsonb round-trip; unknown keys preserved across a read-modify-write |
| Graceful drain | SIGTERM flips `/readyz` to `503 draining` **before** draining |

### Frontend

| Capability | Evidence |
|---|---|
| **Full journey** | 24 Playwright tests green: sign up → onboard → dashboard → feed → save → tracker → sign out |
| **Every page built** | landing, sign-in, sign-up, 4-step onboarding, dashboard, feed, profile, tracker |
| **Bundle budget** | 60.0 KB of 100 KB client JS gzipped; 7.2 KB of 20 KB CSS |
| **Feed latency** | 23ms p50 / 60ms p95 in-network, against a 120ms p95 budget |
| **Dark mode** | Three-state theme (light/dark/system), server-stamped, verified visually on every page |
| **Progressive enhancement** | Feed, filters and every form work with JavaScript disabled |

| Capability | Evidence |
|---|---|
| **SSR** | Real job cards in the initial HTML, verified with **JavaScript disabled entirely** |
| **No theme flash** | `jt_theme=dark` → `data-theme="dark"` in the raw HTML, before first paint |
| Three-state theme | `system` leaves the attribute **empty** so `prefers-color-scheme` decides; it is not resolved server-side |
| **Injection safety** | `jt_theme='"><script>…'` is rejected by allowlist, not interpolated |
| **Bundle budget** | **39.6 KB / 100 KB** client JS, **2.7 KB / 20 KB** CSS, gzipped |
| Filter state in URL | Shareable, back-button-correct, works with no JavaScript |
| **E2E** | **13/13 Playwright tests passing** against the live stack, container-to-container |

## The bundle number closes an open question

[verification-log V10](../research/verification-log.md#v10--frontend-bundle-sizes-resolved-with-the-soft-part-isolated)
listed "our own `/jobs` bundle size" as unverified, because framework baselines
are not the same as an application's real figure.

**Measured: 39.6 KB gzipped, 40% of budget.** That is our own build, not a
comparison blog post, and it is the number the budget should have rested on all
along. The ADR-0002 decision is now supported by first-party evidence rather
than by direction-only benchmarks.

## Bugs the tests caught

Recorded because they are the argument for writing tests first, and because
every one would have been a production incident rather than a test failure.

**1. Pool deadlock in the migrator.** The concurrency test hung 60 s. The
migrator held the advisory lock on one connection and acquired a *second* for
its own queries — while other migrators sat blocked, holding the pool. At
`migrate`'s `MaxConns=2`, every parallel deploy would have hung until the lock
timeout.

**2. Implicit transaction breaking `CREATE INDEX CONCURRENTLY`.** Postgres wraps
multi-statement query strings in an implicit transaction, so the migration failed
with *"cannot run inside a transaction block"* despite the migrator never calling
`Begin`. Fixed with a SQL splitter that understands string literals, quoted
identifiers, dollar quoting and comments.

**3. `__Host-` cookies silently dropped in development.** Login returned 200 and
set a cookie the client discarded: the prefix *requires* `Secure`, dev is plain
HTTP. Invisible from the server side.

**4. Block tags collapsing text.** `StripHTML` produced `"5+ years\n Go"`, and the
leading space broke every line-anchored section-heading match — so skill
classification silently degraded to `mentioned`.

**5. `"100 years of experience"` parsed as 0.** A two-digit capture matched `"00"`
mid-number and passed the plausibility floor. Fixed with word-boundary anchors
and a `lo <= 0` guard.

**6. `"no remote"` classified as remote.** The remote pattern matched inside the
negation. Explicit on-site claims are now checked first — the most specific
statement in a posting must win.

**7. Playwright version drift.** The floating `^1.49.1` range resolved to 1.62.1
while the container image was pinned at 1.49.1, and the mismatch hung silently
with no output. Both are now pinned to the same version.

**8. Every user shared one rate-limit bucket — found end to end, in three parts.**

The E2E page snapshot said it outright: *"The job feed is unavailable (429)"*.
The degradation path worked perfectly, which is how the cause was visible at all
— a page that silently showed zero jobs would have looked like "no results".

Three separate defects had to be fixed before it went away, and each was
invisible until the one before it was corrected: A burst test found the API
returning 429 after ~22 requests from a single address — and the SSR server
calls the API *on behalf of every visitor*, so in production one busy page would
have rate-limited everyone else on the site. 1. **The API ignored `X-Forwarded-For` entirely**, so every request looked like
   it came from the SSR container. `KeyByIP` now honours it — but **only** from
   peers in a `TRUSTED_PROXIES` allowlist (bare addresses or CIDR blocks),
   because trusting it unconditionally makes the limiter trivially bypassable.

2. **SvelteKit never sent the header.** `event.fetch` forwards cookies but not
   arbitrary headers, so the client address had to be attached explicitly in a
   `handleFetch` hook. Fixing only the API side changed nothing.

3. **The allowlist did not match.** I set `172.16.0.0/12` by assumption; the
   compose bridge had been allocated `192.168.97.0/24`, so the check silently
   never fired. Docker assigns this dynamically, which is exactly why guessing
   was wrong — and why a silent allowlist miss is worth designing against.

None of this is reachable by unit tests: it needed real traffic through the real
SSR path, from a real browser, in containers.

**9. A dead zone on the theme toggle.** The icon `<span>` intercepted pointer
events aimed at its own label, so clicks landing exactly on the glyph did
nothing. Playwright reported it as "element intercepts pointer events", which is
the same defect stated precisely.

**10. Vite blocked container-to-container requests.** Every E2E test failed on a
blank page. Vite rejects Host headers it does not recognise as a DNS-rebinding
defence, and inside Docker the app is reached by its **service name** (`web`),
not `localhost` — so `curl` from the host worked while every request from
another container got "Blocked request". A real configuration gap that only a
containerised test could have found: running the suite from the host would have
passed and hidden it.

## Zero is an answer

The profile-completeness panel told every new graduate to "set your years of
experience" — permanently, with no way to dismiss it — because the check read
`TotalYoE > 0` as "has answered". A new graduate has zero years. Zero is the
answer.

`loadProfile` had already destroyed the distinction at the source with
`COALESCE(u.total_yoe, 0)`, so nothing downstream could recover it. The column
is nullable; the profile now carries an explicit `YoEStated` read from
`total_yoe IS NOT NULL`.

This is the same null-versus-zero rule the feed applies to undisclosed salary,
and the codebase states it in three separate design documents. It still went
wrong here, and it went wrong in the direction that punished the users with the
least to show. Guarded by `TestStrength_ZeroYearsIsAnAnswer`.

## The feed's own freshness rule made its index useless

`GET /v1/jobs` was over budget, and the plan showed a sequential scan discarding
9,541 of 10,198 rows on every request.

`jp_live_recent_idx` indexes `posted_at`. The feed filters and sorts on
`COALESCE(posted_at, first_seen_at)` — because a vendor that publishes no date
must still be orderable, and falling back to when we first saw it is the honest
approximation. An index on a column cannot serve a predicate on an expression
over two columns, so the index that existed specifically for this query was
never used by it.

`jp_freshness_idx` now indexes the expression itself, partial on
`status = 'live'`, ordered to match the keyset tuple. **Newest-first went from
287ms to 25ms** measured host-side.

### And the measurement was wrong

The follow-up numbers looked bad too — 171ms median on the match sort — until
the same request was timed three ways:

| Measured | p50 | p95 |
|---|---|---|
| Host curl, one process per request | 171 ms | 258 ms |
| Server's own access log | 41 ms | 69 ms |
| Container-to-container curl | 23 ms | 60 ms |

The budget says **p95 server time ≤ 120 ms**, and server time is 69ms. The extra
~130ms was a `curl` process spawn per iteration plus OrbStack's host-to-container
port forwarding — neither of which any user experiences.

Worth stating because the wrong number nearly bought a query rewrite that would
have optimised nothing. Benchmark from inside the network, or trust the access
log, and confirm against `EXPLAIN ANALYZE` before touching the SQL: the full
feed query, subqueries included, executes in 16ms.

## Server-rendered pages have an interactive gap, and nothing marked it

The longest-running false trail of the session. Controls that worked perfectly
in isolation failed under the E2E suite: the account menu would not open, the
save button would not toggle, the onboarding wizard would not advance. Each was
investigated as a product bug. Each was a click landing before its handler
existed.

Server rendering is what makes the product fast, and the cost is a window where
the page is painted and clickable but not yet wired. Two obvious signals both
turn out to be worthless for detecting it:

- `waitForLoadState('networkidle')` reports that the network settled, which says
  nothing about whether Svelte has mounted.
- SvelteKit's `__sveltekit_*` global is written by an inline script in the SSR'd
  HTML, so it is present *before* any hydration has happened. Waiting on it
  passes instantly and proves nothing — this was used first, and it is why the
  fix appeared not to work.

The root layout now sets `data-hydrated` from a mount effect, which is exactly
the moment handlers attach. Tests wait on that attribute, and it is available to
CSS for holding back affordances that would not work yet.

Worth stating plainly: **the product was never broken here.** A user clicking
early gets the native form submission, which works. Only automation is fast
enough to hit the gap reliably — but a suite that reports phantom failures is
worse than no suite, because the real ones stop being believed.

## The rate limit was 1.7 requests per second

`httpx.NewRateLimit(100, 20, ...)` takes requests **per minute**. Read at a
glance — and it was read at a glance, repeatedly — `100` looks like a generous
per-second allowance. It is 1.7 requests a second.

One signed-in page view costs several API calls, so this was tight even for a
single user and impossible for anything sharing an address: a CI runner, an
office NAT, or the E2E suite, where every browser lives in one container. The
suite tripped it constantly, and the failure surfaced in the UI as **"The job
feed is unavailable (429)"** — which reads as a broken feed, not a working
limiter. Several test failures were chased as feed bugs before the 429 was
spotted in a saved page snapshot.

Now `RATE_LIMIT_PER_MINUTE` / `RATE_LIMIT_BURST`, named for what they actually
measure, defaulting to 600/60 and raised far higher for the dev compose stack.

## Two more the end-to-end suite caught

Both were invisible to unit tests and to manual checks on `localhost`, and both
broke the entire signed-in experience.

**Registration was a dead end.** `POST /v1/auth/register` returned `202
verification_sent` and created no session, on the reasoning that an unverified
address should not have access. Correct in isolation, and in practice it meant a
new user was bounced straight to a sign-in page for an account they had just
created and could not yet use — with no mail path in development, permanently.

Registration now issues a session with `email_verified_at` still NULL.
Verification gates what it should gate — anything we would *send* to the address
— rather than gating access to the product. The cost is stated plainly in the
handler: an attacker can distinguish a new address from an existing one by
whether a cookie comes back. That fiction was already thin, and the per-IP rate
limit is what actually makes enumeration expensive.

**SvelteKit added `Secure` to a cookie the API deliberately sent without it.**
`cookies.set()` defaults `secure` to true on every host except `localhost`, and
the cookie-forwarding helper never set the flag explicitly. A browser on plain
HTTP silently drops a `Secure` cookie, so sign-in returned 200, set a cookie,
and left the user signed out.

Invisible from `localhost`, where SvelteKit's default happens to be correct.
Visible immediately at `http://web:5173`, which is what the E2E suite uses — and
what a staging box behind a TLS-terminating proxy looks like. This is the second
time a cookie attribute has silently broken authentication in development (see
bug 3), and both times the server reported complete success.

## The scheduler held a leadership it did not have

The subtlest bug of the session, and the one most likely to have reached
production.

`cmd/scheduler` is a singleton: it takes a Postgres advisory lock, logs
`scheduler is leader`, and is the only process configured with River's
`PeriodicJobs`. The reasoning was that periodic work must not fire once per
replica, so it belongs on the one process guaranteed to be alone.

That reasoning is correct and the implementation still did not work. River gates
its periodic job enqueuer on **its own** leader election, which is global across
every client connected to the database. Our advisory lock and River's election
are unrelated mechanisms. When another client — the ingestor, the matcher, or in
this case a stray `go run` container — held River's leadership, the leader ran
its own (empty) periodic list, and the scheduler's list never ran at all.

Symptom: the scheduler logged `scheduler is leader`, held its lock correctly,
reported no errors, and enqueued nothing for twenty minutes.

Fix: configure the identical periodic list on **every** worker role. River runs
the enqueuer only on the leader, so the jobs still fire exactly once regardless
of which process wins — the double-enqueue is prevented by River's election, not
by our process topology. The API is excluded because it takes no queues.

The general lesson: when a library has its own coordination primitive, a
separate application-level one does not compose with it. It sits beside it, and
the library's is the one that decides.

## The three scoring defects live data found

None of these were visible against fixtures. All three were found by scoring a
real corpus and reading the top of the list — which is the argument for doing
that before believing any ranking.

**13. A sales role scored 98% for a backend engineer.** *"Professional Services
Commercial Lead"* had no extractable skills, so the 40-point skills component
abstained and was removed from the denominator. The remaining score was decided
entirely by *"you are remote, your years are in range, it was posted today"* —
all true, and none of them evidence that the job is in the reader's field.
Abstention is right for salary, which is genuinely optional, and wrong for
skills, which is the only component establishing the posting is relevant at all.
Unstated skills are now credited at the no-information midpoint instead of
vanishing.

**14. `go to market` registered as the Go programming language.** A *"Mid Market
Account Executive"* posting scored 96%: the extractor matched the English verb
*go*, and that single false token was 40% of the score. Word boundaries already
prevented `going` and `ongoing` — they cannot help with a whole word used in its
ordinary sense. Ambiguous names (`go`, `c`, `r`, `rust`, `swift`, `spring`) now
require corroboration: an unambiguous alias, a qualifying noun, a preposition of
use, a version number, or a terse line under a requirements heading. `swift`,
`rust` and `spring` additionally carry vetoes for their non-technical senses —
SWIFT payment rails, the rust belt, spring semester.

Measured effect: sales and customer-success postings falsely tagged with `go`
fell from **108 to 5**, and the five that remain are mostly Cloudflare presales
*engineers*, who plausibly do write Go.

**15. Matching the one skill a posting mentioned paid out in full.** A
*"Marketing Strategy & Operations Manager"* listing exactly one preferred skill
— SQL — gave 100% coverage and the full 40 points. Coverage is a ratio, and a
ratio over a denominator of one is not evidence. Coverage is now weighted by how
much the posting actually specified, with the unstated remainder credited at the
midpoint; this subsumes defect 13 as the zero-evidence case of the same rule.

Damping the component was not sufficient on its own — the other four could still
carry a thin posting to 81. The **band** is therefore capped below `strong`
whenever skills evidence is partial, because the band is the part a user reads as
a claim rather than as a number.

Each defect has a named regression test carrying the real posting title, and
each was confirmed to fail before the fix.

**Rolling the fixes out revealed a fourth problem.** Corrected code did not
change any existing score: `user_job_scores` is denormalised, and re-ingestion
only re-scores postings whose *content* changed — precisely the wrong set, since
a scoring fix needs to reach the postings that did not change. The stored
`profile_version` column existed but nothing ever read it.

There is now a `rescore_stale` periodic job that finds rows carrying a
superseded version and re-enqueues them through the normal scoring path, 2,000
at a time. Bumping `Scorer.Version` is what deploys a scoring change; forgetting
to bump it means the fix silently reaches nothing.

> **The plan to finish is
> [roadmap-to-completion.md](roadmap-to-completion.md).** The design work is
> complete, so the held list below is now Phase 2 of that plan rather than an
> open question.

## Held until the design lands

The design brief has landed and every item below is now closed. Kept as a record
of what was held and why, not as a live list.

| Item | Where | Note |
|---|---|---|
| ~~Theme + density in the header~~ | **DONE** | Theme moved to Settings. Density **removed entirely** — it changed row padding and nothing else, so its documented accessibility justification was false |
| ~~No Settings page~~ | **DONE** | `/settings`, form-based so it works without JavaScript, which the old header toggle did not |
| ~~No job detail page~~ | **DONE** | `/jobs/{id}` with the full five-component breakdown. An abstaining component shows `—` and a hollow bar, never a zero |
| ~~No loading or skeleton states~~ | **DONE** | `NavProgress` at a 120ms delay, plus per-action `aria-busy`. Deliberately not skeletons for navigation — replacing readable content with grey rectangles destroys information the user still has |
| ~~Dashboard right column runs short~~ | **DONE** | Bands, not a rail. A variable-height list beside a variable-height rail cannot be fixed by tuning the ratio, so nothing sits beside the list any more |
| ~~Job cards are tall and sparse~~ | **DONE** | The waste was horizontal, not vertical: two meta rows totalling ~340px of an available 620px. Merged into one wrapping line |
| ~~Landing page thin below the fold~~ | **DONE** | Shows the score breakdown instead of describing it, states the anti-features, and quotes real corpus figures from `/v1/market` |
| ~~No "new since last visit" marker~~ | **DONE** | Needed a second column: `last_active_at` alone is always ~now, so the marker erased itself. `previous_visit_at` holds the value from before the current visit |
| ~~No keyboard navigation~~ | **DONE** | `j`/`k`/`s`, moving real focus. Enter is deliberately absent — focus sits on a real link, so the browser already handles it, including middle-click and modifier-click |

**Design alignment, 2026-08-17.** The implementation had drifted from the
Claude Design export in ways that were invisible one at a time and obvious side
by side: a cool blue-grey palette instead of the warm stone ramp, a horizontal
filter bar instead of the 272px rail, no global search, no activity heatmap, and
skill names title-cased by SQL into "Aws" and "Graphql". Re-derived from
`Form scope and priorities-6/JobTrack.dc.html` — see
[roadmap §2.7](roadmap-to-completion.md).

**Shipped ahead of the design**, because it is API-only and the design work
depends on it: **`GET /v1/jobs?band=`** filters the feed to specific match bands.
Session 3 of the design asked for it — a "Strong fits · 11" tile needs somewhere
to land, and `sort=match` would drop the user on all 6,677 postings with a header
count contradicting the tile they just clicked. Verified: the dashboard's strong
count and `band=strong` both return 40. Anonymous callers get a 400 rather than a
silently unfiltered feed, and an unknown band is rejected rather than returning
an empty list — "you mistyped a filter" and "nothing matches you" are different
statements and only one is recoverable.

**Nothing else here is broken.** All 24 E2E tests, the Go suite, the unit tests
and the performance budgets pass as they stand. These are improvements the design pass is
expected to specify properly rather than defects to patch ahead of it.

## Not started

| Area | Spec |
|---|---|
| SmartRecruiters and JSON-LD adapters | [source-catalog](../research/source-catalog.md) |
| Hybrid retrieval (pgvector stage) | [matching-and-scoring](../architecture/matching-and-scoring.md) |
| Resume parsing pipeline | [ADR-0007](../architecture/adr/0007-resume-parsing-local-first.md) |
| `sqlc` query generation | [consistency-and-drift](consistency-and-drift.md#2-generated-code) |
| Envoy Gateway manifests | [service-topology §2](../architecture/service-topology.md#2-the-gateway-tier) |

Lever was **dropped from scope**, not deferred: its public board endpoints
returned `{"ok":false,"error":"Document not found"}` or zero jobs for every
token tried. See [source-catalog](../research/source-catalog.md).

`resume-parser` starts, passes probes and drains correctly but does no work yet
— deliberate, so the deployment and shutdown mechanics are proven before the
domain logic lands. `ingestor`, `matcher` and `scheduler` now run real work.

OpenAPI generation **is** wired: `api/openapi.yaml` is the contract, `make
generate` produces `web/src/lib/api.d.ts` from it, and `web/src/lib/types.ts`
is nothing but aliases onto the generated schemas. `make check-generated` is the
CI gate. `sqlc` is still open — the hand-written pgx queries work and converting
them would be churn without a functional gain, so it stays deliberate debt.

Any `make` target whose subject does not exist **fails with a pointer** rather
than being silently absent, and `scripts/check-make-targets.py` fails CI if the
docs reference one that does not exist at all.

## Next

1. Resume parsing, so scores can be built from a CV rather than a typed skill
   list.
2. The pgvector retrieval stage, once the corpus outgrows what trigram blocking
   handles well.
3. `sqlc`, if and when the hand-written queries become the thing slowing changes
   down.
4. Envoy Gateway manifests for the production topology.
