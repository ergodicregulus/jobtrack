<script lang="ts">
  /**
   * Skill entry as removable tags with suggestions.
   *
   * Two constraints shaped this. First, it must work without JavaScript: the
   * real value lives in a plain hidden input holding a comma-separated string,
   * and the visible control only edits that. Second, typing a skill you already
   * have must not silently add a duplicate — canonicalisation happens on the
   * server, but the UI should not present the same word twice while you look
   * at it.
   */
  interface Props {
    value: string[];
    name?: string;
    /** Offered skills as {canonical, label}. Canonical is what gets stored. */
    suggestions?: { canonical: string; label: string }[];
    id?: string;
    /**
     * Canonical name to how it should be WRITTEN.
     *
     * The value array holds canonical names because that is the identifier the
     * form submits and the server matches on, and canonical means lower-case.
     * Rendering it directly is what put "postgresql", "javascript" and "sql"
     * on screen as chips. Falls back to the canonical when a label is missing,
     * which is right for a skill the user has just typed and the server has
     * not seen yet.
     */
    labels?: Record<string, string>;
  }
  let {
    value = $bindable([]),
    name = 'skills',
    suggestions = [],
    id = 'skills',
    labels = {}
  }: Props = $props();

  const label = (skill: string): string => labels[skill] ?? skill;

  let draft = $state('');

  const has = (s: string) => value.includes(s);
  const unused = $derived(suggestions.filter((s) => !has(s.canonical)).slice(0, 14));

  function add(raw: string) {
    const s = raw.trim().toLowerCase().replace(/,$/, '');
    if (!s || has(s) || value.length >= 60) return;
    value = [...value, s];
    draft = '';
  }

  function remove(s: string) {
    value = value.filter((v) => v !== s);
  }

  function onKeydown(e: KeyboardEvent) {
    // Enter and comma both commit. Enter must not submit the surrounding form
    // while the field has text — losing a half-typed list to an accidental
    // submit is the single most annoying thing a tag input can do.
    if (e.key === 'Enter' || e.key === ',') {
      if (draft.trim()) {
        e.preventDefault();
        add(draft);
      }
      return;
    }
    // Backspace on an empty field removes the last tag, which is the
    // convention every tag input shares and users expect without being told.
    if (e.key === 'Backspace' && !draft && value.length) {
      remove(value[value.length - 1]);
    }
  }
</script>

<input type="hidden" {name} value={value.join(',')} />

<div class="picker">
  <div class="tags">
    {#each value as skill (skill)}
      <span class="chip chip-accent">
        {label(skill)}
        <button
          type="button"
          class="x"
          aria-label={`Remove ${label(skill)}`}
          onclick={() => remove(skill)}
        >
          <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true">
            <path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </button>
      </span>
    {/each}

    <input
      class="entry"
      {id}
      type="text"
      bind:value={draft}
      onkeydown={onKeydown}
      onblur={() => draft.trim() && add(draft)}
      placeholder={value.length ? 'Add another…' : 'go, postgresql, kubernetes…'}
      aria-label="Add a skill"
      autocomplete="off"
    />
  </div>

  {#if unused.length}
    <div class="suggest">
      <span class="t-micro">Common:</span>
      {#each unused as s (s.canonical)}
        <button type="button" class="chip chip-plain chip-toggle" onclick={() => add(s.canonical)}>
          + {s.label}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .picker { display: flex; flex-direction: column; gap: var(--s-3); }

  /* The wrapper carries the focus ring rather than the inner input, so the
     whole control reads as one field. :focus-within is what makes that work. */
  .tags {
    display: flex; flex-wrap: wrap; align-items: center;
    gap: 6px;
    min-height: 38px;
    padding: 6px;
    background: var(--bg-raised);
    border-radius: var(--radius);
    box-shadow: var(--e-0);
    transition: box-shadow var(--fast) var(--ease);
  }
  .tags:focus-within { box-shadow: 0 0 0 1px var(--accent), 0 0 0 4px var(--ring-accent); }

  .entry {
    flex: 1 1 140px;
    min-width: 140px;
    border: 0; outline: none;
    background: none;
    color: var(--fg);
    font: inherit;
    padding: 4px;
  }
  .entry::placeholder { color: var(--fg-subtle); }

  /* 24px, not 16.
     
     This is the control that REMOVES a skill, and skills are 40% of every
     match score — a mis-tap here silently changes every result the user sees.
     WCAG 2.2 SC 2.5.8 sets the floor at 24px and it measured 16. The negative
     margin keeps the chip's visual size unchanged: the target grows, the
     drawing does not. */
  .x {
    display: inline-flex; align-items: center; justify-content: center;
    width: 24px; height: 24px;
    margin: -4px -6px -4px 0;
    padding: 0;
    border: 0; background: none;
    color: currentColor;
    opacity: 0.55;
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .x:hover { opacity: 1; background: rgb(0 0 0 / 0.08); }

  .suggest { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
</style>
