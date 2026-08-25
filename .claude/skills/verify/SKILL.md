---
name: verify
description: Use before claiming any change is done, and before committing. The verification ladder for JobTrack, cheapest first, with the traps that produce false results.
allowed-tools: Bash, Read, Grep, Glob
---

# Verifying a change

`make check` is the definition of done. This is the ladder around it, ordered so
the cheapest thing that can fail, fails first.

**Do not report success from a step you did not read the output of.** Every
false "all green" in this repository's history came from a pipeline whose exit
code was swallowed — `cmd | head` returns head's status, not the command's.

## 1. Invariants — under a second, no toolchain

```bash
make arch-check
```

Layering, database access, SQL location, function length, ADR index, citations,
docs reachability, migration safety, schema usage.

**Read the ratchet output, do not just look for green.** "N known, none new" is
correct. Entries that "no longer violate" mean you fixed something and must run
`make arch-check-update`, then **read the diff**: a `-` line is debt paid, a `+`
line is debt added and needs a sentence in the commit message.

## 2. Compile and unit

```bash
docker compose run --rm --no-deps -T tools "gofmt -l ./cmd ./internal && go vet ./... && go test ./... -count=1"
```

Capture the exit code properly:

```bash
... >/tmp/t.log 2>&1; echo "rc=$?"   # NOT  ... | tail
```

## 3. Integration — the layer unit tests cannot reach

```bash
docker compose up -d postgres
docker compose run --rm -T tools "go test -tags=integration -count=1 ./..."
```

This matters more here than in most codebases. ADR-0015 chose
`pgx.RowToStructByName`, which accepts that a column/field mismatch fails at
**runtime**. These tests are where that failure is supposed to happen.

`TestReadsSurviveAllNullableColumns` guards a specific class: a nullable column
scanned into a non-pointer Go field. That bug was fixed three times by hand and
the test still found a fourth instance. **If you add a read path, it belongs in
that test.**

## 4. Exercise the real thing

Tests pass on code that is wrong for reasons no test covers. The broken keyset
pagination — every "load more" returning 500, for every sort — passed the entire
suite, because nothing exercised a cursor.

```bash
docker compose up -d --build api web
curl -sS localhost:8080/healthz
curl -sS "localhost:8080/v1/jobs?limit=2"
# then a filter, a sort, and a SECOND page via next_cursor
```

For anything touching a write path, walk it end to end with a cookie jar:
register, act, and check the effect is visible on a *different* endpoint.

## 5. The interface

```bash
make ui-audit          # target sizes, headings, labels, overflow, contrast
make bench-budget      # 100 KB JS / 20 KB CSS, gzipped
make test-e2e          # Playwright against a real stack
```

Then **look at it**. `PAGES="/" make screenshots` writes to `web/.screenshots/`;
read the PNG. Layout problems are invisible in a passing test suite — two
identical four-card grids stacked on one page is not something any assertion
catches.

## The trap that will cost you a run

**Do not edit files while `make test-e2e` is running.** The dev stack runs `air`,
so a save rebuilds and restarts a service mid-test. It produced two failures
that looked like real regressions in the resume-parser and were a restart race —
the requests immediately before them in the same log had returned 201 and 200.

If a run fails in a way that seems unrelated to the change, check
`docker compose logs <service>` for a restart before believing it.

## Before you say it is done

```bash
make check
```

That is fmt, vet, lint, race tests, build, frontend tests and the performance
budget. If it is green, the change is consistent with everything else here.

State what you ran and what it said. "Tests pass" without the command is not a
verification, it is a claim.
