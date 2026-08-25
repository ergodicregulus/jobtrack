<script lang="ts">
  import type { Activity } from '$lib/types';

  const { activity }: { activity: Activity | null } = $props();

  /**
   * Twelve weeks of real activity, one column per week, Monday at the top.
   *
   * What this is NOT is the thing that makes these grids obnoxious elsewhere:
   * there is no streak, no target, no "you missed a day". A job search is
   * mostly waiting, and a widget that scolds someone for a quiet fortnight is
   * both wrong about the work and unkind about it. The copy says so explicitly,
   * because a grid of squares carries the streak connotation whether we intend
   * it or not.
   *
   * A cell counts applications sent and statuses that genuinely changed —
   * never logins or searches. A grid that lit up for opening the page would be
   * measuring attendance, and this audience would spot that instantly.
   */

  const WEEKS = 12;
  const DAYS = 7;

  interface Cell {
    /** ISO day, or null for a cell outside the window (before `from`). */
    day: string | null;
    count: number;
    label: string;
  }

  /** Local-midnight parse: `new Date('2026-08-17')` is UTC and shifts a day west of Greenwich. */
  function parseDay(iso: string): Date {
    const [y, m, d] = iso.split('-').map(Number);
    return new Date(y, m - 1, d);
  }

  function iso(d: Date): string {
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(
      d.getDate()
    ).padStart(2, '0')}`;
  }

  const dayFormat = new Intl.DateTimeFormat(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short'
  });

  const counts = $derived(new Map((activity?.days ?? []).map((d) => [d.day, d.count])));

  // Column-major so `grid-auto-flow: column` lays weeks out left to right.
  const cells = $derived.by((): Cell[] => {
    if (!activity) return [];
    const start = parseDay(activity.from);
    const out: Cell[] = [];

    for (let w = 0; w < WEEKS; w++) {
      for (let d = 0; d < DAYS; d++) {
        const date = new Date(start);
        date.setDate(start.getDate() + w * DAYS + d);
        const key = iso(date);
        const count = counts.get(key) ?? 0;

        // Days after today are not "zero activity", they are days that have
        // not happened. Rendering them identically to a quiet day would claim
        // something about the future.
        const future = date > new Date();

        out.push({
          day: future ? null : key,
          count,
          label: future
            ? ''
            : count === 0
              ? `No activity on ${dayFormat.format(date)}`
              : `${count} ${count === 1 ? 'change' : 'changes'} on ${dayFormat.format(date)}`
        });
      }
    }
    return out;
  });

  // Three levels, not five. The data is counts of 0–3 for almost everyone, and
  // a five-step ramp on that range invents distinctions the numbers cannot
  // support — the exact error the scoring model is built to avoid.
  function level(c: Cell): string {
    if (c.day === null) return 'future';
    if (c.count === 0) return 'none';
    return c.count <= 2 ? 'low' : 'high';
  }

  const rangeLabel = $derived.by(() => {
    if (!activity) return '';
    const to = parseDay(activity.to);
    return `12 weeks to ${to.toLocaleDateString(undefined, { day: 'numeric', month: 'long' })}`;
  });

  const empty = $derived(!activity || activity.total === 0);

  /**
   * The hovered cell, shown as a readout beside the grid.
   *
   * A native `title` was already here and stays as a fallback, but it waits
   * about a second before appearing and cannot be styled. For a grid whose
   * whole purpose is "what happened on that day", a delay that long means most
   * people never find out.
   */
  let hovered = $state<Cell | null>(null);
</script>

<section class="panel activity" aria-labelledby="act">
  <div class="panel-head">
    <h2 id="act" class="t-heading">Your activity</h2>
    <span class="spacer"></span>
    <span class="t-micro">applications sent and status changes, last 12 weeks</span>
  </div>

  {#if !activity}
    <p class="t-small note">
      Your activity record could not be loaded. Nothing has been lost — this is
      a view of data the tracker already holds.
    </p>
  {:else}
    <div class="body">
      <div class="grid-wrap">
        <p class="eyebrow">{rangeLabel}</p>
        <!--
          A table would be the pedantic markup here and is the wrong call: 84
          cells announced row by row is a minute of noise for a widget that is
          context, not content. The grid is hidden from assistive tech and the
          same information is given as one sentence below it, which is what a
          screen-reader user actually wants from a sparkline.
        -->
        <div
          class="grid"
          aria-hidden="true"
          onmouseleave={() => (hovered = null)}
        >
          {#each cells as c, i (i)}
            <span
              class="cell"
              data-level={level(c)}
              title={c.label}
              onmouseenter={() => (hovered = c.day ? c : null)}
            ></span>
          {/each}
        </div>

        <!--
          Reserved height, not conditional rendering: a readout that appears on
          hover would push the grid down and move the cell out from under the
          cursor, which makes the whole grid feel unstable.
        -->
        <p class="readout" aria-hidden="true">
          {#if hovered}
            <span class="readout-count">{hovered.count}</span>
            {hovered.count === 1 ? 'change' : 'changes'} on {hovered.label.replace(
              /^.*? on /,
              ''
            )}
          {:else}
            <span class="readout-idle">Hover a day for its count</span>
          {/if}
        </p>
      </div>

      <div class="aside">
        <p class="sr-only">
          {#if empty}
            No activity recorded in the last 12 weeks.
          {:else}
            {activity.total} status
            {activity.total === 1 ? 'change' : 'changes'} recorded in the last 12 weeks.
          {/if}
        </p>

        {#if empty}
          <p class="empty-title">Nothing recorded yet</p>
          <p class="t-small">
            This fills in on its own as you apply and as statuses change. It is
            a record of what happened, not a target to hit — there is no streak
            to keep and no number you are supposed to reach.
          </p>
        {:else}
          <div class="legend t-micro" aria-hidden="true">
            <span><span class="cell" data-level="none"></span>none</span>
            <span><span class="cell" data-level="low"></span>1–2</span>
            <span><span class="cell" data-level="high"></span>3 or more</span>
          </div>
          <p class="t-small">
            <strong class="num">{activity.total}</strong>
            {activity.total === 1 ? 'change' : 'changes'} in 12 weeks. Quiet
            stretches are ordinary — most of a search is waiting on other
            people, and this is a record of what happened rather than a target.
          </p>
        {/if}
      </div>
    </div>
  {/if}
</section>

<style>
  .activity { padding: 0; }
  .panel-head {
    display: flex; align-items: center; gap: var(--s-2); flex-wrap: wrap;
    padding: var(--s-3) var(--s-4h);
    box-shadow: 0 1px 0 0 var(--ring);
  }
  .spacer { flex: 1; }

  .body {
    display: flex; flex-wrap: wrap; align-items: flex-start;
    gap: var(--s-5);
    padding: var(--s-4h);
  }

  .grid-wrap { display: flex; flex-direction: column; gap: var(--s-1h); }

  /* Column-major: each column is one week, each row one weekday. */
  .grid {
    display: grid;
    grid-auto-flow: column;
    grid-template-rows: repeat(7, 14px);
    grid-auto-columns: 14px;
    gap: 3px;
  }

  .readout {
    margin: 0.5rem 0 0;
    font-size: var(--t-sm);
    min-height: 1.4em;
    color: var(--fg-muted);
  }

  .readout-count {
    font-family: var(--font-mono);
    font-variant-numeric: tabular-nums;
    font-weight: 600;
    color: var(--fg);
  }

  .readout-idle {
    color: var(--fg-subtle);
  }

  .cell {
    width: 14px; height: 14px;
    border-radius: 2px;
    box-shadow: inset 0 0 0 1px var(--ring);
  }
  /* Levels, not a gradient. Three steps the data can actually support. */
  .cell[data-level='none'] { background: none; }
  .cell[data-level='low'] {
    background: var(--grow-bg);
    box-shadow: inset 0 0 0 1px var(--grow-line);
  }
  .cell[data-level='high'] { background: var(--grow); box-shadow: none; }
  /* A day that has not happened is not a quiet day. */
  .cell[data-level='future'] { box-shadow: none; background: none; }

  .aside {
    flex: 1; min-width: 240px; max-width: 54ch;
    display: flex; flex-direction: column; gap: var(--s-2);
  }
  .empty-title { font-size: var(--t-md); font-weight: 560; }

  .legend { display: flex; align-items: center; gap: var(--s-3); color: var(--fg-subtle); }
  .legend > span { display: inline-flex; align-items: center; gap: var(--s-1); }
  .legend .cell { width: 10px; height: 10px; }

  .note { color: var(--fg-muted); padding: var(--s-4h); }

  @media (max-width: 460px) {
    /* 12 columns of 14px plus gaps overflows a 390px screen once the card's
       own padding is counted, and a heatmap that scrolls sideways is a
       heatmap nobody reads. Smaller cells keep all twelve weeks in view,
       which is the entire point of the shape. */
    .grid { grid-template-rows: repeat(7, 11px); grid-auto-columns: 11px; gap: 2px; }
    .cell { width: 11px; height: 11px; }
  }
</style>
