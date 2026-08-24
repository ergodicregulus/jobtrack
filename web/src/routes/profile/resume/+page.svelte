<script lang="ts">
  import type { ActionData, PageData } from './$types';

  const { data, form }: { data: PageData; form: ActionData } = $props();

  const parsed = $derived(form && 'parsed' in form ? form.parsed : null);

  // What the evidence label actually means, in the user's terms. "experience"
  // is a stronger claim than "skills" and the difference decides whether they
  // should keep it, so the words have to carry that rather than repeat the key.
  const evidenceLabel: Record<string, string> = {
    experience: 'used in a role',
    projects: 'used in a project',
    skills: 'listed',
    summary: 'in your summary',
    education: 'in education',
    unlabelled: 'position unclear',
    other: 'elsewhere'
  };

  const newCount = $derived(parsed?.new_skills ?? 0);
  const blocking = $derived(
    (parsed?.diagnostics ?? []).filter((d: { severity: string }) => d.severity === 'blocking')
  );
  const warnings = $derived(
    (parsed?.diagnostics ?? []).filter((d: { severity: string }) => d.severity !== 'blocking')
  );

  // Below this we say so plainly rather than presenting rubble as data. The
  // same threshold the job feed uses for a posting we struggled to read.
  const lowConfidence = $derived((parsed?.confidence ?? 1) < 0.5);
</script>

<svelte:head><title>Import your CV · JobTrack</title></svelte:head>

<div class="shell page">
  <header class="head">
    <div>
      <h1>Import your CV</h1>
      <p class="t-muted">
        Skills are 40 of the 100 points in every match score. Reading them off
        your CV is quicker than typing them, and usually finds more.
      </p>
    </div>
    <a class="btn" href="/profile">Back to profile</a>
  </header>

  {#if form && 'message' in form && form.message}
    <p class="banner" role="alert">{form.message}</p>
  {/if}

  {#if !parsed}
    <!--
      A plain multipart form. File upload is one of the few things HTML does
      natively and completely, so this works with JavaScript off — which is the
      standing rule here and matters more than usual on a page someone might
      reach from a locked-down work machine.
    -->
    <form method="POST" action="?/upload" enctype="multipart/form-data" class="card upload">
      <label class="field">
        <span class="label">Your CV</span>
        <input
          class="input file"
          type="file"
          name="file"
          accept=".pdf,.docx,.txt,.md,application/pdf,text/plain"
          required
        />
        <span class="t-micro">
          PDF, DOCX or plain text, up to 5 MB. A scanned or photographed CV has
          no text in it — we will say so rather than guess, and it fails
          employer systems for the same reason.
        </span>
      </label>

      <label class="field">
        <span class="label">Name it <span class="t-micro">(optional)</span></span>
        <input class="input" type="text" name="label" placeholder="e.g. Backend CV, 2026" />
      </label>

      <div class="actions">
        <button class="btn btn-primary btn-lg" type="submit">Read my CV</button>
      </div>

      <p class="t-micro privacy">
        Your CV is parsed by an isolated service with no internet access and no
        database credentials. We keep the extracted text, encrypted, and never
        the original file. Nothing is sent to a third party and nothing is used
        to train anything.
      </p>
    </form>
  {:else}
    <!-- The review. Nothing here has touched the profile yet. -->
    <form method="POST" action="?/apply" class="review">
      <input type="hidden" name="id" value={parsed.id} />

      <div class="card summary" class:low={lowConfidence}>
        <div class="summary-head">
          <div>
            <p class="eyebrow">What we found</p>
            <p class="headline">
              {#if newCount > 0}
                {newCount} skill{newCount === 1 ? '' : 's'} you had not told us about
              {:else if parsed.skills.length > 0}
                Nothing new — your profile already has these
              {:else}
                We could not recognise any technologies
              {/if}
            </p>
          </div>
          <div class="confidence">
            <span class="conf-num num">{Math.round(parsed.confidence * 100)}%</span>
            <span class="t-micro">of the document understood</span>
          </div>
        </div>

        {#if lowConfidence}
          <p class="caution">
            <strong>We understood less than half of this file.</strong>
            Check the list below carefully before saving. This is worth knowing
            beyond us: if we struggled with your CV, an employer's applicant
            tracking system very likely will too.
          </p>
        {/if}

        {#each blocking as d (d.code)}
          <p class="caution" role="alert">{d.message}</p>
        {/each}
        {#each warnings as d (d.code)}
          <p class="note">{d.message}</p>
        {/each}
      </div>

      {#if parsed.skills.length > 0}
        <section class="card skills-card">
          <div class="panel-head wrap">
            <h2 class="t-heading">Skills</h2>
            <span class="spacer"></span>
            <span class="t-micro">Untick anything you would not claim in an interview</span>
          </div>

          <ul class="skills">
            {#each parsed.skills as s (s.canonical)}
              <li class="skill" class:known={s.known}>
                <label>
                  <!--
                    Pre-ticked, because the common case is that the parse is
                    right and making someone tick twenty boxes to get the
                    benefit they came for is a tax on our own accuracy. The
                    ones already on the profile are ticked too, so unticking
                    genuinely removes them.
                  -->
                  <input type="checkbox" name="skill" value={s.canonical} checked />
                  <span class="skill-name">{s.label ?? s.canonical}</span>
                  <span class="t-micro evidence">{evidenceLabel[s.evidence] ?? s.evidence}</span>
                  {#if s.known}
                    <span class="t-micro tag">already on your profile</span>
                  {/if}
                </label>
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if parsed.years_of_experience !== null}
        <section class="card years">
          <label>
            <input type="checkbox" name="set_years" />
            <span>
              Set my experience to <strong>{parsed.years_of_experience} years</strong>
              <span class="t-micro block">
                Worked out from the dates on your roles, counting overlapping
                ones once rather than adding them up. Off by default because it
                overwrites what you entered yourself.
              </span>
            </span>
          </label>
        </section>
      {/if}

      <div class="actions sticky">
        <button class="btn btn-primary btn-lg" type="submit">
          Save {parsed.skills.length > 0 ? 'these skills' : 'and continue'} and rescore
        </button>
        <a class="btn btn-lg" href="/profile">Discard</a>
      </div>
    </form>
  {/if}
</div>

<style>
  .page { display: flex; flex-direction: column; gap: var(--s-5); padding-top: var(--s-6); }

  .head { display: flex; align-items: flex-start; gap: var(--s-4); flex-wrap: wrap; }
  .head > div { flex: 1; min-width: 260px; }
  .head h1 { font-size: var(--t-2xl); letter-spacing: var(--tr-2xl); }
  .head p { max-width: 56ch; }

  .banner {
    padding: var(--s-3) var(--s-4);
    border-radius: var(--radius);
    background: var(--shrink-bg);
    color: var(--shrink-ink);
  }

  .upload { display: flex; flex-direction: column; gap: var(--s-4); padding: var(--s-5); max-width: 56ch; }
  .field { display: flex; flex-direction: column; gap: var(--s-2); }

  /* The shared .input gives the box; a file input additionally needs its
     BUTTON sized, which the browser draws and which inherits nothing. Without
     this the control measured 21px tall — under WCAG 2.2 SC 2.5.8 — while
     looking identical to the styled text field beside it. */
  .file { padding: var(--s-2); line-height: 1.6; }
  .file::file-selector-button {
    min-height: 28px;
    margin-right: var(--s-3);
    padding: 0 var(--s-3);
    border: 0;
    border-radius: var(--radius-sm);
    background: var(--bg-sunken);
    box-shadow: inset 0 0 0 1px var(--ring-strong);
    color: var(--fg);
    font: inherit;
    font-size: var(--t-sm);
    cursor: pointer;
  }
  .file::file-selector-button:hover { background: var(--bg-hover); }
  .label { font-size: var(--t-sm); font-weight: 560; }

  .privacy { border-top: 1px solid var(--border); padding-top: var(--s-3); max-width: 60ch; }

  /* Bottom padding equal to the sticky bar's height, so the last card can
     always scroll clear of it. Without this the bar permanently hides whatever
     ends up underneath it — on a phone that was two skill rows. */
  .review { display: flex; flex-direction: column; gap: var(--s-4); padding-bottom: var(--s-8); }

  .summary { padding: var(--s-5); display: flex; flex-direction: column; gap: var(--s-3); }
  .summary-head { display: flex; align-items: flex-start; gap: var(--s-4); flex-wrap: wrap; }
  .summary-head > div:first-child { flex: 1; min-width: 220px; }
  .headline { font-size: var(--t-xl); letter-spacing: var(--tr-xl); font-weight: 620; }

  .confidence { text-align: right; display: flex; flex-direction: column; }
  .conf-num { font-size: var(--t-2xl); font-weight: 620; font-variant-numeric: tabular-nums; }
  /* Amber, not red: a partial read is uncertain, not broken. */
  .summary.low .conf-num { color: var(--uncertain-ink); }

  .caution {
    padding: var(--s-3);
    border-radius: var(--radius);
    background: var(--uncertain-bg);
    color: var(--uncertain-ink);
    font-size: var(--t-sm);
  }
  .note { font-size: var(--t-sm); color: var(--fg-muted); }

  /* .card carries no padding of its own, so without this the heading sat flush
     against the card edge while the rows below it were indented. */
  .skills-card { padding: var(--s-5); }
  /* On a phone the hint has nowhere to sit beside the heading, so it drops
     under it rather than being squeezed into four words per line. */
  .panel-head.wrap { flex-wrap: wrap; row-gap: var(--s-1); }

  .skills { display: flex; flex-direction: column; }
  .skill { box-shadow: 0 1px 0 var(--ring); }
  .skill:last-child { box-shadow: none; }
  .skill label {
    display: flex; align-items: center; gap: var(--s-3); flex-wrap: wrap;
    /* 44px of vertical target on a phone: these are checkboxes in a long list,
       which is exactly where a cramped hit area costs people real mistakes. */
    padding: var(--s-3) 0;
    cursor: pointer;
  }
  .skill-name { font-weight: 520; }
  .evidence { color: var(--fg-subtle); }
  .tag {
    margin-left: auto;
    padding: 2px var(--s-2);
    border-radius: var(--radius-full);
    background: var(--bg-sunken);
    color: var(--fg-subtle);
  }

  .years { padding: var(--s-4); }
  .years label { display: flex; align-items: flex-start; gap: var(--s-3); cursor: pointer; }
  .block { display: block; margin-top: 2px; max-width: 62ch; }

  .actions { display: flex; gap: var(--s-3); flex-wrap: wrap; }
  /* The list can run past a screen, and a save button at the bottom of a long
     form is a button people do not find. */
  .actions.sticky {
    position: sticky;
    bottom: 0;
    padding: var(--s-4) 0 var(--s-3);
    /* Opaque, not a gradient fade. The fade let the card underneath show
       through at 40% and the explanatory text read as a rendering fault. */
    background: var(--bg);
    box-shadow: 0 -1px 0 var(--border);
  }

  @media (pointer: coarse) {
    .skill label { padding: var(--s-4) 0; }
  }
</style>
