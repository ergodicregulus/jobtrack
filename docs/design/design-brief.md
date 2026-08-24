# JobTrack — design brief

## How to use this document (not part of the prompt)

**The brief starts at the horizontal rule below "Session 1". Everything above it
is instructions for you, not for Claude Design.**

The brief is ~5,800 words. Paste it **in full, once**, at the start of a session
— Claude Design needs the data shapes and the principles or it will invent
fields and break the accuracy rules. But **do not ask for every page in one
response.** A single request for ten screens produces ten shallow screens. Work
through it in sessions, in this order, because each one depends on the last.

### Session 1 — the foundation

Paste the entire brief, then add:

> Before designing any page: read the whole brief, then produce **only** the
> design tokens and the core components. Tokens as a `:root` block — colour for
> light and dark, the type scale with its letter-spacing pairs, spacing, radii,
> elevation, motion. Then these components, each in every state (default, hover,
> focus-visible, active, disabled, loading, empty, error): button, chip
> (including the four score bands and the have/lack skill variants), form field,
> panel, stat tile, and **the job card in compact, default and expanded forms**.
> Deliver working HTML + CSS. Do not design pages yet.

The job card is the atom of this product — it appears on four different screens.
Getting it right first is what makes everything after it cohere.

### Session 2 — the two screens that matter most

Session 1 delivered the tokens and components and was accepted. Paste the block
below (from "Three corrections" to the end of the session) as the next message.

---

**Three corrections to Session 1, before anything new.**

1. **The worked example does not add up.** In the expanded card the components
   sum to 94 (37+20+15+12+10) but the chip reads 91. In a product whose entire
   claim is that it shows its working, the worked example must work. Fix the
   figures and check every other number you display against its own breakdown.
2. **`--ink-3` `#807a73` is 4.24:1 on white and fails AA**, and you use it at
   11–12px for eyebrow labels and the "Not disclosed" text. This is the same
   failure you correctly caught for amber. Use `#78716c` (4.80:1) or darker.
   Where `--ink-3` is a 1px ring rather than text it is fine at 4.24 — non-text
   needs only 3:1 — so check each usage rather than replacing globally.
3. **Apply/Save drift to the far right edge** on a wide row and stop reading as
   part of the posting. Cap the action column so the actions stay near the
   content they act on.

**Everything else from Session 1 stands.** The `−` prefix on gap chips, the
de-coloured matched skills, the hollow bar for un-judgeable components, the
band-name-before-number, `pointer: coarse`, and density affecting padding only —
all correct. Do not revisit them.

---

**Correction to the chrome — the theme control is in the wrong place.**

Session 1 put THEME and DENSITY in the persistent top bar. Move both to
**Settings** (§7.10), reachable from the account menu.

The reasoning is a rule worth applying everywhere else too: **persistent real
estate is earned by frequency × value.** A theme control is used approximately
once per user, ever — and with a `system` default, most users never touch it at
all. It cannot justify occupying space on every page forever. This is what
Claude.ai, Slack, Linear and GitHub all do, and they converged on it for the same
reason.

What the header should carry instead:
- Wordmark, doubling as the home link
- Primary navigation — Dashboard · Jobs · Tracker
- **Global search** if you can justify it; it is the one thing that genuinely
  earns persistent space in a product about finding something
- Account menu → Profile · Settings · Sign out

Apply the same test to everything else you are tempted to put in the header, and
say what you rejected.

---

**Now design the jobs feed (§7.5) and the filter system (§7.6)** at all five
widths in §4.1, reusing the Session 1 tokens and job card unchanged.

The **filter panel at 390px is the hardest problem in the product.** Treat it as
the main event. A chip system that works beautifully at 1440px and becomes a
scrolling wall of pills on a phone has failed the audience that uses this on a
commute.

Additional requirements for this session:

**"New since you were last here."** The product knows when the user last visited
(`users.last_active_at`). Mark those rows. This is the honest version of a
retention hook: it answers the actual question someone opens the product with,
using data we genuinely have, and it rewards returning without manufacturing
urgency or streaks. Design the marker, and design what happens when *everything*
is new (a first visit) and when *nothing* is.

**Sort, and its relationship to filtering.** The feed can sort by match, by date
or by compensation. Signed-in users default to match; signed-out users cannot
sort by match at all. Show how sort and filter coexist without competing.

**The signed-out feed.** No scores, no Save. Decide what occupies the space where
the score chip would be, and how the product invites an account **at the moment
of intent** — clicking Save or Apply — without losing the click or nagging.

**Density in practice.** You defined compact and comfortable in Session 1. Show
the feed in both. Compact is what a user scanning forty roles will choose, so it
should be the more convincing of the two.

**Keyboard navigation.** `j`/`k` to move, `Enter` to open, `s` to save. Show the
focus treatment for the active row — this audience expects it and it is part of
what makes the product feel like an instrument.

---

**Two constraints on how you design this, both about longevity.**

**Design for growth, not for today's counts.** The navigation has three items now
and will have five. The filter has six dimensions now and will have eight. The
job card has the fields listed in §6 and will gain more. A layout that only works
at today's numbers will be redesigned within a year, and this system is meant to
last. Show what happens when each of those grows, and where the ceiling is.

**Everything must survive a bad string.** Real data is hostile: a 59-character
title, a four-office location, a company with a one-word name, a posting with
zero skills extracted, a salary in a currency the user does not use. Show the
feed with the worst realistic row, not the best one. If a design only looks good
with tidy data, it is the wrong design — and the data in §10's stress-test block
is genuinely representative.

### Session 3 — the dashboard

Session 2 was accepted with no corrections. Paste the block below as the next
message.

---

**Session 2 is accepted as delivered.** All three corrections landed — the
breakdown now sums to 91 and matches its chip, `--ink-3` is `#78716c`, and the
action column is capped. The filter rail, the 390px sheet with its
outcome-stating button, the "new since your last visit" divider and the growth
ceilings are all right. Nothing to revisit.

Two things from your Session 2 reasoning that Session 3 must stay consistent
with:

- You rejected a notifications bell because "we have nothing urgent enough to
  interrupt for". The dashboard has a **Needs your attention** widget for
  applications that have gone quiet. Make those two positions agree — either
  that urgency genuinely lives only on the dashboard, or say what would change
  the answer.
- You designed "new since your last visit" on the feed. The dashboard has a
  **New today** tile. Decide whether these are the same idea in two places or
  two different ideas, and make the difference legible. Two subtly different
  definitions of "new" in one product is worse than either alone.

---

**Now design the dashboard (§7.4)** — the page every returning user lands on. It
must answer, in this order: *is anything new for me · what needs a reply today ·
how am I doing · what is the market doing.*

**A constraint you need before you start: there is no band filter.** The feed
API filters on country, mode, years, salary, posted-within, skills, source and
free text — and it **sorts** by match, date or pay, but it cannot filter to
"strong matches only". So a "Strong matches — 40" tile has no filtered view to
land on today.

Decide this deliberately and say which you chose:
1. Tiles link to `sort=match` and the tile copy promises ordering, not filtering
2. Tiles carry a band filter, which we will add server-side — it is one
   predicate, so ask for it if the design is better with it
3. Something else

What must not happen is a tile that implies a filter the feed cannot honour, or
that lands the user on a set they cannot explain.

**Required on this screen:**

- **Stat tiles**, clickable, arriving somewhere legible — see the constraint
  above. Whatever they link to, the destination must show *why* it contains what
  it contains, using the filter chips from Session 2.
- **Your best matches** — the compact job card from Session 1, unchanged.
- **Needs your attention** — applications with no movement for 7+ days. It earns
  the top of its column, but only when it has something to say.
- **Pipeline** — counts per stage, each opening the tracker at that stage. Every
  stage shown, including the empty ones, so it still reads as a funnel.
- **Activity heatmap** — roughly 12 weeks of daily job-search activity from
  `application_events.occurred_at`. **A record, not a target**: no streaks, no
  "don't break the chain", no implication that more applications is better. It
  exists so someone can see their own rhythm during a hard few months.
- **Profile strength** — what is missing and *what filling it in would change
  about matching*, not a completeness percentage on its own. Hidden once
  complete.
- **Market context** — live postings, companies, added this week, share remote.
  Background, not a task.

**Three specific problems to solve, all of which the current build gets wrong:**

**1. The right column runs out.** Once profile strength is complete and hidden,
and the pipeline is empty, the right column ends around 40% of the page height
while the matches list continues, leaving a void. Fix the composition so it holds
at every combination of populated and empty widgets — that is eight or so states,
not two.

**2. Nothing is urgent for most users, most days.** The honest common case is: a
few new matches, nothing needing a reply, a pipeline that has not moved. Design
*that* page — not the demo where every widget is full. A dashboard that only
looks composed when busy is the wrong dashboard, because busy is the exception.

**3. The first-run dashboard is a separate deliverable.** A brand-new account has
zero applications, zero saved jobs, an empty heatmap and possibly no scores yet
(scoring runs in the background and takes a minute or two). Every widget shows
its empty state simultaneously, on the one screen that decides whether the user
stays. Design it as a screen in its own right — not as the populated design with
placeholders — and make it a coherent invitation rather than a grid of apologies.

**Also specify:** the greeting (time-aware, using the user's first name), what
the page looks like the moment after onboarding completes, and how the dashboard
reflects an action taken elsewhere — saving a job on the feed should visibly
change the pipeline here.

**Widths:** all five in §4.1. On a phone the two columns become one — specify the
resulting **order**, since "needs your attention" must not fall below the fold on
the screen where it matters most.

### Session 4 — job detail, profile, tracker

Settings arrived early, inside Session 3, and is done. Session 4 takes the three
pages that complete the core loop; Session 5 takes the acquisition path. Paste
the block below as the next message.

---

**Session 3 is accepted as delivered.** The banded layout that removes the
right-hand rail, `—` with "scoring in progress" instead of a zero, "one of them
is about the world; the other is about you", and the three previewable states
are all right.

**Your band-filter request was granted and is already live.** `GET
/v1/jobs?band=strong,plausible` now works: comma-separated, all four bands, and
the counts agree — the dashboard reports 40 strong fits and `band=strong`
returns 40 rows. Two behaviours you should design around:

- **An anonymous caller gets a 400**, not a silently unfiltered feed, with the
  message *"Sign in to filter by how well a role fits you"*. That is your cue for
  the signed-out prompt.
- **Unscored postings are excluded**, since a posting with no band cannot match
  one. Expect the count to drop, and keep it visible next to the filters.

---

**Now design the job detail page (§7.7), the profile page (§7.8) and the tracker
(§7.9)**, at all five widths in §4.1, reusing everything from Sessions 1–3.

---

**Job detail — the signature moment.**

Session 1 sketched the breakdown inside a card. This is where it gets room. It
must carry: the full description, the five-component breakdown, compensation with
its provenance, the AI-screening disclosure where the vendor gives one, and
Apply / Save.

**The reason strings are generated, not written.** The scorer produces them from
real data, so design for what it actually emits. These are verbatim from the
running system:

```
skills        "7 of 9 must-haves. Missing: kafka, terraform"
              "0 of 3 preferred skills — this posting says little about what it needs"
experience    "Asks for 8+ years; you have 7 — often still worth applying"
              "Targets up to 5 years; you have 7"
location      "Remote — matches your preference"
compensation  "Salary not disclosed"
              "Salary is in a different currency"
              "Below your target"
freshness     "Posted today — you are early"
              "Posted over two weeks ago"
```

Note the range: eleven characters to seventy. If a layout needs them all to be a
similar length, it is the wrong layout. **If you want different phrasing, say so
and we will change the generator** — the strings are ours, not the vendor's.

Also specify:
- **The description is long, untrusted HTML** from the employer — headings,
  lists, occasional tables, sometimes several thousand words. Give it a readable
  measure and decide what happens at the top and bottom of it.
- **An unscored posting**, where there is no breakdown to show at all.
- **A signed-out viewer**, who gets the posting but no score.
- **Back to the feed**, preserving scroll position. This is the most-used
  navigation in the product.
- **`parse_confidence < 0.5`** — we understood less than half the posting. Say so
  here more fully than the one-line feed treatment does.

---

**Profile — where match quality is actually controlled.**

Sections: about you · skills · what counts as a fit. A sticky save bar, and it
must say that saving re-scores every live posting — otherwise the feed appears to
change by itself and reads as a bug.

**The skills editor is the highest-leverage component in the product** (§8).
Skills are 40 points of every score, the largest single component, so this one
control determines the quality of every match the user ever sees. It needs
typeahead over a known vocabulary, one-tap suggestions, chips that remove, and it
must degrade to a comma-separated text field with no JavaScript.

Two things to resolve:
- **Zero is a real answer** for years of experience. A new graduate has zero, and
  the field must not read as unanswered when they say so.
- The profile page and onboarding step 3 use the same editor at different
  moments — one is a first pass, one is maintenance. Show both if they differ.

---

**Tracker — the half of the loop that decides whether any of this was worth it.**

A funnel board: Saved · Applied · Screening · Interviewing · Offer · Closed, with
terminal outcomes sharing the last column. **Advancing a status must be one click
from the board** — a tracker that needs a detail view to change a status does not
get updated, and an un-updated tracker is worthless.

- **Stage counts must match the dashboard's pipeline exactly.** They are the same
  numbers in two places, and a user who sees them disagree stops trusting both.
- **Ghosting**: no movement for 21 days is marked automatically. The copy should
  be matter-of-fact — in this market it is the norm, not a personal failure.
- **Phone**: you already chose single column plus a stage filter. Build it with
  the filter chips from Session 2 rather than a new control.
- **Empty**: someone with nothing tracked yet, and someone whose only rows are in
  Closed. The second is the harder and more common one, and it is the moment a
  person most needs the product not to editorialise.

---

**One more thing: a copy bug you should know is already fixed.** The experience
string used to emit `"Targets up to 1 years"`. It now reads `"1 year"` at the
boundary, guarded by a test. If you spot other places where generated text will
read wrong at zero, one or an exact match, list them and we will correct them at
the source rather than in the layout.

### Session 5 — the acquisition path

The last four screens, in one session because they share a single job: getting a
stranger to a populated dashboard. Paste the block below as the next message.

---

**Session 4 is accepted as delivered.** "These are the same numbers as the
dashboard pipeline — one list, counted once", the four tracker states including
*Only closed*, "ghosting is stated, not styled", and "zero years is a filled
field, not a blank one" with its *I am starting out* chip — all right. The stage
chips reused from the feed rather than reinvented is exactly the coherence §2.6
asks for.

---

**Now design onboarding (§7.3), sign in / sign up (§7.2), the system states
(§7.11), and the landing page (§7.1) — in that order.** Landing goes last on
purpose: its hero shows the real product, and designing it before the other
screens exist means inventing a card you then have to reconcile.

---

**Onboarding — four steps, and the highest drop-off point in the product.**

Steps: name · experience · skills · preferences. Progress named ("Step 2 of 4"),
not just a bar. Each step saves to the server, so a closed tab loses nothing. A
skip is offered from step 3 onward.

- Step 3 uses the skills editor you already built, weighted for a first pass —
  you established that distinction in Session 4; hold it.
- **It lands on the first-run dashboard from Session 3**, with scoring in
  progress. Design the seam so those two screens read as one moment.
- Title each step by what the user gets, not what we collect, and say briefly why
  a field matters — "Skills are 40 points of every score" earns the answer.
- Design **leaving and coming back**: someone who abandons at step 2 and returns
  the next day.

**Sign in / sign up.**

Two facts about the real system to design against:

- **Registration signs the user in immediately**, with the email left
  *unverified*. Verification gates outbound email, not access. Nobody is bounced
  to a sign-in page for an account they just created.
- **The password rule is length only** — 12 characters minimum, no composition
  rules. NIST SP 800-63B advises against them. The copy should reassure, and
  `autocomplete` on both fields is a WCAG 3.3.8 requirement here, not a
  convenience.

Also: the signed-out prompt at the moment of intent. Clicking Save or Apply
signed-out, and the feed's band filter returning *"Sign in to filter by how well
a role fits you"*, are the two places an account is genuinely worth asking for.

**System states.** 404 · the API unreachable · a session that expired mid-form ·
a posting we understood less than half of. The feed must degrade **visibly**: a
blank list reads as "there are no jobs", which is a different and untrue
statement.

---

**The landing page — and a warning about it specifically.**

This is the one screen whose job is persuasion rather than work, and it is
therefore the one most likely to come back conventional. Every instinct trained
on marketing pages pulls toward: a centred hero over a gradient, three feature
columns with icons, a testimonial nobody said, a logo bar, a repeated CTA, and
adjectives standing in for evidence.

**None of that belongs here.** §2.5 says this must not look like a job board, and
a generic SaaS landing page is the same failure wearing better clothes. The
audience is engineers who are fluent in that vocabulary and discount it on sight.

What should carry the page instead:

- **The real job card as the hero.** The actual component, with a real score, a
  real breakdown and a visible missing skill. Seeing the product beats describing
  it, and no competitor can show this because none of them compute it.
- **Checkable numbers, not claims.** "6,677 live postings from 61 companies,
  refreshed continuously" — a figure someone could verify — never "thousands of
  opportunities".
- **The difference, stated plainly.** Postings come from the employer's own
  system so the apply link works; scores name what fit and what did not;
  undisclosed stays undisclosed.
- **Two honest calls to action** — create an account, and browse without one.
  Discovery genuinely does not require signing up, and saying so is more
  persuasive than a wall.

Nothing on this page may imply data we do not have. No fabricated testimonial, no
invented company logo, no "trusted by" without anyone to name.

---

**A question to answer in your notes, not with a screen:** having now designed
every page, what in this system would you cut? Ask it of your own work rather
than ours — the fastest route to restraint is naming what is decorative rather
than informative, and you are better placed to see it than we are.

### Session 6 — the audit, and the handoff

The last session. Paste the block below as the next message.

---

**Session 5 is accepted as delivered.** The landing page avoided every failure it
was warned about — no gradient hero, no icon columns, no testimonials, no logo
bar — and "that rose chip is the point … every other product in this category
would show you 84% and let you find that out in the rejection email" is the
product's argument in one sentence against a live component. Onboarding's
"Saved as you go" and its returning-after-leaving preview are right.

---

**Your cut list — five answers, with reasons.**

You asked what should be cut. Four of the five are accepted in some form. Where
we disagree, the reason is stated so you can argue back.

**1. The market strip on the dashboard — TRIM, not cut.**
You are right that "6,677 live postings" restates something the user already
accepted. But "added this week" and "share fully remote" are market *movement*,
not corpus size, and reading the market is a stated purpose of this product. Cut
it from four figures to those two. It stays at the foot of the page where it
costs nothing above the fold.

**2. The "Scored for you" tile — ACCEPTED, with one exception.**
You are right: it reports the size of the corpus, its destination is the feed's
default, and three tiles that each narrow the set beat four where one does not.
Remove it from the steady state.

Keep it in **first run only**, where it is not a statistic but a progress
indicator — "1,204 scored of 6,677" answers "is it working yet", which is the
only question a new user has on that screen. It disappears when scoring
completes. That makes it a state, not a tile.

**3. The compact job card's separate existence — ACCEPTED IN FULL.**
Make the feed row at compact density *be* the dashboard row and delete the second
treatment. This is §2.6 applied to your own work, and it is the cheapest of the
five to act on.

**4. Density as a user-facing setting — ACCEPTED, and the reasoning goes
further than you put it.**
We checked the running code. Density changes `--row-gap` and `--card-pad` and
nothing else — not type size, not target size. It was documented in our codebase
as "an accessibility preference for anyone with a motor or vision impairment",
and that claim is **false**: it does nothing for either. You caught a comment
that had been wrong since it was written.

So: **ship one density** — the current compact — and drop the setting. Do not
make it adaptive: an interface that re-spaces itself after forty rows is a layout
changing under the reader, which is worse than either fixed option. If we want a
genuine accessibility affordance it has to scale type and targets, and browser
zoom already does that better than we would.

**5. The score's precision — DECLINED, and here is why.**
The concern is right and the remedy is not. 91 versus 88 can be one freshness
hour apart, so the difference is not reliably meaningful — agreed.

But the score is the **sort key**. Hiding a number we are actively ordering by is
less transparent, not more: the user would see a ranked list with no visible
reason for the ranking. And §3 requires that every score shows its working; a
band alone is a claim without evidence, which is the "97% match" pattern with the
number removed rather than the problem solved.

The honest arrangement is the one you already built: **the band leads and makes
the claim, the number follows as evidence, and the breakdown is one click away.**
Hold that, and never show a decimal. If you still think it should go, make the
case against the sort-key argument specifically.

---

**Now the audit.** Review everything from Sessions 1–5 as one system and report —
in prose, not new screens — on:

- **Convention drift.** Where does the same idea look or behave differently
  between pages? Where does a colour, a chip or a rail mean two things?
- **Dead numbers.** Every figure in the product should lead somewhere or explain
  something. List any that do neither.
- **Component sprawl.** How many genuinely distinct components exist, versus how
  many nearly-identical ones? Name every pair that should be one.
- **Copy consistency.** The same concept must use the same word everywhere —
  "Not disclosed" versus "Not stated", "gone quiet" versus "ghosted", "Strong
  fit" versus "Strong match". List every term that varies and pick the survivor.
- **Contrast and targets.** Any text below 4.5:1 in either theme, and any target
  below 44px on a coarse pointer.
- **States you did not design.** Anything with no empty, loading or error
  treatment.
- **The token set.** Any token used once, any two that are indistinguishable, and
  anything a component still hard-codes.

Then apply the four accepted cuts and produce the **handoff**: the final token
block, the component set with every state, and each page — as working HTML and
CSS we can port. This is what gets implemented, so completeness matters more
here than novelty.

**Two questions to answer in prose at the end:**

1. **What would you need to see in six months to know this was the wrong
   design?** Name the observable, not the feeling.
2. **What did we ask for that you think is a mistake?** You have followed this
   brief for five sessions; you are better placed than anyone to say where it is
   wrong. Nothing is out of scope, including the principles in §3.

---

# ── Session 1: paste from here down ──

## 1. What you are designing

**JobTrack** is a job-search instrument for software engineers. Not a job board —
an instrument. It reads job postings directly from companies' own applicant
tracking systems (Greenhouse, Ashby), scores each one against the user's profile
with a model that shows its reasoning, and tracks every application through to
an outcome.

It is live and working today with **6,677 real postings from 61 real companies**.
Every number in this brief is real.

Design **every page and every flow**, with **finished copy** — real headings,
real button labels, real empty-state sentences, real error messages. Do not
leave `Lorem ipsum` or `[placeholder]` anywhere. The words are half the design.

### The one sentence that explains the product

*Most job boards show you reposted listings that closed weeks ago, a "95% match"
nobody can explain, and a salary field that says "competitive". JobTrack shows
you the employer's own posting, tells you exactly which of their requirements
you meet and which you don't, and says "not disclosed" when the salary is not
disclosed.*

Everything you design should make that difference visible.

---

## 2. Who is using it

A software engineer looking for a job — usually **while still employed**, so in
stolen twenty-minute windows: a lunch break, a commute, late at night.

Three things follow from that, and they should shape every screen:

1. **They are scanning, not reading.** They will look at 40 roles to shortlist
   3. Rows must be comparable at a glance without opening anything.
2. **They come back repeatedly.** The question on opening is always *"what
   changed since yesterday, and what needs me today?"* — never *"let me browse."*
3. **Job hunting is demoralising.** Rejection is the common case. The tone must
   be calm, factual and respectful. Never chirpy, never gamified, never
   congratulatory about nothing. No confetti. No "You're crushing it!" An empty
   pipeline is not a failure state to be scolded about.

---

## 2.5 The character — what this should feel like

The rest of this brief is constraints. Constraints alone produce something
correct and forgettable, so read this section first and let it drive everything
after it.

**JobTrack should feel like a precision instrument that happens to be kind.**

Think of the confidence of a well-made tool: a good terminal, a Swiss railway
clock, the Linear app, Things 3. Everything is exactly where it should be, in
exactly the weight it deserves, and nothing is shouting. The product knows things
and tells you plainly. **Beauty here comes from precision, restraint, typography
and rhythm — never from ornament.** There is no illustration, no gradient mesh,
no stock photograph of a smiling person at a laptop. If a screen looks good with
the colour removed, it is right.

**What it must not look like:** LinkedIn, Indeed, or any job board. Those are
dense with advertising, gamified engagement, "dream job" language and manufactured
urgency. This audience is fluent in that vocabulary and distrusts it. Landing on
JobTrack should feel like closing twelve browser tabs.

**The tension to hold:** it must be visually striking without being decorative.
The way through is *information density made elegant* — the pleasure of a
well-set table of figures, a list you can read down at speed, a colour that
appears once on a screen and means something when it does. Aim for the reaction
*"this was built by someone who cares"* rather than *"this looks expensive"*.

### Signature moments — design at least two

A product people remember has one or two things they screenshot and send to a
friend. Nothing in the constraints will produce that on its own; you have to
decide it. Strong candidates, though you may find better:

- **The score breakdown.** This is the product's whole argument in one component:
  five weighted components, each with a one-line reason, and the missing skills
  named. Nothing else on the market shows its working. This should be the thing
  people screenshot.
- **The freshness rail** running down a list of forty rows, letting someone see
  the age of everything without reading a date.
- **The gap chips** — the moment a user realises the interface is pointing at what
  they *lack* rather than flattering what they have.
- **The activity heatmap** as a calm record of a hard few months.

Pick the ones you can make genuinely excellent and give them room. A signature
moment that is merely competent is not one.

---

## 2.6 Coherence — the pages must feel like one product

Design these as a **system**, not as ten screens. The test: someone who learns a
convention on the jobs feed should already know how to read the dashboard.

**One component, used everywhere.** The job card on the feed, in the dashboard's
"best matches", on the landing page hero and in the tracker is *the same
component* at different densities — not four separate designs that resemble each
other. Define its compact, default and expanded forms once.

**One vocabulary of signals**, used identically on every page:

| Signal | Means | Appears on |
|---|---|---|
| Score chip + band colour | how well this fits you | feed, dashboard, detail, landing |
| Leading rail | freshness | every job row, everywhere |
| Teal dot chip | a skill you have | feed, detail, profile |
| Rose chip | a skill you lack | feed, detail, dashboard |
| Amber | ageing, uncertain, gone quiet | job rows, tracker, estimates |
| Tabular figures | any number | everywhere, without exception |

If teal means "you have this skill" on the feed, it cannot mean "success" on the
tracker. Pick one meaning per colour and hold it across the whole product.

**Everything numeric leads somewhere.** Every count on the dashboard is a link to
the filtered view behind it. Every stage in the pipeline opens the tracker at that
stage. Every skill chip can filter the feed by that skill. A number that is only
a number is a missed connection — and connecting them is most of what makes the
product feel alive rather than static.

**The round trip must close.** Feed → save → tracker → advance → dashboard
reflects it. Profile → edit skills → feed re-ranks → dashboard's counts change.
Design those loops explicitly and show the state on both ends; a user should
always be able to see that what they did somewhere else took effect.

---

## 3. The product's principles — these are binding

These are not preferences. They are why the product exists, and a design that
breaks them is wrong however attractive it looks.

**Never fabricate a number.** If we don't know something, the UI says we don't
know. There is no "estimated salary", no invented "97% match", no fake activity.

**`null` is not `0`.** An undisclosed salary displays as *"Not disclosed"* — never
as `$0`, never hidden, never guessed. "The employer did not publish this" and
"this role pays nothing" are completely different statements. Same for an unknown
experience requirement and an unknown date.

**Every score shows its working.** A match score is always accompanied by what
matched and — more importantly — **what is missing**. A number the user cannot
interrogate is a number they cannot overrule. The gaps are the decision-relevant
part.

**Estimates are labelled as estimates.** Some vendors publish only a
"last updated" date, not a "first posted" date. Where we inferred, the UI says so.

**Colour encodes direction, never decoration.** And colour is never the *only*
signal — every state also carries text or a shape, for readers with a
colour-vision deficiency.

**Anti-features — do not design these, they will be rejected:**
- Auto-apply, or anything that submits on the user's behalf
- A match percentage with no breakdown
- Urgency manufacture ("3 people are viewing this!", countdown timers)
- Streaks, badges, points, levels
- Anything implying we have data we do not have

---

## 4. Hard technical constraints

The product's promise is that it runs well on a cheap laptop. These are enforced
in CI and a design that breaks them cannot ship.

| Constraint | Limit | Currently |
|---|---|---|
| First-load JS, gzipped | ≤ 100 KB | 60 KB |
| CSS, gzipped | ≤ 20 KB | 7.2 KB |
| Interaction latency (INP p75, 4× CPU throttle) | ≤ 200 ms | — |

- **System fonts only.** No webfonts, no font bytes. `ui-sans-serif, system-ui,
  -apple-system, "Segoe UI", Roboto, …` and a monospace stack for figures.
- **No icon library.** Icons are hand-written inline SVG. Keep the icon set small
  and simple — if a concept needs an illustration to be understood, rewrite the
  label instead.
- **No CSS framework.** Hand-written CSS with custom properties.
- **Server-rendered.** The feed, the filters and every form must work with
  JavaScript disabled. JavaScript may enhance; it may not be required for the
  core loop.
- **No charting library.** Any data visualisation must be plain CSS/SVG — this is
  achievable and part of the aesthetic, not a limitation to design around.
- **Light, dark and system themes**, all three first-class. Dark is not an
  afterthought; roughly half of this audience uses it exclusively.
- **WCAG 2.2 AA**, specifically including 2.4.11 Focus Not Obscured, 2.5.8 Target
  Size (24×24 CSS px minimum) and 3.3.8 Accessible Authentication (password
  managers must be able to fill the auth forms).

### 4.1 Screen sizes — every one of these is first-class

This is not "make it responsive". These are the machines this audience actually
uses, and **each one must be designed, not merely survived**. A layout that
technically reflows but wastes half a 16-inch screen, or that needs horizontal
scrolling on a phone, has failed.

| Class | Design at | Real devices |
|---|---|---|
| **Small phone** | **320 × 568** | iPhone SE, older Android. The floor — nothing may break or scroll sideways below this |
| **Phone** | **390 × 844** | iPhone 14/15/16, Pixel. **The most common single size** |
| **Large phone** | **430 × 932** | iPhone Pro Max, large Android |
| **Tablet portrait** | **820 × 1180** | iPad 10.9" / iPad Air — **touch, but wide** |
| **Tablet landscape** | **1180 × 820** | Same iPad rotated |
| **Large tablet** | **1024 × 1366** | iPad Pro 12.9" portrait |
| **Laptop 14" (Windows)** | **1366 × 768** | The most common Windows laptop. **Short — only 768px tall** |
| **Laptop 14" (MacBook Pro)** | **1512 × 982** | Default scaled resolution |
| **Laptop 16" (MacBook Pro)** | **1728 × 1117** | Default scaled resolution |
| **Desktop** | **1920 × 1080 and wider** | External monitors |

**Four things that are routinely got wrong, and must not be here:**

**1. Vertical space is the scarcer axis on a 14" laptop.** 1366×768 is the single
most common Windows laptop resolution and it is only **768px tall** — a sticky
header, a filter bar and a page heading can consume half of it before a single
job row appears. Count the pixels above the first result at 1366×768 and treat
that number as a budget. The user came to see jobs.

**2. A tablet is a large screen with touch.** This is the case people get wrong
most often, because it falls between the two things they designed. At 1180px wide
an iPad will receive the *desktop* layout, and every target in it must still be
**at least 44×44 CSS px** for a finger — not the 24px minimum that suffices for a
mouse. Do not key touch sizing off viewport width. Use
`@media (pointer: coarse)`, and state where you have done so.

**3. A 16" screen is not just "wider".** The current `.shell` caps content at
1180px, which on a 1728px MacBook leaves ~270px of empty margin each side. Decide
deliberately: either let the container grow, use the space for a genuine
third column, or keep the cap and justify it (there is a real argument — line
length past ~90 characters hurts readability). Any of those is fine. Not deciding
is not.

**4. Landscape phone exists.** 844×390 is a real state — someone turning their
phone sideways. A full-height modal or a sticky bar sized for portrait becomes
unusable at 390px of height. It does not need to be beautiful; it must be usable.

**Per-page responsive behaviour to specify explicitly:**

- **Jobs feed** — where the Apply/Save column moves below the content, and at what
  width. How skill chips truncate rather than wrapping to four lines.
- **Filters** — chip rows wrap on desktop; on a phone they likely need a
  bottom-sheet or collapsible panel. **This is the hardest responsive problem in
  the product — design it fully.**
- **Dashboard** — the two-column grid collapses to one; specify the resulting
  **order**, since "needs your attention" must not fall below the fold on a phone.
- **Tracker** — a six-column board cannot work on a 390px phone. Horizontal
  scroll with snap, an accordion, or a filtered single-column view: pick one and
  design it.
- **Auth** — the two-pane layout drops its right pane below ~860px.
- **Onboarding** — must be comfortable one-handed on a phone; this is where people
  will actually complete it.
- **Job detail** — long description text needs a sensible measure at every width.

**Approach.** Design mobile-first and let breakpoints be decided by where the
*content* breaks, not by device names — the table above is what you verify
against, not a list of hard-coded widths. Use relative units, `flex`/`grid`, and
`max-width: 100%` on media. Wide content (the tracker board, any table, a code
block) scrolls inside its own container; **the page body must never scroll
horizontally at any width in the table.**

---

## 5. The current visual language — improve on it, don't feel bound by it

There is a working design system. Treat it as a starting point with known
weaknesses, not as a specification. If you can do better, do better — but
understand *why* each rule exists before replacing it.

**Palette.** One accent (indigo `#4f46e5` light / `#818cf8` dark) carries all
interaction. Neutrals are warm-cast greys, not pure grey. Three semantic colours
appear **only as small signals** — a 2px rail, a 5px dot, a chip border — never as
large filled surfaces:

- **Teal** `#0d9488` — growing, matched, fresh
- **Rose** `#e11d48` — shrinking, missing, dead
- **Amber** `#d97706` — contested, uncertain, ageing

*A revision that tinted whole card backgrounds with these read as a chart rather
than a product. That is the failure mode to avoid.*

**Type.** 11 / 12 / 14 / 15 / 17 / 21 / 26 / 32 px. Letter-spacing tightens as
size grows (`-0.03em` at 32px, `0` at 14px and below) — this is the single detail
that most separates "crafted" from "default". **All figures use tabular
numerals**, because every number on this site sits in a column that updates.

**Borders are shadows.** `box-shadow: 0 0 0 1px` rather than `border: 1px solid`.
A ring consumes no layout space, so it can thicken on focus without shifting
anything by a pixel.

**Motion.** One curve, `cubic-bezier(0.32, 0.72, 0, 1)`, two durations (130ms and
240ms). Hover lift is **1px** — more reads as a toy. Respect
`prefers-reduced-motion`.

**Known weaknesses you are invited to fix:** the dashboard's right column runs
short and leaves a void; job cards are taller and sparser than they need to be;
the landing page is thin below the fold; there is no loading or skeleton state
anywhere.

---

## 6. Real data — design against these shapes

Do not invent fields. If a widget needs data not listed here, say so explicitly
and explain what would have to be captured.

### A job posting

```
title            "Senior Backend Engineer, Payments"
company_name     "Stripe"
location_raw     "San Francisco / New York City / Remote"   ← can be very long
city, country    "San Francisco", "US"                       ← may be null
mode             remote | hybrid | onsite | unknown
apply_url        the employer's real ATS URL
ats_vendor       "greenhouse" | "ashby"
comp_min/max     230000 / 405000  — MAY BE NULL (undisclosed)
comp_currency    "USD" | "INR" | "EUR" | "GBP" …
comp_source      structured | parsed_text | null   ← parsed_text is less certain
yoe_min/max      5 / 9 — may be null
yoe_confidence   0.0–1.0; below 0.5 the band is UNKNOWN, not zero
posted_at        may be null
posted_at_is_estimate   true = inferred from a "last updated" field
first_seen_at    when we fetched it
ai_screening_disclosed  true | false | NULL (null = vendor doesn't say)
must_have_skills ["go", "postgresql", "kubernetes"]
nice_to_have_skills [...]
parse_confidence 0.0–1.0; below 0.5 we understood less than half the posting
match            { score: 0–100, band, missing_skills: [...] }  ← null if signed out
saved            boolean
```

**`band`** is one of `strong` · `plausible` · `stretch` · `unlikely`. Bands
matter more than the raw number — design them as the primary signal.

The feed can also be **filtered by band** — `?band=strong,plausible` — which
requires a signed-in caller and excludes postings not yet scored.

### The user's profile

```
first_name, last_name, current_title, target_title
total_yoe          number   (0 is a real answer — a new graduate has zero)
yoe_stated         boolean  (distinguishes "zero years" from "never answered")
pref_countries     ["IN", "GB"]
pref_modes         ["remote"]
pref_comp_min      6000000
pref_currency      "INR"
skills             ["go", "kubernetes", "kafka", "postgresql", "terraform"]
```

### An application

```
company_name, role_title
status        saved → applied → recruiter_screen → hm_screen → onsite → offer
              plus terminal: rejected · ghosted · withdrawn
channel       careers_page | job_board | referral | cold_email | recruiter_inbound | other
applied_at, last_activity_at, next_action, next_action_at
```

Every status change also writes an **`application_events`** row with
`occurred_at`, `from_status` and `to_status` — **this is what makes an activity
heatmap real data rather than decoration.**

### Aggregates available

`strong` / `plausible` match counts · postings scored · new in last 24h ·
live postings · companies tracked · added this week · share fully remote ·
count per pipeline stage · applications with no movement for N days ·
`users.last_active_at`, which is what makes "new since your last visit" real.

---

## 7. The pages

### 7.0 The map — design the connections, not just the screens

```
                      ┌──────────────┐
   signed out ───────►│   Landing    │──── Browse without an account ──┐
                      └──────┬───────┘                                 │
                             │ Create an account                       │
                             ▼                                         ▼
                      ┌──────────────┐   4 steps, resumable    ┌──────────────┐
                      │   Sign up    │──►│ Onboarding │───────►│  Jobs feed   │◄─┐
                      └──────────────┘   └────────────┘        └──────┬───────┘  │
                             ▲                    │                   │          │
                      ┌──────┴───────┐            ▼            open ──┤          │
                      │   Sign in    │─────►┌──────────┐              ▼          │
                      └──────────────┘      │Dashboard │◄──┐   ┌────────────┐    │
                                            └────┬─────┘   │   │ Job detail │    │
                       every count is a link ────┤         │   └─────┬──────┘    │
                                                 │         │         │ save      │
                        ┌────────────────────────┼─────────┼─────────┘           │
                        ▼                        ▼         │                     │
                 ┌────────────┐           ┌────────────┐   │                     │
                 │  Tracker   │──advance──┤  Profile   │───┴── re-scores ─────────┘
                 └────────────┘           └────────────┘      the whole feed
```

**Every arrow is a design decision.** Specify for each: what carries over, what
the destination looks like on arrival, and how the user gets back. In
particular:

- **Dashboard → Jobs** must arrive with the filter *visibly applied* and easy to
  remove. Landing on an unexplained subset is disorienting.
- **Dashboard → Tracker** must arrive scrolled or filtered to the stage clicked.
- **Job detail → back** must return to the same scroll position in the feed. This
  is the single most-used navigation in the product.
- **Profile save → feed** re-ranks everything. Say so at the point of saving, or
  the feed appears to change by itself and reads as a bug.
- **Signed-out → Apply or Save** — decide what happens. Prompting for an account
  at the moment of intent is reasonable; losing the click is not.

### 7.1 Landing (signed out)

The only page whose job is persuasion. It must explain the product to someone who
has never heard of it, in a way a sceptical engineer believes.

Lead with the difference, not with adjectives. Show a **real job card** — the
actual component, with a real score breakdown and a visible missing skill — as
the hero proof. Seeing the product beats describing it.

Points to make (write better copy than this, but keep the substance):
- Postings come from the employer's own hiring system, so the apply link works
- Scores name what fit and what didn't
- Undisclosed stays undisclosed
- Server-rendered and light; works on a slow laptop

Include the live count ("6,677 live postings from 61 companies, refreshed
continuously") — a checkable number, not "thousands of opportunities".

Two calls to action: **Create an account** and **Browse without an account** —
discovery genuinely does not require signing up, and saying so builds trust.

### 7.2 Sign in / Sign up

Calm, fast, no navigation to wander off into. Password rules: **length only**
(12+ characters), never composition rules — NIST SP 800-63B advises against them,
and the copy should reassure rather than nag. `autocomplete` on both fields is a
WCAG requirement here, not a nicety.

Sign-up leads directly into onboarding — never bounce a new account to a sign-in
page. Registration signs the user in immediately, with the email left unverified;
verification gates outbound email, not access.

### 7.3 Onboarding — 4 steps

Progress must be **named** ("Step 2 of 4"), not just a bar; an unlabelled bar
leaves people unsure how much is left. Each step saves to the server, so a closed
tab loses nothing. Offer a **skip** from step 3 onward.

1. **Name** — first and last
2. **Experience** — current title, target title, years
3. **Skills** — the most important step; see the chip input in §8
4. **Preferences** — countries, work modes, minimum salary and currency

Title each step by what the *user* gets, not by what we collect. "Let's start
with you", not "Personal information". Each step should say briefly why the field
matters — "Skills are 40% of every match score" earns the answer.

### 7.4 Dashboard — the page they see on every sign-in

This is the most important screen. It answers four questions, in this order:

1. **Is anything new for me?**
2. **What needs a reply today?**
3. **How am I doing overall?**
4. **What is the market doing?**

Required elements:

**Stat tiles — clickable, and this is a specific requirement.** Clicking any tile
navigates to the jobs listing with that filter already applied. Design them so
they clearly read as navigation, not as static figures. A tile that looks
clickable and isn't is the fastest way to make an interface feel broken.

**An activity heatmap** — a calendar grid of the last ~12 weeks showing job-search
activity per day, from `application_events.occurred_at`. Two design requirements:
- It must be **honest about an empty history** — the empty state should invite the
  first application rather than displaying a grid of grey squares.
- It is a **record, not a target.** No streaks, no "don't break the chain", no
  implication that more applications is better.

**Pipeline** — counts per funnel stage, each **clickable through to the tracker
filtered to that stage**. Show every stage including empty ones.

**Needs your attention** — applications with no movement for 7+ days. It earns
the top of the column, **but only when it has something to say.**

**Your best matches** — top 6 with score, company, location, freshness and gaps.

**Market context** — background, not a task; it belongs low on the page.

**Profile strength** — what is missing and *what filling it in would change about
matching*. Not a vanity meter. Hide it once complete.

### 7.5 Jobs listing

A dense, scannable list. Each row must let someone decide *"open or skip"*
without clicking.

Per row: match score and band · title · company · location · work mode ·
compensation (or "Not disclosed") · years required · age · source vendor ·
skill chips · **Apply** and **Save**.

**Skill chips carry the key insight.** A skill the reader *has* is confirmation
and should be quiet. A skill they *lack* is the decision and should be loud. The
eye must land on the gap.

Freshness is encoded on a rail down the leading edge of each row — teal fresh,
fading to amber as it ages — so forty rows can be scanned for recency without
reading a single date.

Also needed: pagination, a **loading/skeleton state**, and an empty state that
**names the filter most likely responsible** and offers one click to relax it.

### 7.6 Filters — chips, not text boxes

Design a filter system where **every choice is a chip or a segmented control** —
nothing the user has to type except free-text search. The user should never
wonder what format to enter, and should never be able to enter something invalid.

- **Work mode** — Remote · Hybrid · On-site (multi-select chips)
- **Location** — country chips. Consider grouping or a "more" affordance.
- **Years of experience** — banded chips (0–2 · 2–5 · 5–8 · 8+), never a numeric
  input. Include a "stretch" affordance: many postings say "5+ years" and mean it
  softly, and self-filtering there costs people real opportunities.
- **Salary** — banded chips in the user's own currency, plus a distinct
  **"Include undisclosed"** control. Roughly a fifth of postings publish no
  salary; filtering them out silently removes a fifth of the market.
- **Freshness** — 24 hours · 3 days · 7 days · 14 days · Any time
- **Source** — Greenhouse · Ashby
- **Match band** — requires an account; excludes unscored postings

Requirements:
- **Every chip shows its result count** so nobody filters blindly into zero.
- **Active filters are summarised and individually removable**, with a "clear all".
- Filter state lives in the **URL**, so a filtered view is shareable.
- It must work as a plain form with **no JavaScript**.
- Design the **mobile** treatment explicitly.

### 7.7 Job detail

The full description, the complete score breakdown by component (skills 40 ·
experience 20 · location 15 · compensation 15 · freshness 10, each with its own
one-line reasoning), the compensation with its provenance, the AI-screening
disclosure where the vendor provides one, and Apply / Save.

The score breakdown is the product's argument made visible. Give it room.

### 7.8 Profile

Sections: about you · skills · what counts as a fit. A sticky save bar, and it
must say that saving re-scores everything. The **skills editor is the
centrepiece** — see §8.

### 7.9 Tracker

A funnel board: Saved · Applied · Screening · Interviewing · Offer · Closed.
Terminal outcomes share one column.

Advancing a status must be **one click from the board**. Show ghosting honestly:
no movement for 21 days is marked automatically, and the copy should be
matter-of-fact — this is the norm in this market, not a personal failure.

### 7.10 Settings

**This is where theme and density live** — not the header; see §7.12. Sections:
appearance, email preferences, and account actions including sign out and delete.

Design it to absorb sections it does not have yet — notifications, email digests,
data export — without becoming a wall of switches.

### 7.11 System states

- **404** — in the product's voice, with a route back
- **API unreachable** — the feed must degrade *visibly*. A blank list reads as
  "there are no jobs", which is a different and untrue statement
- **A posting that failed to parse** — `parse_confidence < 0.5`. Show it, marked
- **Session expired mid-action** — the user must not lose what they typed

### 7.12 App shell

Header: wordmark (doubling as the home link), primary navigation
(Dashboard · Jobs · Tracker), and an account menu leading to Profile, Settings
and Sign out. Footer stating where postings come from. Design the **signed-out**
header too.

**Theme and density do NOT belong in the header.** They live in Settings
(§7.10). The rule, which applies to anything else you are tempted to put up
there: **persistent real estate is earned by frequency × value.**

The one thing that genuinely earns persistent space in a product about finding
something is **global search**. Justify it or leave it out.

Design the header to hold **five** navigation items without redesign.

---

## 8. The skills editor — highest-value single component

Skills are **40% of every match score**, the largest component by a wide margin.
The quality of this one control determines the quality of every match the user
ever sees. It appears in onboarding step 3 and on the profile page.

Requirements:
- Existing skills as **removable chips**
- Typeahead over a known vocabulary (Go, Python, PostgreSQL, Kubernetes…), so a
  user picks the canonical name rather than inventing a variant
- **Suggested common skills** as one-tap chips — reduces typing to almost nothing
- Enter and comma both commit; Backspace on an empty field removes the last chip
- Guidance toward **8–15 skills**: a list of forty says nothing about what
  someone is strongest at
- Must degrade to a plain comma-separated text input with no JavaScript

Consider — and tell us whether it is worth it — grouping suggestions by category
(language, datastore, cloud, framework).

---

## 9. Copy — write all of it

Tone: **precise, plain, quietly confident.** Explain rather than sell. Never
exclamation marks. Never "Oops!". Never blame the user.

Write finished strings for:
- Landing page headline, subhead, feature descriptions, CTAs
- Auth: labels, helper text, every error ("That email and password did not
  match." — never "Invalid credentials")
- All four onboarding steps: titles, blurbs, field help, buttons
- Every dashboard widget title and its empty state
- Filter labels and the empty-results message
- Job card labels: "Not disclosed", "via Greenhouse", freshness, estimate marker
- Score band names — the four bands need names a person would actually use
- Tracker status labels and stage-advance buttons
- Profile section titles and field help
- Every empty state and every error

Empty states deserve particular care. **Every widget has one**, and a new user
sees all of them at once on their first sign-in. That first-run dashboard is a
specific design deliverable, not an afterthought.

**The score reason strings are generated by our scorer, not written per posting.**
Verbatim examples from the running system:

```
skills        "7 of 9 must-haves. Missing: kafka, terraform"
              "0 of 3 preferred skills — this posting says little about what it needs"
experience    "Asks for 8+ years; you have 7 — often still worth applying"
              "Targets up to 5 years; you have 7"
location      "Remote — matches your preference"
compensation  "Salary not disclosed" / "Salary is in a different currency"
freshness     "Posted today — you are early" / "Posted over two weeks ago"
```

Eleven characters to seventy. If a layout needs them all to be a similar length,
it is the wrong layout. If you want different phrasing, say so and we will change
the generator — the strings are ours, not the vendor's.

---

## 10. What to deliver

1. **Every page** in §7, in **light and dark**, at **phone (390) · tablet
   (820 portrait and 1180 landscape) · laptop 14" (1366×768) · laptop 16"
   (1728)** — the full matrix in §4.1, not a desktop design with a phone
   afterthought
2. **The first-run experience** — a brand-new account with zero data, on every
   screen
3. **The filter system** in detail at every width — the phone form is the hard
   case and needs its own treatment
4. **The skills editor** in detail
5. **The activity heatmap**, including its empty state
6. **The job card**, at several score bands and with an undisclosed salary
7. **Loading / skeleton states** for the feed and dashboard
8. **The tracker board on a phone** — a six-column layout cannot survive 390px
   intact, so show the shape you chose
9. **All copy**, finished
10. **The design tokens** — colour, type scale, spacing, elevation, motion — as a
    coherent system
11. A short note on **what you changed and why**, especially anything in §5 you
    replaced, and where you used `pointer: coarse` rather than a width query

### Stress-test data — design against these, not against "Job Title"

```
"Research Engineer / Scientist, Frontier Red Team (Cyber)" · Anthropic
   San Francisco, CA · salary NULL · yoe unknown          ← 55-char title, nothing disclosed
"Security Engineer, Application Security" · OpenAI
   New York City · 234,400–385,000 USD · yoe unknown      ← the well-populated case
"Senior Analyst, Sales Strategy & Operations - Public Sector" · Elastic
   London, United Kingdom · salary NULL · yoe 5–10        ← 59 chars, long location
"Software Engineer, Compute Infrastructure" · OpenAI
   San Francisco / New York City / Seattle / London, UK   ← four-office location string
   230,000–405,000 USD · remote · score 99 strong
```

Three of those four disclose no salary and two have no experience band. That
ratio is representative, and a card design that only looks good fully populated
is the wrong design.

## 10.1 What form the output should take

This is a **real, running codebase**, not a concept. Output that cannot be
implemented is not useful, however attractive.

- **Deliver HTML + CSS**, not a static image. Plain semantic HTML with
  hand-written CSS using custom properties. It does not need to be Svelte — the
  markup and CSS will be ported — but it must be real, working, viewable markup.
- **Design tokens first**, as a `:root` block of custom properties: colour (light
  *and* dark), type scale with its letter-spacing pairs, spacing, radii,
  elevation, motion. Every component then references tokens, never literals.
- **One page per response** is fine and probably better. But define the tokens and
  the shared components once, first, and reuse them by name after that.
- **Show every state** of each component: default, hover, focus-visible, active,
  disabled, loading, empty, error.
- **Annotate the reasoning.** For each screen, a few lines on what you decided
  and why.
- **Stay inside the budget.** No webfonts, no icon library, no charting library,
  no CSS framework.

---

## 11. How this will be judged

- **Accurate** — nothing implies data we don't have; `null` never renders as `0`
- **Scannable** — a user can compare 20 roles without opening one
- **Actionable** — every number leads somewhere; nothing is decorative
- **Honest** — gaps and uncertainty are as visible as strengths
- **Light** — within budget, no webfonts, no icon library, no charting library
- **Complete** — every state designed, including empty, loading and error
- **Kind** — a demoralising activity, made calmer rather than louder
- **Coherent** — one system, not ten screens; a convention learned on one page
  holds on every other
- **Memorable** — at least two signature moments a user would screenshot, and
  nothing that could be mistaken for LinkedIn
- **Sized for real machines** — usable and *considered* at every width in §4.1,
  touch-correct on tablets, and never scrolling sideways
