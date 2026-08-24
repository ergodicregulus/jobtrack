<script lang="ts">
  interface Props {
    label: string;
    value: number | string;
    hint?: string;
    tone?: 'neutral' | 'grow' | 'shrink' | 'accent';
    href?: string;
  }
  const { label, value, hint, tone = 'neutral', href }: Props = $props();
</script>

<!--
  A stat that links somewhere is a button; one that does not is a figure. Making
  the distinction visible matters more than it sounds: a card that looks
  clickable and is not is the fastest way to make an interface feel broken.
-->
<svelte:element
  this={href ? 'a' : 'div'}
  {href}
  class="stat card {tone}"
  class:card-interactive={!!href}
>
  <span class="stat-label">{label}</span>
  <span class="stat-value">{value}</span>
  {#if hint}<span class="t-micro">{hint}</span>{/if}
</svelte:element>

<style>
  .stat {
    position: relative;
    display: flex; flex-direction: column; gap: 3px;
    padding: var(--s-4);
    color: var(--fg);
    overflow: hidden;
  }
  .stat:hover { text-decoration: none; }

  /* A 2px rail on the leading edge carries the tone. Tinting the whole card
     would make a row of four read as a chart rather than as figures. */
  .stat::before {
    content: '';
    position: absolute;
    left: 0; top: 0; bottom: 0;
    width: 2px;
    background: var(--rail, transparent);
  }
  .grow   { --rail: var(--grow); }
  .shrink { --rail: var(--shrink); }
  .accent { --rail: var(--accent); }

  .stat-label { order: -1; }
</style>
