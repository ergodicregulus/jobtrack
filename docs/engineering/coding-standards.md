# Coding standards

> Status: **DECIDED**. Anything marked *enforced* fails CI; the rest is review guidance.

## 1. Comments — the rule that gets asked about

**Comments explain why; code explains what.** A comment restating the line below it is noise and gets
flagged in review.

### Write a comment when

**The code encodes a decision a reader would otherwise second-guess.**

```go
// Lever returns createdAt as epoch milliseconds, not ISO-8601, and this is
// the only vendor that does. See research/source-catalog.md#lever.
postedAt := time.UnixMilli(raw.CreatedAt)
```

**Our own uncertainty must not become the user's penalty.**

```go
// Return full marks rather than a guess when we could not parse the posting's
// YoE reliably. Penalising the user for our extraction failure would make the
// score say something about our parser rather than about their fit.
if posting.YoEConfidence < minYoEConfidence {
    return components.Neutral(weight)
}
```

**The obvious implementation is wrong for a recorded reason.**

```go
// Two consecutive misses, not one. A partial vendor response would otherwise
// close an entire board — a failure mode that has embarrassed every aggregator
// that skipped this check.
if posting.MissingCount >= 2 {
    posting.Close(now)
}
```

**Doc comments on exported identifiers.** Required, and they start with the identifier's name.

### Do not write

```go
// increment i                                    ← restates the code
i++

// ---------- Helpers ----------                  ← section banner; use a file
// SetName sets the name.                         ← adds nothing
// func oldVersion() { ... }                      ← delete it; git remembers
```

### The test

Delete the comment. If a competent reader would now have to reconstruct a decision, put it back. If
they would lose nothing, leave it out.

Density is not a target in either direction. A tricky 20-line function may deserve three comments; a
clear 200-line file may deserve none.

## 2. Errors

**Wrap with context, compare with `errors.Is`/`As`, never with string matching.**

```go
if err != nil {
    return fmt.Errorf("fetch source %d (%s): %w", src.ID, src.Vendor, err)
}
```

Include the identifiers needed to find the thing. `"fetch failed"` is useless at 3 a.m.

**Sentinel errors for expected conditions**, in `domain`:

```go
var (
    ErrNotFound       = errors.New("not found")
    ErrSourceDisabled = errors.New("source disabled by circuit breaker")
)
```

**Never log and return.** Log at the boundary where the decision is made — the HTTP handler or the job
worker. Logging at every frame produces five lines per failure and no more information.

**`panic` only for programmer error** (impossible state, failed startup invariant). The HTTP recovery
middleware exists as a safety net, not as an error-handling strategy.

## 3. Context

Every function that does I/O takes `ctx context.Context` as its first parameter. No exceptions.

- Never store a `Context` in a struct.
- Never pass `nil` — `context.TODO()` if you genuinely have none, and it will be reviewed.
- `ctx` carries cancellation, deadlines, and the trace span. Dropping it breaks tracing silently,
  which is the worst way for it to break.

## 4. Naming

```go
// Good — the name says what it is
func (s *Scorer) ScorePosting(ctx context.Context, r Resume, p Posting) (Scored, error)

// Bad — abbreviations that save nothing and cost a lookup
func (s *Scorer) ScrPst(ctx context.Context, r Rsm, p Pst) (Scr, error)
```

- **No stutter.** `job.Posting`, not `job.JobPosting`.
- **Short names for short scopes**; `i` in a three-line loop is fine, `i` across forty lines is not.
- **Interfaces are named for behaviour**: `Adapter`, `PostingStore`. Never `IFoo` or `FooInterface`.
- **Booleans read as assertions**: `hasDisclosedComp`, not `compFlag`.
- **Units in the name** where ambiguity is possible: `timeoutSeconds`, `sizeBytes`, `compMinorUnits`.

## 5. Interfaces

**Defined by the consumer, not the producer.** `matching` declares the narrow interface it needs and
`store` happens to satisfy it. This is idiomatic Go, and it is what keeps `matching` testable without
a database.

```go
// in internal/matching — small, and exactly what this package uses
type PostingStore interface {
    GetPosting(ctx context.Context, id int64) (job.Posting, error)
}
```

Keep them small. A five-method interface usually means the consumer is doing several things.

## 6. Database access

**SQL lives in `.sql` files**; `sqlc` generates the Go. Never build SQL by string concatenation.

```sql
-- name: ListLiveJobsForFeed :many
-- The status='live' predicate is REQUIRED. Every feed index is partial on it,
-- and omitting it silently drops to a sequential scan over ~3x the rows.
SELECT ... FROM job_postings
WHERE status = 'live' AND country = $1 AND (posted_at, id) < ($2, $3)
ORDER BY posted_at DESC, id DESC
LIMIT $4;
```

Rules:

- **Keyset pagination, never `OFFSET`** on user-facing lists.
- **Transactions are explicit and short.** Never hold one across a network call to something that is
  not the database.
- **`SELECT *` is prohibited** — it breaks when a column is added, which is the one thing
  expand/contract migrations do constantly.
- Every query touching a partial index includes its predicate. There is a `sqlc vet` rule for this.

## 7. Concurrency

- **Never start a goroutine without knowing how it ends.** Every one gets a `ctx`, and something
  waits for it.
- `errgroup` for parallel work with error propagation.
- Bound concurrency explicitly — a semaphore or worker pool, never unbounded `go` in a loop over
  input you do not control.
- **The race detector runs in CI.** Non-negotiable.

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(maxConcurrentHosts)   // politeness is a hard limit, not a suggestion
for _, src := range sources {
    g.Go(func() error { return fetch(ctx, src) })
}
return g.Wait()
```

## 8. Configuration and magic numbers

A literal that a reader cannot immediately justify becomes a named constant, and the name explains the
choice.

```go
// Two consecutive full polls without seeing a posting before closing it.
// One is not enough: a truncated vendor response would close an entire board.
const closureMissThreshold = 2

// Postings collect hundreds of applications within hours, so ranking decays
// fast. See architecture/matching-and-scoring.md §5.
const freshnessHalfLifeDays = 7
```

Anything an operator might want to change without a deploy goes in config, not a constant. Scoring
weights are the primary example ([matching-and-scoring §1](../architecture/matching-and-scoring.md#1-the-model)).

## 9. Frontend (TypeScript)

- **`strict: true`.** No `any` without a comment justifying it.
- **No `default` exports** — named exports keep imports greppable.
- Components take typed props; no prop spreading into unknown shapes.
- **Server data types are generated from the OpenAPI spec.** Hand-written API types are prohibited —
  they drift, silently.
- CSS: scoped to the component, custom properties for tokens, no inline styles for anything themeable.
- **Every colour comes from a token.** A raw hex in a component is a review comment
  ([frontend-architecture §4](../architecture/frontend-architecture.md#4-design-system)).

## 10. Enforced in CI

| Check | Tool |
|---|---|
| Formatting | `gofumpt`, `prettier` |
| Vet + static analysis | `golangci-lint` (govet, staticcheck, errcheck, ineffassign, bodyclose, sqlclosecheck) |
| Race detector | `go test -race` |
| Import boundaries | import-graph linter, **no exemptions** |
| Generated code current | `make generate` must leave the tree clean |
| SQL sanity | `sqlc vet` (includes the partial-index predicate rule) |
| API compatibility | `oasdiff` vs. previous release |
| Proto compatibility | `buf breaking` vs. previous release |
| Frontend budgets | bundle size + Lighthouse CI on the reference profile |
| Accessibility | `axe-core` |
| Vulnerabilities | `govulncheck`, `npm audit` |

## 11. Review checklist

Reviewers are asked to check these, in this order:

1. **Does it violate a principle or anti-feature?** ([principles.md](../product/principles.md)) — if
   yes, stop; nothing else matters.
2. **Does it contradict an ADR?** If so, the change is a superseding ADR, not a PR.
3. **Does a user-visible number trace to the [evidence ledger](../research/evidence-ledger.md) with
   its grade shown?**
4. **New dependency?** The PR must say what it replaces and what breaks if it is abandoned.
5. **Do the comments explain *why*?** Delete any that restate the code.
6. **Is the error message useful at 3 a.m.** — does it contain the identifiers needed to find the
   thing?
7. **Migration:** is it backward-compatible with the currently-deployed release?
8. **Is a failure visible?** Silent degradation is worse than a loud error — a stale feed that says it
   is stale is recoverable; one that pretends to be fresh is not.
