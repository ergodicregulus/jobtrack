<script lang="ts">
  import JobCard from '$lib/components/JobCard.svelte';
  import FilterRail from '$lib/components/FilterRail.svelte';
  import FilterSearch from '$lib/components/FilterSearch.svelte';
  import { attachFeedKeys } from '$lib/keyboard.svelte';
  import type { PageData } from './$types';

  interface Props { data: PageData; }
  const { data }: Props = $props();

  const params = $derived(new URLSearchParams(data.params));
  const jobs = $derived(data.feed.data);

  // "New since your last visit" — about THIS reader, unlike "posted today",
  // which is a fact about the market and identical for everyone.
  //
  // Suppressed on a first visit, when everything would be flagged: a page where
  // every row is tagged tags nothing.
  const since = $derived(data.feed.since_last_visit ? new Date(data.feed.since_last_visit) : null);
  const newCount = $derived(
    since ? jobs.filter((j) => new Date(j.first_seen_at) > since).length : 0
  );
  const showSince = $derived(since !== null && newCount > 0 && newCount < jobs.length);
  const sinceLabel = $derived(
    since ? since.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' }) : ''
  );

  // The "fresh today" count is the headline because it answers the question the
  // product exists to answer: is there anything worth acting on right now?
  const freshCount = $derived(
    jobs.filter((j) => {
      const when = new Date(j.posted_at ?? j.first_seen_at).getTime();
      return (Date.now() - when) / 36e5 <= 24;
    }).length
  );

  // Enhancement only: every one of these actions is reachable by Tab and a
  // click. Returning the cleanup from $effect removes the listener on unmount,
  // so it cannot start acting on the next page.
  $effect(() => attachFeedKeys());

  function nextPageHref(cursor: string): string {
    const next = new URLSearchParams(data.params);
    next.set('cursor', cursor);
    return `/jobs?${next}`;
  }

  /** Names the filter most likely responsible for an empty result set. */
  function likelyCulprit(p: URLSearchParams): string | null {
    if (p.get('q')) return `the search term "${p.get('q')}"`;
    if (p.get('mode')) return 'the work-mode filter';
    const window = p.get('posted_within');
    if (window && window !== 'any') return `the "${window}" freshness window`;
    return null;
  }

  /**
   * Every filter currently on, as removable chips.
   *
   * Derived from the URL rather than tracked separately, so it cannot drift
   * out of step with the feed it claims to describe — the bug this shape
   * prevents is a chip saying "Remote" after the parameter has already gone.
   */
  interface ActiveFilter { key: string; value: string; label: string; removeHref: string; }

  const FILTER_LABELS: Record<string, (v: string) => string> = {
    mode: (v) => ({ remote: 'Remote', hybrid: 'Hybrid', onsite: 'On-site' })[v] ?? v,
    yoe: (v) => `Up to ${v} yrs`,
    comp_min: (v) => `$${(Number(v) / 1000).toFixed(0)}k+`,
    country: (v) => v,
    q: (v) => `“${v}”`,
    skills: (v) => v,
    vendor: (v) => `via ${v}`,
    posted_within: (v) =>
      ({ '1d': 'Today', '24h': 'Today', '3d': 'Last 3 days', '7d': 'This week',
         '14d': 'Two weeks', any: 'Any time' })[v] ?? v,
    comp_disclosed_only: () => 'Salary published only'
  };

  const activeFilters = $derived.by((): ActiveFilter[] => {
    const out: ActiveFilter[] = [];
    for (const [key, label] of Object.entries(FILTER_LABELS)) {
      const raw = params.get(key);
      if (!raw) continue;
      // The default freshness window is not a filter the user chose, so
      // showing it as removable would invite them to "clear" something they
      // never set.
      if (key === 'posted_within' && raw === '7d') continue;

      for (const value of raw.split(',').filter(Boolean)) {
        const next = new URLSearchParams(params);
        const rest = raw.split(',').filter((v) => v !== value);
        if (rest.length) next.set(key, rest.join(','));
        else next.delete(key);
        next.delete('cursor');
        out.push({ key, value, label: label(value), removeHref: `/jobs?${next}` });
      }
    }
    return out;
  });

  const clearHref = '/jobs';

  function relaxedHref(p: URLSearchParams): string {
    const relaxed = new URLSearchParams(p);
    if (relaxed.get('q')) relaxed.delete('q');
    else if (relaxed.get('mode')) relaxed.delete('mode');
    else relaxed.set('posted_within', 'any');
    relaxed.delete('cursor');
    return `/jobs?${relaxed}`;
  }
</script>

<div class="shell page">
  <!--
    The page's real heading, announced but not drawn.

    "25 roles" reads as a heading visually but is a result COUNT, and it sat at
    h1 while the filter rail's "Filters" h2 came before it in the DOM — so the
    document opened at h2, jumped back to h1, then skipped to h3 at the first
    job card. A screen-reader user navigating by heading got no page title at
    all and a broken outline underneath it.
  -->
  <h1 class="sr-only">Jobs</h1>

  <FilterSearch {params} />

  <div data-feed-grid>
    <!--
      The filter rail. Grouped chips with live counts, one group per question a
      reader actually asks: how do I work, how senior is it, what does it pay,
      where is it, how fresh.

      This replaced a horizontal bar of three mode chips plus a freshness
      select. The bar could not grow — a fourth group would have wrapped into a
      second row and a fifth into a third, pushing the results below the fold
      to make room for controls nobody had touched yet.
    -->
    <aside data-wide-only aria-label="Filters">
      <div class="rail-head">
        <h2 class="t-heading">Filters</h2>
        {#if activeFilters.length}
          <span class="spacer"></span>
          <a class="t-small" href={clearHref}>Clear all</a>
        {/if}
      </div>
      <FilterRail facets={data.facets} {params} />
    </aside>

    <div class="results-col">
      <!-- Narrow screens get a disclosure instead of a rail: 272px of filters
           above the first result on a phone is a screen of controls before any
           content. The count on the button is what makes it safe to collapse —
           a hidden filter you cannot see the effect of is how people end up
           staring at an empty list. -->
      <details class="sheet" data-narrow-only>
        <summary data-target>
          <span>Filters</span>
          {#if activeFilters.length}<span class="badge mono">{activeFilters.length}</span>{/if}
        </summary>
        <div class="sheet-body">
          <!--
            The rail's groups are h3, which on desktop sit under the aside's
            "Filters" h2. In the sheet that h2 does not exist — the label is a
            <summary>, which is not a heading — so the groups were orphaned and
            the outline jumped h1 to h3. A heading inside <summary> would be
            announced twice; an sr-only one here is announced once, in the
            right place.
          -->
          <h2 class="sr-only">Filters</h2>
          <FilterRail facets={data.facets} {params} inSheet />
        </div>
      </details>

      <!-- Active filters, individually removable. A "clear all" alone forces
           someone to rebuild four filters to drop one. -->
      {#if activeFilters.length}
        <div class="active" aria-label="Active filters">
          <span class="t-micro">Filtered by</span>
          {#each activeFilters as a (a.key + a.value)}
            <a class="active-chip" href={a.removeHref}>
              {a.label}
              <span class="x" aria-hidden="true">×</span>
              <span class="sr-only">Remove filter {a.label}</span>
            </a>
          {/each}
          <a class="t-micro clear" href={clearHref}>Clear all</a>
        </div>
      {/if}

{#if data.error}
  <!-- Degrade visibly, never silently. A blank list would read as "there are no
       jobs", which is a different and untrue statement. -->
  <div class="notice notice-error" role="alert">
    <p><strong>{data.error}</strong></p>
    <p>The feed could not be loaded. This is our problem, not yours — try again shortly.</p>
  </div>
{/if}

      <section id="results" aria-label="Job results" tabindex="-1">
  <div class="results-head">
    <h2 class="results-title">
      {#if jobs.length}
        <span class="mono">{jobs.length}</span><span class="unit">role{jobs.length === 1 ? '' : 's'}</span>
        {#if freshCount > 0}
          <span class="fresh-badge">
            <span class="fresh-dot" aria-hidden="true"></span>
            <span class="mono">{freshCount}</span> posted today
          </span>
        {/if}
      {:else}
        No matching roles
      {/if}
    </h2>
  </div>

  <!-- Announced politely so a screen-reader user learns that filtering did
       something, rather than silently receiving a different list. -->
  <p class="sr-only" aria-live="polite">
    {jobs.length} results, {freshCount} posted in the last 24 hours.
  </p>

  {#if jobs.length}
    {#if since && newCount === 0}
      <p class="since-none t-small">
        Nothing new since {sinceLabel}. These are the closest matches still open.
      </p>
    {/if}

    <ul class="results">
      {#each jobs as job, i (job.id)}
        {#if showSince && i === newCount}
          <li class="since-divider" aria-hidden="true">
            <span>Everything below was here on your last visit, {sinceLabel}</span>
          </li>
        {/if}
        <JobCard {job} signedIn={data.signedIn} isNew={since !== null && new Date(job.first_seen_at) > since && showSince} />
      {/each}
    </ul>

    <p class="keys t-micro">
      <kbd>j</kbd> <kbd>k</kbd> to move · <kbd>Enter</kbd> to open ·
      <kbd>s</kbd> to save
    </p>

    {#if data.feed.has_more && data.feed.next_cursor}
      <nav class="pager" aria-label="Pagination">
        <a class="btn" href={nextPageHref(data.feed.next_cursor)} rel="next">
          Next page →
        </a>
      </nav>
    {/if}
  {:else if !data.error}
    <!-- An empty state that names the likely cause and offers one click out of
         it. "No results" alone leaves the user to guess which of six filters
         did it. -->
    <div class="notice">
      <p><strong>Nothing matched these filters.</strong></p>
      {#if likelyCulprit(params)}
        <p>The most likely cause is {likelyCulprit(params)}.</p>
      {/if}
      <p class="empty-actions">
        <a class="btn" href={relaxedHref(params)}>Relax that filter</a>
        <a class="btn" href="/">Clear everything</a>
      </p>
    </div>
  {/if}
      </section>
    </div>
  </div>
</div>

<style>
  .rail-head {
    display: flex; align-items: baseline; gap: var(--s-2);
    margin-bottom: var(--s-4);
  }
  .spacer { flex: 1; }

  .results-col { min-width: 0; display: flex; flex-direction: column; gap: var(--s-3); }

  /* A <details> rather than a modal. It needs no JavaScript, the browser
     handles the toggle and its accessibility, and it pushes content down
     instead of trapping focus over it — which for a filter panel is the right
     behaviour, because you want to see the count change as you pick. */
  .sheet { border-radius: var(--radius); background: var(--bg-raised); box-shadow: var(--e-1); }
  .sheet summary {
    display: flex; align-items: center; gap: var(--s-2);
    padding: var(--s-3) var(--s-4);
    font-size: var(--t-base); font-weight: 530;
    cursor: pointer;
    list-style: none;
  }
  .sheet summary::-webkit-details-marker { display: none; }
  .sheet summary::after {
    content: '';
    margin-left: auto;
    width: 7px; height: 7px;
    border-right: 1.5px solid var(--fg-subtle);
    border-bottom: 1.5px solid var(--fg-subtle);
    transform: rotate(45deg) translateY(-2px);
    transition: transform var(--fast) var(--ease);
  }
  .sheet[open] summary::after { transform: rotate(-135deg) translateY(-2px); }
  .sheet-body { padding: 0 var(--s-4) var(--s-4); }
  .badge {
    min-width: 18px; height: 18px;
    display: inline-grid; place-items: center;
    padding: 0 5px;
    border-radius: var(--radius-full);
    background: var(--accent); color: var(--accent-fg);
    font-size: var(--t-xs);
  }

  .active { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-1); }
  .active > .t-micro:first-child { color: var(--fg-subtle); margin-right: var(--s-1); }
  .active-chip {
    display: inline-flex; align-items: center; gap: var(--s-1);
    min-height: 26px; padding: 0 var(--s-2);
    border-radius: var(--radius-full);
    background: var(--accent-bg);
    color: var(--accent-ink);
    box-shadow: inset 0 0 0 1px var(--accent-line);
    font-size: var(--t-sm);
    text-decoration: none;
  }
  .active-chip:hover { background: var(--bg-hover); text-decoration: none; }
  .active-chip .x { font-size: 14px; line-height: 1; opacity: 0.75; }
  .clear { margin-left: var(--s-1); }
  .page { padding-top: var(--s-5); }

  .results-head {
    display: flex; align-items: baseline; justify-content: space-between;
    padding: var(--s-3) 0 var(--s-2);
  }

  .results-title {
    display: flex; align-items: center; gap: var(--s-3);
    font-size: 15px; font-weight: 600;
  }

  .fresh-badge {
    display: inline-flex; align-items: center; gap: var(--s-1);
    padding: 2px var(--s-2);
    font-size: 12px; font-weight: 500;
    color: var(--grow-ink);
    background: var(--grow-bg);
    border: 1px solid var(--grow);
    border-radius: 999px;
  }

  .fresh-dot {
    width: 6px; height: 6px; border-radius: 50%;
    background: var(--grow);
  }

  /* The monospace count and the word after it are separate elements, so the
     gap between them comes from the mono font's advance width rather than a
     space — which rendered as an obvious double space. */
  .unit { margin-left: 0.35em; }

  .results {
    display: flex; flex-direction: column;
    gap: var(--row-gap);
  }

  /* Stated rather than hidden: a shortcut nobody knows about helps nobody.
     Sits below the list, where it is findable without competing with results. */
  .keys { margin-top: var(--s-4); color: var(--fg-subtle); }
  .keys kbd {
    font-family: var(--font-mono);
    font-size: var(--t-xs);
    padding: 1px 5px;
    border-radius: var(--radius-sm);
    background: var(--bg-sunken);
    box-shadow: inset 0 0 0 1px var(--ring);
  }

  .since-none {
    padding: var(--s-3) 0;
    color: var(--fg-muted);
  }

  /* A rule with a label, not a heading. It marks a boundary in a list rather
     than starting a new section, so it must not appear in the document outline
     or be announced as one. */
  .since-divider {
    display: flex; align-items: center; gap: var(--s-3);
    margin: var(--s-3) 0 var(--s-1);
    font-size: var(--t-xs);
    color: var(--fg-subtle);
    white-space: nowrap;
  }
  .since-divider::after {
    content: '';
    flex: 1;
    height: 1px;
    background: var(--ring);
  }

  .pager { display: flex; justify-content: center; padding: var(--s-5) 0; }

  .notice {
    padding: var(--s-5);
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    text-align: center;
    color: var(--fg-muted);
  }
  .notice p + p { margin-top: var(--s-2); }
  .notice-error { border-color: var(--shrink); background: var(--shrink-bg); color: var(--shrink-ink); }

  .empty-actions {
    display: flex; gap: var(--s-2); justify-content: center;
    margin-top: var(--s-4);
  }
</style>
