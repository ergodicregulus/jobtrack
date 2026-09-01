<script lang="ts">
  import { untrack } from 'svelte';
  import { mutate, type MutationFailure } from '$lib/mutate.svelte';
  import type { FeedItem } from '$lib/types';
  import {
    freshnessOf, relativeAge, formatComp, formatYoE, formatMode, formatVendor
  } from '$lib/format';

  interface Props {
    job: FeedItem;
    signedIn?: boolean;
    /** First seen after this reader's previous visit. */
    isNew?: boolean;
  }
  const { job, signedIn = false, isNew = false }: Props = $props();

  // Saved state is optimistic: the button flips immediately and reverts if the
  // request fails. A save that waits for a round trip before acknowledging feels
  // broken on a slow connection, and this action is cheap to undo.
  let saved = $state(untrack(() => job.saved));
  let failure = $state<MutationFailure | null>(null);

  // Re-seed when the server sends a different posting into this slot. Svelte
  // reuses components across keyed list updates, so without this a card
  // recycled for another job would inherit the previous job's saved state.
  $effect(() => {
    saved = job.saved;
  });
  let saving = $state(false);

  // Lower-cased lookups so chip colouring can answer "does this reader have
  // this skill" in constant time per chip rather than scanning both arrays.
  const missing = $derived(
    new Set((job.match?.missing_skills ?? []).map((s) => s.toLowerCase()))
  );
  const matched = $derived(
    job.match
      ? new Set(
          job.must_have_skills
            .map((s) => s.toLowerCase())
            .filter((s) => !missing.has(s))
        )
      : new Set<string>()
  );

  /**
   * The chips actually rendered, gaps first and capped.
   *
   * Gaps lead because a gap is the decision and a match is confirmation. The
   * caps are separate — at most 2 gaps and 4 chips overall — so a card with
   * seven gaps shows two and a count rather than seven, which is what turned
   * the feed into a wall of red once extraction improved.
   */
  const MAX_GAPS = 2;
  const MAX_CHIPS = 4;

  interface Chip { name: string; kind: 'gap' | 'match' | 'plain'; }

  const allSkills = $derived.by((): Chip[] => {
    const gaps: Chip[] = [];
    const matches: Chip[] = [];
    const plain: Chip[] = [];

    for (const skill of job.must_have_skills) {
      const key = skill.toLowerCase();
      if (missing.has(key)) gaps.push({ name: skill, kind: 'gap' });
      else if (matched.has(key)) matches.push({ name: skill, kind: 'match' });
      // A signed-out reader cannot be told either, so the chip stays neutral.
      else plain.push({ name: skill, kind: 'plain' });
    }
    return [...gaps, ...matches, ...plain];
  });

  const shownSkills = $derived.by((): Chip[] => {
    const gaps = allSkills.filter((c) => c.kind === 'gap').slice(0, MAX_GAPS);
    const rest = allSkills.filter((c) => c.kind !== 'gap');
    return [...gaps, ...rest].slice(0, MAX_CHIPS);
  });

  const hiddenCount = $derived(allSkills.length - shownSkills.length);

  // The overflow chip names what it is hiding on hover, so the cap costs
  // information from the glance but never from the page.
  const hiddenTitle = $derived(
    allSkills
      .slice(shownSkills.length)
      .map((c) => (c.kind === 'gap' ? `− ${c.name}` : c.name))
      .join(', ')
  );

  const bandLabel: Record<string, string> = {
    strong: 'Strong match',
    plausible: 'Worth a look',
    stretch: 'A stretch',
    unlikely: 'Unlikely fit'
  };

  async function toggleSave() {
    if (saving) return;
    const next = !saved;
    saved = next; // optimistic: this is cheap to undo, and waiting feels broken
    saving = true;
    failure = null;

    failure = await mutate(
      () => fetch(`/v1/me/saved/${job.id}`, { method: next ? 'PUT' : 'DELETE' }),
      () => { saved = !next; }
    );
    saving = false;
  }

  /**
   * Hiding collapses the card to an undo strip. It does NOT remove it.
   *
   * A row that vanishes under the cursor is disorienting — the list jumps, the
   * thing you were reading is gone, and there is nothing left to click if you
   * were wrong. Gmail's pattern instead: the row stays, shrinks, and offers the
   * way back. The posting is gone from the NEXT feed, which is where the
   * feature actually pays off.
   */
  let hidden = $state(false);
  let hiding = $state(false);

  $effect(() => {
    job.id;
    hidden = false;
  });

  async function hide() {
    if (hiding) return;
    hidden = true;
    hiding = true;
    failure = null;
    failure = await mutate(
      // keepalive: hiding a card and immediately clicking through to another
      // posting is a normal sequence, and without it the browser cancels the
      // in-flight request on navigation — the card comes back next visit and
      // the feature looks broken intermittently. Found by an e2e test that
      // reloaded straight after hiding.
      () => fetch(`/v1/me/dismissals/${job.id}`, { method: 'PUT', keepalive: true }),
      () => { hidden = false; }
    );
    hiding = false;
  }

  async function unhide() {
    if (hiding) return;
    hidden = false;
    hiding = true;
    failure = null;
    failure = await mutate(
      () => fetch(`/v1/me/dismissals/${job.id}`, { method: 'DELETE', keepalive: true }),
      () => { hidden = true; }
    );
    hiding = false;
  }

  const freshness = $derived(freshnessOf(job.posted_at, job.first_seen_at));
  const age = $derived(relativeAge(job.posted_at, job.first_seen_at));
  const comp = $derived(formatComp(job.comp_min, job.comp_max, job.comp_currency, job.comp_period));
  const yoe = $derived(formatYoE(job.yoe_min, job.yoe_max, job.yoe_confidence));
  const mode = $derived(formatMode(job.mode));
  const vendor = $derived(formatVendor(job.ats_vendor));

  // Below 0.5 we understood less than half the posting. The card says so rather
  // than presenting fragments as though they were complete.
  const lowConfidence = $derived(job.parse_confidence < 0.5);

  const location = $derived(job.city ?? job.location_raw);
</script>

<!--
  The card is a list item containing exactly one primary link, so it behaves
  like a list of links to a screen reader instead of a div soup with a click
  handler.
-->
{#if hidden}
  <li class="hidden-row">
    <span class="hidden-what">
      Hidden <span class="hidden-title">{job.title}</span> at {job.company_name}
    </span>
    <button class="btn btn-sm" onclick={unhide} disabled={hiding}>Undo</button>
    {#if failure}
      <span class="t-micro err" role="alert">{failure.message}</span>
    {/if}
  </li>
{:else}
<li class="card" class:low-confidence={lowConfidence} data-freshness={freshness}>
  <!--
    THE FRESHNESS RAIL.

    A vertical bar whose colour encodes posting age: teal when fresh, fading
    through ochre as it stales. It exists because freshness is the single most
    important property of a posting and reading a date on every row is work.
    With the rail you can scan forty cards and SEE which ones are worth opening.

    aria-hidden because the same information is in the text below it — this is a
    redundant visual encoding, never the only one.
  -->
  <div class="rail" aria-hidden="true"></div>

  <div class="body">
    <div class="head">
      {#if job.match}
        <!-- The score leads the row because it is the reason this card is
             ranked where it is. Showing the band as text beside the number
             keeps it readable for anyone who cannot separate the colours. -->
        <span class="score" data-band={job.match.band} title={bandLabel[job.match.band]}>
          {Math.round(job.match.score)}
        </span>
      {/if}
      <!--
        The title opens OUR detail page, not the employer's.

        It used to link straight to the ATS, which meant the score breakdown —
        the one thing no competitor can show — had nowhere to be read. Apply is
        still a direct link to the employer, so nothing is interposed between a
        decision and acting on it.

        It is NOT a primary button, though it was. Twenty-five filled buttons
        down the right edge of a list outweighed twenty-five role titles, which
        inverts what the page is for: on a list the action is to read, and
        applying is what you do after. A product whose first principle is that
        it will not mass-apply for you should not make "Apply" the loudest
        thing on a page the reader has not read yet.
      -->
      <h3 class="title">
        <a href="/jobs/{job.id}">{job.title}</a>
      </h3>
      {#if isNew}
        <span class="new-tag">New</span>
      {/if}

      {#if job.ai_screening_disclosed}
        <!--
          Greenhouse exposes whether the employer runs AI talent matching, and
          an opt-out URL. We report it without editorialising: fewer than half
          of organisations use AI in HR at all, and its presence is not evidence
          of a bad process.
        -->
        <span class="chip chip-warn" title="This employer discloses AI-assisted screening">
          ⚙ AI screening
        </span>
      {/if}
    </div>

    <!--
      One wrapping meta line, not two fixed ones.

      Company/location/mode and salary/years/age/source were separate rows.
      Neither came close to filling the column — together they ran about 340px
      of an available 620px — so every card spent a whole line height on
      whitespace, twenty-five times down the page. Merged, they fill the width
      at desktop and wrap back to two lines on a phone by themselves, which is
      what the second row was really for.

      The order is deliberate: identity first (who and where), then the terms
      (what it pays, what it asks, how old, from where).
    -->
    <div class="meta facts">
      <span class="fact">{job.company_name}</span>

      <span class="meta-sep">·</span>
      <span class="fact location" title={job.location_raw}>{location}</span>

      <!-- Employers routinely set the location field to "Remote", which
           produced "Remote · Remote" on those rows: the same fact twice, in two
           cases, reading as a rendering fault rather than as data. -->
      {#if mode && !location.toLowerCase().includes(mode.toLowerCase())}
        <span class="meta-sep">·</span>
        <span class="fact">{mode}</span>
      {/if}

      <span class="meta-sep">·</span>
      <!-- Compensation. "Not disclosed" is stated, never omitted: an absent
           row would read as "we did not check". -->
      <span class="fact" class:undisclosed={!comp.disclosed}>
        <span class="mono">{comp.text}</span>
        {#if comp.disclosed && job.comp_source === 'parsed_text'}
          <span class="hint" title="Read from the job description, not a published salary field">~</span>
        {/if}
      </span>

      {#if yoe}
        <span class="meta-sep">·</span>
        <span class="fact mono">{yoe}</span>
      {/if}

      <span class="meta-sep">·</span>
      <!-- Age is never hidden behind a filter. Freshness is the product. -->
      <span class="fact age" data-freshness={freshness}>
        {age}{#if job.posted_at_is_estimate}<span class="hint" title="Estimated from the last update; the source does not publish a first-posted date">≤</span>{/if}
      </span>

      <span class="meta-sep">·</span>
      <!-- The ATS vendor, so the user knows this apply link lands in the
           employer's real requisition queue rather than a board's inbox. -->
      <span class="fact via">via {vendor}</span>
    </div>

    {#if shownSkills.length}
      <!--
        Chips are NEUTRAL unless the colour is telling this reader something,
        and the thing worth telling them is what they are MISSING.

        Three revisions to get here. The first coloured every required skill
        teal, which turned a screen of cards into a wall of green and broke the
        rule that colour encodes direction, never decoration. The second
        coloured matches teal and gaps rose — better, but on a good match that
        is eight teal chips and one rose, and the eye goes to the eight. The
        third inverted the weight, quiet matches and loud gaps, which was right
        in principle and then wrong in practice: once skill extraction improved,
        a typical card carried five gaps and the feed became a wall of RED —
        the same failure, in the other colour.

        So the count is capped, not just the styling. At most two gaps are shown
        as chips and the rest become "+N more", because a reader deciding
        whether to open a posting needs to know THAT there are gaps and roughly
        how many, not to read all seven on a list of twenty-five.

        The `−` prefix is what lets a gap read as a gap without any colour at
        all, for the 1-in-12 men with a colour-vision deficiency.
      -->
      <ul class="skills">
        {#each shownSkills as s (s.name)}
          <li
            class="chip"
            class:chip-match={s.kind === 'match'}
            class:chip-miss={s.kind === 'gap'}
            class:chip-plain={s.kind === 'plain'}
          >{#if s.kind === 'gap'}<span class="minus" aria-hidden="true">−</span>{/if}{s.name}</li>
        {/each}
        {#if hiddenCount > 0}
          <li class="chip chip-plain" title={hiddenTitle}>+{hiddenCount} more</li>
        {/if}
      </ul>
    {/if}


    {#if lowConfidence}
      <p class="confidence-note">
        Limited detail — this posting was hard to parse, so some fields may be missing.
      </p>
    {/if}
  </div>

  <div class="actions">
    <a class="btn apply" href={job.apply_url} target="_blank" rel="noopener noreferrer">
      Apply
      <span class="sr-only">at {job.company_name} on {vendor} (opens in a new tab)</span>
    </a>
    {#if signedIn}
      <button
        class="btn btn-sm save"
        aria-pressed={saved}
        onclick={toggleSave}
        disabled={saving}
        title={saved ? 'Remove from tracker' : 'Save to tracker'}
      >
        <svg viewBox="0 0 14 14" width="12" height="12" aria-hidden="true">
          <path
            d="M3.5 1.5h7v11l-3.5-2.6-3.5 2.6z"
            fill={saved ? 'currentColor' : 'none'}
            stroke="currentColor"
            stroke-width="1.3"
            stroke-linejoin="round"
          />
        </svg>
        {saved ? 'Saved' : 'Save'}
      </button>
      <button
        class="btn btn-sm hide-btn"
        onclick={hide}
        disabled={hiding}
        title="Hide this from your feed"
      >
        <svg viewBox="0 0 14 14" width="12" height="12" aria-hidden="true">
          <path d="M1 7s2.2-4 6-4 6 4 6 4-2.2 4-6 4-6-4-6-4z" fill="none"
                stroke="currentColor" stroke-width="1.3" />
          <path d="M2 12L12 2" stroke="currentColor" stroke-width="1.3" />
        </svg>
        Hide
      </button>
      {#if failure}
        <span class="t-micro err" role="alert">
          {failure.message}
          {#if failure.retryable}
            <button class="retry" onclick={toggleSave}>Try again</button>
          {:else if failure.needsAuth}
            <a href="/login?next=/jobs">Sign in</a>
          {/if}
        </span>
      {/if}
    {/if}
    {#if job.ai_opt_out_url}
      <a class="opt-out" href={job.ai_opt_out_url} target="_blank" rel="noopener noreferrer">
        AI opt-out
      </a>
    {/if}
  </div>
</li>
{/if}

<style>
  .card {
    display: grid;
    grid-template-columns: 3px minmax(0, 1fr) auto;
    gap: 0 var(--s-4);
    align-items: start;
    background: var(--bg-raised);
    border-radius: var(--radius-md);
    box-shadow: var(--e-1);
    overflow: hidden;
    transition: box-shadow var(--fast) var(--ease), transform var(--fast) var(--ease);
  }
  .card:hover { box-shadow: var(--e-2); transform: translateY(-1px); }

  /* Low parse confidence gets a dashed outline rather than hidden data: the
     user sees that we are unsure instead of silently receiving less. */
  .card.low-confidence { outline: 1px dashed var(--border-strong); outline-offset: -1px; }

  /* The match score. A tabular numeral rather than a pill: at twenty-five rows
     a column of coloured badges becomes noise, while aligned figures stay
     scannable down the page. */
  .score {
    display: inline-flex; align-items: center; justify-content: center;
    min-width: 30px; height: 21px;
    padding: 0 5px;
    border-radius: var(--radius-sm);
    font-size: var(--t-sm);
    font-weight: 640;
    font-variant-numeric: tabular-nums;
    background: var(--bg-sunken);
    color: var(--fg-muted);
  }
  .score[data-band='strong']    { background: var(--grow-bg); color: var(--grow-ink); }
  .score[data-band='plausible'] { background: var(--accent-bg); color: var(--accent-ink); }
  .score[data-band='stretch']   { background: var(--uncertain-bg); color: var(--uncertain-ink); }

  /* The minus is what makes a gap legible without colour. Slightly heavier
     than the label so it reads as a sign rather than a hyphen. */
  .minus { font-weight: 600; margin-right: 3px; }

  /* Hollow rather than filled: this marks a row as unseen, which is a weaker
     claim than any of the score bands and should not compete with them. */
  .new-tag {
    font-size: var(--t-xs);
    font-weight: 560;
    color: var(--grow-ink);
    padding: 1px 6px;
    border-radius: var(--radius-sm);
    box-shadow: inset 0 0 0 1px rgb(13 148 136 / 0.35);
  }

  /* Matched skills are confirmation, not a decision: a quiet tick rather than a
     coloured block. The gap keeps the full chip treatment, so on a card with
     eight matches and one miss the eye lands on the miss. */
  .skills :global(.chip-match) {
    background: transparent;
    color: var(--fg-muted);
    box-shadow: inset 0 0 0 1px var(--ring);
  }
  .skills :global(.chip-match)::before {
    content: '';
    width: 5px; height: 5px;
    border-radius: var(--radius-full);
    background: var(--grow);
    flex: none;
  }

  .save { gap: 5px; }
  .save[aria-pressed='true'] { color: var(--accent-ink); background: var(--accent-bg); }
  .err { color: var(--shrink-ink); display: block; max-width: 22ch; }
  .retry {
    display: inline; padding: 0; border: 0; background: none;
    color: var(--accent); font: inherit; text-decoration: underline;
    cursor: pointer;
  }

  /* The rail. Full-bleed so it reads as an edge marker, not a decoration. */
  .rail { align-self: stretch; background: var(--border); }
  .card[data-freshness='fresh']  .rail { background: var(--grow); }
  .card[data-freshness='recent'] .rail { background: color-mix(in oklab, var(--grow) 55%, var(--border)); }
  .card[data-freshness='ageing'] .rail { background: var(--uncertain); }
  .card[data-freshness='stale']  .rail { background: var(--border-strong); }

  .body { padding: var(--card-pad); min-width: 0; }

  .head { display: flex; align-items: center; gap: var(--s-2); flex-wrap: wrap; }

  .title { font-size: var(--t-md); font-weight: 580; letter-spacing: var(--tr-md); }
  .title a { color: var(--fg); }
  .title a:hover { color: var(--accent); }

  .location {
    display: inline-block;
    max-width: 30ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    vertical-align: bottom;
  }

  /* Flex with wrap, so the single line becomes two on a narrow screen without
     a breakpoint deciding where. `row-gap` keeps the wrapped line legible
     rather than crammed against the one above it. */
  .facts {
    margin-top: 3px;
    display: flex; flex-wrap: wrap; align-items: baseline;
    column-gap: var(--s-2); row-gap: 2px;
  }
  .fact { display: inline-flex; align-items: center; gap: 2px; }

  /* Undisclosed compensation is muted but present. Removing the row would read
     as "we did not check"; muting it reads as "the employer did not say". */
  .fact.undisclosed { color: var(--fg-subtle); font-style: italic; }

  .age[data-freshness='fresh']  { color: var(--grow-ink); font-weight: 600; }
  .age[data-freshness='ageing'] { color: var(--uncertain-ink); }
  .age[data-freshness='stale']  { color: var(--fg-subtle); }

  .via { color: var(--fg-subtle); }

  .hint {
    font-size: 10px;
    color: var(--fg-subtle);
    cursor: help;
  }

  .skills {
    display: flex; flex-wrap: wrap; gap: var(--s-1);
    margin-top: var(--s-2);
  }

  .confidence-note {
    margin-top: var(--s-2);
    font-size: 12px;
    color: var(--uncertain-ink);
  }

  /* The action column is width-capped so Apply sits beside the content rather
     than pinned to the far edge of a 1400px card, where it reads as belonging
     to the page rather than to this posting. */
  /*
    A row, not a column.
    
    Three stacked buttons made a 116px-wide column about 110px tall, and every
    card inherited that as a floor — a two-line posting was mostly empty space,
    and a list of them read as loosely as a list of long ones. Card height is
    now set by the posting, so a dense result looks dense and a rich one looks
    rich, which is the difference a scanner actually uses.
    
    Aligned to the start rather than centred: the buttons sit level with the
    title, where the eye already is after reading it.
  */
  .actions {
    display: flex;
    align-items: flex-start;
    gap: var(--s-1);
    padding: var(--card-pad);
  }
  .actions :global(.btn) { white-space: nowrap; }
  .apply { white-space: nowrap; }

  /* The hide control is deliberately the quietest thing in the row. It is
     destructive-ish and one click, so it must not compete with Apply. */
  .hide-btn {
    color: var(--fg-subtle);
    white-space: nowrap;
  }

  .hide-btn:hover:not(:disabled) {
    color: var(--fg);
  }

  /* Same vertical rhythm as a card so the list does not jump when one
     collapses — the whole point of not removing the row. */
  .hidden-row {
    display: flex;
    align-items: center;
    gap: var(--s-3);
    flex-wrap: wrap;
    min-height: 44px;
    padding: var(--s-3) var(--s-4);
    border: 1px dashed var(--border);
    border-radius: var(--radius);
    background: var(--bg-sunken);
    font-size: var(--t-sm);
    color: var(--fg-muted);
  }

  .hidden-title {
    color: var(--fg);
    font-weight: 600;
  }
  .save { justify-content: center; }
  .opt-out { font-size: 11px; color: var(--fg-subtle); }

  @media (max-width: 640px) {
    .card { grid-template-columns: 3px 1fr; }
    .actions {
      grid-column: 2;
      width: auto;
      flex-direction: row; align-items: center; justify-content: flex-start;
      padding-top: 0;
    }
  }
</style>
