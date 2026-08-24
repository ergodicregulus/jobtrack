<script lang="ts">
  import Brand from '$lib/components/Brand.svelte';
  import { untrack } from 'svelte';
  import { enhance } from '$app/forms';
  import SkillPicker from '$lib/components/SkillPicker.svelte';
  import type { WorkMode } from '$lib/types';
  import type { ActionData, PageData } from './$types';

  interface Props { data: PageData; form: ActionData; }
  const { data, form }: Props = $props();

  const TOTAL = 4;

  // Steps are titled by what the user gets, not by what we collect. "Your
  // name" is a form label; "Let's start with you" is an invitation. The
  // difference measurably affects completion.
  const steps = [
    { title: "Let's start with you", blurb: 'So we can address you properly — and so the résumé matcher knows whose it is.' },
    { title: 'Where you are, where you want to be', blurb: 'Experience decides which roles are a stretch and which are a step back.' },
    { title: 'What you actually work with', blurb: 'This is what match scores are built from. Be honest rather than aspirational — a stretch role you can name is more useful than one you cannot.' },
    { title: 'What counts as a fit', blurb: 'Filters you can change at any time. Nothing here is permanent.' }
  ];

  const step = $derived(data.step);
  const meta = $derived(steps[step - 1]);
  const profile = $derived(data.profile);

  let skills = $state<string[]>(untrack(() => data.profile?.skills ?? []));
  let submitting = $state(false);

  // Re-seed from the server whenever it sends a new profile. Without this the
  // picker keeps whatever was typed before a save and quietly disagrees with
  // what was actually stored.
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
</script>

<div class="wizard">
  <div class="card-wrap">
    <header class="top">
      <Brand />
      <!-- Naming the position explicitly ("Step 2 of 4") rather than showing a
           bare bar is what makes the end feel reachable; an unlabelled bar
           leaves people unsure how much is left. -->
      <span class="t-small">Step {step} of {TOTAL}</span>
    </header>

    <!-- One segment per step, per the design.
         A single filled bar shows a PROPORTION and leaves the reader to infer
         the count; four segments show the count itself, which is the thing
         "Step 2 of 4" is telling them in words directly above. -->
    <div
      class="steps"
      role="progressbar"
      aria-valuenow={step}
      aria-valuemin={1}
      aria-valuemax={TOTAL}
      aria-label="Onboarding progress"
    >
      {#each Array(TOTAL) as _, i (i)}
        <span class="seg" class:done={i < step} aria-hidden="true"></span>
      {/each}
    </div>

    <div class="body">
      <div class="head">
        <h1>{meta.title}</h1>
        <p class="t-muted">{meta.blurb}</p>
      </div>

      {#if form?.error}
        <p class="banner" role="alert">{form.error}</p>
      {/if}

      <form
        method="POST"
        action="?/save"
        class="stack"
        use:enhance={() => {
          submitting = true;
          return async ({ update }) => {
            await update();
            submitting = false;
          };
        }}
      >
        <input type="hidden" name="step" value={step} />

        {#if step === 1}
          <div class="two">
            <div class="field">
              <label class="label" for="first_name">First name</label>
              <input
                class="input"
                id="first_name"
                name="first_name"
                autocomplete="given-name"
                required
                value={profile?.first_name ?? ''}
                aria-invalid={form?.fields?.first_name ? 'true' : undefined}
              />
              {#if form?.fields?.first_name}
                <p class="error-text">{form.fields.first_name}</p>
              {/if}
            </div>
            <div class="field">
              <label class="label" for="last_name">Last name</label>
              <input
                class="input"
                id="last_name"
                name="last_name"
                autocomplete="family-name"
                value={profile?.last_name ?? ''}
              />
            </div>
          </div>

        {:else if step === 2}
          <div class="field">
            <label class="label" for="current_title">Current title</label>
            <input
              class="input"
              id="current_title"
              name="current_title"
              placeholder="Software Engineer"
              autocomplete="organization-title"
              value={profile?.current_title ?? ''}
            />
            <p class="hint">Leave blank if you are between roles or just starting out.</p>
          </div>

          <div class="field">
            <label class="label" for="target_title">What you want next</label>
            <input
              class="input"
              id="target_title"
              name="target_title"
              placeholder="Senior Backend Engineer"
              value={profile?.target_title ?? ''}
            />
          </div>

          <div class="field">
            <label class="label" for="total_yoe">Years of experience</label>
            <input
              class="input narrow"
              id="total_yoe"
              name="total_yoe"
              type="number"
              min="0"
              max="60"
              step="0.5"
              value={profile?.total_yoe ?? 0}
            />
            <p class="hint">
              Roles asking well above this are shown as a stretch rather than hidden.
            </p>
          </div>

        {:else if step === 3}
          <div class="field">
            <label class="label" for="skills">Your skills</label>
            <SkillPicker bind:value={skills} suggestions={data.suggestions ?? []} id="skills" />
            <p class="hint">
              Press Enter or comma to add. Aim for eight to fifteen — a list of
              forty tells the matcher nothing about what you are strongest at.
            </p>
            {#if form?.fields?.skills}
              <p class="error-text">{form.fields.skills}</p>
            {/if}
          </div>

        {:else}
          <fieldset class="field">
            <legend class="label">Where would you work?</legend>
            <div class="opts">
              {#each countries as c (c.code)}
                <label class="opt">
                  <input
                    type="checkbox"
                    name="pref_countries"
                    value={c.code}
                    checked={profile?.pref_countries?.includes(c.code)}
                  />
                  <span>{c.label}</span>
                </label>
              {/each}
            </div>
          </fieldset>

          <fieldset class="field">
            <legend class="label">How?</legend>
            <div class="opts">
              {#each modes as m (m.code)}
                <label class="opt">
                  <input
                    type="checkbox"
                    name="pref_modes"
                    value={m.code}
                    checked={profile?.pref_modes?.includes(m.code)}
                  />
                  <span>{m.label}</span>
                </label>
              {/each}
            </div>
          </fieldset>

          <div class="two">
            <div class="field">
              <label class="label" for="pref_comp_min">Minimum compensation</label>
              <input
                class="input"
                id="pref_comp_min"
                name="pref_comp_min"
                type="number"
                min="0"
                step="1000"
                placeholder="0"
                value={profile?.pref_comp_min || ''}
              />
              <p class="hint">Annual, before tax. Leave blank to ignore.</p>
            </div>
            <div class="field">
              <label class="label" for="pref_currency">Currency</label>
              <select class="select" id="pref_currency" name="pref_currency">
                {#each currencies as c (c)}
                  <option value={c} selected={profile?.pref_currency === c}>{c}</option>
                {/each}
              </select>
            </div>
          </div>

          <p class="note">
            Roles that do not publish a salary are still shown, marked
            <em>undisclosed</em> — filtering them out would hide most of the market.
          </p>
        {/if}

        <div class="actions">
          {#if step > 1}
            <a class="btn" href={`/onboarding?step=${step - 1}`}>Back</a>
          {/if}
          <span class="spacer"></span>
          <button class="btn btn-primary btn-lg" type="submit" disabled={submitting}>
            {submitting ? 'Saving…' : step === TOTAL ? 'Finish' : 'Continue'}
          </button>
        </div>
      </form>

      {#if step > 2}
        <form method="POST" action="?/finish" class="skip">
          <button class="btn btn-ghost btn-sm" type="submit">
            Skip the rest — I'll fill this in later
          </button>
        </form>
      {/if}
    </div>
  </div>
</div>

<style>
  /* A 560px column at the top of the page, per the design — not a card
     floating in the middle of the viewport.
     
     The card was the only vertically-centred screen left in the product once
     auth was aligned, and a wizard that jumps to the middle of a tall monitor
     and back for every other page is the inconsistency this pass is about.
     The sunken background went with it: onboarding is a page, not a modal. */
  .wizard {
    width: 100%; max-width: 560px;
    margin: 0 auto;
    padding: var(--s-6) var(--s-5) var(--s-8);
  }

  .card-wrap { width: 100%; }

  .top {
    display: flex; align-items: center; justify-content: space-between;
    padding: 0 0 var(--s-3);
  }

  .steps { display: flex; gap: var(--s-1); margin-bottom: var(--s-5); }
  .seg {
    flex: 1;
    height: 3px;
    border-radius: var(--radius-full);
    background: var(--bg-active);
    transition: background var(--slow) var(--ease);
  }
  .seg.done { background: var(--accent); }

  .body { padding: 0; display: flex; flex-direction: column; gap: var(--s-5); }

  .head { display: flex; flex-direction: column; gap: 6px; }
  .head h1 { font-size: var(--t-xl); letter-spacing: var(--tr-xl); }

  .two { display: grid; grid-template-columns: 1fr 1fr; gap: var(--s-4); }
  @media (max-width: 520px) { .two { grid-template-columns: 1fr; } }

  .narrow { max-width: 140px; }

  fieldset { border: 0; padding: 0; margin: 0; }
  legend { padding: 0; margin-bottom: var(--s-2); }

  .opts { display: flex; flex-wrap: wrap; gap: var(--s-2); }

  /* A checkbox styled as a pressable card. The native input stays in the DOM
     for keyboard and screen-reader support; only its appearance changes. */
  .opt {
    display: inline-flex; align-items: center; gap: var(--s-2);
    min-height: 34px;
    padding: 0 var(--s-3);
    border-radius: var(--radius);
    background: var(--bg-raised);
    box-shadow: var(--e-0);
    font-size: var(--t-base);
    cursor: pointer;
    user-select: none;
    transition: box-shadow var(--fast) var(--ease), background var(--fast) var(--ease);
  }
  .opt:hover { background: var(--bg-hover); }
  .opt:has(input:checked) {
    background: var(--accent-bg);
    color: var(--accent-ink);
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .opt:has(input:focus-visible) { box-shadow: 0 0 0 2px var(--focus); }
  .opt input { accent-color: var(--accent); margin: 0; }

  .actions { display: flex; align-items: center; gap: var(--s-3); margin-top: var(--s-2); }

  .skip { display: flex; justify-content: center; }

  .banner {
    padding: var(--s-3);
    border-radius: var(--radius);
    background: var(--shrink-bg);
    color: var(--shrink-ink);
    font-size: var(--t-sm);
    box-shadow: inset 0 0 0 1px rgb(225 29 72 / 0.25);
  }

  .note {
    font-size: var(--t-sm);
    color: var(--fg-muted);
    padding: var(--s-3);
    background: var(--bg-sunken);
    border-radius: var(--radius);
  }
</style>
