<script lang="ts">
  /**
   * The wordmark, in one place.
   *
   * It existed three times — in the layout, the auth shell and onboarding —
   * and the copies had already drifted: the rail was 17px in one and 16px in
   * another, and only the layout carried `min-height: 24px`. That minimum is
   * not cosmetic. The mark is a link, so WCAG 2.2 SC 2.5.8 applies to it like
   * any other control, and it measured 23px until someone fixed it. The fix
   * reached one of the three copies.
   *
   * That is the argument for this component: not that three declarations are
   * untidy, but that a correction applied to one of them is a correction the
   * other two silently do not get.
   */
  const { href = '/' }: { href?: string } = $props();
</script>

<a class="brand" {href}>
  <span class="rail" aria-hidden="true"></span>
  <span class="name">JobTrack</span>
</a>

<style>
  .brand {
    display: inline-flex;
    align-items: center;
    gap: var(--s-2);
    /* SC 2.5.8: it is a link, so the 24px minimum applies. */
    min-height: 24px;
    flex: none;
    color: var(--fg);
    font-weight: 650;
    letter-spacing: -0.02em;
    font-size: var(--t-md);
  }

  .brand:hover {
    text-decoration: none;
  }

  .rail {
    width: 3px;
    height: 17px;
    background: var(--grow);
    border-radius: 2px;
  }
</style>
