# ADR-0023 — The production path, assembled and proven on every push

- **Status:** DECIDED
- **Date:** 2026-09-30
- **Decision drivers:** a production configuration that had never run and could not have; defects
  that every other CI job passed; the gap between "the images build" and "the site works"

## Context

CI built the release images and checked that they were small and shell-free. Nothing ever started
them. Booting `docker-compose.prod.yml` for the first time found every one of these, each fatal or
silently wrong:

| Defect | Effect |
|---|---|
| Postgres pinned to `sha256:9a8b7dfd…c0b9a87` | **The digest did not exist.** The file could never pull its database. |
| `ENV: production` in compose and the k8s ConfigMap | Nothing reads `ENV`. `APP_ENV` fell back to `dev`: every production guard off — a `dev-` placeholder secret accepted — and the 5s drain cut to 250ms. |
| `DATABASE_URL …?sslmode=disable` | With `APP_ENV` set, the prod guard refuses it: every service would stop at boot. |
| No OTel collector on a single host | With `APP_ENV` set, the prod guard refuses an empty endpoint. |
| No `web` image target | Compose and k8s both ran `web:${VERSION}`. Nothing built it. |
| Proxy sent everything, `/v1` included, to web | adapter-node has no `/v1` route. Every browser API call would 404. |
| `/unsubscribe` proxied nowhere | The digest's unsubscribe links, built on the web origin, would 404. |
| `API_BASE_URL` for the web server | Nothing read it; server-side rendering had no way to reach the api. |
| No `ORIGIN` behind TLS | SvelteKit's CSRF check would reject every form POST, sign-in included. |
| Boards registered only by `cmd/seed` | Which ships in no image and refuses production — so no sources, no jobs, ever. |
| `INGEST_MODE` unset | Default is fixture replay: production would have served test fixtures. |
| `INGEST_ENABLED=false` | Logged "workers will idle", then fetched anyway. |
| `deploy.sh` | Applied three manifests that do not exist, required a `matcher` image, never applied web, ingress or the ConfigMap. Service names pointed at namespace `default`; it deploys to `jobtrack`. |

## Decision

**Route by path at the proxy.** `/v1/*` and `/unsubscribe` go to the api, everything else to web —
in Caddy and in the k8s Ingress. One origin either way, so the session cookie stays first-party with
no CORS surface, which was the stated goal. The stated *mechanism* — the SvelteKit server fronting
the api — was never implemented, and implementing it would put every API call, upload and
server-sent event through Node for nothing. The README's architecture diagram already drew the
gateway reaching the api directly.

**Server-side rendering reaches the api over the internal network** (`API_URL`, rewritten in
`handleFetch`), not back out through the public entry. The session cookie is forwarded by hand,
which is SvelteKit's documented pattern for another origin; `Set-Cookie` was already forwarded by hand
in `lib/server/api.ts`.

**Satisfy the production guards; do not weaken them.** Single-host Postgres serves TLS with a
self-signed certificate made at start by the image's own OpenSSL, and connections use
`sslmode=require` — encrypted, unverified, which is the honest trade with no CA to verify against.
Tracing is switched off with `OTEL_TRACES_EXPORTER=none`, the OpenTelemetry-standard opt-out, which the
guard now accepts; a merely empty endpoint is still refused.

**Curated boards are registered at ingestor start** (`store.EnsureBoards`), the way the skill
vocabulary already was — they are product data that ships with the code. Its prune deletes postings,
and it now runs on every deploy, so an empty list is refused as the bug it almost certainly is.

**Production ingests live by default**, over every curated board; `INGEST_ENABLED=false` now actually
idles the workers and stops scheduling.

**CI boots the release images and uses them.** `scripts/prod-smoke.sh` builds all six images, starts
`docker-compose.prod.yml` in an isolated project, and asserts routing over HTTPS through Caddy,
`APP_ENV=prod` in force, boards registered, every database connection on TLS, a CSRF refusal, and a
real sign-up whose cookie reaches the api on the next server-rendered page. `scripts/check-manifests.sh`
renders every k8s manifest and validates it against the Kubernetes schemas.

## Consequences

### Good

- The single-host production path works, end to end, and a regression in it fails CI.
- A class of silent failure is now loud: `check_env_example.py` fails on any key the ConfigMap or the
  production compose file sets that the loader does not read. Run against this morning's files it
  reports exactly `ENV` and `DRAIN_DELAY`, in both.

### Bad, and accepted

- **Kubernetes is validated, not run.** The manifests render and pass strict schema validation, and
  `deploy.sh` is shellcheck-clean and applies files that exist — but nothing here has been applied to a
  real cluster. That needs a cluster and its secrets. The first real deploy should be watched.
- **Compose trusts every private range** for `X-Forwarded-For`. Only containers on this host's compose
  networks can reach the api, and Docker allocates those networks from the private ranges; a wider
  trust would let a client pick its own rate-limit bucket.
- **The database certificate is not verified.** On one host the threat it answers is plaintext on the
  wire, not an impersonated server.
- **The smoke test takes minutes**, so it runs in CI and in `make prod-smoke`, not in `make check`.

## Revisit

On the first deploy to a real cluster: record what `deploy.sh` needed that this could not know, and
turn each finding into a check. If the single-host database ever moves to its own machine, switch to
`sslmode=verify-full` with a real CA.
