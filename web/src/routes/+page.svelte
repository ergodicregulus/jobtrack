<script lang="ts">
  import { compactNumber } from '$lib/format';
  import type { PageData } from './$types';

  const { data }: { data: PageData } = $props();

  const points = [
    {
      head: 'From the employer, not a board',
      body: 'Every posting is read from a company’s own applicant tracking system. The apply link goes to their requisition, so it works.'
    },
    {
      head: 'Scores that show their working',
      body: 'Each match names what fit and what did not. A number you cannot interrogate is a number you cannot use.'
    },
    {
      head: 'Undisclosed stays undisclosed',
      body: 'A salary nobody published is shown as undisclosed, never as zero. An estimated date is labelled as an estimate.'
    },
    {
      head: 'Built to stay out of the way',
      body: 'Server-rendered, no framework bloat, works on a laptop that struggles with a modern job board.'
    }
  ];

  /**
   * A worked example of the breakdown, shown rather than described.
   *
   * The one thing that separates this from every other job site is that the
   * score opens up, and four paragraphs claiming so are less convincing than
   * one instance of it. Labelled "Example" in the markup because these are
   * illustrative figures, not a measurement of anything — the ledger rule
   * covers statistics presented as fact about the world, and this is a
   * demonstration of a mechanism.
   *
   * Note the abstaining row. Including it is the point: the honest failure
   * mode is the part nobody else would put on a landing page.
   */
  const example = {
    score: 78,
    band: 'Plausible fit',
    components: [
      { name: 'Skills', score: 26, max: 40, detail: '4 of 6 must-haves — missing Kafka, Terraform' },
      { name: 'Experience', score: 20, max: 20, detail: '7 years against a 5–9 band' },
      { name: 'Location', score: 15, max: 15, detail: 'Remote, and you are open to remote' },
      { name: 'Compensation', score: 0, max: 15, detail: 'Not disclosed — excluded, not counted as zero', neutral: true },
      { name: 'Freshness', score: 9, max: 10, detail: 'Posted yesterday' }
    ]
  };

  const refusals = [
    {
      head: 'It will not apply for you',
      body: 'Mass-applying is what made every inbox unreadable in the first place. You apply; we make the shortlist worth your time.'
    },
    {
      head: 'It will not invent a number',
      body: 'No made-up match percentages, no "estimated salary" presented as fact. When we do not know, the page says so.'
    },
    {
      head: 'It will not scrape behind a login',
      body: 'Only public first-party feeds an employer chose to publish. Nothing is taken from a site that asked us not to.'
    },
    {
      head: 'It will not rewrite your CV with keywords',
      body: 'Keyword stuffing games a filter and insults the reader on the other side of it. We show you the gap instead.'
    }
  ];
</script>

<div class="hero">
  <div class="shell hero-inner">
    <div class="hero-copy">
      <p class="eyebrow">Job search for software engineers</p>
      <h1>Roles straight from the source, scored against what you actually have.</h1>
      <p class="lede">
        JobTrack reads postings from employers' own hiring systems, scores them
        against your profile, and tracks every application in one place.
      </p>

      <div class="cta">
        <a class="btn btn-primary btn-lg" href="/signup">Create an account</a>
        <a class="btn btn-lg" href="/jobs">Browse without an account</a>
      </div>

      {#if data.stats.live_postings > 0}
        <p class="t-small proof">
          <span class="dot"></span>
          {compactNumber(data.stats.live_postings)} live postings from
          {data.stats.companies} companies, refreshed continuously
        </p>
      {/if}
    </div>

    <!-- The right half of this hero used to be empty. It is now the product's
         single most persuasive artefact, at the only size where it is legible
         without scrolling. -->
    <aside class="demo" aria-label="Example of a score breakdown">
      <div class="demo-head">
        <span class="demo-score">{example.score}</span>
        <div>
          <p class="demo-band">{example.band}</p>
          <p class="t-micro">Senior Backend Engineer · Example</p>
        </div>
      </div>

      <ul class="demo-rows">
        {#each example.components as c (c.name)}
          <li class="demo-row" class:neutral={c.neutral}>
            <span class="demo-name t-small">{c.name}</span>
            <span class="demo-track">
              <span class="demo-fill" style:width={`${(c.score / c.max) * 100}%`}></span>
            </span>
            <span class="demo-num num t-small">
              {#if c.neutral}—{:else}{c.score}<span class="of">/{c.max}</span>{/if}
            </span>
            <span class="demo-detail t-micro">{c.detail}</span>
          </li>
        {/each}
      </ul>

      <p class="demo-foot t-micro">
        Every match opens like this. Nothing is weighted in a way you cannot see.
      </p>
    </aside>
  </div>
</div>

<div class="shell sections">
  <section aria-labelledby="how">
    <h2 id="how" class="section-head">What makes it different</h2>
    <ul class="points">
      {#each points as p (p.head)}
        <li class="card point">
          <h3 class="t-heading">{p.head}</h3>
          <p class="t-small">{p.body}</p>
        </li>
      {/each}
    </ul>
  </section>

  <!-- Stating what a product refuses to do is unusual, and it is the most
       honest signal available: anti-features cost something to keep. -->
  <section aria-labelledby="not">
    <h2 id="not" class="section-head">And what it deliberately will not do</h2>
    <ul class="points refusals">
      {#each refusals as r (r.head)}
        <li class="card point">
          <h3 class="t-heading">{r.head}</h3>
          <p class="t-small">{r.body}</p>
        </li>
      {/each}
    </ul>
  </section>

  {#if data.stats.live_postings > 0}
    <section class="card numbers" aria-labelledby="now">
      <h2 id="now" class="eyebrow">Right now</h2>
      <dl>
        <div>
          <dt>Live postings</dt>
          <dd class="num">{compactNumber(data.stats.live_postings)}</dd>
        </div>
        <div>
          <dt>Companies</dt>
          <dd class="num">{data.stats.companies}</dd>
        </div>
        <div>
          <dt>Added this week</dt>
          <dd class="num">{compactNumber(data.stats.added_this_week)}</dd>
        </div>
        <div>
          <dt>Fully remote</dt>
          <dd class="num">{Math.round(data.stats.remote_share * 100)}%</dd>
        </div>
      </dl>
      <p class="t-micro">
        Counted from the database when this page loaded, not from a slide.
      </p>
    </section>
  {/if}

  <!--
    The closing CTA repeats the hero's offer, so its links must not repeat the
    hero's NAMES. Two "Create an account" links on one page give a screen-reader
    user two identical entries in the links list with nothing to choose between
    them, and it is the same ambiguity that made the E2E locator fail.
    Different words for the same destination, which is also better copy at the
    bottom of a page than at the top of one.
  -->
  <section class="closer">
    <h2>Start with the feed. An account is only needed to score it.</h2>
    <div class="cta">
      <a class="btn btn-primary btn-lg" href="/signup">Sign up and score your matches</a>
      <a class="btn btn-lg" href="/jobs">Take me to the feed</a>
    </div>
  </section>
</div>

<style>
  .hero { padding: var(--s-8) 0 var(--s-7); }

  /* Two columns where there is room for two. The copy column is capped by its
     own `ch` measures, so the demo takes the space the text does not need
     rather than the text stretching to an unreadable line length. */
  .hero-inner {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 0.85fr);
    gap: var(--s-7);
    align-items: center;
  }
  @media (max-width: 940px) {
    .hero-inner { grid-template-columns: 1fr; gap: var(--s-6); }
  }

  .hero-copy { display: flex; flex-direction: column; gap: var(--s-4); }
  .lede { max-width: 52ch; }

  h1 {
    /* No max-width and no manual <br>. Both were here, and together they broke
       the headline as "Roles straight from the / source," — a wrap mid-phrase
       that read as a rendering fault. `text-wrap: balance` distributes the
       lines instead of guessing at a width that only holds for one string. */
    font-size: clamp(28px, 4.4vw, 44px);
    /* Tracking tightens as the size grows; at 44px, default spacing reads as a
       word-processor document rather than a designed page. */
    letter-spacing: -0.032em;
    line-height: 1.08;
    font-weight: 660;
    text-wrap: balance;
    max-width: 18ch;
  }

  .lede { font-size: var(--t-lg); color: var(--fg-muted); letter-spacing: var(--tr-lg); }

  .cta { display: flex; gap: var(--s-3); flex-wrap: wrap; margin-top: var(--s-2); }

  .proof { display: inline-flex; align-items: center; gap: var(--s-2); color: var(--fg-muted); }
  .dot {
    width: 7px; height: 7px;
    border-radius: var(--radius-full);
    background: var(--grow);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--grow) 20%, transparent);
  }

  /* ---- The worked example ---- */

  .demo {
    background: var(--bg-raised);
    border-radius: var(--radius-lg);
    box-shadow: var(--e-3);
    padding: var(--s-5);
    display: flex; flex-direction: column; gap: var(--s-4);
  }

  .demo-head { display: flex; align-items: center; gap: var(--s-3); }
  .demo-score {
    font-size: var(--t-3xl);
    font-weight: 660;
    letter-spacing: var(--tr-3xl);
    font-variant-numeric: tabular-nums;
    color: var(--accent-ink);
  }
  .demo-band { font-size: var(--t-md); font-weight: 560; }

  .demo-rows { display: flex; flex-direction: column; gap: var(--s-3); }
  .demo-row {
    display: grid;
    grid-template-columns: 5.5rem minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--s-2) var(--s-3);
  }
  .demo-detail { grid-column: 1 / -1; margin-top: -4px; }

  .demo-track {
    height: 6px;
    border-radius: var(--radius-full);
    background: var(--bg-sunken);
    box-shadow: inset 0 0 0 1px var(--ring);
    overflow: hidden;
  }
  .demo-fill { display: block; height: 100%; background: var(--accent); border-radius: inherit; }

  /* An abstaining component gets a hollow track and an em dash, never a zero
     bar — a zero would read as "scored badly" when the truth is "not scored". */
  .demo-row.neutral .demo-fill { background: none; }
  .demo-row.neutral .demo-num { color: var(--fg-subtle); }

  /* Fixed width, right-aligned. Each row is its own grid, so an `auto` column
     sized itself per row and the bars ended at five different x-positions —
     which made the tracks look like data when they were only measuring the
     width of "26/40" against "—". */
  .demo-num { font-variant-numeric: tabular-nums; min-width: 3.4rem; text-align: right; }
  .of { color: var(--fg-subtle); }

  .demo-foot { border-top: 1px solid var(--border); padding-top: var(--s-3); }

  /* ---- Below the fold ---- */

  .sections { display: flex; flex-direction: column; gap: var(--s-8); padding-bottom: var(--s-8); }

  .section-head {
    font-size: var(--t-xl);
    letter-spacing: var(--tr-xl);
    font-weight: 620;
    margin-bottom: var(--s-4);
    text-wrap: balance;
  }

  .points {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(248px, 1fr));
    gap: var(--s-3);
  }
  .point { display: flex; flex-direction: column; gap: var(--s-2); padding: var(--s-5); }

  /* The refusals read as a set of promises, so they are visually quieter than
     the features — a claim about what you do belongs louder than one about
     what you don't. */
  .refusals .point { background: var(--bg-sunken); box-shadow: none; }

  .numbers { padding: var(--s-5); display: flex; flex-direction: column; gap: var(--s-3); }
  .numbers dl {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: var(--s-4);
  }
  .numbers dt { font-size: var(--t-sm); color: var(--fg-muted); margin-bottom: 2px; }
  .numbers dd {
    font-size: var(--t-2xl);
    letter-spacing: var(--tr-2xl);
    font-weight: 620;
    font-variant-numeric: tabular-nums;
  }

  .closer { display: flex; flex-direction: column; gap: var(--s-4); align-items: flex-start; }
  .closer h2 {
    font-size: var(--t-xl);
    letter-spacing: var(--tr-xl);
    font-weight: 620;
    max-width: 30ch;
    text-wrap: balance;
  }
</style>
