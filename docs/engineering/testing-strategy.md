# Testing strategy

> Status: **DECIDED**.

## 1. Shape

```
        ╱╲          E2E — ~15 tests
       ╱  ╲         The core loop only. Playwright, seeded stack.
      ╱────╲
     ╱      ╲       Contract — generated from OpenAPI + protobuf
    ╱────────╲      Every endpoint, both directions.
   ╱          ╲
  ╱            ╲    Integration — ~200 tests
 ╱──────────────╲   Real Postgres via testcontainers. Queries, migrations, jobs.
╱                ╲
──────────────────  Unit — ~800 tests, no I/O, milliseconds
                    Scoring, normalisation, dedup, state machines.
```

The pyramid is conventional; the two things worth arguing about are **what we assert** and **what we
refuse to test**.

## 2. Golden files — how ingestion stays maintainable

The single most valuable testing decision in this project.

Every ATS adapter has `testdata/` containing **captured real responses**. Tests parse them and assert
the normalised output.

```
internal/source/lever/
├── adapter.go
├── adapter_test.go
└── testdata/
    ├── acme-full.json          # 120 postings, the happy path
    ├── acme-empty.json         # zero postings — must NOT close the board
    ├── quirk-epoch-millis.json # Lever's non-ISO timestamps
    ├── quirk-missing-comp.json
    └── golden/
        └── acme-full.want.json # expected normalised output
```

Why this earns its keep:

- **A vendor schema change fails exactly one test** and changes exactly one file. Across seven
  vendors with seven different formats, this is what keeps ingestion tractable.
- **CI never touches a live ATS.** Deterministic, fast, and polite
  ([ADR-0004](../architecture/adr/0004-source-acquisition-policy.md)).
- **The seed data comes from the same fixtures**, so dev data and test data cannot drift, and dev data
  exercises every real vendor quirk.

Regenerating is deliberate friction:

```bash
make test-golden UPDATE=1    # then READ THE DIFF before committing
```

A blind `UPDATE=1` that silently accepts a regression is the one way this technique fails, so the
review checklist calls it out specifically.

**Every quirk in [source-catalog.md](../research/source-catalog.md) has a fixture.** That is the
contract between the document and the code — a documented quirk with no fixture is a bug waiting to
regress.

## 3. Scoring — assert bands, never values

```go
func TestScoring_GoldenCorpus(t *testing.T) {
    for _, tc := range loadCorpus(t) {   // ~200 hand-labelled resume/JD pairs
        got := scorer.Score(tc.Resume, tc.Posting)

        // Bands, not values. An exact-score assertion would break on every
        // weight adjustment, and weights are configuration meant to be tuned.
        // Asserting exact numbers produces brittle tests, and brittle tests
        // get deleted rather than fixed.
        require.Equal(t, tc.WantBand, got.Band)
    }
}
```

CI fails below **85% band accuracy**. Alongside this, a distribution test catches the failure mode
the corpus cannot:

```go
// If 'strong' grows from ~5% to 30% of results, must-have classification has
// regressed. This catches more real problems than the corpus does, because it
// notices systemic drift rather than individual cases.
func TestScoring_BandDistribution(t *testing.T) { ... }
```

## 4. Migrations — against the previous release, not an empty database

```bash
make migrate-verify
```

1. Restore a schema snapshot of the **previous release**.
2. Run the new migrations.
3. Assert success, and that the **previous release's queries still work** against the new schema.

Step 3 is the one that matters and the one usually skipped. It is what makes expand/contract real
rather than aspirational: during a rolling deploy both versions run simultaneously, so a migration
that breaks N-1 breaks production even though it "worked"
([deployment-zdt.md](../operations/deployment-zdt.md)).

A migration that only works against an empty database is not a migration.

## 5. Integration tests

`testcontainers` spins a real Postgres with `pgvector`. **No mocked database, ever** — the value is in
catching what a mock cannot: index usage, constraint violations, transaction semantics, `ON CONFLICT`
behaviour, and query plans.

```go
//go:build integration

func TestFeedQuery_UsesPartialIndex(t *testing.T) {
    db := testdb.New(t)          // fresh schema, per-test transaction, auto-rollback
    seedPostings(t, db, 50_000)

    plan := explain(t, db, feedQuery, args)

    // Guards a real regression: dropping the status='live' predicate silently
    // loses the partial index and turns a 5ms query into a seq scan.
    require.Contains(t, plan, "Index Scan using jp_live_recent_idx")
}
```

Asserting on query plans is unusual and deliberate. The performance budget is a promise; a test that
notices when an index stops being used is how the promise is kept.

## 6. Contract tests

Generated from `api/openapi.yaml`, run both directions: the server satisfies the spec; the generated
client handles every documented response. Plus `oasdiff` and `buf breaking` in CI, which fail on a
backward-incompatible change to REST or protobuf respectively.

Together these make the "additive changes only" rule in
[api-design §6](../architecture/api-design.md#6-versioning-and-compatibility) mechanically enforced
rather than a convention.

## 7. E2E — few, and only the core loop

~15 Playwright tests. Slow, flaky-prone, expensive to maintain, and indispensable for exactly one
thing: proving the loop works end to end.

1. Sign up → verify → land on dashboard
2. Upload resume → see the parse diagnostic → correct a field
3. Filter jobs → see scores → open a posting
4. Save → apply → record channel and resume version
5. Advance status → see it in the funnel
6. Channel analytics **suppress a rate below n=25** ← this is a product promise, so it gets a test
7. Full keyboard path through the loop (accessibility)
8. Export data → verify completeness

Test 6 is there because [P4](../product/principles.md#p4--refuse-to-show-numbers-the-user-will-misread)
is a principle a future contributor could "fix" by helpfully showing the percentage. The test makes
the refusal load-bearing.

## 8. Performance tests in CI

| Test | Assertion |
|---|---|
| Feed query, 200k postings | p95 ≤ 120 ms |
| Bundle size, `/jobs` | ≤ 100 KB gz |
| Lighthouse, reference low-end profile | INP ≤ 200 ms, LCP ≤ 1.8 s |
| Ingest throughput | ≥ 500 postings/s normalised |

Run on every PR against a fixed dataset. **A regression fails the build.** Budgets that are only
checked before a release are budgets that get exceeded between releases.

## 9. What we deliberately do not test

Stating this matters as much as the rest — untested code is a decision, and undeclared decisions
become arguments.

| Not tested | Why |
|---|---|
| Generated code (`sqlc`, OpenAPI) | Testing a generator's output tests the generator |
| Third-party libraries | Not our job |
| Trivial getters/setters | Coverage theatre |
| **Exact scoring values** | Not meaningful; bands are the contract |
| Live ATS endpoints | Impolite, non-deterministic, and breaks CI when a vendor has a bad day |
| LLM outputs, when enrichment is enabled | Non-deterministic by nature. We test the **fallback path** instead — the deterministic pipeline must produce a usable result with the LLM disabled, which is also the default |
| Every UI permutation | Component tests for logic, E2E for the loop, and nothing in between |

## 11. Coverage — differentiated, not a single number

A single repo-wide coverage gate produces tests written for the metric. **Per-package thresholds**
produce tests written for the risk. `go-test-coverage` supports exactly this — overall and per-file
thresholds with path-based overrides, configured in `.testcoverage.yml` and enforced in CI.

| Package | Threshold | Rationale |
|---|---:|---|
| `internal/domain/**` | **90%** | Pure logic, no I/O, trivially testable. Low coverage here is a choice, not a constraint |
| `internal/matching/**` | **85%** | Scoring correctness is the product |
| `internal/source/**` | **85%** | Every vendor quirk must have a fixture |
| `internal/resumeparse/**` | **80%** | Parsing edge cases are where defects live |
| `internal/store/**` | 60% | Mostly generated; the queries are covered by integration tests instead |
| `internal/http/**` | 60% | Covered by contract tests, which coverage does not attribute |
| `cmd/**` | **0% (excluded)** | Wiring. Testing it tests the compiler |

**Coverage cannot decrease** on a PR — `go-test-coverage` compares against the base branch. That
ratchet does more real work than any absolute number, because it makes the easy path forward rather
than backward.

### Mutation testing on the scoring engine only

Coverage proves a line executed. It does not prove an assertion would have caught a change to it. For
most packages that gap is acceptable; for **scoring it is not**, because a subtly wrong weight
produces plausible output that no one notices.

`go-mutesting` runs against `internal/matching/**` on a nightly schedule (not per-PR — it is slow),
gated with `--min-msi` at the project's established baseline. A mutation score that drops means a
change is not actually protected by the tests that appear to cover it.

Scoped deliberately to one package. Mutation testing everywhere is a good way to spend a lot of CI
time proving that getters return their fields.

---

## 12. What "everything is covered" actually means here

Coverage percentages are a poor answer to "will something break silently?". These are the checks that
actually make that unlikely, in rough order of value:

| Guarantee | Mechanism |
|---|---|
| A vendor changes their schema | Golden fixtures fail; `parse_confidence` alert; weekly fixture-refresh diff |
| A query loses its index | Integration test asserts the **query plan** |
| An N+1 is introduced | Integration test asserts **query count ≤ 3**, independent of result size |
| A migration breaks the running release | `make migrate-verify` against the previous release's schema |
| A job arg change breaks a rolling deploy | Args round-trip test across release boundaries |
| The API contract drifts | `oasdiff` / `buf breaking`; contract tests generated from the spec |
| Generated code goes stale | `make generate` + dirty-tree check |
| Scoring silently regresses | Golden corpus (band accuracy) **and** band-distribution monitoring |
| A performance budget regresses | Load tests and bundle budgets fail the build |
| A principle is quietly "fixed" | E2E test asserting the n<25 rate **suppression** |
| Docs reference something that no longer exists | Link, make-target, fixture-coverage and env-var checks |

The last two are the unusual ones and both are deliberate. Test 10 protects
[P4](../product/principles.md#p4--refuse-to-show-numbers-the-user-will-misread) from a future
contributor who "helpfully" displays the percentage — a refusal that is not tested is a refusal that
will be removed. And documentation drift gets CI enforcement because docs that lie are worse than docs
that are missing.

Full inventory: [consistency-and-drift](consistency-and-drift.md).

## 10. Test data and privacy

- **No real resumes in the repository.** The golden corpus uses synthetic resumes built to exercise
  specific layouts.
- **Captured ATS responses are reviewed before commit** and stripped of anything resembling personal
  data — recruiter names and contact emails appear in some feeds.
- **CI has no production credentials.** Integration tests use testcontainers; E2E uses a seeded local
  stack. As a side effect this is what makes the vendor-neutrality claim testable: the whole suite
  runs with no cloud credentials present
  ([service-topology §8](../architecture/service-topology.md#8-vendor-neutrality--the-portability-contract)).
