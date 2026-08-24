<script lang="ts">
  import { untrack } from 'svelte';
  import { enhance } from '$app/forms';
  import SkillPicker from '$lib/components/SkillPicker.svelte';
  import type { WorkMode } from '$lib/types';
  import type { ActionData, PageData } from './$types';

  interface Props { data: PageData; form: ActionData; }
  const { data, form }: Props = $props();

  const p = $derived(data.profile);

  let skills = $state<string[]>(untrack(() => data.profile?.skills ?? []));

  // Read-only here: the picker above owns the hand-typed set, and these are
  // changed by re-importing a CV rather than by editing this form.
  const cvSkills = $derived(data.profile?.resume_skills ?? []);
  let submitting = $state(false);

  // Re-seed from the server whenever it sends a new profile, so a save leaves
  // the field showing what was stored rather than what was typed.
  $effect(() => {
    skills = data.profile?.skills ?? [];
  });

  const currencies = ['INR', 'USD', 'EUR', 'GBP', 'CAD', 'AUD', 'SGD'];
  const countries = [
    { code: 'IN', label: 'India' },
    { code: 'US', label: 'United States' },
    { code: 'GB', label: 'United Kingdom' },
    { code: 'DE', label: 'Germany' },
    { code: 'FR', label: 'France' },
    { code: 'CA', label: 'Canada' },
    { code: 'SG', label: 'Singapore' },
    { code: 'AU', label: 'Australia' }
  ];
  // Typed against the generated WorkMode union rather than plain strings, so a
  // value the API would reject is a compile error here instead of a 400 the
  // user discovers by clicking.
  const modes: { code: WorkMode; label: string }[] = [
    { code: 'remote', label: 'Remote' },
    { code: 'hybrid', label: 'Hybrid' },
    { code: 'onsite', label: 'On-site' }
  ];

  const initials = $derived(
    ((p?.first_name?.[0] ?? '') + (p?.last_name?.[0] ?? '')).toUpperCase() || '?'
  );
  const fullName = $derived([p?.first_name, p?.last_name].filter(Boolean).join(' ') || 'Your profile');
</script>

<div class="shell">
  <div class="page">
  <header class="hero">
    <span class="avatar-lg" aria-hidden="true">{initials}</span>
    <div>
      <h1>{fullName}</h1>
      <p class="t-muted">
        {p?.current_title || 'No current title'}
        <!-- The arrow needs explicit margins: HTML collapses the whitespace
             around it, so the literal spaces rendered as "Engineer →Staff". -->
        {#if p?.target_title}<span class="arrow" aria-label="moving toward">→</span>{p.target_title}{/if}
      </p>
    </div>
  </header>

  {#if form?.saved}
    <!-- Saying that a rescore is running prevents the feed appearing to change
         by itself a moment later, which reads as a bug rather than a feature. -->
    <p class="banner ok" role="status">
      Saved. We're re-scoring your matches now — the feed will update shortly.
    </p>
  {/if}
  {#if form?.error}
    <p class="banner" role="alert">{form.error}</p>
  {/if}

  <form
    method="POST"
    class="form-grid"
    use:enhance={() => {
      submitting = true;
      return async ({ update }) => {
        await update({ reset: false });
        submitting = false;
      };
    }}
  >
    <section class="panel">
      <div class="panel-head"><h2 class="t-heading">About you</h2></div>

      <div class="stack">
        <div class="two">
          <div class="field">
            <label class="label" for="first_name">First name</label>
            <input class="input" id="first_name" name="first_name" autocomplete="given-name"
              required value={p?.first_name ?? ''} />
            {#if form?.fields?.first_name}<p class="error-text">{form.fields.first_name}</p>{/if}
          </div>
          <div class="field">
            <label class="label" for="last_name">Last name</label>
            <input class="input" id="last_name" name="last_name" autocomplete="family-name"
              value={p?.last_name ?? ''} />
          </div>
        </div>

        <div class="two">
          <div class="field">
            <label class="label" for="current_title">Current title</label>
            <input class="input" id="current_title" name="current_title"
              placeholder="Software Engineer" value={p?.current_title ?? ''} />
          </div>
          <div class="field">
            <label class="label" for="target_title">Target title</label>
            <input class="input" id="target_title" name="target_title"
              placeholder="Senior Backend Engineer" value={p?.target_title ?? ''} />
          </div>
        </div>

        <div class="field">
          <label class="label" for="total_yoe">Years of experience</label>
          <input class="input narrow" id="total_yoe" name="total_yoe" type="number"
            min="0" max="60" step="0.5" value={p?.total_yoe ?? 0} />
          <p class="hint">Drives the experience component of every match score.</p>
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head">
        <h2 class="t-heading">Skills</h2>
        <span class="spacer"></span>
        <span class="t-micro">{skills.length} listed</span>
      </div>
      <SkillPicker
        bind:value={skills}
        suggestions={data.suggestions ?? []}
        labels={data.profile?.skill_labels ?? {}}
        id="profile-skills"
      />
      <p class="hint mt">
        Weighted at 40% of every match score — the largest single component.
      </p>

      <!--
        Skills a CV contributed, shown as their own group.
        Separate from the picker above because the editor PATCHes that set
        wholesale — folding these in would silently convert every inferred
        skill into a declared one on the next save. Showing them at all is what
        makes importing a CV visibly do something: the first version returned
        only hand-typed skills, so fifteen imported ones changed nothing on
        screen and the feature looked broken.
      -->
      {#if cvSkills.length > 0}
        <div class="from-cv mt">
          <p class="t-micro">
            From your CV — counted in every match score, and removable by
            re-importing.
          </p>
          <ul class="cv-chips">
            {#each cvSkills as skill (skill)}
              <li class="chip chip-plain">{data.profile?.skill_labels?.[skill] ?? skill}</li>
            {/each}
          </ul>
        </div>
      {/if}

      <!--
        Offered next to the manual picker rather than buried in settings,
        because this is the moment the effort is obvious: someone typing their
        eighth chip is exactly who should be told there is a faster way. The
        claim is specific and measured, not "save time".
      -->
      <p class="import mt">
        <a href="/profile/resume">Import from your CV</a>
        <span class="t-micro">
          Reads your skills off a DOCX in one step. Typical CVs list two to
          three times what people type by hand, and nothing is saved until you
          have reviewed it.
        </span>
      </p>
    </section>

    <section class="panel">
      <div class="panel-head"><h2 class="t-heading">What counts as a fit</h2></div>

      <div class="stack">
        <fieldset class="field">
          <legend class="label">Countries</legend>
          <div class="opts">
            {#each countries as c (c.code)}
              <label class="opt">
                <input type="checkbox" name="pref_countries" value={c.code}
                  checked={p?.pref_countries?.includes(c.code)} />
                <span>{c.label}</span>
              </label>
            {/each}
          </div>
        </fieldset>

        <fieldset class="field">
          <legend class="label">Work mode</legend>
          <div class="opts">
            {#each modes as m (m.code)}
              <label class="opt">
                <input type="checkbox" name="pref_modes" value={m.code}
                  checked={p?.pref_modes?.includes(m.code)} />
                <span>{m.label}</span>
              </label>
            {/each}
          </div>
        </fieldset>

        <div class="two">
          <div class="field">
            <label class="label" for="pref_comp_min">Minimum compensation</label>
            <input class="input" id="pref_comp_min" name="pref_comp_min" type="number"
              min="0" step="1000" placeholder="0" value={p?.pref_comp_min || ''} />
          </div>
          <div class="field">
            <label class="label" for="pref_currency">Currency</label>
            <select class="select" id="pref_currency" name="pref_currency">
              {#each currencies as c (c)}
                <option value={c} selected={p?.pref_currency === c}>{c}</option>
              {/each}
            </select>
          </div>
        </div>
      </div>
    </section>

    <!-- The save bar sticks to the bottom of the viewport. On a long form, a
         button that scrolls out of reach is the most common reason edits are
         made and then lost. -->
    <div class="save-bar">
      <span class="t-small">Changes re-score every live posting for you.</span>
      <button class="btn btn-primary" type="submit" disabled={submitting}>
        {submitting ? 'Saving…' : 'Save changes'}
      </button>
    </div>
  </form>
  </div>
</div>

<style>
  /* Bottom padding equal to the sticky bar's height plus its offset, so the
     last section can always be scrolled clear of it. */
  .page {
    display: flex; flex-direction: column; gap: var(--s-4);
    padding-top: var(--s-6); padding-bottom: var(--s-8);
    max-width: 780px;
    /* See the note in settings: .shell centres already, so this must not. */
    margin-right: auto;
  }

  .import { display: flex; flex-direction: column; gap: 2px; }

  .from-cv { display: flex; flex-direction: column; gap: var(--s-2); }
  .cv-chips { display: flex; flex-wrap: wrap; gap: var(--s-1); }

  .hero { display: flex; align-items: center; gap: var(--s-4); margin-bottom: var(--s-2); }
  .hero h1 { font-size: var(--t-2xl); letter-spacing: var(--tr-2xl); }

  .arrow { display: inline-block; margin: 0 0.4em; color: var(--fg-subtle); }

  .avatar-lg {
    display: inline-flex; align-items: center; justify-content: center;
    width: 56px; height: 56px;
    flex: none;
    border-radius: var(--radius-full);
    background: var(--accent-bg);
    color: var(--accent-ink);
    font-size: var(--t-xl);
    font-weight: 620;
    box-shadow: inset 0 0 0 1px var(--ring-accent);
  }

  .form-grid { display: flex; flex-direction: column; gap: var(--s-4); }

  .two { display: grid; grid-template-columns: 1fr 1fr; gap: var(--s-4); }
  @media (max-width: 560px) { .two { grid-template-columns: 1fr; } }

  .narrow { max-width: 140px; }
  .mt { margin-top: var(--s-3); }

  fieldset { border: 0; padding: 0; margin: 0; }
  legend { padding: 0; margin-bottom: var(--s-2); }

  .opts { display: flex; flex-wrap: wrap; gap: var(--s-2); }
  .opt {
    display: inline-flex; align-items: center; gap: var(--s-2);
    min-height: 34px; padding: 0 var(--s-3);
    border-radius: var(--radius);
    background: var(--bg-raised);
    box-shadow: var(--e-0);
    font-size: var(--t-base);
    cursor: pointer; user-select: none;
    transition: box-shadow var(--fast) var(--ease), background var(--fast) var(--ease);
  }
  .opt:hover { background: var(--bg-hover); }
  .opt:has(input:checked) {
    background: var(--accent-bg); color: var(--accent-ink);
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .opt:has(input:focus-visible) { box-shadow: 0 0 0 2px var(--focus); }
  .opt input { accent-color: var(--accent); margin: 0; }

  .save-bar {
    position: sticky; bottom: var(--s-4);
    display: flex; align-items: center; gap: var(--s-4); flex-wrap: wrap;
    padding: var(--s-3) var(--s-4);
    /* Opaque, not 92% with a blur. A sticky bar sits ON TOP of whatever it is
       scrolled over, and the translucency let that content read straight
       through it — "Import from your CV" appeared to run into the middle of
       the save button. A backdrop blur is a nice effect on a surface with
       nothing important behind it; this one always has a form behind it. */
    background: var(--bg-raised);
    border-radius: var(--radius-md);
    box-shadow: var(--e-2);
  }
  .save-bar .t-small { flex: 1; min-width: 200px; }

  .banner {
    padding: var(--s-3) var(--s-4);
    border-radius: var(--radius);
    background: var(--shrink-bg);
    color: var(--shrink-ink);
    font-size: var(--t-sm);
    box-shadow: inset 0 0 0 1px rgb(225 29 72 / 0.25);
  }
  .banner.ok {
    background: var(--grow-bg);
    color: var(--grow-ink);
    box-shadow: inset 0 0 0 1px rgb(13 148 136 / 0.3);
  }
</style>
