<script lang="ts">
  import { enhance } from '$app/forms';
  import AuthShell from '$lib/components/AuthShell.svelte';
  import type { ActionData, PageData } from './$types';

  interface Props { data: PageData; form: ActionData; }
  const { data, form }: Props = $props();

  let submitting = $state(false);
</script>

<AuthShell title="Welcome back" subtitle="Pick up where you left off.">
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
    <input type="hidden" name="next" value={data.next} />

    {#if form?.error}
      <!-- role=alert so a screen reader announces the failure. Without it the
           only feedback is visual and a non-sighted user is left guessing. -->
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
      <!-- WCAG 2.2 3.3.8 Accessible Authentication: password managers must be
           able to fill this, so autocomplete is a conformance requirement here
           rather than a convenience. -->
      <input
        class="input"
        id="password"
        name="password"
        type="password"
        autocomplete="current-password"
        required
      />
    </div>

    <button class="btn btn-primary btn-lg btn-block" type="submit" disabled={submitting}>
      {submitting ? 'Signing in…' : 'Sign in'}
    </button>
  </form>

  {#snippet footer()}
    New here? <a href="/signup">Create an account</a>
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
</style>
