<script lang="ts">
  import { invalidateAll } from '$app/navigation';
  import type { SavedSearch } from '$lib/types';

  interface Props {
    searches: SavedSearch[];
    /** The filters currently applied, so the save button can name them. */
    params: URLSearchParams;
  }
  const { searches, params }: Props = $props();

  /**
   * Confirmation is INLINE and it persists.
   *
   * A toast is not confirmation. It disappears before a slow reader has
   * finished the sentence, it is invisible to a screen reader that was
   * mid-utterance, and it leaves the person who blinked unsure whether the save
   * worked — which is the exact anxiety the feature exists to remove.
   */
  let saveState = $state<'idle' | 'saving' | 'saved' | 'error'>('idle');
  let message = $state('');

  /**
   * The generated name, from the active facets.
   *
   * Right most of the time, and editable before saving. eBay's pattern: it
   * removes a naming step at the moment someone wants to SAVE something, not
   * name it. Falls back to a date rather than to "Untitled", which tells the
   * reader nothing they could use to pick between two of them.
   */
  function suggestName(p: URLSearchParams): string {
    const parts: string[] = [];
    const q = p.get('q');
    if (q) parts.push(`"${q}"`);
    for (const key of ['skills', 'country', 'mode', 'vendor'] as const) {
      const v = p.get(key);
      if (v) parts.push(v.split(',').slice(0, 2).join(', '));
    }
    const comp = p.get('comp_min');
    if (comp) parts.push(`≥${Number(comp).toLocaleString()}`);
    const yoe = p.get('yoe');
    if (yoe) parts.push(`${yoe} yrs`);

    if (parts.length === 0) {
      return `All roles · ${new Date().toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}`;
    }
    return parts.join(' · ').slice(0, 80);
  }

  const suggested = $derived(suggestName(params));

  // Already saved? Compare the query string, not the name: two names for the
  // same filters is a duplicate the user cannot see.
  const current = $derived(params.toString());
  const alreadySaved = $derived(searches.some((s) => s.query === current));

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (saveState === 'saving') return;

    const form = event.target as HTMLFormElement;
    const name = String(new FormData(form).get('name') ?? '').trim();
    if (!name) return;

    saveState = 'saving';
    try {
      const res = await fetch('/v1/me/searches', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ name, query: current })
      });
      if (res.ok) {
        saveState = 'saved';
        message = 'Saved';
        await invalidateAll();
        return;
      }
      // Say what happened. "Something went wrong" gives the reader nothing to
      // act on, and a duplicate name is entirely fixable by them.
      const problem = await res.json().catch(() => null);
      saveState = 'error';
      message = problem?.detail ?? `Could not save (${res.status}).`;
    } catch {
      saveState = 'error';
      message = 'Could not reach the server. Nothing was saved.';
    }
  }

  async function remove(id: number) {
    await fetch(`/v1/me/searches/${id}`, { method: 'DELETE' });
    await invalidateAll();
  }

  // Opening a search clears only its own badge. Fire-and-forget: the navigation
  // must not wait on the mark, and a failed mark costs a stale badge rather
  // than a broken link.
  function markRun(id: number) {
    fetch(`/v1/me/searches/${id}/run`, { method: 'POST', keepalive: true }).catch(() => {});
  }
</script>

<section class="saved" aria-labelledby="saved-h">
  <h3 id="saved-h">Saved searches</h3>

  {#if searches.length === 0}
    <p class="none">
      Save a set of filters and the feed remembers it — with a count of what has
      arrived since you last looked.
    </p>
  {:else}
    <ul>
      {#each searches as s (s.id)}
        <li>
          <a class="go" href="/jobs?{s.query}" onclick={() => markRun(s.id)}>
            <span class="name">{s.name}</span>
            {#if s.is_default}
              <span class="tag" title="Applied when you open the feed">default</span>
            {/if}
            {#if s.new_since > 0}
              <!-- A count, not a dot. "12 new" is actionable; a dot is a nag. -->
              <span class="badge">{s.new_since > 99 ? '99+' : s.new_since} new</span>
            {/if}
          </a>
          <button
            type="button"
            class="remove"
            onclick={() => remove(s.id)}
            aria-label="Delete saved search {s.name}"
          >
            ✕
          </button>
        </li>
      {/each}
    </ul>
  {/if}

  {#if alreadySaved}
    <p class="state" aria-live="polite">These filters are already saved.</p>
  {:else if saveState === 'saved'}
    <p class="state ok" aria-live="polite">{message}</p>
  {:else}
    <form onsubmit={save}>
      <label class="sr-only" for="search-name">Name for these filters</label>
      <input
        id="search-name"
        name="name"
        type="text"
        maxlength="80"
        value={suggested}
        autocomplete="off"
      />
      <button type="submit" class="btn btn-sm" disabled={saveState === 'saving'}>
        {saveState === 'saving' ? 'Saving…' : 'Save these filters'}
      </button>
      {#if saveState === 'error'}
        <p class="state bad" role="alert">{message}</p>
      {/if}
    </form>
  {/if}
</section>

<style>
  .saved {
    padding-top: var(--s-4);
    margin-top: var(--s-4);
    border-top: 1px solid var(--border);
  }

  h3 {
    font-size: var(--t-sm);
    font-weight: 600;
    margin: 0 0 var(--s-2);
  }

  .none {
    margin: 0 0 var(--s-3);
    font-size: var(--t-xs);
    color: var(--fg-subtle);
    line-height: 1.5;
  }

  ul {
    list-style: none;
    margin: 0 0 var(--s-3);
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  li {
    display: flex;
    align-items: center;
    gap: 2px;
  }

  .go {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--s-2);
    /* 24px minimum: it is a link, so SC 2.5.8 applies. */
    min-height: 28px;
    padding: 4px var(--s-2);
    border-radius: var(--radius-sm);
    color: var(--fg);
    font-size: var(--t-sm);
    text-decoration: none;
  }

  .go:hover {
    background: var(--bg-hover);
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tag {
    font-family: var(--font-mono);
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--fg-subtle);
    flex: none;
  }

  .badge {
    margin-left: auto;
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--t-xs);
    font-variant-numeric: tabular-nums;
    /* --grow means "new and worth looking at", which is what this is. */
    color: var(--grow-ink);
    background: var(--grow-bg);
    padding: 1px 6px;
    border-radius: var(--radius-full);
  }

  .remove {
    flex: none;
    min-width: 28px;
    min-height: 28px;
    border: 0;
    background: transparent;
    /* --fg-subtle, not --fg-faint: the glyph is rendered text and --fg-faint
       only clears the 3:1 required of non-text. tokens.test.ts enforces it. */
    color: var(--fg-subtle);
    cursor: pointer;
    border-radius: var(--radius-sm);
    font-size: var(--t-xs);
  }

  .remove:hover {
    background: var(--bg-hover);
    color: var(--shrink-ink);
  }

  form {
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
  }

  input {
    width: 100%;
    min-height: 32px;
    padding: 0 var(--s-2);
    font: inherit;
    font-size: var(--t-sm);
    color: var(--fg);
    background: var(--bg-raised);
    border: 0;
    border-radius: var(--radius-sm);
    box-shadow: 0 0 0 1px var(--ring-strong);
  }

  .state {
    margin: 0;
    font-size: var(--t-xs);
    color: var(--fg-subtle);
  }

  .state.ok {
    color: var(--grow-ink);
    font-weight: 600;
  }

  .state.bad {
    color: var(--shrink-ink);
  }
</style>
