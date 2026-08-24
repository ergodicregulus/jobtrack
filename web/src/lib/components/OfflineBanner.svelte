<script lang="ts">
  /**
   * Tells the user the connection is gone, before they find out by clicking.
   *
   * The honest asymmetry of `navigator.onLine` decides the whole design here:
   * `false` means the browser is certain there is no network, but `true` means
   * only "an interface is up" — a captive portal or a dead uplink still reports
   * online. So this shows on the `offline` event and hides on `online`, and
   * never claims to have restored anything. "Back online" would be a promise
   * the browser cannot make.
   *
   * Reading the feed still works offline: pages already rendered stay readable,
   * and that is worth saying, because the alternative reading of a connection
   * banner is "this app is now broken".
   */
  let offline = $state(false);

  $effect(() => {
    // Read once at mount for the case where the page was restored from
    // bfcache while the connection was already down.
    offline = navigator.onLine === false;

    const down = () => (offline = true);
    const up = () => (offline = false);
    window.addEventListener('offline', down);
    window.addEventListener('online', up);
    return () => {
      window.removeEventListener('offline', down);
      window.removeEventListener('online', up);
    };
  });
</script>

{#if offline}
  <!--
    role="status" rather than "alert": losing a connection is not an emergency
    that should interrupt whatever a screen reader is reading, but it does need
    to be announced when there is a pause.
  -->
  <div class="offline" role="status">
    <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
      <path
        d="M1 5.5A11 11 0 0 1 15 5.5M3.5 8.5a7.5 7.5 0 0 1 9 0M6 11.5a3.5 3.5 0 0 1 4 0"
        fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"
      />
      <path d="M2 2l12 12" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
    </svg>
    <span>You are offline. Pages already open still work; saving will not.</span>
  </div>
{/if}

<style>
  .offline {
    position: sticky;
    top: 0;
    z-index: 55;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--s-2);
    padding: var(--s-2) var(--s-4);
    /* Amber, per the signal vocabulary: contested or uncertain, not a
       failure. Red here would say something broke, and nothing did. */
    background: var(--uncertain-bg);
    color: var(--uncertain-ink);
    font-size: var(--t-xs);
    text-align: center;
  }

  .offline svg { flex: none; }
</style>
