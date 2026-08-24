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

- `web/scripts/palette.mjs` — **the palette, in OKLCH. Never type a colour into
  `app.css`.** Edit the ramp, run `node scripts/palette.mjs`, paste the block.
  `palette.test.ts` fails if the two disagree.
- `web/src/app.css` — tokens, `@font-face`, and the shared system.
- `.svelte` `<style>` blocks — component-scoped styles. This is the intended
  pattern, not a mess to clean up: scoped styles that share a *name* do not
  share a meaning, and Svelte keeps them apart. Extract a component when the
  same *thing* exists twice, which is a correctness argument — the wordmark's
  WCAG target-size fix reached one of its three copies and not the other two.
- `web/src/lib/tokens.test.ts` — the contrast suite.
- `web/static/fonts/` — self-hosted woff2. The CSP forbids a font CDN.

## Before claiming done

```bash
make web-test        # includes the contrast assertions
make bench-budget    # the performance budget
make ui-audit        # loads every page at 390/834/1440 and checks the invariants
```

## Type and colour, and why they are what they are

**Instrument Sans + IBM Plex Mono**, self-hosted, 40 KB against a 45 KB budget.
Mono is structural: every measured value — score, count, salary, date — is set
in it with `tabular-nums`, because a score is a measurement and a column of them
must not jitter. Do not use mono decoratively.

The accent is `oklch(35% 0.09 258)`, a deep ink blue. It was Tailwind
`indigo-600` until 2026-08-25; `palette.test.ts` now forbids that hex by name.

**Two traps, both already walked into once:**

1. The accent cannot leave the blue-violet family. Teal, rose and amber already
   carry meaning and the accent must stay distinguishable from all three under
   common colour-vision deficiencies. That constraint is *why* indigo was there.
   What fixed it was dropping the chroma and the lightness, not changing hue.
2. **Do not reach for a high-contrast serif on warm cream with a terracotta
   accent.** `phase-5 §6.2/6.3` proposes exactly that, and it has since become
   the single most recognisable machine-generated look — adopting it would solve
   the stated problem by walking into a newer version of it. That section is
   superseded on this point.
