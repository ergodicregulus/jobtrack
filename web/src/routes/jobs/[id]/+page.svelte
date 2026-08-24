<script lang="ts">
  import { untrack } from 'svelte';
  import { mutate, type MutationFailure } from '$lib/mutate.svelte';
  import { formatComp, formatYoE, formatMode, formatVendor, relativeAge } from '$lib/format';
  import type { PageData } from './$types';

  const { data }: { data: PageData } = $props();
  const j = $derived(data.posting);

  const comp = $derived(formatComp(j.comp_min, j.comp_max, j.comp_currency, j.comp_period));
  const yoe = $derived(formatYoE(j.yoe_min, j.yoe_max, j.yoe_confidence));
  const mode = $derived(formatMode(j.mode));
  const vendor = $derived(formatVendor(j.ats_vendor));

  const bandLabel: Record<string, string> = {
    strong: 'Strong fit',
    plausible: 'Worth a look',
    stretch: 'A stretch',
    unlikely: 'Long shot'
  };

  // Component order is the weighting order, heaviest first, so the reader sees
  // what mattered most before what mattered least.
  const componentOrder = ['skills', 'experience', 'location', 'compensation', 'freshness'];
  const components = $derived(
    [...(j.match?.components ?? [])].sort(
      (a, b) => componentOrder.indexOf(a.name) - componentOrder.indexOf(b.name)
    )
  );

  const componentLabel: Record<string, string> = {
    skills: 'Skills',
    experience: 'Experience',
    location: 'Location',
    compensation: 'Compensation',
    freshness: 'Freshness',
    parse: 'Readability'
  };

  let saved = $state(untrack(() => j.saved));
  let saving = $state(false);
  let failure = $state<MutationFailure | null>(null);

  /**
   * A posting can close while this page is open — someone reads a description
   * for ten minutes, and the role is filled in the ninth.
   *
   * The page cannot know that on its own without polling, which would cost a
   * request per reader per interval for a rare event. But the first write after
   * the closure finds out immediately: the API only serves live postings, so a
   * save comes back 404. That is the honest trigger, and it is free.
   *
   * What this does NOT do is claim the employer's own posting is gone. Our
   * index dropped it; the ATS page may well still accept an application. So
   * Apply stays clickable and the notice says what we actually know.
   */
  let closed = $state(false);

  $effect(() => {
    saved = j.saved;
  });

  async function toggleSave() {
    if (saving || closed) return;
    const next = !saved;
    saved = next;
    saving = true;
    failure = null;

    failure = await mutate(
      () => fetch(`/v1/me/saved/${j.id}`, { method: next ? 'PUT' : 'DELETE' }),
      () => { saved = !next; }
    );
    if (failure?.gone) closed = true;
    saving = false;
  }

  const lowConfidence = $derived(j.parse_confidence < 0.5);
</script>

<svelte:head>
  <title>{j.title} · {j.company_name} · JobTrack</title>
</svelte:head>

<div class="shell page">
  <!-- Back to the feed rather than browser-back alone: someone arriving from a
       shared link has no history to go back to. -->
  <a class="back t-small" href="/jobs">← All jobs</a>

  <header class="head">
    <div class="head-main">
      {#if j.match}
        <span class="band" data-band={j.match.band}>
          <span class="band-score num">{Math.round(j.match.score)}</span>
          {bandLabel[j.match.band] ?? j.match.band}
        </span>
      {/if}
      <h1>{j.title}</h1>
      <p class="meta">
        <span class="company">{j.company_name}</span>
        <span class="meta-sep">·</span>
        <span>{j.location_raw || 'Location not stated'}</span>
        {#if mode && !j.location_raw.toLowerCase().includes(j.mode.toLowerCase())}
          <span class="meta-sep">·</span><span>{mode}</span>
        {/if}
        <span class="meta-sep">·</span>
        <span>{relativeAge(j.posted_at, j.first_seen_at)}</span>
        {#if j.posted_at_is_estimate}
          <span class="est" title="Estimated from the employer's last update; the source publishes no first-posted date">
            estimated
          </span>
        {/if}
      </p>
    </div>

    <div class="head-actions">
      <a class="btn btn-primary btn-lg" href={j.apply_url} target="_blank" rel="noopener noreferrer">
        Apply on {j.company_name}
      </a>
      {#if data.signedIn}
        <button
          class="btn"
          aria-pressed={saved}
          onclick={toggleSave}
          disabled={saving || closed}
          aria-busy={saving}
        >
          {saved ? 'Saved' : 'Save'}
        </button>
        {#if failure && !closed}
          <span class="t-micro err" role="alert">
            {failure.message}
            {#if failure.retryable}
              <button class="linkish" onclick={toggleSave}>Try again</button>
            {:else if failure.needsAuth}
              <a href="/login?next=/jobs/{j.id}">Sign in</a>
            {/if}
          </span>
        {/if}
      {:else}
        <a class="btn" href="/login?next=/jobs/{j.id}">Sign in to save</a>
      {/if}
      <span class="t-micro via">via {vendor}</span>
    </div>
  </header>

  {#if closed}
    <p class="caution" role="alert">
      <strong>This posting closed while you were reading it.</strong>
      It is no longer in our index, so it cannot be saved or tracked. The
      employer's own page may still be up — the Apply button above still goes
      there.
      <a href="/jobs">Back to the feed</a>
    </p>
  {/if}

  {#if lowConfidence}
    <p class="caution" role="note">
      <strong>We understood less than half of this posting.</strong>
      Some fields below may be missing or wrong. Read it on
      {j.company_name}'s own page before deciding.
    </p>
  {/if}

  <div class="grid">
    <article class="col-main">
      <section class="panel">
        <div class="panel-head">
          <h2 class="t-heading">The posting, as the employer published it</h2>
        </div>
        <!--
          The employer's own markup. It is sanitised at ingest — we never render
          untrusted HTML as-is — and reproduced rather than reformatted, because
          rewriting an employer's words is how a job board introduces errors it
          then cannot explain.
        -->
        <div class="description">
          {@html j.description_html}
        </div>
      </section>
    </article>

    <aside class="col-side">
      <section class="panel">
        <div class="panel-head"><h2 class="t-heading">How well does this fit you?</h2></div>

        {#if !data.signedIn}
          <p class="t-small">
            Scores need a profile — there is no way to tell you what you are
            missing without knowing what you have.
          </p>
          <a class="btn btn-sm btn-primary mt" href="/signup">Create an account</a>
        {:else if !j.match}
          <p class="t-small">
            Not scored yet. Scoring runs in the background and usually catches up
            within a minute or two of a posting arriving.
          </p>
        {:else}
          <ul class="components">
            {#each components as c (c.name)}
              <li class="component" class:abstained={c.neutral}>
                <div class="component-head">
                  <span class="component-name">{componentLabel[c.name] ?? c.name}</span>
                  <span class="component-figure num">
                    {#if c.neutral}
                      —
                    {:else}
                      {c.score.toFixed(0)} / {c.max.toFixed(0)}
                    {/if}
                  </span>
                </div>
                <!--
                  A hollow bar for a component that abstained, not an empty one.
                  "We could not judge this" and "this scored nothing" are
                  different statements and must not look the same.
                -->
                <div class="bar" class:hollow={c.neutral}>
                  {#if !c.neutral}
                    <div class="bar-fill" style:width={`${(c.score / c.max) * 100}%`}></div>
                  {/if}
                </div>
                <p class="component-detail t-micro">{c.detail}</p>
              </li>
            {/each}
          </ul>

          {#if j.match.missing_skills.length}
            <p class="gaps">
              <span class="t-micro">You are missing:</span>
              {j.match.missing_skills.join(', ')}
            </p>
          {:else}
            <p class="t-micro mt">Missing nothing they marked as required.</p>
          {/if}

          {#if j.match.confidence < 1}
            <p class="t-micro caution-inline">
              Confidence is reduced because part of this posting could not be read.
            </p>
          {/if}

          <p class="weights t-micro">
            Weights are fixed and shown: skills 40, experience 20, location 15,
            compensation 15, freshness 10. A component we could not judge says so
            rather than scoring zero.
          </p>
        {/if}
      </section>

      <section class="panel">
        <div class="panel-head"><h2 class="t-heading">Compensation</h2></div>
        <p class="comp-figure" class:undisclosed={!comp.disclosed}>{comp.text}</p>
        {#if comp.disclosed && j.comp_source === 'parsed_text'}
          <p class="t-micro">
            Read out of the description, not a published salary field — treat it
            as the employer's wording rather than a structured figure.
          </p>
        {:else if !comp.disclosed}
          <p class="t-micro">
            This employer published no salary. That is not the same as a low one
            — about a fifth of postings disclose nothing.
          </p>
        {/if}
        {#if yoe}
          <p class="t-small mt"><strong>Experience:</strong> {yoe}</p>
        {/if}
      </section>

      {#if j.ai_screening_disclosed}
        <section class="panel">
          <div class="panel-head"><h2 class="t-heading">AI screening</h2></div>
          <p class="t-small">
            This employer discloses that AI is used in their hiring process.
            Reported without comment: fewer than half of organisations use it at
            all, and its presence is not evidence of a bad process.
          </p>
          {#if j.ai_disclaimer}
            <p class="t-micro mt">{j.ai_disclaimer}</p>
          {/if}
          {#if j.ai_opt_out_url}
            <a class="t-small" href={j.ai_opt_out_url} target="_blank" rel="noopener noreferrer">
              Their opt-out process →
            </a>
          {/if}
        </section>
      {/if}
    </aside>
  </div>
</div>

<style>
  .page { display: flex; flex-direction: column; gap: var(--s-4); padding-top: var(--s-5); }

  .back { align-self: flex-start; color: var(--fg-muted); }
  .back:hover { color: var(--fg); }

  .head { display: flex; align-items: flex-start; gap: var(--s-5); flex-wrap: wrap; }
  .head-main { flex: 1; min-width: 280px; display: flex; flex-direction: column; gap: var(--s-2); align-items: flex-start; }
  .head h1 { font-size: var(--t-2xl); letter-spacing: var(--tr-2xl); }

  .head-actions {
    display: flex; flex-direction: column; gap: var(--s-2);
    align-items: stretch; min-width: 190px;
  }
  .via { color: var(--fg-subtle); text-align: center; }
  .err { color: var(--shrink-ink); }
  .linkish {
    display: inline; padding: 0; margin-left: var(--s-1); border: 0;
    background: none; color: inherit; font: inherit;
    text-decoration: underline; cursor: pointer;
  }

  .band {
    display: inline-flex; align-items: center; gap: var(--s-2);
    padding: 3px var(--s-2);
    border-radius: var(--radius-sm);
    font-size: var(--t-sm);
    font-weight: 560;
    background: var(--bg-sunken);
    color: var(--fg-muted);
  }
  .band-score { font-weight: 660; }
  .band[data-band='strong']    { background: var(--grow-bg);      color: var(--grow-ink); }
  .band[data-band='plausible'] { background: var(--accent-bg);    color: var(--accent-ink); }
  .band[data-band='stretch']   { background: var(--uncertain-bg); color: var(--uncertain-ink); }

  .company { font-weight: 560; color: var(--fg); }
  .est { color: var(--uncertain-ink); font-size: var(--t-xs); cursor: help; }

  .caution {
    padding: var(--s-3) var(--s-4);
    border-radius: var(--radius);
    background: var(--uncertain-bg);
    color: var(--uncertain-ink);
    font-size: var(--t-sm);
    box-shadow: inset 0 0 0 1px rgb(217 119 6 / 0.28);
  }
  .caution-inline { color: var(--uncertain-ink); margin-top: var(--s-2); }

  .grid { display: grid; grid-template-columns: minmax(0, 1.6fr) minmax(280px, 1fr); gap: var(--s-4); align-items: start; }
  .col-side { display: flex; flex-direction: column; gap: var(--s-4); }

  /* A measure, not the full column width. Long prose set to 1000px is unreadable
     however much space there is. */
  .description { max-width: 68ch; font-size: var(--t-base); line-height: 1.6; }
  .description :global(h1),
  .description :global(h2),
  .description :global(h3) {
    font-size: var(--t-md); font-weight: 600; margin: var(--s-5) 0 var(--s-2);
  }
  .description :global(p) { margin: 0 0 var(--s-3); }
  .description :global(ul), .description :global(ol) {
    margin: 0 0 var(--s-3); padding-left: var(--s-5); list-style: disc;
  }
  .description :global(li) { margin-bottom: var(--s-1); }
  .description :global(a) { color: var(--accent); }
  .description :global(strong) { font-weight: 600; }
  /* An employer's table must scroll inside itself; the page never scrolls
     sideways. */
  .description :global(table) { display: block; overflow-x: auto; max-width: 100%; }

  .components { display: flex; flex-direction: column; gap: var(--s-4); }
  .component-head { display: flex; align-items: baseline; gap: var(--s-3); }
  .component-name { flex: 1; font-size: var(--t-base); font-weight: 540; }
  .component-figure { font-size: var(--t-sm); color: var(--fg-muted); }

  .bar { height: 4px; margin: 6px 0 4px; background: var(--bg-sunken); border-radius: var(--radius-full); }
  .bar-fill { height: 100%; background: var(--grow); border-radius: inherit; }
  /* Hollow, not empty: an outline says "not judged", a bar at zero says "scored
     nothing", and they are different claims. */
  .bar.hollow { background: transparent; box-shadow: inset 0 0 0 1px var(--ring-strong); }

  .component-detail { color: var(--fg-subtle); }
  .abstained .component-name { color: var(--fg-muted); }

  .gaps { margin-top: var(--s-4); font-size: var(--t-sm); color: var(--shrink-ink); }
  .weights { margin-top: var(--s-4); color: var(--fg-subtle); }
  .mt { margin-top: var(--s-2); }

  .comp-figure { font-family: var(--font-mono); font-size: var(--t-lg); font-variant-numeric: tabular-nums; }
  .comp-figure.undisclosed { font-family: var(--font); font-size: var(--t-base); font-style: italic; color: var(--fg-muted); }

  @media (max-width: 900px) {
    .grid { grid-template-columns: 1fr; }
    /* The score comes before the description on a narrow screen: it is the
       decision, and the description is the detail behind it. */
    .col-side { order: -1; }
  }

  @media (pointer: coarse) {
    .head-actions :global(.btn) { min-height: 44px; }
  }
</style>
