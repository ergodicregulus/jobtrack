---
name: design-law
description: Use when building or changing any JobTrack user interface — a page, a component, colour, type, or a number shown to a user. JobTrack's specific design rules, which generic frontend-design guidance cannot know.
allowed-tools: Read, Grep, Glob, Bash, Edit, Write
---

# JobTrack's design law

Generic design guidance will make this look competent. These rules are what make
it look like *this product*. Load `frontend-design` for craft; load this for the
rules that are specific to a job-search instrument that refuses to fabricate.

## The idea the interface has to carry

**This is an instrument, not a feed.** A user is being shown a judgement about
their career made by a model that must show its working. Every rule below follows
from that.

## Non-negotiable

**1. Never render a number the system did not compute.**
No placeholder statistics, no lorem-ipsum metrics, no invented testimonials, no
fabricated user counts, no stock photography of people at laptops. On a product
whose entire pitch is that it does not fabricate, a fake number on the marketing
page is the worst thing that could ship. If you need content, **the corpus is the
content** — live counts, a real score breakdown, real cited findings from
`docs/research/evidence-ledger.md`.

**2. `null` is not `0`.**
An undisclosed salary says *undisclosed*. An unscored posting shows `—`. Only
14.6% of postings disclose compensation; rendering the other 85.4% as ₹0 is a lie
the layout makes convenient.

**3. Colour encodes direction, never decoration.**
`--grow` teal = growing / matched / fresh. `--shrink` rose = shrinking / missing /
dead. `--uncertain` amber = contested / ageing. Tokens are named for what they
*mean*, not what they look like. Nothing is coloured because it looks nice.

**4. Colour is never the only signal.**
Every state carries text or a shape too. Gap chips carry a `−` prefix so a gap
reads with colour vision removed entirely.

**5. Bands lead, numbers follow.**
`91 Strong fit`, never a bare `91`. A raw number implies a precision the model
does not have.

**6. Matches quiet, gaps loud — then capped.**
Two gap chips plus "+N more". Five gaps per card made a wall of red once
extraction improved.

**7. One accent, everything else neutral.**
Semantic colours appear only as small signals — a rail, a dot, a chip border —
never as large surfaces. Tinting whole card backgrounds made it read as a chart,
not a product.

**8. Contrast is computed, never eyeballed.**
`web/src/lib/tokens.test.ts` asserts the token set in both themes with alpha
compositing. Three of the original design's own values failed AA and were taken a
step darker. **Add a token, add its assertion.**

**9. Targets ≥ 24px** (WCAG 2.2 SC 2.5.8), 44px under `@media (pointer: coarse)`.

**10. It works with JavaScript off.**
Filters are links. Forms are forms. The filter sheet is `<details>` — no JS, no
focus trap. State lives in the URL, so every filtered view is shareable and
cmd-clickable.

## Budgets, enforced in CI

| Budget | Limit |
|---|---|
| First-load JS, gzipped, `/jobs` | ≤ 100 KB (currently 75.1) |
| CSS, gzipped | ≤ 20 KB (currently 10.5) |
| INP p75 @ 4× CPU throttle | ≤ 200 ms |

`make bench-budget` enforces the first two. A regression fails CI; raising a
budget needs a written trade in the PR.

## Where things live

- `web/src/app.css` — tokens and the system. **New shared style goes here.**
- `.svelte` `<style>` blocks — component-specific only. There are currently 1,809
  lines scattered across 13 of them against 736 in `app.css`, which is backwards
  and is being corrected. Do not add to the pile.
- `web/src/lib/tokens.test.ts` — the contrast suite.

## Before claiming done

```bash
make web-test        # includes the contrast assertions
make bench-budget    # the performance budget
make ui-audit        # loads every page at 390/834/1440 and checks the invariants
```

## Known weakness, so you do not reproduce it

The accent is `#4f46e5` — Tailwind's `indigo-600` — on `ui-sans-serif, system-ui`.
Those are the two most-repeated choices in machine-generated interfaces, and no
amount of correct spacing compensates. The warm stone ground (`#fbfaf9` / `#1c1b19`)
is *not* the problem and should stay. If you are changing the accent or the type,
that is an ADR: see `docs/engineering/phase-5-production-readiness.md` §6.
