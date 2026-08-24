<script lang="ts">
  import Brand from '$lib/components/Brand.svelte';
  import '../app.css';
  import { page } from '$app/state';
  import NavProgress from '$lib/components/NavProgress.svelte';
  import OfflineBanner from '$lib/components/OfflineBanner.svelte';
  import type { LayoutData } from './$types';

  interface Props { data: LayoutData; children: import('svelte').Snippet; }
  const { data, children }: Props = $props();

  let menuOpen = $state(false);

  const nav = [
    { href: '/dashboard', label: 'Dashboard' },
    { href: '/jobs', label: 'Jobs' },
    { href: '/tracker', label: 'Tracker' }
  ];

  // Onboarding and auth pages get a bare shell: navigation during a focused
  // linear task is an invitation to abandon it, and every extra control on a
  // sign-in page is a place to go that is not signing in.
  const bare = $derived(
    ['/login', '/signup', '/onboarding'].some((p) => page.url.pathname.startsWith(p))
  );

  const current = $derived(page.url.pathname);
  const initials = $derived(
    data.profile
      ? ((data.profile.first_name[0] ?? '') + (data.profile.last_name[0] ?? '')).toUpperCase() || '?'
      : '?'
  );

  // Close the menu on navigation. Without this the panel stays open over the
  // new page, which reads as a broken control.
  $effect(() => {
    current;
    menuOpen = false;
  });

  // Mark the document once the client app has actually taken over.
  //
  // Every page here is server-rendered, so controls are visible and clickable
  // long before their handlers exist. Nothing in the SSR'd HTML distinguishes
  // "painted" from "interactive": SvelteKit's own `__sveltekit_*` global is
  // written by an inline script in the initial response, so waiting on it
  // proves only that the HTML arrived.
  //
  // This effect runs after mount, which is exactly the moment handlers are
  // attached. The E2E suite waits on it, and CSS can use it to hold back
  // affordances that would not work yet.
  $effect(() => {
    document.documentElement.dataset.hydrated = 'true';
  });
</script>

<svelte:head>
  <title>JobTrack</title>
  <meta
    name="description"
    content="Fresh engineering roles from company applicant tracking systems, not job boards."
  />
</svelte:head>

<NavProgress />
<OfflineBanner />

<!-- WCAG 2.4.1: a keyboard user must be able to reach the content without
     tabbing through the whole header on every navigation. -->
<a class="skip-link" href="#main">Skip to content</a>

{#if !bare}
  <header class="site-header">
    <div class="shell bar">
      <Brand href={data.signedIn ? '/dashboard' : '/'} />

      {#if data.signedIn}
        <nav class="nav" aria-label="Main">
          {#each nav as item (item.href)}
            <a
              href={item.href}
              class="nav-link"
              aria-current={current.startsWith(item.href) ? 'page' : undefined}
            >
              {item.label}
            </a>
          {/each}
        </nav>
      {/if}

      <!--
        Global search, once the header has room for it without crowding the nav.

        A GET form to /jobs, so it works with JavaScript off and lands on the
        same filtered page the feed's own search produces — one search, one
        result page, rather than a header box that behaves differently from the
        one twenty pixels below it.

        Hidden below 900px rather than collapsed to an icon: on a phone the
        feed's own search is one tap away and a second entry point competes
        with the nav for a row that has no space to spare.
      -->
      <!--
        Shown to everyone, signed in or not.

        It was inside the signed-in branch, which meant an anonymous visitor on
        a desktop had NO search box anywhere: the header's was absent and the
        feed's is hidden above 900px by the pairing below. The feed is public
        on purpose — discovery must not require an account — so its search
        cannot be an account feature either.
      -->
      <form
          data-search
          class="global-search"
          method="GET"
          action="/jobs"
          role="search"
          aria-label="Search jobs"
        >
          <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <circle cx="7" cy="7" r="4.5" fill="none" stroke="currentColor" stroke-width="1.5" />
            <path d="M10.5 10.5L14 14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
          </svg>
          <label class="sr-only" for="jt-search">Search roles, skills and companies</label>
          <input id="jt-search" type="search" name="q" placeholder="Search roles, skills…" autocomplete="off" />
        </form>

      <!--
        No theme control here.

        Persistent real estate is earned by frequency times value, and a theme
        is chosen roughly once per user, ever — with `system` as the default,
        most never touch it at all. It lives in Settings, which is where
        Claude.ai, Slack, Linear and GitHub all put theirs, for the same reason.
      -->
      <div class="header-actions">
        {#if data.signedIn}
          <div class="menu-wrap">
            <button
              class="avatar"
              aria-haspopup="true"
              aria-expanded={menuOpen}
              aria-label="Account menu"
              onclick={() => (menuOpen = !menuOpen)}
            >
              {initials}
            </button>

            {#if menuOpen}
              <!-- Click-away catcher. A transparent full-screen button is the
                   simplest thing that also works for keyboard users, because it
                   is focusable and Escape-able rather than a bare div. -->
              <button
                class="scrim"
                aria-label="Close menu"
                onclick={() => (menuOpen = false)}
              ></button>

              <!--
                Deliberately NOT role="menu".

                role="menu" is a contract: it promises arrow-key navigation,
                Home/End, type-ahead and focus management, and a screen-reader
                user who hears "menu" expects all of it. This is a small popover
                with two controls and none of that behaviour, so claiming the
                role would describe an interaction that does not exist.

                It also did not survive contact with the markup — wrapping the
                sign-out button in a <form> broke the menu/menuitem parent
                relationship, and the button stopped being exposed as a menu
                item at all. A plain labelled group of ordinary links and
                buttons is honest, works with Tab, and needs no JavaScript.
              -->
              <div class="menu rise" aria-label="Account">
                <div class="menu-head">
                  <span class="menu-name">{data.profile?.first_name || 'Your account'}</span>
                  <span class="t-micro">{data.profile?.target_title || 'Engineer'}</span>
                </div>
                <a class="menu-item" href="/profile">Profile</a>
                <a class="menu-item" href="/settings">Settings</a>
                <form method="POST" action="/logout">
                  <button class="menu-item danger" type="submit">Sign out</button>
                </form>
              </div>
            {/if}
          </div>
        {:else}
          <a class="btn btn-sm" href="/login">Sign in</a>
          <a class="btn btn-sm btn-primary" href="/signup">Get started</a>
        {/if}
      </div>
    </div>
  </header>
{/if}

<!-- tabindex="-1" is what makes the skip link actually work. Without it the
     target is not focusable, browsers move focus inconsistently or not at all,
     and the link scrolls the page while leaving the keyboard where it was —
     which is worse than having no skip link, because it looks like it worked. -->
<main id="main" class:bare tabindex="-1">
  {@render children()}
</main>

{#if !bare}
  <footer class="site-footer">
    <div class="shell">
      <p>
        Postings come from employers' own applicant tracking systems, so every
        apply link lands in a real requisition queue.
      </p>
    </div>
  </footer>
{/if}

<style>
  .site-header {
    position: sticky;
    top: 0;
    z-index: 20;
    background: color-mix(in oklab, var(--bg) 82%, transparent);
    backdrop-filter: saturate(1.6) blur(12px);
    box-shadow: 0 1px 0 var(--ring);
  }

  .bar {
    display: flex; align-items: center; gap: var(--s-5);
    height: 58px;
  }

  /* The same rail that marks freshness on every card, used as the wordmark.
     The identity IS the idea. */

  /* min-width:0 and overflow-x are what stop the nav forcing the PAGE wider.
     A flex item defaults to min-width:auto — it refuses to shrink below its
     content — so on a 390px phone the three links pushed the bar to 415px and
     every page in the product scrolled sideways. The nav scrolls within itself
     instead, which is what the design specifies. */
  .nav {
    display: flex; align-items: center; gap: 2px;
    min-width: 0;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .nav::-webkit-scrollbar { display: none; }

  /* display is owned by [data-search] in app.css and deliberately NOT set
     here. Svelte scopes component styles with a generated class, which
     outranks a bare attribute selector — so a `display: flex` on this rule
     beat `[data-search] { display: none }` and the header search rendered at
     390px, where it is supposed to be hidden and where the feed's own search
     is already showing. Specificity, not the media query, was the bug. */
  .global-search {
    position: relative;
    align-items: center;
    margin-left: var(--s-3);
  }
  .global-search svg {
    position: absolute; left: 10px;
    color: var(--fg-faint); /* non-text */
    pointer-events: none;
  }
  .global-search input {
    width: 100%;
    min-height: 32px;
    padding: 0 var(--s-2) 0 30px;
    border: 0;
    border-radius: var(--radius-full);
    background: var(--bg-sunken);
    box-shadow: inset 0 0 0 1px var(--ring);
    font-size: var(--t-sm);
  }
  .global-search input::placeholder { color: var(--fg-subtle); }
  .global-search input:focus-visible { outline: 2px solid var(--focus); outline-offset: 1px; }

  .nav-link {
    /* The design's nav item: 36px minimum, pill radius, one padding. */
    display: inline-flex; align-items: center;
    min-height: 36px;
    padding: 0 var(--s-3);
    border-radius: var(--radius);
    color: var(--fg-muted);
    font-size: var(--t-base);
    font-weight: 500;
    transition: color var(--fast) var(--ease), background var(--fast) var(--ease);
  }
  .nav-link:hover { color: var(--fg); background: var(--bg-hover); text-decoration: none; }
  /* The active marker is a filled pill, per the design system.
     
     It was a 2px underline at `bottom: -18px`, an offset chosen to reach the
     header's lower edge. It did not: the bar is 58px, the link centres at
     ~46px, so the rule landed at ~64px — six pixels BELOW the header, floating
     in the page. A magic offset that has to agree with a height, a padding and
     a font size will disagree with one of them eventually. The pill needs no
     offset and is what the design specified in the first place. */
  .nav-link[aria-current='page'] {
    background: var(--bg-hover);
    color: var(--fg);
    font-weight: 560;
  }

  .header-actions {
    display: flex; align-items: center; gap: var(--s-2);
    margin-left: auto;
  }

  .avatar {
    display: inline-flex; align-items: center; justify-content: center;
    width: 32px; height: 32px;
    border: 0;
    border-radius: var(--radius-full);
    background: var(--accent-bg);
    color: var(--accent-ink);
    font-size: var(--t-sm);
    font-weight: 620;
    letter-spacing: 0.02em;
    cursor: pointer;
    box-shadow: inset 0 0 0 1px var(--ring-accent);
    transition: box-shadow var(--fast) var(--ease);
  }
  .avatar:hover { box-shadow: inset 0 0 0 1px var(--accent); }

  .menu-wrap { position: relative; }

  .scrim {
    position: fixed; inset: 0;
    background: transparent;
    border: 0;
    cursor: default;
    z-index: 30;
  }

  .menu {
    position: absolute;
    top: calc(100% + 8px); right: 0;
    z-index: 40;
    min-width: 208px;
    padding: var(--s-1);
    background: var(--bg-raised);
    border-radius: var(--radius-md);
    box-shadow: var(--e-3);
  }

  .menu-head {
    display: flex; flex-direction: column; gap: 1px;
    padding: var(--s-2) var(--s-3) var(--s-3);
    margin-bottom: var(--s-1);
    box-shadow: 0 1px 0 var(--ring);
  }
  .menu-name { font-weight: 580; font-size: var(--t-base); }

  .menu-item {
    display: block; width: 100%;
    padding: 8px var(--s-3);
    text-align: left;
    background: none; border: 0;
    border-radius: var(--radius-sm);
    color: var(--fg);
    font-size: var(--t-base);
    cursor: pointer;
  }
  .menu-item:hover { background: var(--bg-hover); text-decoration: none; }
  .menu-item.danger:hover { color: var(--shrink-ink); background: var(--shrink-bg); }

  /* flex:1 is what pushes the footer to the bottom on short pages. */
  main { flex: 1; padding-bottom: var(--s-8); }
  main.bare { padding-bottom: 0; }

  .site-footer {
    box-shadow: 0 -1px 0 var(--ring);
    padding: var(--s-5) 0;
    font-size: var(--t-sm);
    color: var(--fg-subtle);
  }

  @media (max-width: 720px) {
    .bar { gap: var(--s-3); }
    /* The nav keeps its natural position between the wordmark and the account
       menu.

       It used to be `order: 3; width: 100%`, intended to drop it onto its own
       row — but `.bar` has no flex-wrap and a fixed 58px height, so the width
       did nothing and the order simply moved the nav AFTER the avatar. The
       header read brand, avatar, nav, which looks like a mistake because it is
       one. The nav now shrinks and scrolls within itself (see .nav above),
       which is what that rule was working around. */
    .site-header { position: static; }
  }
</style>
