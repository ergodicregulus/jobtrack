<script lang="ts">
  import type { Facets } from '$lib/types';

  interface Props {
    facets: Facets | null;
    params: URLSearchParams;
    /** Rendered inside the narrow sheet, where the heading is supplied by the sheet. */
    inSheet?: boolean;
  }
  const { facets, params, inSheet = false }: Props = $props();

  /**
   * Every filter is a chip with a live count, and every chip is a LINK.
   *
   * Links, not buttons with JavaScript handlers, for three reasons that all
   * matter more than the extra markup:
   *
   *  1. It works with JavaScript off, which is the standing rule here.
   *  2. The state lives in the URL, so a filtered feed is shareable and the
   *     back button does what a back button should.
   *  3. Middle-click and cmd-click open a filtered view in a new tab, which is
   *     exactly how people compare two sets of results.
   *
   * The counts are the point. A filter chip with no number is an invitation to
   * click into zero results and conclude the product is empty — the count is
   * what turns filtering from a guess into a decision.
   */

  interface Chip {
    label: string;
    /** Query value; empty string clears the parameter. */
    value: string;
    count: number | null;
  }
  interface Group {
    key: string;
    label: string;
    hint: string;
    chips: Chip[];
    /** True when several values may be on at once (mode is, everything else is not). */
    multi?: boolean;
    note?: string;
  }

  const n = (v: number | undefined): number | null => (v === undefined ? null : v);

  const groups = $derived.by((): Group[] => {
    const f = facets;
    return [
      {
        key: 'mode',
        label: 'Work style',
        hint: 'pick any',
        multi: true,
        chips: [
          { label: 'Remote', value: 'remote', count: n(f?.modes?.remote) },
          { label: 'Hybrid', value: 'hybrid', count: n(f?.modes?.hybrid) },
          { label: 'On-site', value: 'onsite', count: n(f?.modes?.onsite) }
        ],
        note:
          f?.modes?.unknown
            ? `${f.modes.unknown.toLocaleString()} postings do not say. They are included unless you pick a style.`
            : undefined
      },
      {
        key: 'yoe',
        label: 'Experience',
        hint: 'the level the posting asks for',
        chips: [
          { label: '0–2 yrs', value: '2', count: n(f?.yoe?.['0-2']) },
          { label: '3–5 yrs', value: '5', count: n(f?.yoe?.['3-5']) },
          { label: '6–8 yrs', value: '8', count: n(f?.yoe?.['6-8']) },
          { label: '9+ yrs', value: '12', count: n(f?.yoe?.['9+']) }
        ],
        note: f?.yoe?.unstated
          ? `${f.yoe.unstated.toLocaleString()} postings state no range, so they are never filtered out by this.`
          : undefined
      },
      {
        key: 'comp_min',
        label: 'Pays at least',
        hint: 'published figures only',
        chips: [
          { label: '$100k', value: '100000', count: n(f?.comp?.['100000']) },
          { label: '$150k', value: '150000', count: n(f?.comp?.['150000']) },
          { label: '$200k', value: '200000', count: n(f?.comp?.['200000']) }
        ]
      },
      {
        key: 'country',
        label: 'Country',
        hint: 'where the role is based',
        chips: topCountries(f)
      },
      {
        key: 'field',
        label: 'Kind of work',
        hint: 'we hide what we can name as something else',
        chips: engineeringChips(f)
      },
      {
        key: 'posted_within',
        label: 'Posted',
        hint: 'freshness is the product',
        chips: [
          { label: 'Today', value: '1d', count: null },
          { label: 'This week', value: '7d', count: null },
          { label: 'Two weeks', value: '14d', count: null },
          { label: 'Any time', value: 'any', count: null }
        ]
      }
    ];
  });

  /**
   * Two chips, not three, from a three-way split.
   *
   * The column stores software / other / unknown, and the feed's default hides
   * only `other` — so "Engineering" here means software PLUS the ones we could
   * not classify. That is deliberate: the classifier can name 23% of the corpus
   * as software and leaves 43% unknown, and hiding what we cannot name would
   * hide software jobs along with everything else. See ADR-0018.
   *
   * Exposing the raw three would make the reader carry that reasoning. Two
   * choices — the filtered view and the unfiltered one — is the question they
   * actually have.
   */
  function engineeringChips(f: Facets | null): Chip[] {
    const counts = f?.fields;
    if (!counts) return [];
    const software = counts.software ?? 0;
    const unknown = counts.unknown ?? 0;
    const other = counts.other ?? 0;
    return [
      { label: 'Engineering', value: '', count: software + unknown },
      { label: 'Everything', value: 'all', count: software + unknown + other }
    ];
  }

  /** The five biggest, so the rail does not become a country list. */
  function topCountries(f: Facets | null): Chip[] {
    const entries = Object.entries(f?.countries ?? {})
      .filter(([code]) => code !== 'unknown')
      .sort((a, b) => b[1] - a[1])
      .slice(0, 5);
    return entries.map(([code, count]) => ({ label: code, value: code, count }));
  }

  const selected = $derived.by(() => {
    const out = new Map<string, Set<string>>();
    for (const g of groups) {
      const raw = params.get(g.key) ?? '';
      out.set(g.key, new Set(raw.split(',').filter(Boolean)));
    }
    // The freshness window has a default, so "nothing in the URL" still means
    // a chip is on. Without this the rail claims no filter while the feed is
    // filtered, which is the worst kind of disagreement.
    if (!params.get('posted_within')) out.set('posted_within', new Set(['7d']));
    // Same reason: the default kind-of-work filter is active with no parameter
    // in the URL, so the rail has to show it on or it claims an unfiltered feed
    // while the feed is filtered.
    if (!params.get('field')) out.set('field', new Set(['']));
    return out;
  });

  function isOn(groupKey: string, value: string): boolean {
    return selected.get(groupKey)?.has(value) ?? false;
  }

  /** The URL this chip would produce — toggling itself, resetting the cursor. */
  function href(g: Group, c: Chip): string {
    const next = new URLSearchParams(params);
    const current = selected.get(g.key) ?? new Set<string>();

    if (g.multi) {
      const set = new Set(current);
      if (set.has(c.value)) set.delete(c.value);
      else set.add(c.value);
      if (set.size) next.set(g.key, [...set].join(','));
      else next.delete(g.key);
    } else if (current.has(c.value)) {
      next.delete(g.key);
    } else {
      next.set(g.key, c.value);
    }

    // A cursor from the previous result set points into a list that no longer
    // exists, so changing a filter must always return to page one.
    next.delete('cursor');
    return `/jobs?${next}`;
  }

  const includeUndisclosed = $derived(params.get('comp_disclosed_only') !== 'true');

  function undisclosedHref(): string {
    const next = new URLSearchParams(params);
    if (includeUndisclosed) next.set('comp_disclosed_only', 'true');
    else next.delete('comp_disclosed_only');
    next.delete('cursor');
    return `/jobs?${next}`;
  }

  const hasComp = $derived(Boolean(params.get('comp_min')));
</script>

<div class="rail" class:sheet={inSheet}>
  {#each groups as g (g.key)}
    <section class="group">
      <div class="group-head">
        <h3 class="group-label">{g.label}</h3>
        <span class="t-micro hint">{g.hint}</span>
      </div>

      <div class="chips">
        {#each g.chips as c (c.value)}
          <!--
            aria-current, not aria-pressed. These are links, and aria-pressed
            is only defined on a button — a screen reader given it here either
            ignores it or announces something the role does not support.
            aria-current="true" is the correct way to say "this one is active"
            for an item in a set, and the sr-only word makes the action
            unambiguous rather than leaving the user to infer that clicking an
            active filter removes it.
          -->
          <a
            class="fchip"
            class:on={isOn(g.key, c.value)}
            href={href(g, c)}
            aria-current={isOn(g.key, c.value) ? 'true' : undefined}
            data-sveltekit-noscroll
            data-target-sm
          >
            <span>{c.label}</span>
            {#if c.count !== null}
              <span class="count mono">{c.count.toLocaleString()}</span>
            {/if}
            <span class="sr-only">
              {isOn(g.key, c.value) ? '(active — select to remove)' : ''}
            </span>
          </a>
        {/each}
      </div>

      {#if g.note}
        <p class="t-micro note">{g.note}</p>
      {/if}
    </section>
  {/each}

  <!--
    The undisclosed-salary choice, stated rather than made silently.

    Any salary filter necessarily excludes every posting that publishes no
    figure, and that is about a fifth of the market. Hiding that behind a
    filter would quietly remove a large slice of real jobs from someone's
    search without telling them — so it is a visible switch with the number on
    it, and it only appears once a salary filter is actually on.
  -->
  {#if hasComp && facets?.comp_undisclosed}
    <section class="group undisclosed">
      <a class="switch" href={undisclosedHref()} data-sveltekit-noscroll data-target>
        <span class="box" class:checked={includeUndisclosed} aria-hidden="true">
          {#if includeUndisclosed}✓{/if}
        </span>
        <span>
          <span class="switch-label">Include roles with no salary published</span>
          <span class="t-micro">
            <span class="mono">{facets.comp_undisclosed.toLocaleString()}</span> postings —
            about a fifth of the market. Excluding them is a real choice, so we
            never make it for you.
          </span>
        </span>
      </a>
    </section>
  {/if}
</div>

<style>
  .rail { display: flex; flex-direction: column; gap: var(--s-4h); }
  .rail.sheet { gap: var(--s-5); }

  .group { display: flex; flex-direction: column; gap: var(--s-2); }
  .group-head { display: flex; align-items: baseline; gap: var(--s-2); flex-wrap: wrap; }
  .group-label { font-size: var(--t-sm); font-weight: 600; letter-spacing: var(--tr-sm); }
  .hint { color: var(--fg-subtle); }

  .chips { display: flex; flex-wrap: wrap; gap: var(--s-1); }

  /* A chip is a link that looks like a control. 28px tall clears WCAG 2.2
     2.5.8 (24px) on a mouse; the pointer query in app.css takes it to 44px on
     touch, where the same 28px is a miss-tap. */
  .fchip {
    display: inline-flex; align-items: center; gap: var(--s-1);
    min-height: 28px;
    padding: 0 var(--s-2);
    border-radius: var(--radius-full);
    background: var(--bg-raised);
    box-shadow: inset 0 0 0 1px var(--ring-strong);
    color: var(--fg);
    font-size: var(--t-sm);
    text-decoration: none;
    transition: background var(--fast) var(--ease), box-shadow var(--fast) var(--ease);
  }
  .fchip:hover { background: var(--bg-hover); text-decoration: none; }
  .fchip.on {
    background: var(--accent);
    color: var(--accent-fg);
    box-shadow: none;
  }

  /* The count is information, not decoration: it is the thing that stops
     someone filtering blindly into zero results. */
  .count { font-size: var(--t-xs); color: var(--fg-subtle); font-variant-numeric: tabular-nums; }
  .fchip.on .count { color: var(--accent-fg); opacity: 0.8; }

  .note { color: var(--fg-subtle); line-height: 1.55; max-width: 34ch; }

  .undisclosed { padding-top: var(--s-3); box-shadow: 0 -1px 0 0 var(--ring); }
  .switch {
    display: flex; align-items: flex-start; gap: var(--s-2);
    color: inherit; text-decoration: none;
    padding: var(--s-1) 0;
  }
  .switch:hover { text-decoration: none; }
  .switch-label { display: block; font-size: var(--t-sm); }
  .box {
    flex: none;
    width: 16px; height: 16px; margin-top: 2px;
    display: grid; place-items: center;
    border-radius: var(--radius-sm);
    box-shadow: inset 0 0 0 1px var(--ring-strong);
    font-size: 11px; line-height: 1;
  }
  .box.checked { background: var(--accent); color: var(--accent-fg); box-shadow: none; }
</style>
