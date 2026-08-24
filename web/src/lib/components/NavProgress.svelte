<script lang="ts">
  import { navigating } from '$app/state';

  /**
   * A thin progress bar across the top during navigation.
   *
   * The pages here are server-rendered, so a click that needs a round trip left
   * the previous page on screen with no sign anything was happening — on a slow
   * connection that reads as a dead button, and people click again.
   *
   * Deliberately NOT a full-page skeleton for navigations. Replacing readable
   * content with grey rectangles because the *next* page is loading destroys
   * information the user still has. A skeleton is right when there is nothing
   * to show yet; a progress bar is right when there is.
   *
   * The delay matters as much as the bar. Showing it instantly makes every fast
   * navigation flash, which reads as jitter rather than feedback. Nielsen's
   * threshold for "instantaneous" is about 100ms, so anything quicker than that
   * should show nothing at all.
   */
  const SHOW_AFTER_MS = 120;

  let visible = $state(false);

  $effect(() => {
    if (!navigating.to) {
      visible = false;
      return;
    }
    const timer = setTimeout(() => (visible = true), SHOW_AFTER_MS);
    return () => clearTimeout(timer);
  });
</script>

{#if visible}
  <!--
    aria-hidden with a separate live region: announcing a progress bar's every
    frame is noise, but a screen-reader user still needs to know a page change
    is under way.
  -->
  <div class="bar" aria-hidden="true"></div>
  <span class="sr-only" role="status">Loading…</span>
{/if}

<style>
  .bar {
    position: fixed;
    top: 0; left: 0; right: 0;
    height: 2px;
    z-index: 60;
    background: linear-gradient(90deg, transparent, var(--accent), transparent);
    background-size: 40% 100%;
    background-repeat: no-repeat;
    animation: sweep 1.1s var(--ease) infinite;
  }

  @keyframes sweep {
    0%   { background-position: -40% 0; }
    100% { background-position: 140% 0; }
  }

  /* An animation that cannot be stopped is a barrier for anyone who gets
     motion sickness. A static bar still says "working". */
  @media (prefers-reduced-motion: reduce) {
    .bar { animation: none; background: var(--accent); background-size: 100% 100%; }
  }
</style>
