# ADR-0002 — Frontend framework

- **Status:** **DECIDED** — SvelteKit, signed off 2026-08-15
- **Date:** 2026-08-15
- **Decision drivers:** performance on low-end laptops, dependency minimisation, dashboard
  interaction cost, ecosystem risk

## Context

You proposed **Next.js or Vue/Nuxt**, on the reasoning that they are "very light on resources" and
support parallelisation and SSR.

The SSR and server-work half of that reasoning is exactly right and is preserved in every option
below. **The "light on resources" half does not survive contact with the benchmark data**, and since
you explicitly asked me to validate the stack rather than accept it, this ADR presents the numbers
and disagrees with the premise.

The product constraint is unusually specific: *"snappy on lower-end laptops"*, with the Game Boy
analogy — a fixed, small resource budget spent well. That constraint discriminates strongly between
these options, where most product requirements would not.

### The workload is a dashboard, which matters

Framework comparisons usually measure a content page. Ours is a filter-heavy, list-heavy,
chart-bearing authenticated app. For that shape:

- Initial bundle matters, but **interaction cost matters more** — INP on filter changes, list
  re-renders, chart mounts.
- We have already decided filtering happens server-side
  ([P1](../../product/principles.md#p1--ship-data-not-javascript)), so the client renders ~25 cards
  at a time. That *narrows* the gap between frameworks.
- But the low-end-laptop constraint is about the CPU cost of hydration and re-render, which is where
  the gap is widest.

## Options

> **Re-verified 2026-08-15.** The earlier version of this table cited blog benchmarks. The two rows
> that decide the question are now measured facts; the correlational evidence is separated out below
> and explicitly de-weighted. Working:
> [verification-log V10](../../research/verification-log.md#v10--frontend-bundle-sizes-resolved-with-the-soft-part-isolated).

**Runtime library size — A-grade, verifiable from the published packages** `[A-24]`:

| Library | Min + gzip | Why |
|---|---:|---|
| React + ReactDOM | **~48 KB** | Runtime framework |
| Vue | **~33 KB** | Runtime framework |
| Svelte | **~1.6 KB** | **A compiler** — the framework largely disappears at build time |

**Framework baseline page weight — A-grade, from real build output** `[A-25]`. Next.js App Router
"First Load JS shared by all" measures **80.4 KB** and **102 KB** in two public examples; one
documented Pages→App Router migration went **141 KB → 200 KB (+42%)**, a shared-baseline increase
every route inherits. Community guidance from those same measurements: **< 130 KB good, 170–200 KB
warning, 200 KB+ investigate.**

| | **SvelteKit 2 / Svelte 5** | **Next.js 16 (App Router)** | **Nuxt 4 / Vue 3** |
|---|---|---|---|
| Minimal page JS | **~15–20 KB** | **80–102 KB (measured)** | ~60–80 KB |
| Reactivity model | **Compiled — no VDOM** | VDOM + reconciliation | VDOM + reactivity proxies |
| SSR / streaming | ✓ | ✓ (RSC — most sophisticated) | ✓ |
| Server-side data loading | ✓ `load` functions | ✓ Server Components | ✓ `useAsyncData` |
| Scoped CSS built in | **✓ — no CSS-in-JS dependency** | ✗ (needs a solution) | ✓ |
| Built-in stores / state | **✓ runes — no state library** | ✗ (Zustand/Redux typical) | ✓ Pinia (extra dep) |
| Ecosystem size | Smaller | **Largest** | Large |
| Hiring pool | Smaller | **Largest** | Large |
| Charting | Layerchart, or hand-rolled SVG | Recharts, visx, many | Many |
| Deploy target | Node, or static + API | Node (or a platform) | Node |

### Reading the evidence honestly

**What is now fact:** the ~46 KB runtime gap and the 80–102 KB Next.js App Router baseline. Both are
independently checkable, and together they decide the budget arithmetic below.

**What is correlational and stays that way.** The best methodology-stated real-user comparison uses
CrUX + HTTP Archive, homepages only: all-websites baseline **40.5%** passing Core Web Vitals, Astro
**> 50%**, SvelteKit above baseline, **Next.js ~25%**, **Nuxt ~20%**.

That gap looks decisive and should not be read as such, for four reasons:

1. **The publisher is Astro** — a direct competitor to Next.js and Nuxt.
2. **The authors name their own confounders**: version bias (older frameworks carry a long tail of
   legacy sites on outdated versions), homepages only, unexamined framework-age effects.
3. **HTTP Archive's own methodology note is explicit** — *"correlation does not equal causation. A
   technology being highly correlated with good (or poor) performance does not necessarily indicate
   that technology is the cause"* `[A-26]`.
4. **It is a 2023 report.** Next.js 16 and mature RSC adoption postdate it.

Much of that spread is plausibly *who picks each framework and for what*. Astro marketing sites are
not comparable workloads to Next.js applications. Treated as weak supporting evidence only `[B-36]`.

**Struck entirely:** the "65% smaller" and "1,200 vs 850 RPS" figures that appeared in the first draft.
No traceable methodology; not repeated anywhere in this repository.

**What cuts the other way, and is real:** Next.js has the largest ecosystem and hiring pool. React
Server Components can keep client JS small *with discipline* — but the measured baseline is 80–102 KB
before application code, and the Pages→App migration data shows the trajectory is upward, not
downward.

**What cuts the other way, and is real:** Next.js has the largest ecosystem and hiring pool. React
Server Components can keep client JS small *if used with discipline* — but discipline is the operative
word, and the default trajectory of a Next.js app is heavier, not lighter. Your prior artifacts were
React with Recharts, so there is genuine existing familiarity to weigh.

## Decision

**SvelteKit 2 with Svelte 5 runes.** Signed off 2026-08-15, with the ecosystem risk understood and
accepted.

Three reasons, in order of weight:

1. **It is the only option that satisfies the stated constraint by default rather than by
   discipline.** Against a 100 KB first-load budget: SvelteKit starts near **15 KB** and leaves ~85 KB
   for the application. Next.js App Router starts at a **measured 80–102 KB** `[A-25]` — meaning the
   *baseline alone* consumes the entire budget at the high end, before a single line of product code.
   Budgets you can only hit by constant vigilance get missed.

2. **It has the lowest dependency count**, which is an explicit principle
   ([P6](../../product/principles.md#p6--every-dependency-is-a-liability)). Scoped CSS and state
   management are built in. The React path needs a styling solution and a state library before
   writing a line of product code — that is the ≤ 6 runtime dependency budget half-spent on
   infrastructure.

3. **It matches the interaction profile.** No VDOM diff on every filter change is precisely the cost
   we are trying not to pay on a 2019 laptop.

**Charts:** hand-rolled SVG components for the dashboard's three or four chart types, with the design
system's semantic colours (teal = growing, rose = shrinking, ochre = contested). This sounds like more
work than importing a library, and for four chart types it is roughly the same work — while removing
a 40–90 KB dependency and giving exact control over theming and accessibility.

## Consequences

### Good

- The performance budget is comfortable rather than adversarial.
- Fewer dependencies, fewer upgrade treadmills, smaller supply-chain surface.
- Scoped CSS and runes remove two entire categories of decision.

### Bad, and accepted

- **Smaller hiring pool.** Real, and the main risk. Mitigated by the fact that Svelte is genuinely
  quick to learn for anyone who knows JS and a component framework — days, not weeks.
- **Smaller ecosystem.** We will hand-roll things that have an off-the-shelf React answer. Mostly
  charts, which we chose to hand-roll anyway.
- **Less battle-tested at very large scale** than Next.js. Irrelevant at our scale; noted honestly.
- **Departed from the originally stated preference** (Next.js/Vue). Raised explicitly and signed off
  rather than assumed.

## The fallback, recorded

Kept for the record, since it was costed as part of the decision. **If this is ever revisited toward
Next.js, the product still works.** What would change:

| Item | Adjustment |
|---|---|
| First-load JS budget | Raise from 100 KB → **160 KB** for `/jobs`. Below that is not realistically achievable |
| Discipline required | Server Components by default; `"use client"` becomes a reviewed exception, not a habit |
| Charts | Still hand-rolled SVG. Recharts would consume the entire remaining budget |
| Extra dependencies | +1 styling (Tailwind), +1 state if needed. Runtime dep budget 6 → 8 |
| Everything else | **Unchanged.** The API contract, SSR strategy, and all backend decisions are framework-independent |

Nuxt 4 sits between the two: ~60–80 KB baseline, scoped CSS and reactivity built in, Pinia as one
extra dependency. If the tie-breaker had been "not React, but a larger ecosystem than Svelte", Nuxt
would have been the reasonable middle.

**Note for whoever revisits this:** the API contract is OpenAPI-first and the backend is entirely
framework-independent, so a future framework change costs the `web/` directory and nothing else.

## Revisit

If the first real dashboard screen misses the INP budget on the reference low-end profile despite the
budget being met, the problem is our rendering approach, not the framework — re-examine virtualisation
and chart mounting before re-opening this.

## Sources

`[B-22]` framework bundle-size and INP comparisons, 2026. Graded B for direction, C for magnitude —
see [evidence-ledger.md](../../research/evidence-ledger.md).
