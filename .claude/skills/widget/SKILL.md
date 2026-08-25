---
name: widget
description: Use when adding or changing a widget — any chart, heatmap, counter or panel on the homepage or dashboard. Decides which data shape it needs before any code is written, and keeps it honest at scale.
allowed-tools: Read, Grep, Glob, Bash, Edit, Write
---

# Adding a widget

Widgets are the product's direction: each one makes the corpus more legible.
They are also where a schema quietly acquires a cost curve nobody priced, so the
shape is decided **before** anything is written.

## 1. Which shape is it? — one question decides

> **Does the widget's query have a `user_id` in its WHERE clause?**

**Yes → query the live tables directly.** The read is bounded by one person's
own activity, so the corpus can grow forever without touching it. The
applications heatmap reads about 1,200 rows for a heavy job seeker, by index, and
that number does not change when the product reaches a million users.

**No → read a rollup.** A global widget has nothing to filter by, so the naive
version scans everything and gets slower for every reader as coverage grows.
That is not a slow query, it is a widget that cannot ship.

This is [ADR-0017](../../../docs/architecture/adr/0017-widget-data-comes-from-rollups.md),
and it exists so this stops being a judgement call.

## 2. If it needs a rollup

Follow `source_daily` (migration 0018) — it is the reference instance.

- **Daily grain**, `date` not `timestamptz`. Every widget so far is a calendar
  chart. An hourly grain multiplies rows by 24 to serve a resolution no design
  has asked for.
- **Every column must be rebuildable from the operational tables by ONE
  statement.** This is the rule, not a preference: a rollup you can rebuild is a
  cache, and a bug in it is fixed by re-running the statement. A rollup you can
  only accumulate has become a system of record, and a bug in it needs a
  migration.

  `source_daily` has no poll counters for exactly this reason. Charting polls,
  304s and errors would be useful, but nothing records a poll as an event, so
  those columns could only be accumulated. They need an operational record
  first.
- **Narrow.** Counts and sums. A rollup carrying a JSONB blob is not a rollup —
  see [ADR-0016](../../../docs/architecture/adr/0016-scores-are-computed-not-materialised.md)
  for what one such column cost.
- **Written by the maintenance queue**, never on the request path. Idempotent
  upsert on `(entity, day)`, over a window rather than a cursor, so re-running is
  how a bug is fixed.
- **Emit zero rows, not missing rows.** Build the day axis from
  `generate_series`, not from the data. On a chart a gap and a zero look
  different and mean different things — "we saw nothing" is a fact, "we have no
  data" is an absence — and the client cannot tell them apart unless the zero is
  actually sent.

## 3. Price it before you build it

Do the arithmetic and put it in the commit message. Two numbers:

```
rows added per day  = entities × 1        (a rollup)
rows read per render = ?                  (must not grow with the corpus)
```

If the second number grows with the corpus, the shape is wrong — go back to
step 1.

For reference, the numbers that forced ADR-0016: a per-user materialised table
reached **1,952 MB for 244 users**, 78.4% of the database, projecting to **16 TB
at a million users** and 203 TB once the corpus grows. It existed to avoid **22
ms** of CPU.

## 4. The endpoint

1. **`api/openapi.yaml` first.** Handlers and the TypeScript client are
   generated from it; writing the handler first means writing it twice.
2. `make generate` to regenerate `web/src/lib/api.d.ts`.
3. Store function in `internal/store`, returning a domain type — never
   `pgx.Rows`. The handler does HTTP and nothing else
   ([ADR-0015](../../../docs/architecture/adr/0015-hand-written-sql-in-a-store-layer.md)).
4. Aggregate for the display in the handler, not the client. `source_daily`
   keeps per-source grain and `/v1/market/ingest` folds it to vendor, because
   sixty-five lines is not a chart anyone can read and three to eight is — and
   keeping the finer grain means a per-company view later needs no migration.
5. Cache header if it is public and the rollup only moves hourly.

## 5. Drawing it

Load `/design-law` — it carries the rules generic charting advice cannot know.
The ones that bite on a chart:

- **`null` is not `0`.** A day with no data is a break in the line, not a dip to
  zero. This is the same rule that makes an undisclosed salary say *undisclosed*.
- **Colour encodes direction, never decoration.** If a line means "shrinking",
  it is `--shrink`. Do not colour series by whatever looks nice.
- **Bands lead, numbers follow** — and on hover, give the reader the actual
  count. That is what a heatmap cell is for.
- Tabular figures for every number (`--font-mono`, `tabular-nums`) so a column
  does not jitter.
- It must survive JavaScript being off. An SVG rendered server-side does; a
  charting library does not, and none is worth 40 KB of the budget.

## 6. Verify

`/verify` is the ladder. For a widget specifically, the step people skip:
**look at it**, at 390px as well as 1440. `make screenshots` and read the PNG.
A chart that is technically correct and unreadable has still failed.
