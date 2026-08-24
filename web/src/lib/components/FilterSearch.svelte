<script lang="ts">
  interface Props { params: URLSearchParams; }
  const { params }: Props = $props();

  const query = $derived(params.get('q') ?? '');

  /**
   * Search is its own control, above the results and separate from the rail.
   *
   * A plain GET form: filter state lives in the URL, which makes a filtered
   * view shareable, back-button-correct, and functional with no JavaScript.
   *
   * The hidden inputs are what makes searching NON-destructive. A GET form
   * submits only its own fields, so without them typing a query would silently
   * drop every chip the reader had set — they would search within "remote,
   * $150k+, this week" and get results from the whole corpus, with the rail
   * still showing the filters as active. That disagreement is the worst
   * failure this page can have.
   */
  const carried = $derived(
    [...params.entries()].filter(([k]) => k !== 'q' && k !== 'cursor')
  );
</script>

<form
  data-search-alt
  class="search-form"
  method="GET"
  action="/jobs"
  role="search"
  aria-label="Search jobs"
>
  {#each carried as [key, value] (key + value)}
    <input type="hidden" name={key} {value} />
  {/each}

  <label class="search">
    <span class="sr-only">Search job titles, skills and companies</span>
    <svg class="search-icon" viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
      <circle cx="7" cy="7" r="4.5" fill="none" stroke="currentColor" stroke-width="1.5" />
      <path d="M10.5 10.5L14 14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
    </svg>
    <input
      type="search"
      name="q"
      value={query}
      placeholder="Search roles, skills, companies…"
      autocomplete="off"
    />
  </label>

  <button class="btn btn-primary" type="submit">Search</button>
</form>

<style>
  /* display is owned by [data-search-alt] in app.css, which pairs this with
     the header's search so exactly one is visible at any width. */
  .search-form { gap: var(--s-2); align-items: center; }

  .search {
    position: relative;
    flex: 1; min-width: 0;
    display: flex; align-items: center;
  }
  .search-icon {
    position: absolute; left: var(--s-3);
    color: var(--fg-faint); /* non-text */
    pointer-events: none;
  }
  .search input {
    width: 100%;
    min-height: 36px;
    padding: 0 var(--s-3) 0 34px;
    border: 0;
    border-radius: var(--radius);
    background: var(--bg-raised);
    box-shadow: inset 0 0 0 1px var(--ring-strong);
    font-size: var(--t-base);
  }
  .search input::placeholder { color: var(--fg-subtle); }
  .search input:focus-visible { outline: 2px solid var(--focus); outline-offset: 1px; }
</style>
