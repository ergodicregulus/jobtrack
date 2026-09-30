# ADR-0021 — `INGEST_MODE=fixture` replays golden files at the HTTP boundary

- **Status:** DECIDED
- **Date:** 2026-09-30
- **Decision drivers:** a documented safety property that no code implemented; CI
  unable to seed a corpus without breaking ADR-0004; nine adapters that must not
  each grow their own replay

## Context

`INGEST_MODE` was validated by `internal/config` (`fixture|recorded|live`, default
`fixture`), logged once by `cmd/ingestor`, and **read by nothing else**.
`internal/jobs/ingest.go` called `adapter.Fetch` unconditionally.

Three statements were therefore false:

| Claim | Where | Reality |
|---|---|---|
| "fixture — replay golden files. No network at all." | `.env.example`, [dev-environment §7](../../engineering/dev-environment.md) | Every mode fetched live. A developer running `make dev` polled real ATS endpoints. |
| `INGEST_MODE=live requires INGEST_LIVE_ALLOWLIST` | `internal/config` | Enforced, but it gates the *label*. Fetching happened in every mode, so the allowlist protected nothing. |
| "Never hit a live ATS in CI" | CLAUDE.md, [testing-strategy](../../engineering/testing-strategy.md) | True only because CI could not ingest at all — which is why its e2e suite found zero postings and failed on an empty feed. |

This is the failure mode [ADR-0014](0014-rules-are-enforced-by-scripts.md) exists
to prevent, in the one subsystem where the consequence is outbound traffic to
third parties.

## Options

| Option | Verdict |
|---|---|
| **Replay at the `HTTPDoer` boundary** — one type, injected where the client is built | **Chosen.** Every vendor already passes through it, so no adapter changes, and the whole `Fetch` path still runs: pagination, conditional requests, the detail sweep, cursor arithmetic. Those are the parts that break. |
| **A seed path calling `Parse` on fixtures directly** | Rejected. Adapters do expose `Parse(body)`, so this is easy — and it skips every one of the code paths above, testing the parser that golden tests already cover. |
| **Refuse to fetch in fixture mode, and seed from SQL** | Rejected. A second description of the schema, drifting from the first. |
| **Record and replay per board (98 captures)** | Rejected for now. It is the honest maximum, and it needs a capture harness and 98 files nobody will refresh. Revisit if per-company realism ever matters. |

## Decision

`source.Replay` implements `source.HTTPDoer` and serves the committed
`internal/source/<vendor>/testdata` fixtures, resolved from the request host and
path. `internal/jobs` builds it for every mode except `live`.

- An **unmapped host is an error**, never a passthrough. The guarantee is "no
  request leaves the process", and a fallback to the network would void it.
- It sets an `ETag` derived from the fixture bytes and honours `If-None-Match`, so
  the 304 branch — where the real ingestor spends most of its life — is exercised
  rather than bypassed.
- Fixtures are read from the module root, found by walking up to `go.mod`.
  Resolving against the working directory was the first attempt and the first test
  killed it: `go test ./internal/jobs/` runs two directories down. The containers
  happen to run from `/src`, which would have made it look correct everywhere
  except where someone ran it.
- `recorded` replays too. It is documented as "a captured session with realistic
  timing"; the timing does not exist, and replaying without it is strictly closer
  to the promise than fetching live.

`cmd/seed -ingest` additionally spreads replayed `posted_at` over the last 14 days
and sets `posted_at_is_estimate = true`.

## Consequences

### Good

- The default is now **offline**. A fresh clone and CI both ingest from fixtures
  and reach nothing, which is what the configuration always claimed.
- CI can seed a real corpus: 351 live postings across 97 companies, 100% carrying
  a description. The e2e suite went from 18 failures to 47/47, and `ui-audit`
  reports no findings.
- `internal/jobs` has its first tests, one of the three packages CLAUDE.md's gap
  table names. They assert the property that matters: only `live` gets a client
  that can dial.

### Bad, and accepted

- **Every board of a vendor replays that vendor's captured board.** The corpus is
  realistic in shape, volume and field distribution, and it is the same postings
  under many company names. Good enough to render, filter, sort and score against;
  not a substitute for looking at production data.
- **Seeded `posted_at` is derived, not observed.** A fixture is one day's capture,
  so replaying it yields postings 30 days to 5 years old, and the feed defaults to
  7 days because freshness is the product. The two cannot meet. The shift is
  confined to `cmd/seed`, preserves the real order and relative spacing so ages
  vary across cards, and sets the `posted_at_is_estimate` flag the schema already
  carries for a date we computed rather than read. The ingest path is untouched: a
  real fetch still records what the board reported.
- **The host→fixture map is a place to forget a vendor.** A tenth adapter whose
  host is unmapped fails loudly on its first fixture-mode fetch, and
  `TestFetch_EveryAdapterYieldsPostingsFromItsReplay` fails in CI before that.

## Revisit

When per-company realism starts to matter — a dedup change that needs genuinely
different boards, or a scoring change measured against real variety. At that point
build the capture harness and record per board, and supersede this.
