<script lang="ts">
  import { invalidateAll } from '$app/navigation';
  import type { Dismissal } from '$lib/types';

  /**
   * What the reader chose not to see, and the way back.
   *
   * Hiding was one-way: the feed remembered a dismissal forever and offered no
   * way to review or undo one after the page that made it. The in-card undo
   * strip only lives until you navigate. A control you cannot reverse is a trap,
   * and on a list people skim fast the mis-click is certain.
   *
   * A <details> rather than a panel, because most readers do not want this open
   * and it has to work with JavaScript off — the summary is the disclosure, and
   * the count in it is the reason to open it.
   */
  const { hidden }: { hidden: Dismissal[] } = $props();

  let busy = $state<number | null>(null);

  async function restore(postingID: number) {
    busy = postingID;
    await fetch(`/v1/me/dismissals/${postingID}`, { method: 'DELETE', keepalive: true }).catch(
      () => {}
    );
    await invalidateAll();
    busy = null;
  }

  const reasons: Record<string, string> = {
    not_interested: 'not interested',
    wrong_location: 'wrong location',
    wrong_level: 'wrong level',
    wrong_comp: 'pay',
    already_applied: 'already applied'
  };
</script>

{#if hidden.length}
  <details class="hidden-box">
    <summary>
      <span>Hidden</span>
      <span class="count mono">{hidden.length}</span>
    </summary>

    <p class="note t-micro">
      These are kept out of your feed. Restoring one brings it straight back.
    </p>

    <ul>
      {#each hidden as h (h.posting_id)}
        <li>
          <div class="what">
            <span class="title">{h.title}</span>
            <span class="meta t-micro">
              {h.company}{#if h.reason && reasons[h.reason]} · {reasons[h.reason]}{/if}
            </span>
          </div>
          <button
            type="button"
            class="restore"
            onclick={() => restore(h.posting_id)}
            disabled={busy === h.posting_id}
            aria-label="Restore {h.title}"
          >
            {busy === h.posting_id ? '…' : 'Restore'}
          </button>
        </li>
      {/each}
    </ul>
  </details>
{/if}

<style>
  .hidden-box {
    padding-top: var(--s-4);
    margin-top: var(--s-4);
    border-top: 1px solid var(--border);
  }

  summary {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    /* 24px floor: it is the control that opens this. */
    min-height: 28px;
    font-size: var(--t-sm);
    font-weight: 600;
    cursor: pointer;
    list-style: none;
  }

  summary::-webkit-details-marker {
    display: none;
  }

  summary::before {
    content: '▸';
    color: var(--fg-subtle);
    font-size: 10px;
    transition: transform 120ms ease;
  }

  details[open] summary::before {
    transform: rotate(90deg);
  }

  .count {
    font-variant-numeric: tabular-nums;
    color: var(--fg-subtle);
    font-weight: 400;
  }

  .note {
    margin: var(--s-2) 0 var(--s-3);
    color: var(--fg-subtle);
    line-height: 1.5;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
  }

  li {
    display: flex;
    align-items: center;
    gap: var(--s-2);
  }

  .what {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .title {
    font-size: var(--t-sm);
    color: var(--fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta {
    color: var(--fg-subtle);
  }

  .restore {
    flex: none;
    min-height: 28px;
    padding: 0 var(--s-2);
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    box-shadow: inset 0 0 0 1px var(--ring);
    color: var(--fg-muted);
    font-size: var(--t-xs);
    cursor: pointer;
  }

  .restore:hover:not(:disabled) {
    color: var(--fg);
    background: var(--bg-hover);
  }
</style>
