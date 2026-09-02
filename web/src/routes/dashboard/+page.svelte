<script lang="ts">
  import PageHeader from '$lib/components/PageHeader.svelte';
  import StatCard from '$lib/components/StatCard.svelte';
  import ActivityHeatmap from '$lib/components/ActivityHeatmap.svelte';
  import { relativeDay, compactNumber } from '$lib/format';
  import type { PageData } from './$types';

  const { data }: { data: PageData } = $props();

  const d = $derived(data.dashboard);

  // The greeting is time-aware because the same words all day read as canned.
  // Computed on the client so it reflects the reader's clock, not the server's.
  const hour = new Date().getHours();
  const timeGreeting = hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening';

  const bandLabel: Record<string, string> = {
    strong: 'Strong match',
    plausible: 'Plausible',
    stretch: 'Stretch',
    unlikely: 'Unlikely'
  };

  /**
   * True when the location text already conveys the work mode.
   *
   * Employers routinely set the location field to "Remote", which produced
   * "Remote · remote" on every such row — the same fact twice, in two cases,
   * which reads as a rendering fault rather than as data.
   */
  function locationImplies(location: string, mode: string): boolean {
    return location.toLowerCase().includes(mode.toLowerCase());
  }

  const maxStage = $derived(Math.max(1, ...(d?.pipeline ?? []).map((s) => s.count)));
  const pipelineTotal = $derived((d?.pipeline ?? []).reduce((n, s) => n + s.count, 0));

  // "Worth a look" is capped in the copy rather than the query. A user cannot
  // act on 2,568 things, and a four-digit number in a dashboard tile reads as
  // noise the moment it stops being countable.
  const plausibleLabel = $derived(
    !d ? '0' : d.matches.plausible > 999 ? '999+' : String(d.matches.plausible)
  );
</script>

<div class="shell page">
  {#if data.error}
    <p class="banner" role="alert">{data.error}</p>
  {:else if d}
    {#if data.welcome}
      <div class="welcome rise">
        <div>
          <p class="welcome-title">You're set up.</p>
          <p class="t-small">
            We're scoring {compactNumber(d.market.live_postings)} live postings against your
            profile now. Matches appear here as they land.
          </p>
        </div>
        <a class="btn btn-primary" href="/jobs">Browse jobs</a>
      </div>
    {/if}

    <PageHeader
      title={`${timeGreeting}${d.greeting ? `, ${d.greeting.split(' ')[0]}` : ''}`}
      sub={d.matches.new_today > 0
        ? `${d.matches.new_today} new posting${d.matches.new_today === 1 ? '' : 's'} matched you in the last day.`
        : "Nothing new in the last day — here's where things stand."}
    >
      {#snippet action()}
        <a class="btn btn-primary" href="/jobs">Find roles</a>
      {/snippet}
    </PageHeader>

    <!-- Row 1: the numbers worth acting on. -->
    <section class="stats" aria-label="Your matches">
      <StatCard
        label="Strong matches"
        value={d.matches.strong}
        hint="Most requirements met"
        tone="grow"
        href="/jobs?sort=match"
      />
      <StatCard
        label="Worth a look"
        value={plausibleLabel}
        hint="A gap or two"
        href="/jobs?sort=match"
      />
      <StatCard
        label="New today"
        value={d.matches.new_today}
        hint="Posted in the last 24h"
        tone="accent"
        href="/jobs?posted_within=1d"
      />
      <StatCard
        label="Ranked for you"
        value={compactNumber(d.matches.considered)}
        hint="Most recent, of {compactNumber(d.market.live_postings)} live"
      />
    </section>

    <!--
      The tracker leads the page, and its two halves sit side by side.

      The funnel was in the right-hand rail beneath the match list, which put
      the record of what someone is actually DOING below a list of things they
      might do. A job search is mostly the former.

      Paired with the grid rather than stacked full-width for a specific
      reason: the funnel's bar track is capped at 340px on purpose — a 950px
      bar for the number 3 reads as a progress meter that is nearly finished
      rather than as a count — so a full-width panel leaves half of itself
      empty. They also belong together: the funnel is where things stand now,
      the grid is how they got there.

      One column below 62rem, where the grid falls directly beneath the funnel.
    -->
    <div class="tracker-row">
      <!-- Pipeline: a horizontal funnel reads better than a bar chart at this
           size, and needs no charting library. Every stage links into the
           tracker filtered to it — a count you cannot click is a dead end. -->
      <section class="panel pad" aria-labelledby="pl">
        <div class="panel-head bare">
          <h2 id="pl" class="t-heading">Your pipeline</h2>
          <span class="spacer"></span>
          <a class="t-small" href="/tracker">Open tracker</a>
        </div>

        {#if pipelineTotal === 0}
          <p class="t-small">
            Nothing tracked yet. Save a role from the feed and it starts here —
            the tracker is what turns a scattered search into something with a
            shape.
          </p>
          <a class="btn btn-sm" href="/jobs" style="margin-top: var(--s-3)">Find something to save</a>
        {:else}
          <ul class="funnel">
            {#each d.pipeline as stage (stage.status)}
              <li>
                <a class="stage" href="/tracker#{stage.status}">
                  <span class="stage-label t-small">{stage.label}</span>
                  <span class="bar-track">
                    <span
                      class="bar-fill"
                      style:width={`${(stage.count / maxStage) * 100}%`}
                      class:zero={stage.count === 0}
                    ></span>
                  </span>
                  <span class="stage-count num">{stage.count}</span>
                </a>
              </li>
            {/each}
          </ul>
          <p class="t-micro empty-note">
            Empty stages stay visible so the funnel keeps its shape.
          </p>
        {/if}
      </section>

      <!--
        Directly beneath the funnel, because the two answer the same question at
        different resolutions: the funnel is where things stand now, the grid is
        how they got there. Separating them put a match list between a cause and
        its effect.
      -->
      <ActivityHeatmap activity={data.activity} />
    </div>

    <!-- Urgency earns the top of the page, not a slot in a sidebar. Above the
         match list on every screen, so it cannot fall below the fold on a
         phone — but only when it has something to say. An empty panel in the
         most valuable position teaches people to ignore that position. -->
    {#if d.needs_reply.length > 0}
      <section class="panel urgent" aria-labelledby="nr">
        <div class="panel-head">
          <h2 id="nr" class="t-heading">Needs your attention</h2>
        </div>
        <ul class="actions-list">
          {#each d.needs_reply as item (item.id)}
            <li>
              <a href="/tracker" class="action">
                <span class="pulse" aria-hidden="true"></span>
                <span class="action-body">
                  <span class="action-role">{item.role_title || 'Role'}</span>
                  <span class="t-micro">
                    {item.company_name} · no movement in {item.days_since} days
                  </span>
                </span>
              </a>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    <!--
      Matches beside the pipeline: two thirds to one third.

      An earlier revision stacked everything full width to kill a void left by
      a rail of conditional panels. That fixed the void and lost the glance —
      the pipeline is a "how am I doing" widget and belongs beside the list, not
      below six rows of it. The void is now filled by the activity grid, which
      is full width because twelve weeks needs the room.
    -->
    <!--
      Two columns only when there IS a second column.

      The rail holds "Sharpen your matches", which hides itself at 100% — good
      reasoning with a bad consequence: the 2fr/1fr grid kept the empty 1fr, so
      a complete profile got a half-width matches panel beside a void, directly
      under two full-width rows. That ragged right edge is most of why the page
      read as unfinished.
    -->
    <div data-dash-two={d.strength.percent < 100 ? 'pair' : 'single'}>
      <!-- Top matches: the reason someone opens this page. -->
      <section class="panel matches-panel" aria-labelledby="tm">
        <div class="panel-head">
          <h2 id="tm" class="t-heading">Your best matches</h2>
          <span class="spacer"></span>
          <a class="t-small" href="/jobs?sort=match">See all</a>
        </div>

        {#if d.top_matches.length === 0}
          <div class="empty">
            <p class="empty-title">No matches scored yet</p>
            <p class="t-small">
              Scoring runs in the background and usually takes a minute or two
              after you finish your profile. Refresh shortly.
            </p>
          </div>
        {:else}
          <ul class="matches">
            {#each d.top_matches as job (job.id)}
              <li class="match">
                <div class="match-main">
                  <div class="row-tight wrap">
                    <!-- The band is the score's title attribute, not a repeated
                         label: six identical "Strong match" strings down the
                         right edge are noise, and the colour plus the number
                         already carry the meaning. -->
                    <span class="score" data-band={job.band} title={bandLabel[job.band] ?? job.band}>
                      {Math.round(job.score)}
                    </span>
                    <a class="match-title" href="/jobs/{job.id}">{job.title}</a>
                  </div>
                  <p class="meta">
                    <span>{job.company_name}</span>
                    <span class="meta-sep">·</span>
                    <span class="loc" title={job.location}>
                      {job.location || 'Location not stated'}
                    </span>
                    {#if job.mode && !locationImplies(job.location, job.mode)}
                      <span class="meta-sep">·</span>
                      <span>{job.mode}</span>
                    {/if}
                    <span class="meta-sep">·</span>
                    <span>{relativeDay(job.posted_at)}</span>
                  </p>
                  {#if job.missing_skills.length}
                    <!-- Showing what is MISSING is the honest version of a match
                         score. A number alone invites the user to trust it; the
                         gaps let them judge it. -->
                    <p class="t-micro missing">
                      Missing: {job.missing_skills.slice(0, 4).join(', ')}
                    </p>
                  {/if}
                </div>
                <a class="btn btn-sm apply" href={job.apply_url} target="_blank" rel="noopener noreferrer">
                  Apply
                  <span class="sr-only">to {job.title} at {job.company_name} (opens in a new tab)</span>
                </a>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <div class="rail">
        <!-- Profile strength.
             Placed on the dashboard rather than buried on the profile page
             because match quality is the product and this is the only control
             the user has over it. Each row states what the field CHANGES, not
             merely that it is empty — a completeness bar with no stated
             consequence is a vanity metric people learn to ignore. Hidden once
             complete: a permanent 100% badge is decoration. -->
        {#if d.strength.percent < 100}
          <section class="panel pad" aria-labelledby="ps">
            <div class="panel-head bare">
              <h2 id="ps" class="t-heading">Sharpen your matches</h2>
              <span class="spacer"></span>
              <span class="t-small num">{d.strength.percent}%</span>
            </div>

            <div class="progress" style="margin-bottom: var(--s-4)">
              <div class="progress-fill" style:width={`${d.strength.percent}%`}></div>
            </div>

            <ul class="strength">
              {#each d.strength.items.filter((i) => !i.done).slice(0, 3) as item (item.field)}
                <li>
                  <a href="/profile" class="strength-row">
                    <span class="strength-label">{item.label}</span>
                    <span class="t-micro">{item.why}</span>
                  </a>
                </li>
              {/each}
            </ul>
          </section>
        {/if}

      </div>
    </div>


    <!-- Market context spans the full width rather than sitting in a column.
         It is background, not a task, so it belongs at the bottom where it
         closes the page instead of competing with the matches — and it fills
         the void the two-column layout otherwise left underneath itself. -->
    <section class="panel market-strip" aria-labelledby="mk">
      <h2 id="mk" class="eyebrow">The market right now</h2>
      <dl class="market">
        <div><dt>Live postings</dt><dd class="num">{compactNumber(d.market.live_postings)}</dd></div>
        <div><dt>Companies tracked</dt><dd class="num">{d.market.companies}</dd></div>
        <div><dt>Added this week</dt><dd class="num">{compactNumber(d.market.added_this_week)}</dd></div>
        <div><dt>Fully remote</dt><dd class="num">{Math.round(d.market.remote_share * 100)}%</dd></div>
      </dl>
    </section>
  {/if}
</div>

<style>
  .page { display: flex; flex-direction: column; gap: var(--s-5); padding-top: var(--s-6); }


  .welcome {
    display: flex; align-items: center; gap: var(--s-4); flex-wrap: wrap;
    padding: var(--s-4);
    border-radius: var(--radius-md);
    background: var(--accent-bg);
    box-shadow: inset 0 0 0 1px var(--ring-accent);
  }
  .welcome > div { flex: 1; min-width: 240px; }
  .welcome-title { font-weight: 600; color: var(--accent-ink); }

  .stats {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    gap: var(--s-3);
  }

  .rail { display: flex; flex-direction: column; gap: var(--s-4h); }

  /* The list panel drops its own padding so a row can span the full card and
     take a hover background edge to edge — a hover that stops short of the
     card edge reads as a misaligned box rather than as a row. The padding
     moves onto the head and the rows instead. */
  .matches-panel { padding: 0; }
  .matches-panel .panel-head {
    margin: 0;
    padding: var(--s-3) var(--s-4h);
    box-shadow: 0 1px 0 0 var(--ring);
  }
  .matches-panel .empty { padding: var(--s-4h); }

  .pad { padding: var(--s-4h); }
  .panel-head.bare { box-shadow: none; margin: 0 0 var(--s-3); padding: 0; }

  /* A stage is a link, so the count is clickable — a number you cannot act on
     is a dead end, and every one of these has a filtered view behind it. */
  .stage {
    display: grid; grid-template-columns: 88px minmax(0, 1fr) 28px;
    align-items: center; gap: var(--s-2);
    padding: var(--s-1) 0;
    color: inherit; text-decoration: none;
    border-radius: var(--radius-sm);
  }
  .stage:hover { background: var(--bg-hover); text-decoration: none; }
  .empty-note { margin-top: var(--s-3); color: var(--fg-subtle); line-height: 1.6; }

  .matches { display: flex; flex-direction: column; }
  .match {
    display: flex; align-items: center; gap: var(--s-3);
    padding: var(--row-y) var(--s-4h);
    box-shadow: 0 1px 0 var(--ring);
  }
  .match:last-child { box-shadow: none; }
  .match:hover { background: var(--bg-hover); }

  /* Full width earned the row an action. Reading "your best matches" and
     having to click through to do anything about them was a list that
     described work rather than starting it. */
  .apply { flex: none; }
  .match:hover .apply, .match:focus-within .apply { background: var(--bg-hover); }
  .match:last-child { box-shadow: none; padding-bottom: 0; }
  .match-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }

  .match-title {
    font-weight: 560;
    color: var(--fg);
    font-size: var(--t-md);
    letter-spacing: var(--tr-md);
  }
  .match-title:hover { color: var(--accent); }

  /* The score is a numeral, not a badge: at 6 rows a row of coloured pills
     becomes noise, while a tabular figure stays scannable. */
  .score {
    display: inline-flex; align-items: center; justify-content: center;
    min-width: 30px; height: 22px;
    padding: 0 5px;
    border-radius: var(--radius-sm);
    font-size: var(--t-sm);
    font-weight: 640;
    font-variant-numeric: tabular-nums;
    background: var(--bg-sunken);
    color: var(--fg-muted);
  }
  .score[data-band='strong']    { background: var(--grow-bg); color: var(--grow-ink); }
  .score[data-band='plausible'] { background: var(--accent-bg); color: var(--accent-ink); }
  .score[data-band='stretch']   { background: var(--uncertain-bg); color: var(--uncertain-ink); }

  .missing { color: var(--shrink-ink); }

  /* A posting listing six offices produces a 400px meta line that shoves the
     date off the row. One line, ellipsised, with the full text on hover. */
  /* Wide enough for "San Francisco, New York, or Remote" now that the row is
     full width. The old 22ch was sized for the rail and truncated to
     "San Francisco, Ne…", which hid the fact that a role had several
     locations — exactly the detail that decides whether it is worth a click. */
  .loc {
    max-width: 46ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .actions-list { display: flex; flex-direction: column; gap: var(--s-1); }
  .action {
    display: flex; align-items: flex-start; gap: var(--s-3);
    padding: var(--s-2) var(--s-2) var(--s-2) 0;
    border-radius: var(--radius-sm);
    color: var(--fg);
  }
  .action:hover { text-decoration: none; background: var(--bg-hover); padding-left: var(--s-2); }
  .action-body { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .action-role { font-weight: 540; font-size: var(--t-base); }

  .pulse {
    flex: none;
    width: 7px; height: 7px;
    margin-top: 6px;
    border-radius: var(--radius-full);
    background: var(--uncertain);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--uncertain) 20%, transparent);
  }

  .tracker-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--s-4);
    align-items: start;
  }

  @media (max-width: 62rem) {
    .tracker-row { grid-template-columns: minmax(0, 1fr); }
  }

  .funnel { display: flex; flex-direction: column; gap: var(--s-2); }
  /* The track is capped rather than fluid. Full width turned a funnel whose
     largest stage is a handful of applications into a 950px bar for the number
     3, which reads as a progress meter that is nearly finished rather than as
     a count. A bar's length only means something against a neighbouring bar,
     so the comparison needs to fit in one glance. */
  .stage { display: grid; grid-template-columns: 88px minmax(0, 340px) 28px; align-items: center; gap: var(--s-3); }
  .bar-track { height: 6px; background: var(--bg-sunken); border-radius: var(--radius-full); overflow: hidden; }
  .bar-fill {
    display: block; height: 100%;
    background: var(--accent);
    border-radius: inherit;
    transition: width var(--slow) var(--ease);
  }
  /* An empty stage still needs a visible track, or the funnel looks truncated
     rather than empty. */
  .bar-fill.zero { background: transparent; }
  .stage-count { text-align: right; font-size: var(--t-sm); color: var(--fg-muted); }

  .strength { display: flex; flex-direction: column; gap: var(--s-1); }

  .strength-row {
    display: flex; flex-direction: column; gap: 1px;
    padding: var(--s-2);
    margin: 0 calc(-1 * var(--s-2));
    border-radius: var(--radius-sm);
    color: var(--fg);
  }
  .strength-row:hover { background: var(--bg-hover); text-decoration: none; }
  .strength-label { font-size: var(--t-base); font-weight: 540; }

  .market-strip { display: flex; flex-direction: column; gap: var(--s-3); }

  .market {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: var(--s-4);
    margin: 0;
  }
  .market > div { display: flex; flex-direction: column-reverse; gap: 2px; }
  .market dt { font-size: var(--t-sm); color: var(--fg-muted); }
  .market dd {
    margin: 0;
    font-size: var(--t-xl);
    letter-spacing: var(--tr-xl);
    font-weight: 600;
  }

  .banner {
    padding: var(--s-4);
    border-radius: var(--radius);
    background: var(--shrink-bg);
    color: var(--shrink-ink);
    box-shadow: inset 0 0 0 1px rgb(225 29 72 / 0.25);
  }
</style>
