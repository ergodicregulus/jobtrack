# Plan — CI green, and `INGEST_MODE=fixture` made real

**Status:** IN PROGRESS. 11 of 16 CI jobs green as of b2b9e79. Three still red, and
they share one root cause: the corpus is empty in CI because fixture mode does not exist.

**Done when:** `gh run view <latest>` shows every job green on `main`, and
`INGEST_MODE=fixture` provably makes no network request (a test asserts it).

## The finding that blocks the rest

`INGEST_MODE` is validated by `internal/config` (`fixture|recorded|live`, default
`fixture`), logged once by `cmd/ingestor`, and **never read by anything**.
`internal/jobs/ingest.go:158` calls `adapter.Fetch(ctx, src)` unconditionally, which
does real HTTP through `internal/source/fetch.go:Do`.

So three documented claims are false:

- `.env.example` and [dev-environment §7](../dev-environment.md): "fixture — replay
  golden files. No network at all." There is no replay. The dev stack hits live ATS
  endpoints.
- The `INGEST_MODE=live requires INGEST_LIVE_ALLOWLIST` guard is enforced, but it
  gates only the *label*; fetching happens in every mode, so the allowlist protects
  nothing.
- [testing-strategy](../testing-strategy.md) and CLAUDE.md: "Never hit a live ATS in
  CI." Currently true only because CI cannot ingest at all — which is also why the
  e2e suite finds zero postings.

## Why a fixture transport, not a seed flag

Adapters already separate `Fetch` from `Parse(body)` / `ParseDetail(body)`, so a seed
path could call `Parse` on a golden file directly. That was rejected: it would bypass
pagination, the conditional-request logic, the detail sweep and the cursor handling —
the parts most likely to break. Serving fixtures at the `HTTPDoer` boundary exercises
the whole `Fetch` path and needs **zero adapter changes**, because
`greenhouse.New(client, ua)` already takes the client as an interface.

Host patterns are the mapping key and they are stable, documented in
[source-catalog.md](../../research/source-catalog.md): `boards-api.greenhouse.io`,
`api.ashbyhq.com`, `api.smartrecruiters.com`, `{t}.recruitee.com`,
`apply.workable.com`, `{t}.{dc}.myworkdayjobs.com`, `{t}.jobs.personio.de`,
`{t}.keka.com`, `{t}.bamboohr.com`.

Accept up front, and say so in the ADR: every board of a vendor replays that vendor's
captured board, so fixture mode gives a realistic *shape* and volume, not per-company
truth. That is what makes it deterministic and offline.

## Steps

1. **[opus]** `internal/source/replay.go` — a `Replay` type implementing `HTTPDoer`.
   Resolve vendor from the request host, list-vs-detail from the path, and serve the
   matching `testdata/` file as a synthetic `*http.Response` with an `ETag`. An
   unmapped host returns an error naming the host — never a real request.
2. **[opus]** Wire it in `internal/jobs/ingest.go:ensureAdapters`: the client is
   `Replay` unless `cfg.Ingest.Mode == "live"`. `recorded` maps to replay with timing
   until it means something more.
3. **[opus]** A test that fails if fixture mode can reach the network — inject a
   transport that fails the test on any call, run a full `Fetch` per vendor.
4. **[sonnet]** ADR-0021 recording the decision, the accepted downside above, and the
   revisit trigger. Update the ADR index table.
5. **[sonnet]** Correct `.env.example`, dev-environment §7 and CLAUDE.md so the
   documented behaviour matches the code.
6. **[opus]** CI: seed with `-ingest` in the e2e, load and accessibility jobs now that
   it is offline and deterministic. Re-run and read the three failures.
7. **[opus]** Remaining e2e assertions after data exists —
   `acceptance.spec.ts:33` wants "live postings from", `feed.spec.ts:18` wants
   `li.card`, `feed.spec.ts:93` wants a count on every filter chip.

## Also open, smaller

- `check_env_example.py` guards `.env.example` against the loader. The **CI workflow
  env blocks** are a third copy of the same list and are unguarded — the e2e job was
  missing `RESUME_PARSER_URL` and `RESUME_ENCRYPTION_KEY` for exactly this reason.
  Extend the invariant to `.github/workflows/*.yml`.
- `problem+json` type URIs still use `jobtrack.dev`, a domain we do not own. Changing
  them touches `api/openapi.yaml` and the generated types.
