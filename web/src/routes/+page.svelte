<script lang="ts">
  import { compactNumber } from '$lib/format';
  import IngestChart from '$lib/components/IngestChart.svelte';
  import AbsenceField from '$lib/components/AbsenceField.svelte';
  import { reveal } from '$lib/reveal';
  import type { PageData } from './$types';

  const { data }: { data: PageData } = $props();

  /**
   * A worked example of the breakdown, shown rather than described.
   *
   * The one thing separating this from every other job site is that the score
   * opens up, and four paragraphs claiming so are less convincing than one
   * instance of it. Labelled "example" in the markup because these are
   * illustrative figures, not a measurement of anything — the ledger rule
   * covers statistics presented as fact about the world, and this demonstrates
   * a mechanism.
   *
   * The abstaining row is the point of the whole component. It is the honest
   * failure mode, and it is the part nobody else would put on a landing page.
   */
  const report = {
    role: 'Senior Backend Engineer',
    score: 78,
    band: 'Plausible fit',
    rows: [
      { name: 'Skills', got: 26, max: 40, note: '4 of 6 must-haves. Missing Kafka and Terraform.' },
      { name: 'Experience', got: 20, max: 20, note: '7 years against a stated 5–9 band.' },
      { name: 'Location', got: 15, max: 15, note: 'Remote, and you are open to remote.' },
      {
        name: 'Compensation',
        got: null,
        max: 15,
        note: 'The employer published no salary. Excluded from the total — not counted as zero.'
      },
      { name: 'Freshness', got: 9, max: 10, note: 'Posted yesterday.' }
    ]
  };

  /**
   * Coverage, stated in both directions.
   *
   * The right-hand column is the more useful half and no competitor will
   * publish it. A reader deciding whether to trust the left column needs to
   * know where it stops.
   */
  const seen = [
    ['Greenhouse, Ashby, SmartRecruiters', 'read directly, every few hours'],
    ["Each employer's own requisition", 'the apply link lands in a real queue'],
    ['Salary where it is published', `${data.stats.live_postings ? '' : ''}shown exactly as stated`],
    ['Skills a posting actually names', 'matched against what you have']
  ];

  const unseen = [
    ['LinkedIn', 'no route to its index without an account, and we never make one'],
    ['Naukri, Indeed', 'aggregators; we read the source instead'],
    ['Anything behind a login', 'a site that asked us not to read it is not read'],
    ['Roles never posted publicly', 'referral-only hiring is invisible to everyone, us included']
  ];

  const refusals = [
    ['It will not apply for you', 'Mass-applying is what made every inbox unreadable. You apply; we make the shortlist worth your time.'],
    ['It will not invent a number', 'No made-up match percentages, no "estimated salary" presented as fact. When we do not know, the page says so.'],
    ['It will not scrape behind a login', 'Only public first-party feeds an employer chose to publish.'],
    ['It will not rewrite your CV with keywords', 'Keyword stuffing games a filter and insults the reader on the other side of it. We show you the gap instead.']
  ];

  const share = (x: number) => (data.stats.live_postings ? `${Math.round(x * 100)}%` : '—');

  // The software share is the claim in the masthead, counted. The unclassified
  // share sits beside it because the first number alone would read as "the
  // rest is not software", and most of the rest is the classifier abstaining.
  const figures = $derived([
    { label: 'Live postings', value: compactNumber(data.stats.live_postings) },
    { label: 'Employers', value: String(data.stats.companies) },
    { label: 'Added this week', value: compactNumber(data.stats.added_this_week) },
    { label: 'Fully remote', value: share(data.stats.remote_share) },
    { label: 'Software engineering', value: share(data.stats.software_share) },
    { label: 'Not yet classified', value: share(data.stats.unclassified_share) }
  ]);
</script>

<svelte:head>
  <title>JobTrack — a job search that tells you what it doesn't know</title>
  <meta
    name="description"
    content="Roles read from employers' own hiring systems, scored against your profile, with every score opened up — including the parts it refused to guess."
  />
</svelte:head>

<div class="page">
  <!--
    The report header. Mono, ruled, and carrying a real timestamp — the visual
    grammar of a measurement rather than a marketing page, and it is true: these
    figures were counted when this page was served.
  -->
  <header class="masthead">
    <span class="meta">Job search instrument · software engineering</span>
    <span class="rule" aria-hidden="true"></span>
  </header>

  <section class="hero">
    <h1>
      A job search that tells you<br />
      what it <em>doesn't know</em>.
    </h1>
    <p class="lede">
      JobTrack reads postings from employers' own hiring systems and scores them against your
      profile. Every score opens up — including the parts it refused to guess.
    </p>
    <div class="actions">
      <a class="btn primary" href="/signup">Create an account</a>
      <a class="btn" href="/jobs">Browse without one</a>
    </div>

    <!--
      Proof at the point of decision, not three screens further down. A reader
      deciding whether to sign up is weighing one question — is there anything
      in here for me — and a real count answers it where "thousands of
      opportunities" does not.
    -->
    {#if data.stats.live_postings}
      <p class="proof">
        <span class="dot" aria-hidden="true"></span>
        <span class="num">{compactNumber(data.stats.live_postings)}</span> live postings from
        <span class="num">{data.stats.companies}</span> employers, read again every few hours
      </p>
    {/if}
  </section>

  <!--
    THE SIGNATURE.

    The headline says the product tells you what it does not know; this is that
    sentence drawn. It sits directly under the hero because it is the argument,
    not an illustration of one — and because the hero was a headline in a
    half-empty frame, which is the shape of a page that has nothing to show.
  -->
  {#if data.coverage && data.coverage.live > 0}
    <section use:reveal class="reveal-target signature" aria-label="What the corpus does not know">
      <AbsenceField coverage={data.coverage} shows={data.shows} />
    </section>
  {/if}

  <!--
    The signature element.

    A score breakdown at full width, set like an instrument readout: the total
    at left, the components as measured rows, and the abstaining row given more
    visual weight than the rows that scored. That inversion is deliberate. Every
    other product buries what it could not determine; here it is the thing the
    eye lands on.
  -->
  <section use:reveal class="reveal-target report" aria-label="Example score breakdown">
    <div class="report-head">
      <span class="tag">Example breakdown</span>
      <span class="role">{report.role}</span>
    </div>

    <div class="report-body">
      <div class="total">
        <span class="score">{report.score}</span>
        <span class="band">{report.band}</span>
        <span class="outof">of 85 available</span>
      </div>

      <ol class="rows">
        {#each report.rows as row (row.name)}
          {@const abstained = row.got === null}
          <li class:abstained>
            <span class="row-name">{row.name}</span>
            <span class="track" aria-hidden="true">
              {#if abstained}
                <span class="fill-none"></span>
              {:else}
                <span class="fill" style:width="{(row.got! / row.max) * 100}%"></span>
              {/if}
            </span>
            <span class="value">
              {#if abstained}
                <span class="dash">—</span>
                <span class="stamp">abstained</span>
              {:else}
                {row.got}<span class="sep">/</span>{row.max}
              {/if}
            </span>
            <span class="note">{row.note}</span>
          </li>
        {/each}
      </ol>
    </div>

    <p class="report-foot">
      Nothing is weighted in a way you cannot see. A component that abstains leaves the total
      smaller, so an unknown never poses as a zero.
    </p>
  </section>

  <!--
    Coverage in both directions. The right column is the distinctive asset:
    stating what we cannot see is what makes the left column credible.
  -->
  <section use:reveal class="reveal-target ledger">
    <h2 class="section-head">What we can see, and what we cannot</h2>
    <div class="cols">
      <div class="col">
        <h3 class="col-head can">Read directly</h3>
        <dl>
          {#each seen as [term, detail] (term)}
            <dt>{term}</dt>
            <dd>{detail}</dd>
          {/each}
        </dl>
      </div>
      <div class="col">
        <h3 class="col-head cannot">Out of reach</h3>
        <dl>
          {#each unseen as [term, detail] (term)}
            <dt>{term}</dt>
            <dd>{detail}</dd>
          {/each}
        </dl>
      </div>
    </div>
  </section>

  <section use:reveal class="reveal-target figures" aria-label="Current corpus">
    <h2 class="section-head">Counted when this page loaded</h2>
    <dl class="figure-row">
      {#each figures as f (f.label)}
        <div class="figure">
          <dt>{f.label}</dt>
          <dd>{f.value}</dd>
        </div>
      {/each}
    </dl>
    <p class="figure-foot">
      Read from the database as this page was served, not from a slide written last quarter.
    </p>

    <!--
      The same claim, over time. The figures above are a snapshot and a snapshot
      can be staged; a month of daily counts cannot, and it shows the boring days
      as well as the good ones.
    -->
    <div class="chart-wrap">
      <IngestChart ingest={data.ingest} />
    </div>
  </section>

  <section use:reveal class="reveal-target refusals">
    <h2 class="section-head">What it will not do</h2>
    <ul>
      {#each refusals as [head, body] (head)}
        <li>
          <span class="no" aria-hidden="true">✕</span>
          <span class="refusal-body">
            <strong>{head}</strong>
            {body}
          </span>
        </li>
      {/each}
    </ul>
  </section>

  <section use:reveal class="reveal-target close">
    <h2>Start with the feed.<br />An account is only needed to score it.</h2>
    <div class="actions">
      <a class="btn primary" href="/signup">Create an account</a>
      <a class="btn" href="/jobs">Take me to the feed</a>
    </div>
  </section>
</div>

<style>
  /* ---------------------------------------------------------------------------
     The landing page is the one place with its own layout language.

     Every rule here is scoped to this route: the shared system lives in
     app.css, and nothing below is reused elsewhere. The measure is narrow and
     the rhythm is vertical — a report reads top to bottom, and the previous
     version's side-by-side hero fought that.
     --------------------------------------------------------------------------- */

  .page {
    --measure: 62rem;
    max-width: var(--measure);
    margin: 0 auto;
    padding: 0 clamp(1.25rem, 5vw, 2.5rem) 6rem;
  }

  /* Report masthead ------------------------------------------------------- */

  .masthead {
    display: flex;
    align-items: center;
    gap: 0.9rem;
    padding: 2.5rem 0 0;
  }

  .rule {
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  .meta {
    font-family: var(--font-mono);
    font-size: var(--t-xs);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--fg-subtle);
  }

  /* Hero ------------------------------------------------------------------ */

  .hero {
    padding: clamp(3rem, 9vw, 5.5rem) 0 clamp(2.5rem, 6vw, 4rem);
  }

  h1 {
    /* The one place the type is allowed to be loud. Heavy, tight, and large
       enough that the width axis we declined to buy is not missed. */
    font-size: clamp(2.5rem, 7.5vw, 4.4rem);
    line-height: 1.03;
    letter-spacing: -0.038em;
    font-weight: 700;
    margin: 0;
    text-wrap: balance;
  }

  h1 em {
    font-style: normal;
    color: var(--accent-ink);
    /* Underlined rather than coloured alone: the phrase carries the page's
       whole argument, and colour is never the only signal. */
    box-shadow: inset 0 -0.14em 0 var(--accent-line);
  }

  .lede {
    max-width: 46ch;
    margin: 1.5rem 0 0;
    font-size: var(--t-lg);
    line-height: 1.55;
    color: var(--fg-muted);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.6rem;
    margin-top: 2rem;
  }

  .btn {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 0 1.15rem;
    border-radius: var(--radius-md);
    font-size: var(--t-md);
    font-weight: 600;
    text-decoration: none;
    color: var(--fg);
    box-shadow: 0 0 0 1px var(--ring-strong);
    transition: background var(--fast) var(--ease);
  }

  .btn:hover {
    background: var(--bg-hover);
  }

  .btn.primary {
    background: var(--accent);
    color: var(--accent-fg);
    box-shadow: none;
  }

  .btn.primary:hover {
    background: var(--accent-hover);
  }

  .proof {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin: 1.4rem 0 0;
    font-size: var(--t-sm);
    color: var(--fg-subtle);
  }

  .proof .num {
    font-family: var(--font-mono);
    font-weight: 500;
    color: var(--fg);
    font-variant-numeric: tabular-nums;
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: var(--radius-full);
    background: var(--grow);
    flex: none;
  }

  /* The readout ----------------------------------------------------------- */

  .report {
    border-radius: var(--radius-lg);
    background: var(--bg-raised);
    box-shadow: var(--e-2);
    overflow: hidden;
  }

  .report-head {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
    flex-wrap: wrap;
    padding: 1rem 1.4rem;
    border-bottom: 1px solid var(--border);
    background: var(--bg-sunken);
  }

  .tag {
    font-family: var(--font-mono);
    font-size: var(--t-xs);
    letter-spacing: 0.07em;
    text-transform: uppercase;
    color: var(--fg-subtle);
  }

  .role {
    font-size: var(--t-md);
    font-weight: 600;
  }

  .report-body {
    display: grid;
    grid-template-columns: minmax(0, 8.5rem) minmax(0, 1fr);
    gap: clamp(1rem, 3vw, 2.25rem);
    padding: 1.6rem 1.4rem;
  }

  .total {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    align-self: start;
    padding-top: 0.1rem;
  }

  .score {
    font-size: clamp(3.4rem, 9vw, 4.6rem);
    font-weight: 700;
    line-height: 0.9;
    letter-spacing: -0.05em;
    font-variant-numeric: tabular-nums;
  }

  .band {
    font-size: var(--t-md);
    font-weight: 600;
    color: var(--grow-ink);
  }

  .outof {
    font-family: var(--font-mono);
    font-size: var(--t-xs);
    color: var(--fg-subtle);
  }

  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }

  .rows li {
    display: grid;
    grid-template-columns: minmax(0, 7.5rem) minmax(0, 1fr) auto;
    grid-template-areas:
      'name track value'
      'note note  note';
    align-items: center;
    gap: 0.35rem 0.85rem;
    padding: 0.7rem 0;
    border-top: 1px solid var(--border);
  }

  .rows li:first-child {
    border-top: 0;
    padding-top: 0;
  }

  .row-name {
    grid-area: name;
    font-size: var(--t-base);
    font-weight: 600;
  }

  .track {
    grid-area: track;
    display: block;
    height: 6px;
    border-radius: var(--radius-full);
    background: var(--bg-active);
    overflow: hidden;
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--accent);
  }

  .value {
    grid-area: value;
    font-family: var(--font-mono);
    font-size: var(--t-base);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .sep {
    /* --fg-subtle, not --fg-faint: the slash is rendered text, and --fg-faint
       only clears the 3:1 required of non-text. tokens.test.ts enforces this,
       and caught it here. */
    color: var(--fg-subtle);
    padding: 0 0.1em;
  }

  .note {
    grid-area: note;
    font-size: var(--t-sm);
    color: var(--fg-subtle);
    line-height: 1.45;
  }

  /* The abstaining row is the loudest thing in the component, not the
     quietest. A hatched track reads as "no measurement taken" rather than as
     an empty bar, which is what a zero would look like. */
  .rows li.abstained {
    background: var(--uncertain-bg);
    margin: 0 -1.4rem;
    padding-left: 1.4rem;
    padding-right: 1.4rem;
    box-shadow: inset 3px 0 0 var(--uncertain);
  }

  .fill-none {
    display: block;
    height: 100%;
    background-image: repeating-linear-gradient(
      -45deg,
      var(--uncertain-line) 0 2px,
      transparent 2px 6px
    );
  }

  .abstained .dash {
    color: var(--uncertain-ink);
    font-weight: 600;
  }

  .stamp {
    font-family: var(--font-mono);
    font-size: var(--t-xs);
    letter-spacing: 0.07em;
    text-transform: uppercase;
    color: var(--uncertain-ink);
    margin-left: 0.5rem;
  }

  .abstained .note {
    color: var(--fg-muted);
  }

  .report-foot {
    margin: 0;
    padding: 1rem 1.4rem;
    border-top: 1px solid var(--border);
    background: var(--bg-sunken);
    font-size: var(--t-sm);
    color: var(--fg-muted);
    line-height: 1.5;
  }

  /*
    The signature gets more air than any other section. Tines and Wispr both
    earn their sense of craft partly by letting one element own a screen; the
    previous version gave every section the same margin, which reads as a list.
  */
  .signature {
    margin: clamp(3rem, 7vw, 6rem) 0 clamp(3.5rem, 8vw, 7rem);
  }

  /* Shared section rhythm -------------------------------------------------- */

  .ledger,
  .figures,
  .refusals,
  .close {
    padding-top: clamp(3.5rem, 8vw, 5.5rem);
  }

  .section-head {
    font-size: var(--t-xl);
    font-weight: 700;
    letter-spacing: var(--tr-xl);
    margin: 0 0 1.5rem;
  }

  /* Coverage ledger -------------------------------------------------------- */

  .cols {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(17rem, 1fr));
    gap: 1.5rem 2.5rem;
  }

  .col-head {
    font-family: var(--font-mono);
    font-size: var(--t-xs);
    letter-spacing: 0.07em;
    text-transform: uppercase;
    margin: 0 0 0.9rem;
    padding-bottom: 0.5rem;
    border-bottom: 1px solid var(--border);
  }

  .col-head.can {
    color: var(--grow-ink);
  }

  .col-head.cannot {
    color: var(--shrink-ink);
  }

  .col dl {
    margin: 0;
  }

  .col dt {
    font-size: var(--t-base);
    font-weight: 600;
    margin-top: 0.9rem;
  }

  .col dd {
    margin: 0.15rem 0 0;
    font-size: var(--t-sm);
    color: var(--fg-subtle);
    line-height: 1.5;
  }

  /* Live figures ----------------------------------------------------------- */

  .figure-row {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(9rem, 1fr));
    gap: 1.25rem;
    margin: 0;
    padding-top: 0.4rem;
    border-top: 2px solid var(--fg);
  }

  .figure dt {
    font-family: var(--font-mono);
    font-size: var(--t-xs);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--fg-subtle);
  }

  .figure dd {
    margin: 0.2rem 0 0;
    font-size: clamp(2rem, 5vw, 2.6rem);
    font-weight: 700;
    letter-spacing: -0.035em;
    font-variant-numeric: tabular-nums;
    line-height: 1;
  }

  .figure-foot {
    margin: 1.1rem 0 0;
    font-size: var(--t-sm);
    color: var(--fg-subtle);
  }

  .chart-wrap {
    margin-top: clamp(2rem, 4vw, 3rem);
    padding-top: clamp(1.5rem, 3vw, 2rem);
    border-top: 1px solid var(--border);
  }

  /* Refusals --------------------------------------------------------------- */

  .refusals ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .refusals li {
    display: flex;
    gap: 0.9rem;
    padding: 1rem 0;
    border-top: 1px solid var(--border);
  }

  .refusals li:first-child {
    border-top: 0;
  }

  .no {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--t-base);
    line-height: 1.5;
    color: var(--shrink-ink);
  }

  .refusal-body {
    font-size: var(--t-base);
    color: var(--fg-subtle);
    line-height: 1.55;
  }

  .refusal-body strong {
    display: block;
    color: var(--fg);
    font-weight: 600;
  }

  /* Close ------------------------------------------------------------------ */

  .close h2 {
    font-size: clamp(1.7rem, 4.5vw, 2.4rem);
    font-weight: 700;
    letter-spacing: -0.03em;
    line-height: 1.12;
    margin: 0;
    text-wrap: balance;
  }

  @media (max-width: 34rem) {
    .report-body {
      grid-template-columns: minmax(0, 1fr);
    }

    .rows li {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'name  value'
        'track track'
        'note  note';
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .btn {
      transition: none;
    }
  }
</style>
