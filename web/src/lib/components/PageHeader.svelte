<script lang="ts">
  import type { Snippet } from 'svelte';

  /**
   * The title block every page opens with.
   *
   * Extracted because it existed twice, byte for byte, in the dashboard and the
   * tracker — and not at all on the feed, which is why the three pages read as
   * three products. That is the correctness argument design-law makes for
   * extraction rather than a tidiness one: the wordmark's target-size fix once
   * reached one of its three copies and not the other two, and a duplicated
   * header is the same trap with a slower fuse.
   *
   * The action slot is optional. A page with nothing to do at the top level
   * gets a title and a sentence, and the row stays the same height either way.
   */
  interface Props {
    title: string;
    /** One sentence on what this page is showing. Never decorative. */
    sub?: string;
    action?: Snippet;
  }
  const { title, sub, action }: Props = $props();
</script>

<header class="page-head">
  <div class="text">
    <h1>{title}</h1>
    {#if sub}<p class="t-muted">{sub}</p>{/if}
  </div>
  {#if action}{@render action()}{/if}
</header>

<style>
  .page-head {
    display: flex;
    align-items: flex-end;
    gap: var(--s-4);
    flex-wrap: wrap;
  }

  .text {
    flex: 1;
    min-width: 240px;
  }

  h1 {
    font-size: var(--t-2xl);
    letter-spacing: var(--tr-2xl);
    margin: 0;
  }

  p {
    margin: var(--s-1) 0 0;
  }
</style>
