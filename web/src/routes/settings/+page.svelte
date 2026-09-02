<script lang="ts">
  import { untrack } from 'svelte';
  import { enhance } from '$app/forms';
  import type { Theme } from '$lib/types';
  import type { ActionData, PageData } from './$types';

  interface Props { data: PageData; form: ActionData; }
  const { data, form }: Props = $props();

  let theme = $state<Theme>(untrack(() => data.theme));
  let saving = $state(false);

  $effect(() => {
    theme = data.theme;
  });

  // Applied optimistically so the change is instant. The server has the same
  // value in a cookie by the time the next page renders, so there is no flash.
  function preview(next: Theme) {
    theme = next;
    const root = document.documentElement;
    root.dataset.theme = next === 'system' ? '' : next;
  }

  const themes: { value: Theme; label: string; hint: string }[] = [
    { value: 'light', label: 'Light', hint: '' },
    { value: 'dark', label: 'Dark', hint: '' },
    {
      value: 'system',
      label: 'System',
      hint: 'Follows your operating system, including when it switches at sunset'
    }
  ];
</script>

<div class="shell">
  <div class="page">
  <header class="head">
    <h1>Settings</h1>
    <p class="t-muted">Saved to your account, so it follows you to another machine.</p>
  </header>

  {#if form?.saved}
    <p class="banner ok" role="status">Saved.</p>
  {/if}
  {#if form?.error}
    <p class="banner" role="alert">{form.error}</p>
  {/if}

  <section class="panel">
    <div class="panel-head"><h2 class="t-heading">Appearance</h2></div>

    <form
      method="POST"
      action="?/appearance"
      use:enhance={() => {
        saving = true;
        return async ({ update }) => {
          await update({ reset: false });
          saving = false;
        };
      }}
    >
      <fieldset class="field">
        <legend class="label">Colour theme</legend>
        <div class="opts">
          {#each themes as t (t.value)}
            <label class="opt">
              <input
                type="radio"
                name="theme"
                value={t.value}
                checked={theme === t.value}
                onchange={() => preview(t.value)}
              />
              <span>{t.label}</span>
            </label>
          {/each}
        </div>
        {#each themes as t (t.value)}
          {#if t.hint && theme === t.value}
            <p class="hint">{t.hint}</p>
          {/if}
        {/each}
      </fieldset>

      <!--
        There is no density control, and its absence is explained rather than
        silent. It changed row padding and nothing else — not type size, not
        target size — so calling it an accessibility preference was untrue.
        Browser zoom does the real job and does it better.
      -->
      <p class="note">
        There is one row density. A setting for it used to live here and was
        removed: it changed the space between rows without changing text size or
        tap targets, so it did not do the accessibility job it claimed to.
        Browser zoom scales everything, which is what someone who needs larger
        text actually wants.
      </p>

      <div class="actions">
        <button class="btn btn-primary" type="submit" disabled={saving}>
          {saving ? 'Saving…' : 'Save appearance'}
        </button>
      </div>
    </form>
  </section>

  <!--
    Email is a consent, and it is presented as one.

    Off by default and never pre-ticked: a pre-ticked box is not consent, and on
    the one product whose argument is that it does not overclaim, it would be the
    cheapest possible thing to get wrong. The copy says what will actually
    arrive, so the decision is made on the real thing rather than on "updates".
  -->
  <section class="panel">
    <div class="panel-head"><h2 class="t-heading">Email</h2></div>
    <dl class="rows">
      <div>
        <dt>Weekly digest</dt>
        <dd>
          <form method="POST" action="?/digest" use:enhance>
            <input type="hidden" name="on" value={data.digest ? 'false' : 'true'} />
            <button class="btn btn-sm" type="submit">
              {data.digest ? 'Turn off' : 'Turn on'}
            </button>
            <p class="t-small hint">
              {#if data.digest}
                On. One email per saved search per week, listing only what arrived
                since you last looked. Nothing is sent in a week with nothing new.
              {:else}
                Off. When on, you get one email per saved search per week listing
                what arrived since you last looked — and none at all in a quiet week.
              {/if}
            </p>
          </form>
        </dd>
      </div>
    </dl>
  </section>

  <section class="panel">
    <div class="panel-head"><h2 class="t-heading">Account</h2></div>
    <dl class="rows">
      <div>
        <dt>Profile and matching</dt>
        <dd><a href="/profile">Edit your profile →</a></dd>
      </div>
      <div>
        <dt>Sign out</dt>
        <dd>
          <form method="POST" action="/logout">
            <button class="btn btn-sm" type="submit">Sign out</button>
          </form>
        </dd>
      </div>
    </dl>
  </section>
  </div>
</div>

<style>
  /* A standalone action link is a target, not prose: SC 2.5.8's exemption
     covers links INSIDE a sentence, and this one is alone in its cell. It
     measured 17px tall. */
  .rows dd a {
    display: inline-flex; align-items: center;
    min-height: 24px;
  }

  /* margin-right:auto, not margin:auto.
     .shell already centres the page at 1180px; a max-width inside it centres a
     SECOND time, which moved this page's left edge from 153px to 395px while
     the header's wordmark stayed at 153. Two different layouts in one app,
     visible the moment you click between Settings and Dashboard. The cap stays
     — a settings form at 1180px is an unreadable line length — but it hangs off
     the same gutter as everything else. */
  .page {
    display: flex; flex-direction: column; gap: var(--s-4);
    padding-top: var(--s-6);
    max-width: 700px;
    margin-right: auto;
  }

  .head { display: flex; flex-direction: column; gap: 4px; }
  .head h1 { font-size: var(--t-2xl); letter-spacing: var(--tr-2xl); }

  fieldset { border: 0; padding: 0; margin: 0; }
  legend { padding: 0; margin-bottom: var(--s-2); }

  .opts { display: flex; flex-wrap: wrap; gap: var(--s-2); }

  .opt {
    display: inline-flex; align-items: center; gap: var(--s-2);
    min-height: 36px; padding: 0 var(--s-4);
    border-radius: var(--radius);
    background: var(--bg-raised);
    box-shadow: var(--e-0);
    font-size: var(--t-base);
    cursor: pointer; user-select: none;
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

  /* WCAG 2.5.8 is 24px, but a tablet gets the desktop layout with a finger. */
  @media (pointer: coarse) {
    .opt { min-height: 44px; }
  }

  .note {
    margin-top: var(--s-4);
    padding: var(--s-3);
    background: var(--bg-sunken);
    border-radius: var(--radius);
    font-size: var(--t-sm);
    color: var(--fg-muted);
  }

  .actions { margin-top: var(--s-4); }

  .rows { display: flex; flex-direction: column; gap: var(--s-3); margin: 0; }
  .rows > div { display: flex; align-items: center; gap: var(--s-4); flex-wrap: wrap; }
  .rows dt { flex: 1; min-width: 160px; font-size: var(--t-base); }
  .rows dd { margin: 0; }

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
