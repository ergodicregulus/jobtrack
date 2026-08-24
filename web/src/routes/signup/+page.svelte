<script lang="ts">
  import { enhance } from '$app/forms';
  import AuthShell from '$lib/components/AuthShell.svelte';
  import type { ActionData } from './$types';

  const { form }: { form: ActionData } = $props();

  let password = $state('');
  let submitting = $state(false);

  const MIN = 12;

  // Length is the only rule. Composition rules (a symbol, a digit, a capital)
  // measurably push people toward predictable substitutions and are advised
  // against by NIST SP 800-63B; length is what actually resists guessing.
  const remaining = $derived(Math.max(0, MIN - password.length));
  const strong = $derived(password.length >= MIN);
</script>

<AuthShell
  title="Create your account"
  subtitle="Two fields now, a short profile next, then your matches."
>
  <form
    method="POST"
    class="stack"
    use:enhance={() => {
      submitting = true;
      return async ({ update }) => {
        await update();
        submitting = false;
      };
    }}
  >
    {#if form?.error}
      <p class="banner" role="alert">{form.error}</p>
    {/if}

    <div class="field">
      <label class="label" for="email">Email</label>
      <input
        class="input"
        id="email"
        name="email"
        type="email"
        autocomplete="email"
        required
        value={form?.email ?? ''}
      />
    </div>

    <div class="field">
      <label class="label" for="password">Password</label>
      <input
        class="input"
        id="password"
        name="password"
        type="password"
        autocomplete="new-password"
        minlength={MIN}
        required
        bind:value={password}
        aria-describedby="pw-hint"
      />
      <!-- aria-live=polite, not assertive: this updates on every keystroke and
           an assertive region would interrupt the user constantly. -->
      <p class="hint" id="pw-hint" aria-live="polite">
        {#if !password}
          At least {MIN} characters. A phrase you will remember beats a short
          scramble you will not.
        {:else if strong}
          <span class="ok">Long enough.</span>
        {:else}
          {remaining} more character{remaining === 1 ? '' : 's'}.
        {/if}
      </p>
    </div>

    <button class="btn btn-primary btn-lg btn-block" type="submit" disabled={submitting}>
      {submitting ? 'Creating…' : 'Create account'}
    </button>
  </form>

  {#snippet footer()}
    Already have an account? <a href="/login">Sign in</a>
  {/snippet}
</AuthShell>

<style>
  .banner {
    padding: var(--s-3);
    border-radius: var(--radius);
    background: var(--shrink-bg);
    color: var(--shrink-ink);
    font-size: var(--t-sm);
    box-shadow: inset 0 0 0 1px rgb(225 29 72 / 0.25);
  }
  .ok { color: var(--grow-ink); font-weight: 550; }
</style>
