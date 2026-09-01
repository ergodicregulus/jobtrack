<script lang="ts">
  /**
   * How the list is ordered.
   *
   * The API has accepted `newest`, `comp` and `match` since the feed was built
   * and nothing ever exposed them, so every reader has been looking at one
   * ordering with no way to change it. Sorting a job search by salary or by
   * freshness is not an advanced feature; it is most of what a person does with
   * a list of jobs.
   *
   * Links, not a <select>. The filters are links for the same reason — the page
   * works with JavaScript off, every ordering is a shareable URL, and the back
   * button means what it says. A select would need a submit button or a script
   * to be usable at all.
   */
  interface Props {
    params: URLSearchParams;
    /** Best match needs a profile to match against. */
    canMatch?: boolean;
  }
  const { params, canMatch = false }: Props = $props();

  const options = $derived(
    [
      { value: 'newest', label: 'Newest' },
      { value: 'comp', label: 'Salary' },
      canMatch ? { value: 'match', label: 'Best match' } : null
    ].filter((o) => o !== null)
  );

  /**
   * The active option, including the one nobody chose.
   *
   * The loader applies `match` for a signed-in reader with a finished profile
   * and `newest` for everyone else, so "no sort in the URL" is still a sort. A
   * control that showed nothing selected would be claiming the list is unordered
   * when it is not.
   */
  const active = $derived(params.get('sort') ?? (canMatch ? 'match' : 'newest'));

  function href(value: string): string {
    const next = new URLSearchParams(params);
    next.set('sort', value);
    // A cursor points into the previous ordering. Keeping it would page into
    // the middle of a list that no longer exists.
    next.delete('cursor');
    return `/jobs?${next}`;
  }
</script>

<div class="sort" role="group" aria-label="Sort order">
  <span class="label t-micro">Sort</span>
  {#each options as o (o.value)}
    <a
      class="opt"
      class:on={o.value === active}
      href={href(o.value)}
      aria-current={o.value === active ? 'true' : undefined}
      data-sveltekit-noscroll
      data-target-sm
    >
      {o.label}
    </a>
  {/each}
</div>

{#if active === 'comp'}
  <!--
    Said out loud, because the alternative reads as a broken sort. Only about a
    seventh of postings publish a salary; the rest have nothing to order by and
    sit at the end. Silently showing a reader a long tail of blank salaries
    after the paid ones would look like the sort gave up halfway.
  -->
  <p class="caveat t-small">
    Roles that publish no salary cannot be ordered, so they come last.
  </p>
{/if}

<style>
  .sort {
    display: flex;
    align-items: center;
    gap: var(--s-1);
    flex-wrap: wrap;
  }

  .label {
    color: var(--fg-subtle);
    margin-right: var(--s-1);
  }

  .opt {
    /* 24px floor for a link, SC 2.5.8. */
    min-height: 28px;
    display: inline-flex;
    align-items: center;
    padding: 0 var(--s-2);
    border-radius: var(--radius-sm);
    color: var(--fg-muted);
    font-size: var(--t-sm);
    text-decoration: none;
    transition: color 120ms ease, background 120ms ease;
  }

  .opt:hover {
    color: var(--fg);
    background: var(--bg-hover);
  }

  .opt.on {
    color: var(--fg);
    font-weight: 600;
    background: var(--bg-active);
  }

  .caveat {
    margin: 0 0 var(--s-2);
    color: var(--fg-subtle);
    text-align: right;
  }
</style>
