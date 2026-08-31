<script lang="ts">
  import type { Coverage } from '$lib/types';

  /**
   * The corpus, with its holes shown.
   *
   * Every other job site draws the data it has. This draws the data it does
   * NOT have — a field of marks where a hollow one is a role we could not read
   * something about. It is the headline made literal, and it is the one graphic
   * on this page that no competitor could copy without first admitting the same
   * thing about themselves.
   *
   * FOUR DIMENSIONS, not one, because they differ by nearly four times: we hold
   * a pay figure for 15% of roles and a work mode for 56%. A graphic locked to
   * the worst of those would be making a point; one you can switch through is
   * reporting.
   *
   * Switching is a CSS attribute change, not a re-render. Each mark carries the
   * dimensions it is KNOWN for as a token list, and the container's data-shows
   * decides which token counts. That is the whole mechanism: no state, no
   * recomputation, and the controls are real links so it works with JavaScript
   * off.
   */
  interface Props {
    coverage: Coverage;
    /** Which dimension the server rendered. Comes from the URL so a link is shareable. */
    shows?: string;
  }
  const { coverage, shows = 'pay' }: Props = $props();

  // Enough marks to read as a field rather than a bar, few enough to stay
  // legible at 390px and cheap to send. 440 gzips to well under 2 KB because
  // every mark is near-identical markup.
  const MARKS = 440;

  const DIMENSIONS = [
    {
      key: 'pay',
      label: 'Pay',
      known: coverage.comp,
      // Written as what WE know, never as what the employer disclosed. Most of
      // these employers did publish a salary somewhere; we simply do not have
      // it, and blaming them for our gap would be the same dishonesty in the
      // opposite direction.
      missing: 'no pay figure we could read'
    },
    { key: 'experience', label: 'Experience', known: coverage.yoe, missing: 'no experience range we could read' },
    { key: 'mode', label: 'Work mode', known: coverage.mode, missing: 'no work mode we could determine' },
    { key: 'skills', label: 'Skills', known: coverage.skills, missing: 'not one skill we recognised' }
  ];

  const perMark = $derived(Math.max(1, Math.round(coverage.live / MARKS)));

  /**
   * A stable scatter.
   *
   * The marks are shuffled deterministically so the graphic is identical on the
   * server and after hydration, and identical between two readers. Random
   * placement would make the field flicker on every render and the SSR markup
   * disagree with the client.
   *
   * Each dimension gets its OWN ordering. Reusing one would put the same marks
   * hollow in every view, which reads as "the roles missing pay are the roles
   * missing skills" — a correlation we have not measured and have no business
   * implying.
   */
  function hash(i: number, seed: number): number {
    let x = ((i + 1) * 2654435761) ^ (seed * 40503);
    x = (x ^ (x >>> 15)) >>> 0;
    return x;
  }

  const tokens = $derived.by(() => {
    const out: string[][] = Array.from({ length: MARKS }, () => []);
    DIMENSIONS.forEach((d, seed) => {
      const k = coverage.live > 0 ? Math.round((d.known / coverage.live) * MARKS) : 0;
      const order = Array.from({ length: MARKS }, (_, i) => i).sort(
        (a, b) => hash(a, seed + 1) - hash(b, seed + 1)
      );
      for (const i of order.slice(0, k)) out[i].push(d.key);
    });
    return out.map((t) => t.join(' '));
  });

  const pct = (n: number) =>
    coverage.live > 0 ? Math.round((n / coverage.live) * 1000) / 10 : 0;

  const shown = $derived(DIMENSIONS.find((d) => d.key === current) ?? DIMENSIONS[0]);

  // Derived from `shown`, which follows the client-side switch — NOT from the
  // `shows` prop, which only ever holds what the server rendered. Deriving the
  // number from the prop and the sentence from the state meant clicking Skills
  // redrew the field, changed the words, and left "85%" sitting above them.
  const unknownPct = $derived(Math.round((100 - pct(shown.known)) * 10) / 10);

  // The rollup's own timestamp, not "now". Saying when we last looked is the
  // honest version of a live-updating counter.
  const readAt = $derived(
    // 24-hour, so the eyebrow stays a fixed width and does not gain a stray
    // "PM" that breaks the monospace rhythm.
    new Date(coverage.as_of).toLocaleTimeString('en-GB', {
      hour: '2-digit',
      minute: '2-digit'
    })
  );

  // Client-side switching is an enhancement over the links. Without JS the
  // hrefs still work; with it, nothing round-trips.
  let current = $state(shows);
  $effect(() => {
    current = shows;
  });
  function pick(event: MouseEvent, key: string) {
    // Let modified clicks open a new tab, as any link should.
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    event.preventDefault();
    current = key;
    history.replaceState(null, '', key === 'pay' ? '/' : `/?shows=${key}`);
  }
</script>

<figure class="absence">
  <figcaption class="head">
    <span class="eyebrow">
      Live corpus · <span class="num">{coverage.live.toLocaleString()}</span> roles · read {readAt}
    </span>
  </figcaption>

  <div class="controls" role="group" aria-label="Choose what to show">
    {#each DIMENSIONS as d (d.key)}
      <a
        class="chip"
        class:on={d.key === current}
        href={d.key === 'pay' ? '/' : `/?shows=${d.key}`}
        aria-current={d.key === current ? 'true' : undefined}
        onclick={(e) => pick(e, d.key)}
      >
        {d.label}
        <span class="chip-num">{pct(d.known)}%</span>
      </a>
    {/each}
  </div>

  <!--
    aria-hidden with a sentence below, the same choice the ingest chart makes:
    440 marks announced one at a time is a minute of noise, and the sentence is
    what a screen-reader user actually wants from a field.
  -->
  <div class="field" data-shows={current} aria-hidden="true">
    {#each tokens as t, i (i)}
      <i class="m" data-known={t} style:--i={i}></i>
    {/each}
  </div>

  <p class="reading">
    <strong class="big">{unknownPct}%</strong>
    of live roles have {shown.missing}.
    <!--
      Every phrase completes "N% of live roles have …", so they are noun
      phrases, not clauses. The first draft read "have we hold no pay figure",
      which is what happens when the sentence is assembled from fragments
      written for a different frame.
    -->
    <span class="quiet">
      Each mark stands for about {perMark} roles. The arrangement is arbitrary; the proportion is
      not.
    </span>
  </p>

  <p class="sr-only">
    Of {coverage.live.toLocaleString()} live roles, we hold a pay figure for
    {coverage.comp.toLocaleString()} ({pct(coverage.comp)}%), an experience range for
    {coverage.yoe.toLocaleString()} ({pct(coverage.yoe)}%), a work mode for
    {coverage.mode.toLocaleString()} ({pct(coverage.mode)}%), and at least one recognised skill for
    {coverage.skills.toLocaleString()} ({pct(coverage.skills)}%).
  </p>
</figure>

<style>
  .absence {
    margin: 0;
  }

  .head {
    display: flex;
    align-items: baseline;
    gap: var(--s-3);
    flex-wrap: wrap;
  }

  .eyebrow {
    font-family: var(--font-mono);
    font-size: var(--t-xs);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--fg-subtle);
  }

  .num {
    font-variant-numeric: tabular-nums;
    color: var(--fg);
  }

  .controls {
    display: flex;
    flex-wrap: wrap;
    gap: var(--s-2);
    margin: var(--s-3) 0 var(--s-4);
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    /* 24px floor, SC 2.5.8. */
    min-height: 32px;
    padding: 0 var(--s-3);
    border-radius: var(--radius-full);
    box-shadow: inset 0 0 0 1px var(--ring);
    color: var(--fg-muted);
    font-size: var(--t-sm);
    text-decoration: none;
    background: transparent;
    transition: color 120ms ease, box-shadow 120ms ease, background 120ms ease;
  }

  .chip:hover {
    color: var(--fg);
    background: var(--bg-hover);
  }

  .chip.on {
    color: var(--accent-fg);
    background: var(--accent);
    box-shadow: none;
  }

  .chip-num {
    font-family: var(--font-mono);
    font-variant-numeric: tabular-nums;
    font-size: var(--t-xs);
    opacity: 0.75;
  }

  /*
    The field. auto-fill rather than a fixed column count so the same 440 marks
    reflow from a wide band on desktop to a denser block on a phone without the
    component knowing anything about breakpoints.
  */
  .field {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(18px, 1fr));
    gap: 7px;
    margin-bottom: var(--s-5, 2rem);
  }

  /*
    Narrow screens shrink the mark; they never drop one.
    
    At 18px the 440 marks become a 32-row wall on a phone that pushes the rest
    of the page off the bottom. The obvious fix — render fewer marks below a
    breakpoint — is not available: every mark is 1/440th of the proportion, and
    dropping a tail changes the number the graphic is reporting. So the grid
    gets denser instead and the arithmetic stays exact.
  */
  @media (max-width: 640px) {
    .field {
      grid-template-columns: repeat(auto-fill, minmax(11px, 1fr));
      gap: 4px;
    }

    .m {
      border-radius: 2px;
      box-shadow: inset 0 0 0 1px var(--fg-subtle);
    }
  }

  .m {
    aspect-ratio: 1;
    border-radius: 3px;
    /* Unknown is the default state: a hollow mark, not an absent one. A gap
       would read as "nothing here"; a ring reads as "something here we could
       not measure", which is the actual claim. */
    /* --fg-faint, not --ring-strong. The ring tokens exist to separate
       surfaces and sit at 1.59:1 (light) and 2.08:1 (dark) — fine for a card
       edge, and nowhere near the 3:1 a graphical object conveying information
       needs. tokens.test.ts is what caught it: the hollow mark is a claim, so
       it is held to the same bar as any other.

       --fg-faint was the next attempt and missed at 2.9951:1 — by five
       thousandths, in one theme. Worth recording because it is precisely the
       margin an eye would have waved through. --fg-subtle clears it in both.
       1.5px rather than 1 for the same reason: at this size a hairline reads as
       noise, and the hollow mark is the half of this graphic that matters. */
    box-shadow: inset 0 0 0 1.5px var(--fg-subtle);
    background: transparent;
    transition: background 220ms ease, box-shadow 220ms ease;
  }

  /*
    Known marks fill. One attribute on the container switches all 440 at once —
    no re-render, no JS in the path, and the server-rendered markup is already
    correct before hydration.
  */
  .field[data-shows='pay'] .m[data-known~='pay'],
  .field[data-shows='experience'] .m[data-known~='experience'],
  .field[data-shows='mode'] .m[data-known~='mode'],
  .field[data-shows='skills'] .m[data-known~='skills'] {
    background: var(--accent);
    box-shadow: none;
  }

  .reading {
    margin: 0;
    max-width: 62ch;
    font-size: var(--t-base);
    line-height: 1.5;
    color: var(--fg-muted);
  }

  .big {
    font-family: var(--font-mono);
    font-variant-numeric: tabular-nums;
    font-size: var(--t-2xl);
    font-weight: 600;
    color: var(--fg);
    letter-spacing: -0.02em;
    margin-right: 0.2rem;
  }

  .quiet {
    display: block;
    margin-top: var(--s-2);
    font-size: var(--t-xs);
    color: var(--fg-subtle);
  }

  /*
    The field assembles once, in reading order. It is the only motion on the
    page that is not a hover, and it exists because watching the holes appear
    tells the story faster than the sentence under it does.
  */
  @media (prefers-reduced-motion: no-preference) {
    .m {
      animation: settle 340ms ease-out backwards;
      animation-delay: calc(var(--i) * 1.4ms);
    }
  }

  @keyframes settle {
    from {
      opacity: 0;
      transform: scale(0.4);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }
</style>
