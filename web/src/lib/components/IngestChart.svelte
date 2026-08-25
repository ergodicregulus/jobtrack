<script lang="ts">
  import type { IngestSeries } from '$lib/types';

  /**
   * What the corpus did, per ATS vendor, over the last month.
   *
   * Inline SVG rather than a charting library. The first-load JS budget is
   * 100 KB and the smallest credible charting library is a third of it, to draw
   * five polylines that are arithmetic. It also has to work with JavaScript off,
   * which a client-side chart does not — this one is server-rendered markup and
   * needs no hydration at all.
   *
   * It reads the source_daily rollup through /v1/market/ingest, not the postings
   * table, because this query has no user to filter by. See ADR-0017.
   *
   * This is the honest version of "look how much data we have": a number that
   * moves while you watch, drawn from the same rows the product serves.
   */
  const { ingest }: { ingest: IngestSeries | null } = $props();

  const W = 720;
  const H = 200;
  const PAD = { top: 12, right: 8, bottom: 22, left: 34 };

  /**
   * The level, not the flow.
   *
   * `new` — postings first seen that day — was tried first and is the wrong
   * series for this chart, for a reason that only appeared once it was drawn:
   * every posting in a young corpus shares one first_seen_at, so the initial
   * import is a single 7,406 spike that compresses every subsequent day into
   * the axis. The data was correct and the chart was unreadable.
   *
   * `live` is what the question actually asks — how many roles each system is
   * carrying — and it degrades gracefully: three separated lines that rise as
   * boards grow and fall when they are cleared out. The flow is still in the
   * rollup for a widget that wants it.
   */
  const lines = $derived(ingest?.series ?? []);
  const days = $derived(ingest?.days ?? []);

  const peak = $derived(Math.max(1, ...lines.flatMap((s) => s.live)));

  function x(i: number, n: number): number {
    if (n <= 1) return PAD.left;
    return PAD.left + (i * (W - PAD.left - PAD.right)) / (n - 1);
  }

  function y(v: number): number {
    return H - PAD.bottom - (v / peak) * (H - PAD.top - PAD.bottom);
  }

  function path(values: number[]): string {
    return values.map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i, values.length).toFixed(1)},${y(v).toFixed(1)}`).join(' ');
  }

  /**
   * Series colour is identity, not state.
   *
   * The semantic tokens are deliberately NOT used here: --grow and --shrink mean
   * "growing" and "shrinking" everywhere else in the product, and spending them
   * on "this line is Greenhouse" would break that the moment a chart shows a
   * vendor in decline. These are tints of the accent plus one neutral, which
   * carry no meaning beyond telling the lines apart.
   */
  const STROKES = [
    'var(--accent)',
    'color-mix(in oklab, var(--accent) 55%, var(--bg))',
    'var(--fg-muted)',
    'color-mix(in oklab, var(--accent) 30%, var(--fg-muted))'
  ];

  const shortDay = (iso: string) => {
    const [, m, d] = iso.split('-');
    return `${Number(d)}/${Number(m)}`;
  };

  // Four labels across the axis: enough to orient, few enough to read.
  const ticks = $derived(
    days.length === 0
      ? []
      : [0, Math.floor((days.length - 1) / 3), Math.floor((2 * (days.length - 1)) / 3), days.length - 1]
          .filter((v, i, a) => a.indexOf(v) === i)
          .map((i) => ({ i, label: shortDay(days[i]) }))
  );

  // The latest day's total, not a sum over the window: summing a level across
  // days counts the same posting once per day it was live, which is a number
  // that means nothing and would be the largest figure on the page.
  const total = $derived(lines.reduce((sum, s) => sum + (s.live.at(-1) ?? 0), 0));
</script>

{#if ingest && days.length > 1 && total > 0}
  <figure class="chart">
    <figcaption>
      <span class="title">Roles we are tracking, by system</span>
      <span class="sub">last {days.length} days · {total.toLocaleString()} live now</span>
    </figcaption>

    <!--
      aria-hidden with a prose equivalent below, for the same reason as the
      activity grid: a polyline announced point by point is a minute of noise,
      and the sentence is what a screen-reader user actually wants from a
      sparkline.
    -->
    <svg viewBox="0 0 {W} {H}" preserveAspectRatio="none" role="img" aria-hidden="true">
      <!-- Baseline and peak only. A full gridline set is more ink than the data. -->
      <line class="axis" x1={PAD.left} y1={y(0)} x2={W - PAD.right} y2={y(0)} />
      <line class="axis faint" x1={PAD.left} y1={y(peak)} x2={W - PAD.right} y2={y(peak)} />
      <text class="tick" x={PAD.left - 6} y={y(peak) + 4} text-anchor="end">{peak}</text>
      <text class="tick" x={PAD.left - 6} y={y(0) + 4} text-anchor="end">0</text>

      {#each ticks as t (t.i)}
        <text class="tick" x={x(t.i, days.length)} y={H - 6} text-anchor="middle">{t.label}</text>
      {/each}

      {#each lines as s, i (s.vendor)}
        <path class="line" d={path(s.live)} style:stroke={STROKES[i % STROKES.length]} />
      {/each}
    </svg>

    <ul class="legend">
      {#each lines as s, i (s.vendor)}
        <li>
          <span class="swatch" style:background={STROKES[i % STROKES.length]}></span>
          {s.vendor}
          <span class="count">{(s.live.at(-1) ?? 0).toLocaleString()}</span>
        </li>
      {/each}
    </ul>

    <p class="sr-only">
      {total.toLocaleString()} postings are live across {lines.length} applicant tracking
      systems, tracked daily over the last {days.length} days.
      {#each lines as s (s.vendor)}
        {s.vendor}: {(s.live.at(-1) ?? 0).toLocaleString()}.
      {/each}
    </p>
  </figure>
{/if}

<style>
  .chart {
    margin: 0;
  }

  figcaption {
    display: flex;
    align-items: baseline;
    gap: 0.6rem;
    flex-wrap: wrap;
    margin-bottom: 0.9rem;
  }

  .title {
    font-size: var(--t-base);
    font-weight: 600;
  }

  .sub {
    font-family: var(--font-mono);
    font-size: var(--t-xs);
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--fg-subtle);
  }

  svg {
    display: block;
    width: 100%;
    height: clamp(150px, 22vw, 200px);
    overflow: visible;
  }

  .axis {
    stroke: var(--border);
    stroke-width: 1;
  }

  .axis.faint {
    stroke-dasharray: 2 4;
  }

  .tick {
    font-family: var(--font-mono);
    font-size: 10px;
    fill: var(--fg-subtle);
  }

  .line {
    fill: none;
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
    /* vector-effect keeps the stroke 2px after preserveAspectRatio="none"
       stretches the viewBox horizontally; without it the lines thin out as the
       container widens. */
    vector-effect: non-scaling-stroke;
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem 1.1rem;
    list-style: none;
    margin: 0.9rem 0 0;
    padding: 0;
    font-size: var(--t-sm);
    color: var(--fg-muted);
  }

  .legend li {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .swatch {
    width: 14px;
    height: 3px;
    border-radius: 2px;
    flex: none;
  }

  .count {
    font-family: var(--font-mono);
    font-variant-numeric: tabular-nums;
    color: var(--fg);
  }
</style>
