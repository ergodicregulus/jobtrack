<script lang="ts">
  /**
   * The frame shared by sign-in and sign-up.
   *
   * Two panes: the form on the left, a statement of what the product does on
   * the right. The right pane is not decoration — someone arriving from a link
   * needs to know what they are signing into, and a bare form on an empty page
   * gives them nothing to decide with. It collapses away below 860px, where the
   * form is the only thing that matters.
   */
  interface Props {
    title: string;
    subtitle: string;
    children: import('svelte').Snippet;
    footer: import('svelte').Snippet;
  }
  const { title, subtitle, children, footer }: Props = $props();

  const points = [
    {
      head: 'Straight from the employer',
      body: 'Postings come from company applicant tracking systems, so the apply link lands in a real requisition queue — not a reposted listing that closed weeks ago.'
    },
    {
      head: 'Matched on what you actually have',
      body: 'Scores are built from your skills, experience and location, and every one shows its reasoning — including what you are missing.'
    },
    {
      head: 'Nothing hidden behind a filter',
      body: 'Undisclosed salary is shown as undisclosed, never as zero. An estimated date is labelled as an estimate.'
    }
  ];
</script>

<div class="auth">
  <div class="pane form-pane">
    <div class="form-inner">
      <a class="brand" href="/">
        <span class="brand-rail" aria-hidden="true"></span>
        <span>JobTrack</span>
      </a>

      <div class="head">
        <h1>{title}</h1>
        <p class="t-muted">{subtitle}</p>
      </div>

      {@render children()}

      <div class="foot t-small">{@render footer()}</div>
    </div>
  </div>

  <aside class="pane pitch" aria-hidden="true">
    <div class="pitch-inner">
      <p class="eyebrow">Why this exists</p>
      <ul class="points">
        {#each points as p (p.head)}
          <li>
            <span class="dot"></span>
            <div>
              <p class="point-head">{p.head}</p>
              <p class="t-small">{p.body}</p>
            </div>
          </li>
        {/each}
      </ul>
    </div>
  </aside>
</div>

<style>
  /* Content-sized and top-aligned, per the design.
     
     It was a 50/50 grid at min-height:100vh with both panes vertically
     centred, which produced two effects the design avoids: a full-height rule
     down the middle that made the page read as two unrelated halves, and 277px
     of empty space above the form on a 900px viewport. The design sizes the
     panes to their content, centres the PAIR horizontally, and starts them
     near the top. */
  .auth {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--s-6);
    padding: var(--s-4) var(--s-5) var(--s-7);
  }
  @media (min-width: 860px) {
    .auth {
      flex-direction: row;
      justify-content: center;
      gap: var(--s-8);
      padding-top: var(--s-7);
    }
  }

  .form-inner { width: 100%; max-width: 380px; display: flex; flex-direction: column; gap: var(--s-5); }
  .form-pane { flex: none; }

  /* The rule belongs to the pitch, not to the page: it separates two blocks of
     content rather than slicing the window in half. Hidden below 860px, where
     the pitch sits underneath rather than beside. */
  .pitch { display: none; }
  @media (min-width: 860px) {
    .pitch {
      display: block;
      flex: 0 1 420px;
      padding-left: var(--s-8);
      box-shadow: inset 1px 0 0 0 var(--ring);
    }
  }

  .brand {
    display: inline-flex; align-items: center; gap: var(--s-2);
    color: var(--fg); font-weight: 650; letter-spacing: -0.02em;
    font-size: var(--t-md);
  }
  .brand:hover { text-decoration: none; }
  .brand-rail { width: 3px; height: 17px; background: var(--grow); border-radius: 2px; }

  .head { display: flex; flex-direction: column; gap: 6px; }
  .head h1 { font-size: var(--t-2xl); letter-spacing: var(--tr-2xl); }

  .foot { color: var(--fg-muted); }

  /* No background fill.
     
     A sunken surface made sense when the pane was a full-height column; on a
     content-sized pane it becomes a grey block that stops mid-page wherever
     the text happens to end. The hairline in the rule above is the whole
     separation the design uses, and it is enough. */
  .pitch-inner { max-width: 400px; display: flex; flex-direction: column; gap: var(--s-5); }

  .points { display: flex; flex-direction: column; gap: var(--s-5); }
  .points li { display: flex; gap: var(--s-3); }

  .dot {
    flex: none;
    width: 7px; height: 7px;
    margin-top: 6px;
    border-radius: var(--radius-full);
    background: var(--grow);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--grow) 18%, transparent);
  }

  .point-head { font-weight: 580; margin-bottom: 2px; }

  /* Below 860 the pitch is hidden by the base rule above; nothing further to
     undo here now that .auth is a flex column by default. */
</style>
