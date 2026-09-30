# API design

> Status: **DECIDED**. The OpenAPI 3.1 document at `api/openapi.yaml` is the **source of truth**;
> this page explains the conventions behind it.

## 1. Style

**REST over JSON, versioned in the path, OpenAPI-first.**

Not GraphQL: our client is our own, the query shapes are known in advance, and GraphQL's flexibility
would let the browser express queries whose cost we cannot bound — directly against
[P1](../product/principles.md#p1--ship-data-not-javascript). Not gRPC-Web at the edge: JSON over HTTP
is debuggable with `curl`, cacheable by standard infrastructure, and readable in a browser network
tab. gRPC is used internally, once, for `api` → `resume-parser`
([service-topology §4](service-topology.md#4-communication)).

**OpenAPI-first is a workflow commitment, not a documentation gesture:**

```
api/openapi.yaml  ──┬──> Go server types + request validation
                    ├──> TypeScript client for web/
                    ├──> contract tests (both directions)
                    └──> oasdiff in CI — a breaking change fails the build
```

Handwritten types on either side of the boundary are prohibited. Drift between spec and
implementation is the failure this eliminates.

## 2. Resources

```
POST   /v1/auth/register            POST /v1/auth/login   POST /v1/auth/logout
GET    /v1/me                       PATCH /v1/me

GET    /v1/jobs                     # the feed — filters, sort, keyset pagination
GET    /v1/jobs/{id}
GET    /v1/jobs/{id}/score          # component breakdown for the current user
GET    /v1/jobs/facets              # counts per filter value, for the filter UI

GET    /v1/companies/{id}
GET    /v1/companies/{id}/posture   # entry-level openness, cadence, disclosure rate

GET    /v1/resumes                  POST /v1/resumes      # multipart upload
GET    /v1/resumes/{id}             PATCH /v1/resumes/{id}   DELETE /v1/resumes/{id}
POST   /v1/resumes/{id}/default

GET    /v1/applications             POST /v1/applications
PATCH  /v1/applications/{id}                               # status transitions
GET    /v1/applications/analytics   # channel table, with suppression applied

GET    /v1/watchlists               POST /v1/watchlists    DELETE /v1/watchlists/{id}

GET    /v1/events                   # SSE: score updates, new matches
GET    /v1/export                   # full data export
DELETE /v1/me                       # account deletion
```

Routed with `net/http.ServeMux` (Go 1.22+), which handles `GET /v1/jobs/{id}` natively — no router
dependency ([ADR-0001](adr/0001-go-for-backend.md)).

## 3. Pagination

**Keyset, never offset.** With `OFFSET 1000` Postgres still sorts and discards a thousand rows; keyset
stays flat regardless of depth.

```http
GET /v1/jobs?limit=25&cursor=eyJyayI6ODIuNCwiaWQiOjkxNH0
```

```json
{
  "data": [ ... ],
  "page": {
    "next_cursor": "eyJyayI6NzguMSwiaWQiOjg4Mn0",
    "has_more": true
  }
}
```

The cursor is base64 of `{rank_key, id}` — opaque to clients, and **not** a stable pointer: the feed
changes as postings arrive. Deep pagination is capped at 20 pages, which is not a limitation but a
signal, since a user paging past 500 results should be filtering instead. `has_more` is returned
rather than a total count: an exact count over a filtered 200k-row set costs more than the page
itself, and nobody reads page 400.

## 4. Filtering

```http
GET /v1/jobs
  ?country=IN&region=KA&mode=hybrid,remote
  &yoe_min=0&yoe_max=3&yoe_stretch=true
  &comp_min=2000000&comp_currency=INR&comp_disclosed_only=false
  &posted_within=7d
  &skills=python,postgresql
  &match_band=strong,plausible
  &sort=best_match
```

Conventions:

- **Repeated values are comma-separated**, not repeated params. Shorter URLs; filter state lives in
  the query string so a filtered view is shareable and back-button-correct.
- **`yoe_stretch=true` widens the band upward by 2 years** and marks those results. Default `true` —
  a large share of SDE-1 postings say "2+ years" and that band is soft in practice
  ([problem-statement §6](../product/problem-statement.md#6-the-india--bengaluru-layer)).
- **`comp_disclosed_only` is separate from `comp_min`.** Only ~80% of postings disclose salary
  `[A-12]`; collapsing "below your floor" into "not disclosed" would silently drop a fifth of the
  market. The API keeps them distinct because the UI must.
- **Unknown parameters are a 400**, not silently ignored. A typo'd filter that quietly returns
  unfiltered results is the worst possible failure mode here.

`GET /v1/jobs/facets` returns counts per filter value for the *current* filter context, so the UI can
show `Remote (1,240)` and never let a user filter blindly into zero results.

## 5. Errors

RFC 9457 `application/problem+json`, always. One shape, machine-readable, and never a bare string.

```json
{
  "type": "https://github.com/ergodicregulus/jobtrack/blob/main/docs/architecture/api-design.md#validation-failed",
  "title": "Validation failed",
  "status": 400,
  "detail": "yoe_min must be less than or equal to yoe_max",
  "instance": "/v1/jobs",
  "errors": [
    { "field": "yoe_min", "code": "range", "message": "must be <= yoe_max" }
  ],
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

`trace_id` is present on **every** error and is the W3C trace ID. A user can paste it into a support
message and we can find the exact request. This is cheap and disproportionately useful.

### Problem types

RFC 9457 **allows** a non-resolvable `type` URI. What it says is narrower and it is the part that
binds us: *"If the type URI is a locator (e.g., those with an `http` or `https` scheme), dereferencing
it SHOULD provide human-readable documentation for the problem type."* Ours is an `https` locator, so
that obligation applies — and it now points at this section, which is the documentation.

It previously pointed at `jobtrack.dev/problems/...`: a locator, resolving, on a domain owned by
somebody else. Every error response cited a stranger's website as the explanation of our errors.

A URN would have been the other honest option — no domain, no obligation. A resolving link to real
documentation is more useful to whoever is reading the error at 2 a.m., so it is worth the obligation
of keeping the anchors below in step, which a test enforces.

`TestProblemKindsAreDocumented` in `internal/httpx` fails if a constructor here gains a kind with no
heading below, because a type URI whose fragment resolves to nothing is worse than no link.

<a id="validation-failed"></a>**validation-failed** — 400. The request was malformed, named an unknown
parameter, or failed a field constraint. `errors[]` names each field, its code and a message.

<a id="unauthorized"></a>**unauthorized** — 401. No session, or one that has expired. Sign in again;
retrying the same request unchanged will not help.

<a id="not-found"></a>**not-found** — 404. No such resource, or one this session may not see. The two
are deliberately indistinguishable: telling an unauthorised caller that something exists is itself a
disclosure.

<a id="conflict"></a>**conflict** — 409. The resource changed under you, or the write would duplicate
something unique. Re-read and decide; this is not retryable as-is.

<a id="payload-too-large"></a>**payload-too-large** — 413. The body exceeded the limit for that
endpoint. Resume uploads are the usual case.

<a id="unprocessable"></a>**unprocessable** — 422. Well-formed and understood, but not actionable — a
resume with no extractable text, for instance. The `detail` says what to do differently.

<a id="rate-limited"></a>**rate-limited** — 429. Too many requests. `Retry-After` is always set; honour
it rather than backing off on a guess.

<a id="internal"></a>**internal** — 500. Our fault. `detail` is deliberately generic: the underlying
error is logged against the `trace_id` and never sent, because internal detail in an error body is an
information leak.

<a id="unavailable"></a>**unavailable** — 503. A dependency is down or the service is draining.
Retryable.

| Status | Used for |
|---|---|
| 400 | Malformed request, unknown parameter, validation failure |
| 401 | No or invalid session |
| 403 | Authenticated but not permitted |
| 404 | Not found, **or** not visible to this user — we do not distinguish, to avoid enumeration |
| 409 | Conflict, e.g. an invalid application state transition |
| 413 | Upload too large |
| 422 | Semantically invalid, e.g. an unparseable resume |
| 429 | Rate limited — always with `Retry-After` |
| 503 | Dependency unavailable; **includes `Retry-After`** |

## 6. Versioning and compatibility

`/v1` in the path. Within a version we make only **additive** changes:

- ✅ Add an optional request field, a response field, an enum value, an endpoint
- ❌ Remove or rename a field, change a type, tighten validation, change a default

`oasdiff` runs in CI against the previous release's spec and fails the build on a breaking change.
This is the same discipline `buf breaking` provides on the protobuf side — the tooling differs, the
rule does not.

**Clients must ignore unknown fields.** Stated in the spec, enforced in the generated TS client, and
the reason an additive change is safe during a rolling deploy where two API versions serve
simultaneously.

## 7. Authentication and rate limits

Session cookie: `HttpOnly`, `Secure`, `SameSite=Lax`, server-side record so revocation is immediate
([data-model §6](data-model.md#6-watchlists-and-sessions)). CSRF via the double-submit pattern on
state-changing requests.

Rate limits are **layered**, and the layering is deliberate:

| Layer | Scope | Enforced at |
|---|---|---|
| Global per-IP | 100 req/min | **Gateway** — cheap rejection before touching the app |
| Per-session | 300 req/min | `api` |
| Resume upload | 10/hour/user | `api` |
| Export | 3/day/user | `api` |

The gateway handles the volumetric layer because a request rejected at the edge costs nothing; the
application handles per-user quotas because those need identity the gateway does not resolve.
Authorisation decisions are **always** in the application — a rule living in gateway config cannot be
unit-tested and will drift from the code it protects.

## 8. SSE for live updates

`GET /v1/events` streams: a new posting matching a watchlist, a score finishing computation, an
application status change from email classification (v2).

WebSockets were rejected — the traffic is one-directional, and SSE works through ordinary HTTP
infrastructure with automatic browser reconnection and no protocol upgrade. Connections carry a
30-second heartbeat, cap at 15 minutes, and the client reconnects with `Last-Event-ID`.

## 9. Caching

| Endpoint | Policy |
|---|---|
| `/v1/jobs` | `private, max-age=0, must-revalidate` — personalised and freshness-critical |
| `/v1/jobs/{id}` | `private, max-age=60` |
| `/v1/jobs/facets` | `private, max-age=60` — also cached in-process, shared across users with the same filter prefix |
| `/v1/companies/{id}` | `public, max-age=300` |
| Static assets | `public, max-age=31536000, immutable` — content-hashed filenames |

`ETag` on all `GET`s. The feed is deliberately not cached at the edge: a cached feed is a stale feed,
and [P2](../product/principles.md#p2--freshness-is-the-product) makes that unacceptable.

## 10. Conventions worth stating

- **Time is RFC 3339 UTC**, always, with the offset. Never a naive timestamp, never epoch seconds.
- **Money is an integer of minor units plus an ISO-4217 code.** No floats anywhere near currency.
- **`null` means "unknown"; a missing field means "not applicable".** The distinction is load-bearing
  for compensation.
- **IDs are opaque strings** in the API even though they are `bigint` internally, so the internal type
  can change without a breaking API change.
- **Every list response is an object with `data` and `page`**, never a bare array — a bare array
  cannot be extended without a breaking change.
