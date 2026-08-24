# Roadmap to completion

> **Status:** written 2026-08-16 after the six design sessions closed. **All
> four phases resolved as of 2026-08-17**; a verification pass on 2026-08-24
> found seven defects that every green gate had missed — see
> [§5](#5-a-verification-pass-and-what-it-found). Every figure here was verified
> against the running system, not estimated.

This is the plan to take JobTrack from *"a working backend with a provisional
frontend"* to *"the product the design and the principles describe."*

It is ordered so that each phase is shippable on its own and nothing is built
twice. Where a phase has a decision in it, the decision is stated rather than
deferred, and the reasoning is recorded so it can be argued with later.

---

## 0. Where we actually are

Verified 2026-08-17 against the live stack. The 2026-08-16 figures are kept
beside them, because the deltas are the story of Phases 1–3.

| | 2026-08-16 | 2026-08-17 | 2026-08-24 |
|---|---|---|---|
| Go source files | 59 | 74 | **76** |
| Svelte components / routes | 16 | 22 | **22** |
| Migrations | 13 | 14 | **17**, all applied, none dirty |
| Playwright end-to-end tests | 24 | 42 | **43**, all passing |
| Go adapter test files | 1 of 3 vendors | 2 of 3 | **3 of 3** |
| ATS vendors | 2 | 3 | **3** |
| Live postings | 6,677 / 61 companies | 8,985 / 65 | **15,379 / 65**, 0 source errors |
| Live postings in India | 278 | 631 | **1,114** |
| Scores computed | ~96,000 | 578,769 | **1,208,308** |
| Time to score one posting (62 users) | — | 32.09 s | **0.49 s** |
| First-load JS | 60 KB | 74.5 KB | **75.0 KB** of a 100 KB budget |
| CSS | 7.2 KB | 10.2 KB | **10.5 KB** of a 20 KB budget |
| Resume formats supported | none | PDF, DOCX, plain text | unchanged |
| Unreadable postings in the top 100 | 42 | **11** | not re-measured |
| Score correlation: skills vs freshness | 0.184 / 0.274 | **0.428 / 0.241** | not re-measured |
| `NOT_YET` make targets | 3 | 0 | **0**, macro deleted |

The corpus grew 71% on 2026-08-24 without a new source being added: one vendor's
board had been silently truncated at 2,000 postings of 4,770. That is §5.2.

**All four phases are resolved.** The backend was in good shape and is now
correct in four places it measurably was not; the frontend has been rebuilt to
the design; resume parsing ships for PDF, DOCX and text; Phase 4's triggers were
measured and mostly answered "do not build".

Two of the most valuable outcomes here were **decisions not to change code**:
the `skillsFullEvidence` threshold and the freshness weighting both looked wrong
and measured fine. Both are recorded with their numbers so the next person
starts from the measurement rather than the same hypothesis.

---

## Phase 1 — Correctness debts, before anything is built on top

Three items. All are small, all are backend or token level, and all are
**currently wrong in production code**. They come first because the frontend
rebuild would otherwise inherit them.

### 1.1 The teal token fails WCAG AA

`--teal` `#0d9488` is used as text in the score chip (`91 Strong fit`, 15px) and
the "New" tag (11px). On its own `--teal-soft` ground it measures **3.32:1**
against a 4.5:1 requirement.

This is the most-repeated element in the product. **Change to `#0f766e`**, which
measures 4.86:1 on the same ground and is visually near-identical.

Worth noting for the record: this is the *third* instance of the same class of
bug — amber was caught in design session 2, `--ink-3` in the same session, and
teal survived an audit that was explicitly looking for it. **Contrast must be
computed, never eyeballed.** Add a check that walks the token file and fails on
any text pairing below 4.5:1, so the fourth instance is impossible.

- **Test:** a unit test over the token set, not a visual review.
- **Done when:** the check exists and passes, and no token pairing used for text
  is below 4.5:1 in either theme.

### 1.2 Automatic ghosting asserts something we do not know

`GhostSweepWorker` sets `applications.status = 'ghosted'` after 21 days without
movement. This violates §3 of the design brief — *"anything implying we have data
we do not have"* — and it does so twice over:

1. Silence is not evidence of intent. A hiring freeze, a delayed loop or a
   recruiter on leave all look identical to being ghosted.
2. **Our records only contain what the user typed.** Someone who received a reply
   and did not log it has their application moved to Closed by us, based on the
   absence of data in our own database rather than in the world.

The design raised this unprompted and it is correct. The utility is worth
keeping; the verdict is not.

**Change:** stop writing `status = 'ghosted'` automatically. Surface *"no movement
for N days"* as a derived fact — which the dashboard's "Needs your attention"
widget already does honestly — and leave `ghosted` as a status a person can
choose. Retire the worker or repurpose it to maintain the derived field only.

- **Test:** an application untouched for 30 days keeps its status, and appears in
  the attention query.
- **Done when:** no code path writes a terminal status the user did not choose.

### 1.3 Score grouping — RESOLVED, and the measurement found something bigger

The design asked whether showing `91` beside `88` implies a distinction the model
cannot support. Rather than reason about it, we measured. [ADR-0009](../architecture/adr/0009-score-granularity-and-skill-coverage.md)
records it in full. **The display question turned out to be the wrong
question.**

Measured against the live corpus, **the ranking correlated more strongly with a
posting's age (0.256) than with whether the reader could do the job (0.173)** —
because the skills vocabulary held 47 terms and extracted **nothing from 48.7%
of live postings**. An abstaining skills component removes 40 of 100 points from
the denominator, dropping the average available weight to 48.4 and handing
freshness up to 40% of the score.

Decisions: the visible score stays (the sort-key argument holds); grouping
near-ties is deferred as treating a symptom; **the vocabulary was expanded from
47 to ~120 terms**, each with its aliases and, where it collides with ordinary
English, a context rule and a test. `tf`, `torch` and a bare `lambda` were
deliberately *not* added and have tests saying why.

---

### Phase 1 — COMPLETE (2026-08-16)

| Item | Outcome |
|---|---|
| 1.1 Contrast | `--fg-subtle` was 3.20:1 and `--fg-faint` 2.06:1 — both carrying real text. Fixed, and **28 contrast assertions** now compute every text pairing in both themes. Verified to fail before the fix |
| 1.2 Ghosting | `GhostSweepWorker` deleted. Nothing writes a terminal status on a user's behalf. `days_since_activity` reports the silence as a fact. It had never actually closed anything, so no data repair was needed |
| 1.3 Score granularity | [ADR-0009](../architecture/adr/0009-score-granularity-and-skill-coverage.md). Display unchanged; the measurement found a much larger problem and fixing it took two changes, not one |

**What 1.3 actually cost, and why it was worth it.** The vocabulary grew from 47
to 113 terms — and changed nothing, because `posting_skills` joins the `skills`
table and that table was filled from a *second* list. Every new skill was
discarded at the join, silently. Fixed by making the vocabulary the single source
of truth, with two tests to stop the lists drifting again.

Then extraction reached 16.1%-zero and abstention still sat at 73.2%, because
**73.7% of extractions are classified `mentioned`** and the scorer read only
`must_have`/`nice_to_have`. Postings without a "Requirements" heading were having
28,056 successfully-extracted skills thrown away.

| | Before | After |
|---|---|---|
| Postings with zero skills | 48.7% | **16.1%** |
| Skills component abstaining | 80.5% | **20.3%** |
| Average skills per posting | 1.71 | **3.57** |

**One finding is left open on purpose.** ADR-0009's own test was *"if freshness
still outranks skills on a corpus we can actually read, the weighting is wrong."*
It does (0.274 vs 0.184). That condition has fired and needs a successor ADR — a
weighting change alters every score in the product and should not be tacked onto
a change about coverage. `make coverage` is the instrument.

---

## Phase 2 — The frontend rebuild

### In progress

| Done | Note |
|---|---|
| **Settings page** | `/settings`, form-based. Works with JavaScript off, which the header toggle never did |
| **Theme out of the header** | Persistent real estate is earned by frequency × value |
| **Density removed** | Across CSS, the shell, `hooks.server.ts`, the domain type and the OpenAPI contract. Rows written earlier keep the inert key rather than being migrated |
| **Job detail page** | `/jobs/{id}` and `GET /v1/jobs/{id}`. The five weighted components, each with the reason it earned what it did, replayed from what the scorer stored rather than recomputed |
| **Feed titles link to detail** | They pointed straight at the ATS, so the breakdown had nowhere to be read. Apply is still a direct employer link |
| **"New since your last visit"** | `last_active_at` existed and **nothing ever wrote to it**, so anything built on it would have compared against a seed value. Migration 0014 adds `previous_visit_at`: one timestamp updated as you browse is always ~now, so the marker would erase itself the moment it was read |
| **Keyboard navigation** | `j`/`k`/`Enter`/`s`. Moves real focus rather than a separate highlight, so the focus ring and the cursor cannot drift apart. A bare letter typed into search stays text — there is a test for that |

29 E2E tests passing. Budget: 65 KB of 100 KB JS, 8.0 KB of 20 KB CSS.

### Remaining


**One pass, not nine.** Everything in the held list is now specified by design
sessions 1–6, so this is implementation rather than decision-making. Landing it
piecemeal would mean rewriting the same components repeatedly as later pages
arrive.

### 2.1 Tokens and components

Port the design's `:root` block and the eleven components. This is the
foundation; nothing else starts until it is in place and every component has its
full state set — default, hover, focus-visible, active, disabled, loading, empty,
error.

Carry over these decisions from the sessions, which are already settled:

- **One accent, semantic colour as small signals only.** Matched skills are a
  neutral chip with a 5px teal dot; a gap is the only coloured object in a row.
- **Gap chips carry a `−` prefix**, so the gap reads without colour at all.
- **Bands lead, numbers follow.** `91 Strong fit`, never a bare figure.
- **Borders are ring shadows**, so focus can thicken without moving a pixel.
- **One density**, the current compact. The setting is removed — it changed
  padding only and never type or target size, so its documented accessibility
  justification was false.
- **44px targets on `@media (pointer: coarse)`**, 24px minimum otherwise.

### 2.2 Shell and settings

- Move theme out of the header into a **Settings page** that does not exist yet.
  `/v1/me/preferences` already backs it.
- Header becomes wordmark · nav · global search · account menu, built to hold
  five nav items before it needs overflow.
- Delete the density control and clear any stale `data-density` attribute on
  mount.

### 2.3 The pages

In this order, because each depends on the last:

1. **Jobs feed + filter system** — every filter a chip with a live count, active
   filters individually removable, state in the URL, working with JavaScript
   off. The 390px filter sheet is the hardest piece: one group at a time, and a
   primary button that states the outcome (*"Show 1,720 postings"*) so nobody
   applies a filter blind.
2. **Dashboard** — banded full-width layout, not a two-column grid with a rail
   that runs out. Stat tiles link to real filtered views (the `band` filter now
   exists). Three states: ordinary day, busy, first run.
3. **Job detail** — the score breakdown in full. This is the signature moment and
   it currently has nowhere to live.
4. **Profile + skills editor** — skills are 40% of every score, so this one
   control determines the quality of every match a user ever sees.
5. **Tracker** — one-click stage advance, single column plus stage chips on a
   phone, reusing the feed's filter chips rather than a new control.
6. **Onboarding, auth, landing, system states.**

### 2.4 The four states the design deliberately left undesigned — DONE

Named rather than hidden, and they were ours to specify. All four are built,
with 5 unit tests and 5 E2E tests covering the paths nobody exercises by hand.

- **A failed save, anywhere** — the important one, since every optimistic update
  assumed success. `web/src/lib/mutate.svelte.ts` is now the single handler for
  all three call sites (feed card, tracker, job detail), replacing three
  different ad-hoc `catch` blocks, one of which printed raw status codes at the
  user. It makes four distinctions a bare try/catch does not:

  | Case | What the user is told | Retry offered? |
  |---|---|---|
  | Request never arrived | offline vs. "could not reach the server" | yes |
  | 401 | session expired, sign in — nothing lost | no, a sign-in link |
  | 404 | the posting is no longer live | no, `gone` flips the page |
  | 409 | the server's own reason, verbatim | no — it will fail identically |
  | 5xx | "on our side", never the status code | yes |

  `gone` is deliberately narrow: it drives a destructive UI change, so a test
  asserts no other status sets it.

- **Tracker loading** — the stage button kept its label instead of collapsing to
  `…`, which had shrunk the button mid-click and moved every control below it.
  `aria-busy` carries it for screen readers; a 5px pulsing dot carries it
  visually at zero layout cost.

- **Offline** — a sticky amber banner driven by the `offline`/`online` events.
  Amber, not red, per the signal vocabulary: nothing broke. It says *"pages
  already open still work; saving will not"*, because the alternative reading of
  a connection banner is "this app is dead". It never announces "back online" —
  `navigator.onLine === true` only means an interface is up, and claiming a
  restored connection would be a promise the browser cannot make.

- **A posting that closes while you are reading it** — no polling. The API only
  serves live postings, so the first write after a closure returns 404, which is
  a free and honest trigger. The page then says so, disables Save, and
  **leaves Apply working**: our index dropped the role, but the employer's own
  ATS page may still accept an application, and claiming otherwise would be
  asserting something we have not checked.

### 2.6 Held items from the scaffold — DONE

| Item | What it was | What changed |
|---|---|---|
| **Dashboard right column ran short** | One long match list beside a rail of three *conditional* panels. On an account with nothing urgent and a complete profile, one of three rendered and left ~290px of void — so the layout was worst exactly when the user had least going on | Bands, not a rail. Nothing sits beside the list. Urgency moved to the top of the page where it cannot be orphaned; pipeline and profile-strength pair (and the survivor goes full width when the other is hidden). The list gaining full width also fixed the `San Francisco, Ne…` truncation that had 400px of empty rail beside it |
| **Job cards tall and sparse** | Company/location/mode and salary/years/age/source were two fixed rows, together running ~340px of an available 620px — a line height of whitespace, 25 times down the page | One flex line that wraps back to two on a phone by itself. No breakpoint decides where |
| **Landing page thin below the fold** | Hero with an empty right half, four cards, footer. It *claimed* "scores that show their working" and never showed one | The breakdown itself fills the hero — including an abstaining row, because the honest failure mode is the part nobody else would put on a landing page. Below it: what it will not do (the anti-features, which cost something to keep), and real corpus figures from `/v1/market` |
| **No loading or skeleton states** | Blank page during navigation | `NavProgress` (120ms delay — under Nielsen's ~100ms "instantaneous" threshold nothing should flash), plus per-action `aria-busy`. Deliberately not skeletons for navigation: replacing readable content with grey rectangles destroys information the user still has |

Two defects found by looking at the screenshots rather than by testing:

- The dashboard's funnel bars went full width, turning a count of **3** into a
  950px bar that read as a nearly-complete progress meter. Capped at 340px — a
  bar's length only means something against its neighbour.
- Two links named "Create an account" on the landing page. Ambiguous for a
  screen reader's link list, and it broke a strict-mode locator, which is how it
  surfaced.

### 2.7 Bringing the app back in line with the design — DONE

The implementation had drifted from the Claude Design output in ways that were
invisible one at a time and obvious side by side. Re-derived from
`Form scope and priorities-6/JobTrack.dc.html`, the final cumulative export.

**Tokens.** The design's warm stone ramp adopted verbatim — `#fbfaf9` ground,
`#1c1b19` ink, warm `#131211` in dark — replacing a cool blue-grey palette that
made the indigo accent read as stock. Kept our token NAMES (`--grow`/`--shrink`
encode direction; `--teal`/`--rose` encode hue) so no component had to change.

Three of the design's own values failed contrast and were taken one step
darker, which is the same call made once before on teal:

| Token | Design | Measured | Ours |
|---|---|---|---|
| `--grow-ink` (score numbers) | `#0d9488` | 3.44:1 | `#0f766e` (5.02) |
| `--uncertain-ink` ("estimated") | `#b45309` | 4.39:1 | `#92400e` (6.51) |
| `--fg-subtle` (205 text uses) | `#78716c` | 4.40:1 | `#6b645f` (5.33) |

The contrast test gained **alpha compositing** to catch these: the chip
backgrounds are now `rgb(r g b / a)` so one tint works on every surface, and a
ratio measured against a translucent colour answers a question nobody is
looking at.

**Layout primitives.** The design's `data-*` attributes ported into `app.css` —
`feed-grid`, `dash-two`, `detail-grid`, `board`, `search`, `wide-only`,
`narrow-only`, `target`. They are the handful of rules inline styles cannot
express, and keeping them in one block makes the whole app's responsive
behaviour readable at once.

**What was missing entirely:**

- **The filter rail** — 272px, five grouped chip sets, every chip a LINK with a
  live count (so it works JS-off, is shareable, and cmd-clicks into a new tab).
  Replaced a horizontal bar of three mode chips that could not grow. Needed new
  YoE and salary facets; the salary counts are cumulative because that is what
  the chip claims. Below 1040px it becomes a `<details>` sheet — no JavaScript,
  no focus trap.
- **Active-filter chips**, individually removable. "Clear all" alone forces
  someone to rebuild four filters to drop one.
- **Global search in the header**, ≥900px, paired with the feed's own on a
  single breakpoint so exactly one exists at any width.
- **The activity heatmap** — 12 weeks x 7 days, three levels, counts on hover.

**The heatmap needed a data source that did not exist.**
`application_events` was created in the first migration and **nothing had ever
written to it**, so the grid would have rendered empty forever while looking
like a working feature. Status changes now record an event — and only real
transitions do, because counting a no-op PATCH would inflate a record whose
entire value is being an honest one.

The first implementation was one clever CTE joining `SELECT ... FOR UPDATE`
against a sibling `UPDATE`. It compiled, ran, returned 204 and logged nothing:
within a single statement those two do not see each other's rows, so the join
was empty every time. Replaced with three plain statements in a transaction.

**Skill display names.** They were `initcap(canonical)` in SQL, which put
**"Aws", "Graphql", "Llm", "Node.Js", "Postgresql" and "Ci/Cd"** on every card,
chip and gap line in the product. Now a table in the vocabulary, applied with
`DO UPDATE` so the fix reaches rows that already exist. The profile and resume
endpoints return labels alongside canonicals — the canonical is an identifier
and is lower-case by design; rendering it is what produced the bug from the
other direction.

**Gap chips capped.** The rule "matches quiet, gaps loud" was right in
principle and wrong once extraction improved: a typical card carried five gaps
and the feed became a wall of red — the same failure as the original wall of
green, in another colour. Now at most two gap chips plus "+N more", each with a
`−` prefix so a gap reads without colour at all.

**Tracker.** Columns capped at `calc(100vh - 15rem)` with independent scroll.
With 33 applications the page was **5,100px tall** — five empty columns running
alongside one enormous one. A funnel whose stages cannot be seen together is
not showing a funnel.

**Two bugs found by the suite, both real:**

- Search was inside the signed-in branch while the feed's copy hides above
  900px, so an **anonymous desktop visitor had no search at all**. The feed is
  public on purpose; its search cannot be an account feature.
- `air` restarts a service before its 5s drain completes, so the replacement
  binds a held port and dies — a container reporting *running* with no process
  in it, for the third time this project. Drain is now 250ms when
  `APP_ENV=dev`; the delay exists for a load balancer, and there is none here.

### 2.9 A rendering audit, and what it found — DONE

Screenshots catch what eyes catch. `make ui-audit` catches what eyes miss: it
loads every page at 390, 834 and 1440 and checks the invariants this repo
already states somewhere — nothing outside the viewport, no page scrolling
sideways, every interactive target ≥24px (SC 2.5.8), one `h1` and no skipped
levels, every control labelled, no text clipped by a fixed height, and every
page's content starting at the same x as the wordmark.

**148 findings on the first run. Ten were real:**

| Finding | Cause |
|---|---|
| Every phone page scrolled sideways (415px in a 390px viewport) | The header nav is a flex item, and flex items default to `min-width: auto` — they refuse to shrink below their content. The design's own `navBarStyle` says `min-width:0; overflow:auto`; ours did not |
| Header read **brand, avatar, nav** on a phone | A `.nav { order: 3; width: 100% }` rule meant to drop the nav onto its own row. `.bar` has no `flex-wrap` and a fixed 58px height, so the width did nothing and the order just moved it past the avatar |
| The header search rendered at 390px, where it is hidden | Svelte scopes component styles with a generated class, which outranks a bare attribute selector — `.global-search { display: flex }` beat `[data-search] { display: none }`. Specificity, not the media query |
| The active-tab underline floated **6px below the header** | `bottom: -18px`, an offset that had to agree with a 58px bar, a 7px padding and a 14px font. The design specifies a filled pill and no underline at all |
| Skill-remove buttons were 16px | The control that removes a skill — 40% of every score — at two thirds of the minimum target size |
| Wordmark 23px, a settings link 17px | Both are links, so SC 2.5.8 applies |
| The resume upload's inputs were unstyled and 21px tall | A shared `.input` class exists in `app.css`; that page simply never used it |
| `/jobs` outline ran h1 → h3 | The results count was the `h1` while the filter rail's `h2` preceded it in the DOM, and the collapsed sheet's `h3` groups had no `h2` above them |
| Settings and Profile content started at **395px and 330px**, not 154px | A `max-width` inside the already-centred `.shell` centres a second time. Two different layouts in one app, visible on every click between Settings and Dashboard |
| The tracker was **5,293px tall on a phone**, with a column sliced down the middle | The design specifies *"single column plus stage chips on a phone"* and it had never been built |

Also brought in line with the design in the same pass: the auth screen (a
content-sized, top-aligned pair rather than a 50/50 grid with a full-height rule
and 277px of empty space above the form) and onboarding (a 560px top-aligned
column with a **segmented** progress bar, rather than a card floating in the
middle of the viewport — it was the last vertically-centred screen left).

**The audit is now green and runs as a target.** Three of its checks were
themselves wrong at first and are worth recording, because each was a false
positive that would have trained someone to ignore it: `.sr-only` is a 1px
clipping box on purpose; a native checkbox renders at 13px in every browser and
its *label* is the real target; and a heading hidden from view is still a
heading in the accessibility tree.

### 2.8 Verification for this phase

- **Update the E2E tests in the same pass**, not after. They assert on current
  selectors and copy and will immediately reveal anything a redesign silently
  dropped. That is a feature, and it worked: the rewrite broke four tests, and
  two of them were pointing at real defects rather than stale selectors.
- Re-check both budgets: 100 KB JS, 20 KB CSS.
- `make screenshots`, then **look at them**. Every page, both widths, both
  themes.
  Every layout bug in this project so far was found by looking, not by testing —
  the 5,100px tracker and the sticky bar reading through its own background were
  both found this way, by a test suite that was entirely green.

**Result: `make check` green, 41 E2E passing, 74.7 KB JS of 100, 10.2 KB CSS of
20.**

One caution worth writing down for whoever picks this up: three of the four
E2E failures during this pass were **flakes caused by a service that had
silently died**, not by the change under test. Before debugging a test, check
`docker compose logs <service> --tail 5`. A container reporting *running* has
proved, three times now, to be no evidence at all that the process inside it is
alive.

---

## Phase 3 — Resume parsing — COMPLETE (PDF, DOCX, plain text)

**The largest available improvement to the product**, and the reason is
arithmetic: skills are 40 points of every score, and users typed them by hand.
Measured on a real CV through the running stack: **15 skills extracted where a
hand-typed profile had 5**, and after applying them the top of the feed went
from 5-of-8 engineering roles to 7-of-8.

### What was built

| Piece | Where | Note |
|---|---|---|
| Extraction | `internal/resume/text.go` | DOCX via `archive/zip` + `encoding/xml`, **zero dependencies** — a DOCX is a zip of XML and a library would buy only a supply chain. Format detected by **magic bytes, never the filename**: an attacker controls the extension, and a parser handed the wrong format is a parser being fuzzed |
| Section classification | `internal/resume/sections.go` | Headings anchored `^...$`, unlike the job-posting equivalent's prefix match — a CV heading *is* the line. Capped at 6 words, or a bullet starting "Experience building…" re-classifies the rest of the document |
| Understanding | `internal/resume/parse.go` | Skills with **provenance** (used-in-a-role beats merely-listed), years from date ranges, contact block, structural confidence, user-facing diagnostics |
| Encryption at rest | `internal/crypt` | Stdlib AES-256-GCM, nonce prepended. Its **own key**, separate from `SESSION_SECRET`: rotating a session secret logs everyone out and is therefore done casually, and must not be able to make every stored CV unreadable |
| The isolated service | `cmd/resume-parser` | `POST /parse`. Bytes in, JSON out, no state, no database, no egress |
| Upload and review | `POST /v1/me/resume` → `POST /v1/me/resume/{id}/apply` | Two steps on purpose: upload **proposes**, apply **commits** |
| The review UI | `/profile/resume` | Plain multipart form, works with JavaScript off |

### Decisions taken that the ADR did not settle

- **The parse-confidence contract** is *structural*, not a feeling: each check is
  something a CV reliably has (contact block, skills, dated roles), so a low
  number always has specific missing pieces the diagnostics then name. The
  number and the explanation cannot disagree.
- **The original file is deliberately not stored.** ADR-0007 is privacy-driven
  and the least risky place for a CV is nowhere; we keep the extracted text
  encrypted and the structured parse. The accepted cost is re-*extraction* after
  an extractor change — re-*parsing* an improved ruleset over stored text still
  works, which covers most improvements.
- **Provenance is enforced, not annotated.** `apply` accepts only skills the
  parse actually proposed, or the endpoint would be an arbitrary add-skill API
  wearing a resume's name and `origin='resume'` would be a lie. There is a test
  that sends an unproposed skill and asserts it is dropped.
- **Overlapping roles are unioned, never summed.** A job plus a contract is one
  span of time; summing invents years the candidate does not have, which is
  exactly the flattering error a CV parser must not make.

### Two real bugs this phase surfaced

- **`resume-parser` had never once started.** `DATABASE_URL` was required
  unconditionally by config, while compose deliberately blanks it for this
  service to prove the isolation. The binary crash-looped and `air` restarted
  it, so the container reported *running* the entire time. Now required for
  every service except this one.
- **Importing a CV appeared to do nothing.** `GET /v1/me/profile` returned only
  `origin='user'` skills, so fifteen imported ones changed nothing on screen.
  Fixed with a separate `resume_skills` field rather than merging: the editor
  PATCHes `skills` wholesale, so one list would silently promote every inferred
  skill to a declared one on the next save.

### PDF — done, and it corrected an assumption in ADR-0007

`pdftotext` as a subprocess from the already-isolated parser
([ADR-0012](../architecture/adr/0012-pdf-extraction-via-subprocess.md)). Zero
new `go.mod` entries; a crash, hang or allocation storm dies with the child
rather than the service, which is the same argument that created this service
one level further in.

**The surprise: `-layout` is wrong for CVs, and ADR-0007 said to use it.**
ADR-0007 states that layout-aware linearisation is the key step and that naive
extraction interleaves two-column resumes. Measured on a real two-column PDF,
that is backwards — `-layout` reproduces the *visual* columns and produces the
interleaving; plain `pdftotext` emits *document* order and keeps the sidebar
whole, then the body whole.

| On the two-column fixture | Reading order | `-layout` |
|---|---|---|
| Skills recovered | **7 of 7** | headings merged into body lines |
| Dated roles | **both** | glued to sidebar entries |
| Parse confidence | **1.00** | — |

The image moves from `distroless/static` to `distroless/base-debian12` (83 MB,
needs libc) and keeps no shell and no package manager; `pdftotext` plus exactly
its `ldd` closure is copied from a Debian stage, so a missing library fails the
**build**, not the first upload. `deploy/k8s/resume-parser.yaml` now exists and
encodes the posture ADR-0007 promised: no service-account token, read-only root,
seccomp, a 512 Mi cap, an in-memory `/tmp`, and a default-deny NetworkPolicy
whose only egress is DNS and the trace collector.

Poppler is handed a **file, not a pipe**: a PDF's xref table is at the end, so a
reader must seek backwards, and a pipe produced a bare `exit 1` with no message.
`-q` is deliberately off — quiet mode suppresses the syntax errors that are the
only thing distinguishing "this is a scan" from "these bytes arrived truncated".

---

## Open, and deliberately not guessed at

Three things were found by measurement during Phases 2–3 and are **not** fixed,
because each needs its own measurement rather than a plausible-looking constant
— which is exactly how the ADR-0011 bug shipped in the first place.

All three items listed here previously have been closed, two of them by
**measuring and then deciding not to make the change**:

1. ~~**PDF extraction**~~ — done, [ADR-0012](../architecture/adr/0012-pdf-extraction-via-subprocess.md).
2. ~~**`skillsFullEvidence = 2`**~~ — **hypothesis measured and rejected.** Median
   score is flat across stated depth (43.8 / 41.7 / 43.3 / 45.6), so ADR-0011
   removed the inflation. `strong` does concentrate at the threshold, but 11 of
   the top 12 there are genuine engineering roles the reader matches well —
   tightening it would delete eleven right answers to remove one wrong one.
   [A-00d](../research/evidence-ledger.md#a-00d).
3. ~~**ADR-0009's successor on freshness**~~ — [ADR-0013](../architecture/adr/0013-freshness-weighting-re-measured.md).
   Re-measured after the abstention recalibration: skills now outranks freshness
   **0.428 to 0.241** for a complete profile and the condition un-fires. The
   weight was **not** changed.
4. ~~**Orphaned score rows**~~ — the maintenance sweep now collects scores for
   non-live postings, bounded per run. 38,973 rows draining.
5. ~~**Whether the ingestion claims in this document were true**~~ — checked on
   2026-08-24. Several were not; [§5](#5-a-verification-pass-and-what-it-found).

### What remains, and it is one thing

**Nothing in the model asks whether a posting is in the reader's FIELD.**

"Product Support Specialist" scores 84.6 on *2 of 2 must-haves* because it names
two technologies the reader genuinely has. No amount of evidence weighting fixes
that — the evidence is real. `users` carries `current_title` and `target_title`
and the scorer uses neither.

Related, from ADR-0013: **a thin profile is still ranked substantially by
recency** (skills 0.227 vs freshness 0.272 for a 3-skill profile), and it lands
hardest on new users, who can do least about it.

Both are the same missing capability — the model has no notion of *what kind of
job this is* — and both need a corpus of real profiles before a constant is
chosen. Picking one now would repeat exactly the mistake
[ADR-0011](../architecture/adr/0011-abstention-credit-calibration.md) documents
at length. That is the honest stopping point.

---

## Phase 4 — Coverage and scale — RESOLVED

Every item here carried a stated reason to wait. Each was **measured** rather
than assumed, which closed most of them without writing code — the point of
having numeric triggers in the first place.

### Infrastructure: no trigger has fired, nothing was built

Measured 2026-08-17 against the live stack:

| Proposed | Trigger | Actual | Distance |
|---|---|---|---|
| Separate vector store | `count(posting_embeddings) > 20M` | 8,985 postings | **0.04%** of the trigger |
| Redis for facets | `>4 api replicas` **and** `>5% DB CPU` | 1 replica | not close |
| PgBouncer | `db_connections_in_use > 400` sustained | 20 | **5%** of the trigger |

Not built, and that is the decision rather than a deferral. `caching-and-storage.md`
says adding infrastructure without the trigger firing requires an ADR; none is
warranted because none has fired.

**`sqlc` stays unconverted.** Recorded as deliberate debt in
[consistency-and-drift](consistency-and-drift.md): the hand-written pgx queries
work, and converting them is churn with no functional gain.

### SmartRecruiters — built, on capability rather than breadth

The roadmap's own test for a new source was *"more sources add breadth, not
capability"*. This one passes it, measured:

| Vendor | Extracted skills classified as must/nice |
|---|---|
| Greenhouse | 26.0% |
| Ashby | 24.2% |
| **SmartRecruiters** | **54.1%** |

SmartRecruiters splits a posting into named sections and one is
`qualifications`, which the extractor already treats as a requirements heading.
Every other source hands us one blob and depends on the employer happening to
write a heading we recognise — which is why 73.7% of extractions land as merely
`mentioned`. These postings produce **real must-haves**.
[A-00f](../research/evidence-ledger.md#a-00f), [ADR-0007](../architecture/adr/0007-resume-parsing-local-first.md)'s
sibling problem solved from the source side.

It is also the first two-phase adapter: the list has no descriptions, so bodies
are a second request. Bounded at 250 details per poll and skipped entirely when
the list hashes identically, so a steady-state board costs one request and a
4,800-posting board backfills over several polls rather than arriving as a burst
against someone else's API.

> **This paragraph was false when written, and stayed false for a week.** The
> backfill had nowhere to record its progress, so it re-read the same 250
> postings on every poll and 77.8% of the vendor's live postings never got a
> description. The board was also being truncated at 2,000 of 4,770 by
> `maxPages`. Both are fixed and measured in [§5.1](#51-the-smartrecruiters-detail-sweep-never-advanced)
> and [§5.2](#52-maxpages-truncated-the-largest-board-to-41-of-itself); the
> capability claim above still holds, but [A-00f](../research/evidence-ledger.md#a-00f)'s
> 54.1% was measured on the 344 postings that happened to have bodies and is due
> a re-measurement now that the rest have them.

**A trap worth recording:** the company identifier is case-sensitive and is not
the domain. `bosch` returns an empty board; `BoschGroup` returns 4,803. Five
candidate boards initially looked dead for that reason alone, and
SmartRecruiters would have been written off as a vendor.

### India coverage — closed

The thinnest market in the corpus, and the one this project's own user is in.

| | Before | After |
|---|---|---|
| Live postings in India | 278 | **631** |
| Total corpus | 6,770 | **8,985** |
| Companies | 61 | **65** |

Swiggy (46 postings, all India) is the first Indian-headquartered company in the
corpus. Bosch contributes 372 more. A Darwinbox or Keka adapter is still the
route to real depth here, and is now the only remaining coverage gap.

### A real bug this surfaced

`QueueBulk` exists so that *"a full rescore must never starve live scoring"* —
two workers against the live queue's ten. **The stale-score sweep was bypassing
it**: it enqueues `ScorePostingArgs`, whose own `InsertOpts` default to
`QueueScore`, so a version bump flooded precisely the queue the split was built
to protect.

Observed while ingesting 2,200 new postings: 3,553 jobs backed up on the live
queue and interactive dashboard latency went from ~4s to ~15s. The sweep now
overrides the queue explicitly. A newly-ingested posting still goes to
`QueueScore`, because that one genuinely is time-sensitive — it is the
difference between appearing in the feed now and in an hour.

### `make drift-check` — the last `NOT_YET` stub, now real

Rebuilds the schema from `migrations/` in a throwaway database and diffs
**catalogue introspection** of both — 333 facts covering columns, constraints,
indexes and enums.

Not Atlas, which `consistency-and-drift.md` named. Atlas would add a tool to the
image *and* a second declaration of the schema to keep in step: one more pair of
things that can drift, in order to detect drift.

A `pg_dump` text diff was tried first and abandoned for two concrete reasons —
pg_dump emits multi-line statements, so filtering unwanted objects line by line
leaves orphaned fragments; and pg_dump 17 stamps every run with a random
`\restrict` token, so two dumps of the *same* database differ.

Verified by planting an out-of-band `ALTER TABLE companies ADD COLUMN`: the
check failed and named the exact column.

---

## 5. A verification pass, and what it found

Run 2026-08-24 against the live stack, checking the claims in this document
rather than re-reading the code that produced them. `make check`, `make
test-e2e`, `make ui-audit` and `make drift-check` were all green when it
started. **Seven defects, none of which any gate could see.**

The common shape: every one of them was a claim written down and never
measured, and in three cases the instrument that should have caught it was
looking at a population the defect could not appear in.

### 5.1 The SmartRecruiters detail sweep never advanced

77.8% of live SmartRecruiters postings — 1,861 of 2,393 — held **no description
at all** ([A-00g](../research/evidence-ledger.md#a-00g)). They scored as
abstentions, and the corpus-wide zero-skill rate had gone from the 16.1% this
document reports to 32.3%.

Phase 4 claims a 4,800-posting board "backfills over several polls". It does
not. `fillDescriptions` fetched `jobs[0:250]` with nowhere to record progress,
so every poll re-read the same first 250 and the rest of the board would never
have got a body at any polling frequency, ever. The comment above the cap
asserted a mechanism — *"ingest is incremental, the store already knows which
ExternalIDs it holds"* — that nothing implemented.

Fixed with `sources.detail_cursor` (migration 0015, expand-only). The
unchanged-board short-circuit now fires only **after** a full sweep, because
until then an identical board is not a reason to skip: the postings past the
cursor still have no body. Verified against the live board: 0 → 250 → 500 → 750.

A second bug surfaced in the first live run — two boards advanced 250 places
having filled *nothing*, because the database pool was saturated and every
detail fetch was cancelled. The cursor now advances over a window only if it
either filled something or was not interrupted, which distinguishes "these
postings have no body" from "we were cut off".

### 5.2 `maxPages` truncated the largest board to 41% of itself

`maxPages = 20` — 2,000 postings — commented as *"comfortably more than any
board we watch"*. BoschGroup returns **4,770**. We had 1,713 of them, and
`ReconcileAbsent` read the missing two thirds as postings that had closed.

Raised to 60 with the measurement in the comment and a test that fails when a
watched board outgrows it. **The corpus went from 8,985 to 15,379 live postings
and India coverage from 631 to 1,114 — a 71% and a 77% increase, with no new
source added.** The single most valuable change in this pass, and it was one
constant.

### 5.3 The coverage instrument could not see either of them

`make coverage` reported 13.7% zero-skill while the corpus stood at 32.3%
([A-00h](../research/evidence-ledger.md#a-00h)). Its query was
`WHERE length(description_text) > 400 ORDER BY id LIMIT 1500`:

- the `> 400` filter excluded every posting with no body — precisely the
  population that had broken;
- `ORDER BY id LIMIT 1500` is not a sample. It is the oldest 1,500 rows, which
  meant the first board ever ingested and nothing added since.

It now reports the corpus total, the share with no body to read, and the
extraction rate over a sample ordered by `md5(id)`. **"We could not read it" and
"we read it and found nothing" are separate lines, because they have separate
fixes.** Projected corpus-wide zero-skill share: 44.6%.

This is the third time in this project a number has been believed because it was
printed by something we wrote. It is the reason ADR-0011 exists.

### 5.4 Ashby published hourly rates and we called them annual

Three live postings said "$30–45" and "$62.98–86.54" **per year**
([A-00j](../research/evidence-ledger.md#a-00j)). One company published the same
band twice — `hour` where we parsed it from prose, `year` where we read the
vendor's own structured field — so the corpus contained its own contradiction.

Two stacked defects: the JSON key `interval` was bound to a field named
`InterviewType`, the field named `Interval` read a key Ashby does not send, and
`normaliseInterval` defaulted anything unrecognised to `"year"`. **The default
is what hid it** — every wrong answer looked like an ordinary salary. It now
returns empty for an unrecognised period, which stores as NULL, and the card
renders a band with no period rather than the wrong one.

This is a direct breach of the product's central rule, in production, for three
months.

### 5.5 The Ashby adapter had no tests, and `capture-source` could not make one

CLAUDE.md requires golden-file tests for every source adapter.
`internal/source/ashby/` contained one file: `adapter.go`. 2,493 live postings —
a quarter of the corpus — parsed by code with no test of any kind, which is why
5.4 survived.

The reason it had none is worth recording: `make capture-source` takes a
`VENDOR` argument and **hard-codes the Greenhouse URL**, so capturing an Ashby
fixture wrote a Greenhouse response into the Ashby testdata directory. The flag
chose the output folder and nothing else. Fixed with a URL per vendor; then
eleven tests written against real captured boards, covering the compensation
shapes, unlisted postings, secondary locations, and a malformed body.

### 5.6 Scoring was one database round trip per user

Not found by looking for it — the end-to-end suite started failing 13 of 43
tests, with `GET /v1/me/dashboard` returning 500 after a ten-second deadline,
once §5.2 tripled the corpus.

`upsertScore` ran one `Exec` per user inside the fan-out loop, so scoring a
posting cost **32.09 seconds** for 62 users
([A-00i](../research/evidence-ledger.md#a-00i)) — linear in network round trips,
in the one workload whose entire shape is fan-out. 14,700 jobs backed up on the
interactive queue and the dashboard timed out behind them.

Both fan-out directions now write through one batched statement:

| | Before | After |
|---|---|---|
| One posting, 62 users | 32.09 s | **0.49 s** |
| Score jobs per minute | ~19 | **~1,806** |
| End-to-end suite | 13 failing, 8.5 min | **43 passing, 1.9 min** |

`jsonb_to_recordset` rather than `unnest`, because `missing_skills` is an array
per row and Postgres arrays are rectangular.

**The queue split was not the problem.** The roadmap's earlier fix — routing the
stale sweep to `score_bulk` — was correct and held. What starved the queue this
time was a genuine ingest of 12,000 postings at the honest per-job cost, and the
honest per-job cost was 65× what it needed to be.

### 5.7 The dashboard read 15,000 rows to print four numbers

Surfaced by §5.6's fix, not fixed by it. With the scoring backlog gone,
`GET /v1/me/dashboard` still returned 500 at its ten-second deadline whenever
ingestion was active ([A-00k](../research/evidence-ledger.md#a-00k)).

Four `count(*) FILTER (...)` aggregates over one join read **every one of a
user's ~15,000 score rows** to evaluate filters that match a few hundred. A
covering index took it to 68 ms — and then it decayed back to 12,149 heap
fetches and 13.7 seconds, because `user_job_scores` is rewritten wholesale on
every rescore, so its visibility map is never clean enough for an index-only
scan to hold. **An index-only scan is the wrong instrument for a table that is
continuously rewritten**, and the first fix was reaching for it.

Two changes, both measured:

- **Split the query into three**, each reading only what it needs — bands by
  range scan (26 ms), new-today driven from the indexed freshness side (655 ms),
  total by a plain count (7 ms). Still one batch, so still one round trip.
  3,565 ms → ~690 ms.
- **Autovacuum on this table's own churn rate.** The same count measured
  1,598 ms at 283,138 dead tuples and 285 ms straight after a vacuum — a 5.6x
  swing with nothing changed but how recently the table was cleaned. Postgres'
  default lets a 1.2M-row table reach 240,000 dead tuples first;
  `autovacuum_vacuum_scale_factor` is 0.02 here now (migration 0017).

The total-scored count stays an honest `count(*)`. It is genuinely proportional
to the corpus and there is no cheaper exact answer — an estimate on a stat tile
labelled "Scored for you" would be a fabricated number.

**Result: three consecutive clean end-to-end runs — 43 passing in 50s, 1.1 min
and 2.0 min — under active ingestion, where the same suite had been failing 13
of 43.**

### What this pass says about the gates

Every defect above sat behind a green `make check`. Three were invisible because
the instrument measuring them had been scoped to a population that excluded the
failure; one because the code had no test at all; one because a comment
described a mechanism nobody had implemented.

`make ui-audit` also failed twice during this pass for reasons unrelated to
rendering — it slept a flat 700 ms per page and reported "0 h1" on pages that
render one, under load. A check that fails for reasons other than what it
measures is a check people re-run instead of read. It waits on the hydration
signal now.

**A green suite is evidence that the things we thought to check are still true.
It is not evidence that the product works.** The measurements in this section
all came from asking the database what was actually in it.

---

## The standards this work is held to

These are not new. They are what the whole project has been held to, written down
so the last phases do not quietly drop them.

**Never fabricate a number.** `null` is never rendered as `0`. An undisclosed
salary says undisclosed; an unscored posting shows `—`, not zero; an estimated
date is labelled an estimate. This rule caught three separate defects during the
design sessions and it is the product's whole differentiator.

**Every test must fail before its fix.** Every regression test written in this
project was confirmed to fail against the buggy code first. A test that has never
failed is a test that proves nothing.

**Measure, do not reason.** The three worst bugs found so far — a sales role
scoring 98%, `go to market` parsing as Golang, a feed doing a sequential scan —
were all invisible to fixtures and obvious against live data. And one performance
"problem" turned out to be the measurement itself: host-side `curl` reported
171 ms where the server log said 41 ms.

**Comments explain why.** Every non-obvious decision in this codebase carries the
reason it was made, including the ones that were wrong first.

**Verify claims, including our own.** The teal failure survived an audit that was
explicitly checking for it. Contrast gets computed. Counts get cross-checked
against the query behind them. Assertions in a summary are not evidence.

---

## Sequencing, and what must not be reordered

```
Phase 1  ──►  Phase 2  ──►  Phase 3  ──►  Phase 4
correctness   frontend      resume         coverage
debts         rebuild       parsing        and scale
```

- **Phase 1 before Phase 2.** The teal token and the ghosting behaviour would
  both be inherited by the rebuild and then have to be undone in it.
- **Phase 2 as one pass.** The held list is nine items that share components. Any
  two of them shipped separately means building the same card twice.
- **Phase 3 after Phase 2.** Resume parsing surfaces in the profile and
  onboarding, both of which Phase 2 rebuilds. Doing it first means designing
  against a UI that is about to be replaced.
- **Phase 4 last, and only against a trigger.** Every item in it is a scale
  answer, and we do not have the scale problem yet.

## What "done" means

The product is complete when a stranger can land on it, understand what it is,
create an account, describe themselves in four steps, see roles ranked by a score
that shows its working, apply through a link that lands in a real requisition
queue, track what happens next, and be told the truth at every step about what we
know and what we do not.

Everything above is in service of that sentence. Nothing that does not serve it
belongs in the plan.
