<script lang="ts">
  import PageHeader from '$lib/components/PageHeader.svelte';
  import { invalidateAll } from '$app/navigation';
  import { mutate, type MutationFailure } from '$lib/mutate.svelte';
  import { relativeDay } from '$lib/format';
  import type { PageData } from './$types';
  import type { TrackedItem } from '$lib/types';

  const { data }: { data: PageData } = $props();

  // Columns are the funnel. Terminal outcomes share one column rather than
  // getting three of their own — a board with an "Offer" column two pixels wide
  // beside three wide rejection columns is demoralising and not informative.
  const columns = [
    { key: 'saved', label: 'Saved', statuses: ['saved'] },
    { key: 'applied', label: 'Applied', statuses: ['applied', 'referred'] },
    { key: 'screening', label: 'Screening', statuses: ['recruiter_screen', 'hm_screen'] },
    { key: 'onsite', label: 'Interviewing', statuses: ['onsite'] },
    { key: 'offer', label: 'Offer', statuses: ['offer'] },
    { key: 'closed', label: 'Closed', statuses: ['rejected', 'ghosted', 'withdrawn'] }
  ];

  const nextStatus: Record<string, { to: string; label: string } | null> = {
    saved: { to: 'applied', label: 'Mark applied' },
    applied: { to: 'recruiter_screen', label: 'Got a screen' },
    referred: { to: 'recruiter_screen', label: 'Got a screen' },
    recruiter_screen: { to: 'hm_screen', label: 'Hiring manager' },
    hm_screen: { to: 'onsite', label: 'Moved to onsite' },
    onsite: { to: 'offer', label: 'Got an offer' },
    offer: null,
    rejected: null,
    ghosted: null,
    withdrawn: null
  };

  let busy = $state<number | null>(null);
  let failure = $state<MutationFailure | null>(null);
  let lastAttempt = $state<{ item: TrackedItem; to: string } | null>(null);

  /**
   * The stage shown on a narrow screen. Defaults to the first stage that has
   * anything in it — landing on an empty "Saved" when 33 applications are one
   * tap away would look like a broken page.
   */
  const activeStage = $derived.by(() => {
    const asked = data.stage;
    if (asked && columns.some((c) => c.key === asked)) return asked;
    const firstWithItems = columns.find((c) =>
      data.items.some((i: TrackedItem) => c.statuses.includes(i.status))
    );
    return firstWithItems?.key ?? columns[0].key;
  });

  const grouped = $derived(
    columns.map((c) => ({
      ...c,
      items: data.items.filter((i: TrackedItem) => c.statuses.includes(i.status))
    }))
  );

  async function advance(item: TrackedItem, to: string) {
    busy = item.id;
    failure = null;
    lastAttempt = { item, to };

    // No optimistic move here, unlike Save. The server decides derived fields —
    // applied_at, the channel default — so guessing the resulting row would
    // show the user something the database does not agree with. The revert is
    // therefore a no-op: nothing changed locally to undo.
    failure = await mutate(
      () =>
        fetch(`/v1/me/saved/${item.id}`, {
          method: 'PATCH',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ status: to })
        }),
      () => {}
    );

    if (!failure) await invalidateAll();
    busy = null;
  }
</script>

<div class="shell page">
  <PageHeader
    title="Tracker"
    sub={`${data.items.length} application${data.items.length === 1 ? '' : 's'}. ` +
      'Long silences are noted; nothing is closed on your behalf.'}
  >
    {#snippet action()}
      <a class="btn btn-primary" href="/jobs">Add from jobs</a>
    {/snippet}
  </PageHeader>

  {#if data.error}
    <p class="banner" role="alert">{data.error}</p>
  {/if}
  {#if failure}
    <p class="banner" role="alert">
      {failure.message}
      {#if failure.retryable && lastAttempt}
        <button class="retry" onclick={() => advance(lastAttempt!.item, lastAttempt!.to)}>
          Try again
        </button>
      {:else if failure.needsAuth}
        <a href="/login?next=/tracker">Sign in</a>
      {/if}
    </p>
  {/if}

  {#if data.items.length === 0}
    <div class="panel empty">
      <p class="empty-title">Nothing tracked yet</p>
      <p class="t-small">
        Save a role from the jobs feed and it appears here. The tracker is what
        turns a scattered search into something you can see the shape of.
      </p>
      <a class="btn btn-primary" href="/jobs">Browse jobs</a>
    </div>
  {:else}
    <!--
      Stage chips, narrow screens only.

      A six-column board on a 390px phone shows one and a half columns with the
      second sliced down the middle, and the page grows as tall as the biggest
      stage — 5,293px with 33 applications. The design's answer is one column at
      a time with chips to switch, reusing the feed's filter-chip pattern rather
      than inventing a control.

      Links with the stage in the URL, so it works with JavaScript off, is
      shareable, and the back button behaves. Same reasoning as the feed's
      filters.
    -->
    <nav class="stages" data-narrow-only aria-label="Pipeline stage">
      {#each grouped as col (col.key)}
        <a
          class="stage-chip"
          class:on={activeStage === col.key}
          href="/tracker?stage={col.key}"
          aria-current={activeStage === col.key ? 'true' : undefined}
          data-target-sm
        >
          {col.label}
          <span class="count mono">{col.items.length}</span>
        </a>
      {/each}
    </nav>

    <div class="board scroll-y">
      {#each grouped as col (col.key)}
        <section
          class="column"
          class:hidden-narrow={activeStage !== col.key}
          aria-labelledby={`col-${col.key}`}
        >
          <div class="col-head">
            <h2 id={`col-${col.key}`} class="eyebrow">{col.label}</h2>
            <span class="count num">{col.items.length}</span>
          </div>

          <ul class="cards">
            {#each col.items as item (item.id)}
              <li class="card tracked">
                <p class="role">{item.role_title || 'Role'}</p>
                <p class="t-micro">{item.company_name}</p>

                <p class="t-micro when">
                  {#if item.applied_at}
                    Applied {relativeDay(item.applied_at)}
                  {:else}
                    Saved {relativeDay(item.updated_at)}
                  {/if}
                </p>

                <!--
                  A fact, not a verdict.

                  This used to read "No reply for 21 days" and appear only once
                  a background job had set status='ghosted' — which asserted
                  something about the employer from an absence of data in OUR
                  records, which contain only what the user typed. The silence
                  is reported; what it means is theirs to decide, and the
                  status is theirs to set.
                -->
                {#if item.days_since_activity >= 21 && !['offer', 'rejected', 'withdrawn'].includes(item.status)}
                  <span class="chip chip-warn">
                    No movement for {item.days_since_activity} days
                  </span>
                {/if}

                <div class="card-actions">
                  {#if item.apply_url}
                    <a class="btn btn-sm btn-ghost" href={item.apply_url}
                      target="_blank" rel="noopener noreferrer">Open</a>
                  {/if}
                  {#if nextStatus[item.status]}
                    <!--
                      The label stays put while the request is in flight.
                      Swapping it for "…" shrank the button mid-click, which
                      moved every control below it and told a screen reader
                      nothing. aria-busy says the same thing properly, and the
                      dot carries it visually without changing the width.
                    -->
                    <button
                      class="btn btn-sm"
                      onclick={() => advance(item, nextStatus[item.status]!.to)}
                      disabled={busy === item.id}
                      aria-busy={busy === item.id}
                    >
                      {nextStatus[item.status]!.label}
                      {#if busy === item.id}<span class="spin" aria-hidden="true"></span>{/if}
                    </button>
                  {/if}
                </div>
              </li>
            {/each}

            {#if col.items.length === 0}
              <li class="placeholder t-micro">Nothing here</li>
            {/if}
          </ul>
        </section>
      {/each}
    </div>
  {/if}
</div>

<style>
  .page { display: flex; flex-direction: column; gap: var(--s-4); padding-top: var(--s-6); }


  /* Horizontal scroll is contained here, never on the page body. */
  .stages {
    display: flex; flex-wrap: wrap; gap: var(--s-1);
    margin-bottom: var(--s-3);
  }
  .stage-chip {
    display: inline-flex; align-items: center; gap: var(--s-1);
    min-height: 30px;
    padding: 0 var(--s-2);
    border-radius: var(--radius-full);
    background: var(--bg-raised);
    box-shadow: inset 0 0 0 1px var(--ring-strong);
    color: var(--fg);
    font-size: var(--t-sm);
    text-decoration: none;
  }
  .stage-chip:hover { background: var(--bg-hover); text-decoration: none; }
  .stage-chip.on { background: var(--accent); color: var(--accent-fg); box-shadow: none; }
  .stage-chip .count { font-size: var(--t-xs); color: var(--fg-subtle); }
  .stage-chip.on .count { color: var(--accent-fg); opacity: 0.8; }

  .board {
    display: grid;
    grid-auto-flow: column;
    /* 264px, not 206. Below about 260 a card's title wraps to three lines and
       the company name truncates, which is most of what the card is for. Six
       columns at 264 overflow 1440, so the board scrolls sideways — that is
       the trade the design makes, and it is the right one: a readable card
       you scroll to beats an unreadable one you do not. */
    grid-auto-columns: minmax(264px, 1fr);
    gap: var(--s-3);
    overflow-x: auto;
    padding-bottom: var(--s-3);

    /* A scrolling region has to look like one.
       
       Six 264px columns overflow 1440, which is a deliberate trade — a readable
       card you scroll to beats an unreadable one you do not — but the last
       column was simply cut mid-word at the viewport edge with nothing to say
       there was more. The reader sees a broken layout, not a scrollable one.
       
       Pure CSS, no listener: the two gradients are pinned to the content
       (background-attachment: local) and the two shadows to the container
       (scroll), so each shadow is only visible while there is content past that
       edge. It costs nothing and it disappears on its own at the ends.
       
       Mixed from --fg rather than black: a black shadow on the dark theme's
       near-black ground is invisible, which is exactly how the first version
       shipped looking identical to no affordance at all. */
    background:
      linear-gradient(to right, var(--bg), transparent) left center / 24px 100% no-repeat local,
      linear-gradient(to left, var(--bg), transparent) right center / 24px 100% no-repeat local,
      radial-gradient(farthest-side at 0 50%,
        color-mix(in oklab, var(--fg) 20%, transparent), transparent)
        left center / 16px 100% no-repeat scroll,
      radial-gradient(farthest-side at 100% 50%,
        color-mix(in oklab, var(--fg) 20%, transparent), transparent)
        right center / 16px 100% no-repeat scroll;
  }

  .column { display: flex; flex-direction: column; gap: var(--s-2); min-width: 0; }

  /* Each column scrolls on its own.
     Without this the page is as tall as the BIGGEST column — 33 applications
     made it 5,100px, with five empty columns running alongside one enormous
     one. A funnel whose stages cannot be seen together is not showing a
     funnel. The cap keeps all six on one screen, which is the entire reason
     this view is a board rather than a list. */
  .cards {
    display: flex; flex-direction: column; gap: var(--s-2);
    /* A board with two applications was a 130px strip above 600px of nothing,
       which reads as a page that failed to load rather than a pipeline that is
       mostly empty. The minimum gives the six stages enough height to be
       legible AS a pipeline; the maximum is what keeps them all on one screen
       when they fill up. */
    min-height: clamp(280px, 38vh, 440px);
    max-height: calc(100vh - 15rem);
    overflow-y: auto;
    /* Room for the focus ring on the last card, which a flush overflow clips. */
    padding: 2px;
    margin: -2px;
  }

  @media (max-width: 1039px) {
    /* One column at a time, chosen by the chips above. The board stops being a
       board and becomes a list, so it neither scrolls sideways nor caps its
       own height — the page scroll is the only scroll. */
    .board { display: block; overflow-x: visible; }
    .column.hidden-narrow { display: none; }
    .cards { max-height: none; overflow-y: visible; }
    .col-head { display: none; } /* the chip already names the stage and its count */
  }

  .col-head {
    display: flex; align-items: center; gap: var(--s-2);
    padding: 0 var(--s-1) var(--s-2);
    box-shadow: 0 1px 0 var(--ring);
  }
  .count { margin-left: auto; font-size: var(--t-sm); color: var(--fg-subtle); }


  .tracked {
    display: flex; flex-direction: column; gap: 2px;
    padding: var(--s-3);
  }

  .role { font-weight: 560; font-size: var(--t-base); line-height: 1.3; }
  .when { margin-top: var(--s-1); }

  .card-actions { display: flex; gap: var(--s-1); margin-top: var(--s-2); flex-wrap: wrap; }

  .placeholder {
    padding: var(--s-4) var(--s-3);
    text-align: center;
    border-radius: var(--radius);
    box-shadow: inset 0 0 0 1px var(--ring);
    color: var(--fg-subtle);
  }

  /* A single dot that fades, not a spinner ring: it fits on the button's
     baseline and costs no layout. */
  .spin {
    display: inline-block;
    width: 5px; height: 5px;
    margin-left: var(--s-1);
    border-radius: var(--radius-full);
    background: currentColor;
    animation: pulse 900ms var(--ease) infinite;
  }

  @keyframes pulse { 0%, 100% { opacity: 0.25; } 50% { opacity: 1; } }

  @media (prefers-reduced-motion: reduce) {
    .spin { animation: none; opacity: 0.6; }
  }

  .retry {
    display: inline; padding: 0; margin-left: var(--s-2); border: 0;
    background: none; color: inherit; font: inherit;
    text-decoration: underline; cursor: pointer;
  }

  .banner {
    padding: var(--s-3) var(--s-4);
    border-radius: var(--radius);
    background: var(--shrink-bg);
    color: var(--shrink-ink);
    box-shadow: inset 0 0 0 1px rgb(225 29 72 / 0.25);
  }
</style>
