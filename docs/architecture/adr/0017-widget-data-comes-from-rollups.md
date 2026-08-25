# ADR-0017 — Per-user widgets query directly; global widgets read daily rollups

- **Status:** DECIDED
- **Date:** 2026-08-25
- **Decision drivers:** the product's direction is more widgets; two widget shapes have opposite
  cost curves, and the difference is not obvious from either one in isolation

## Context

The stated direction is to keep adding widgets — a homepage chart of jobs polled per source over
time, a dashboard heatmap of applications with per-day counts on hover, and more after those. Widgets
are how the product becomes legible, so the schema has to make adding one cheap.

The trap is treating them as one problem. They have opposite cost curves, and choosing the wrong
shape is expensive in a way that only appears at scale.

**Per-user widgets read one user's rows.** The applications heatmap reads
`application_events WHERE user_id = $1` over twelve weeks. A heavy job seeker generates perhaps 300
applications a year and four status transitions each: **~1,200 rows**, reached by index. At 1M users
the table holds 1.2 billion rows and every read still touches about 1,200 of them. **Corpus size is
irrelevant to the query.** A rollup here would add a write path, a backfill and a staleness window to
buy nothing.

**Global widgets read everything.** "Jobs polled per source per day" has no user to filter by. Today
it would scan `posting_observations`, which reached 213,102 rows within weeks and grows with every
poll of every board forever. That query gets slower for every user, which is the definition of a
widget that cannot ship.

Their sizes differ by orders of magnitude:

| Shape | Rows read per render | Grows with |
|---|---|---|
| Per-user (heatmap) | ~1,200 | that user's own activity |
| Global, raw (`posting_observations`) | all of it — 213k and rising | total polls, forever |
| Global, rolled up | 65 sources × 365 days = **23,725/year** | entities × days |

At a thousand sources and a decade, the rollup is 3.65M rows — still a table you can scan.

## Options

**A. Every widget queries the operational tables directly.** No new schema. Works until the first
global widget, then does not, and the failure arrives as a slow homepage rather than as an error.

**B. A general time-series store, or a metrics system.** Prometheus, Timescale, ClickHouse. Correct
for high-cardinality operational metrics. Wrong here: this is product data a user reads, it must join
to `sources` and `companies`, and it must survive as long as the account does. It also contradicts
[ADR-0003](0003-postgres-single-datastore.md) with no fired trigger.

**C. Roll up global widgets to a daily grain in Postgres; leave per-user widgets querying directly.**

## Decision

**C.** The rule, stated so it decides the next widget without another ADR:

> **If a widget's query has a `user_id` in its WHERE clause, it queries the live tables. If it does
> not, it reads a rollup.**

1. **Daily grain**, `date` not `timestamptz`. Every widget so far is a calendar chart; an hourly
   grain multiplies rows by 24 to serve a resolution no design asks for.
2. **Rollups are derived and disposable.** Each one must be rebuildable from the operational tables
   by a single statement, so a bug is fixed by recomputing rather than by migrating. A rollup that
   cannot be rebuilt has become a system of record, which is a different and much more expensive
   thing.
3. **Written by the maintenance queue**, once per day per entity, upserted on `(entity, day)`. Never
   written on the request path.
4. **Narrow.** Counts and sums only. A rollup carrying a JSONB blob is a table pretending to be a
   rollup — see [ADR-0016](0016-scores-are-computed-not-materialised.md) for what that cost.
5. **First instance:** `source_daily`, which serves the homepage chart. It replaces nothing; it makes
   a widget possible that today's schema cannot answer at all.

## Consequences

### Good

- A global widget becomes a small query against a small table, at a cost independent of how much the
  corpus has grown.
- The rule is mechanical, so "which shape is this widget" stops being a judgement call.
- Rebuildability means a wrong rollup is a bug, not an incident.

### Bad, and accepted

- **Up to 24 hours stale.** Acceptable for a chart of a trend; unacceptable for a counter that must
  agree with a live figure, which is why `/v1/market` keeps computing live.
- **Two places to update** when a new fact is worth charting: the operational write and the rollup.
  The rebuild-from-source rule limits the damage — the rollup can always be recomputed to match.
- **A rollup can be wrong for a day** if the job fails. It needs the same monitoring as any other
  maintenance job, and its absence should be visible in the chart as a gap rather than a zero — a
  missing day is not a day with no jobs.

## Revisit

- If a widget needs finer than daily, add an hourly rollup for **that widget**, do not change the
  grain of the existing ones.
- If rollup rows exceed **50M**, or a rebuild takes longer than an hour, revisit the grain and the
  retention rather than the datastore.
- If a per-user widget's query ever reads more than ~10,000 rows for one user, it has stopped being
  per-user and this rule sends it to a rollup.
