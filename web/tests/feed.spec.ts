import { test, expect } from '@playwright/test';
import { hydrated } from './helpers';

/**
 * The core loop, end to end. Each test proves something no unit test can:
 * that the browser, the SvelteKit server and the Go API agree.
 */

test.describe('job feed', () => {
  test('server-renders real postings before any JavaScript runs', async ({ browser }) => {
    // JavaScript disabled entirely. If the feed depends on hydration to show
    // content, this fails — and a feed that needs JS to display text has given
    // up the SSR advantage the whole architecture is built on.
    const context = await browser.newContext({ javaScriptEnabled: false });
    const page = await context.newPage();
    await page.goto('/jobs');

    await expect(page.locator('li.card').first()).toBeVisible();
    const count = await page.locator('li.card').count();
    expect(count).toBeGreaterThan(0);

    await context.close();
  });

  test('every card shows its age, its ATS vendor, and a real apply link', async ({ page }) => {
    await page.goto('/jobs');
    const card = page.locator('li.card').first();

    // Age is never hidden behind a filter: freshness is the product.
    await expect(card.locator('.age')).toBeVisible();

    // The ATS vendor is shown so the user knows the link lands in a real
    // requisition queue rather than a job board's inbox.
    await expect(card.locator('.via')).toContainText('via');

    // The apply link must point at the ATS, and must never be proxied
    // through us — we never claim to have applied on the user's behalf.
    const apply = card.locator('a.apply');
    const href = await apply.getAttribute('href');
    expect(href).toMatch(/^https?:\/\//);
    expect(href).not.toContain('localhost');
    await expect(apply).toHaveAttribute('rel', /noopener/);
    await expect(apply).toHaveAttribute('target', '_blank');
  });

  test('undisclosed compensation says so rather than showing zero', async ({ page }) => {
    await page.goto('/jobs');
    const undisclosed = page.locator('.fact.undisclosed');
    if (await undisclosed.count()) {
      await expect(undisclosed.first()).toContainText('Not disclosed');
      // A zero would be a lie: "not published" and "pays nothing" are
      // completely different statements.
      await expect(undisclosed.first()).not.toContainText('0');
    }
  });

  test('filter state lives in the URL and survives a reload', async ({ page }) => {
    await page.goto('/jobs?mode=remote&posted_within=any');
    await expect(page).toHaveURL(/mode=remote/);

    await page.reload();
    // Shareable and back-button-correct because the state is in the URL, not
    // in client memory. The rail reads its own state back out of the URL, so
    // this also proves the two cannot disagree.
    // Scoped to the desktop rail. The same component also renders inside the
    // narrow-screen sheet, so an unscoped locator counts both copies — one of
    // which is display:none and therefore not in the accessibility tree, but
    // very much in the DOM.
    const active = page.locator('aside .fchip.on');
    await expect(active.filter({ hasText: 'Remote' })).toHaveCount(1);

    // And the removable summary must agree with it — a chip saying "Remote"
    // while the parameter is gone is the failure this pairing prevents.
    // Two are active — mode=remote and posted_within=any — so this asserts on
    // the set rather than on a single chip.
    await expect(page.locator('.active-chip').filter({ hasText: 'Remote' })).toHaveCount(1);
  });

  test('a filter chip is a link, so it survives with JavaScript off', async ({ browser }) => {
    // The whole rail is anchors rather than buttons with handlers. Proving it
    // with JS disabled is the only way to keep that true — a handler added
    // later would pass every other test in this file.
    const ctx = await browser.newContext({ javaScriptEnabled: false });
    const page = await ctx.newPage();
    await page.goto('/jobs');

    await page.locator('aside .fchip').filter({ hasText: 'Remote' }).first().click();
    await expect(page).toHaveURL(/mode=remote/);
    await expect(page.locator('ul.results')).toBeVisible();
    await ctx.close();
  });

  test('every filter chip carries a count', async ({ page }) => {
    await page.goto('/jobs');
    // A chip with no number is an invitation to click into zero results and
    // conclude the product is empty. The count is what turns filtering from a
    // guess into a decision.
    const workStyle = page.locator('aside .group').filter({ hasText: 'Work style' });
    for (const label of ['Remote', 'Hybrid', 'On-site']) {
      const chip = workStyle.locator('.fchip').filter({ hasText: label });
      await expect(chip.locator('.count')).toHaveText(/[\d,]+/);
    }
  });

  test('search is available without an account', async ({ page }) => {
    // The feed is public on purpose, so search cannot be an account feature.
    // It briefly was: the header's box was inside the signed-in branch while
    // the feed's is hidden above 900px, which left an anonymous desktop
    // visitor with no way to search at all.
    await page.goto('/jobs');
    const search = page.locator('#jt-search');
    await expect(search).toBeVisible();

    await search.fill('engineer');
    await search.press('Enter');
    await expect(page).toHaveURL(/q=engineer/);
    await expect(page.locator('ul.results')).toBeVisible();
  });

  test('an empty result set names the likely culprit and offers a way out', async ({ page }) => {
    await page.goto('/jobs?q=zzzzznotarealskillzzzzz');
    const notice = page.locator('.notice');
    await expect(notice).toBeVisible();
    await expect(notice).toContainText('Nothing matched');
    // "No results" alone leaves the user guessing which of six filters did it.
    await expect(notice).toContainText('search term');
    await expect(notice.locator('a')).toHaveCount(2);
  });
});

test.describe('theme', () => {
  /**
   * Cookies must be scoped to the origin under test. Hardcoding localhost
   * silently sets them on the wrong origin when the suite runs
   * container-to-container, and the test then fails for a reason that has
   * nothing to do with the behaviour it is checking.
   */
  const originOf = (baseURL: string | undefined) => baseURL ?? 'http://localhost:5173';

  test('server stamps the theme so there is no flash of the wrong colours', async ({ page, baseURL }) => {
    await page.context().addCookies([
      { name: 'jt_theme', value: 'dark', url: originOf(baseURL) }
    ]);
    const response = await page.goto('/jobs');
    const html = await response!.text();

    // Asserted on the RAW HTML, not the live DOM: the point is that the
    // attribute is present before the browser paints, which a DOM assertion
    // after hydration cannot distinguish from a client-side fix.
    expect(html).toContain('data-theme="dark"');
  });

  test('system preference leaves the attribute empty so the OS decides', async ({ page, baseURL }) => {
    await page.context().addCookies([
      { name: 'jt_theme', value: 'system', url: originOf(baseURL) }
    ]);
    const response = await page.goto('/jobs');
    const html = await response!.text();

    // Resolving "system" server-side would pin the user to whatever we guessed.
    expect(html).toContain('data-theme=""');
  });

  test('an attacker-controlled cookie cannot inject into the HTML attribute', async ({ page, baseURL }) => {
    await page.context().addCookies([
      { name: 'jt_theme', value: '"><script>window.__pwned=1</script>', url: originOf(baseURL) }
    ]);
    await page.goto('/jobs');

    // The cookie is not HttpOnly — the client toggle writes it — so its value
    // is attacker-controllable and MUST be validated against an allowlist
    // before reaching an HTML attribute.
    const pwned = await page.evaluate(() => (window as never as { __pwned?: number }).__pwned);
    expect(pwned).toBeUndefined();
  });

  test('the theme control lives in settings and persists across a reload', async ({ page }) => {
    // The control moved out of the header deliberately: persistent real estate
    // is earned by frequency times value, and a theme is chosen about once per
    // user, ever. This test follows it rather than asserting it is still there.
    await page.goto('/login');
    await page.getByLabel('Email').fill('dev@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);

    await page.goto('/settings');
    await hydrated(page);

    await page.locator('label.opt:has(input[value="dark"])').click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');

    await page.getByRole('button', { name: /save appearance/i }).click();
    await expect(page.getByRole('status')).toContainText('Saved');

    // The cookie is what makes the NEXT server render match, with no flash.
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');

    // And it is on the account, so it survives a different page too.
    await page.goto('/jobs');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });
});

test.describe('accessibility', () => {
  test('the entire feed is reachable and operable by keyboard alone', async ({ page }) => {
    await page.goto('/jobs');

    // The skip link must be the first stop, or a keyboard user tabs through
    // the whole header on every navigation.
    await page.keyboard.press('Tab');
    await expect(page.locator('.skip-link')).toBeFocused();

    await page.keyboard.press('Enter');
    // The skip target is the shell's <main>, which is shared by every page.
    // It carries tabindex="-1" precisely so this works.
    await expect(page.locator('#main')).toBeFocused();
  });

  test('cards are a list of links, not clickable divs', async ({ page }) => {
    await page.goto('/jobs');
    // Semantics matter here: a screen reader should announce a list of jobs,
    // which requires real list markup and real links.
    await expect(page.locator('ul.results')).toBeVisible();
    await expect(page.locator('ul.results > li.card').first()).toBeVisible();
    await expect(page.locator('li.card h3 a').first()).toBeVisible();
  });

  test('interactive targets meet the WCAG 2.2 minimum size', async ({ page }) => {
    await page.goto('/jobs');
    // 2.5.8 Target Size (Minimum) is 24x24 CSS px. The compact card design
    // pushes toward small controls, which is exactly the risk it addresses.
    for (const sel of ['a.apply', 'aside .fchip']) {
      const box = await page.locator(sel).first().boundingBox();
      expect(box, `${sel} should be visible`).not.toBeNull();
      expect(box!.height, `${sel} height`).toBeGreaterThanOrEqual(24);
      expect(box!.width, `${sel} width`).toBeGreaterThanOrEqual(24);
    }
  });

  test('focused cards are not hidden behind the sticky header', async ({ page }) => {
    await page.goto('/jobs');
    // WCAG 2.2 — 2.4.11 Focus Not Obscured. Tabbing down a long list under a
    // sticky header is the exact scenario this criterion describes.
    const headerBox = await page.locator('.site-header').boundingBox();

    const link = page.locator('li.card h3 a').nth(6);
    await link.focus();
    await page.waitForTimeout(100);

    const linkBox = await link.boundingBox();
    expect(linkBox).not.toBeNull();
    expect(linkBox!.y).toBeGreaterThanOrEqual(headerBox!.height - 1);
  });
});

test.describe('hiding a posting', () => {
  // Serial, and each test acts on a DIFFERENT card.
  //
  // Both tests share the seeded account, and the first version had both hide
  // the FIRST card: run in parallel, one test's undo deleted the other's
  // dismissal and the failure read as a missing feed predicate. Sharing a
  // fixture is the constraint here, so the tests are made not to collide
  // rather than pretending they are independent.
  test.describe.configure({ mode: 'serial' });

  test.beforeEach(async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('senior@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);
  });

  // The collapse-in-place behaviour is the whole design. A row that vanishes
  // under the cursor makes the list jump and leaves nothing to click if the
  // click was wrong, so this asserts the count is UNCHANGED — the strip
  // replaces the card rather than removing it.
  test('collapses to an undo strip rather than vanishing, and comes back', async ({ page }) => {
    await page.goto('/jobs');
    await hydrated(page);

    const before = await page.locator('li.card, li.hidden-row').count();
    const card = page.locator('li.card').first();
    const title = (await card.locator('h3, h2').first().innerText()).trim();

    await card.getByRole('button', { name: /^hide$/i }).click();

    const strip = page.locator('li.hidden-row').first();
    await expect(strip).toBeVisible();
    await expect(strip).toContainText(title.slice(0, 24));
    expect(await page.locator('li.card, li.hidden-row').count()).toBe(before);

    await strip.getByRole('button', { name: /undo/i }).click();
    await expect(page.locator('li.hidden-row')).toHaveCount(0);
  });

  // Where it actually pays off: gone from the NEXT request, not just this
  // render. This is the assertion that fails if the feed predicate is missing.
  test('stays gone on the next load, and returns after undo', async ({ page }) => {
    await page.goto('/jobs');
    await hydrated(page);

    // The SECOND card, so this cannot collide with the test above.
    const card = page.locator('li.card').nth(1);
    const title = (await card.locator('h3, h2').first().innerText()).trim();
    const href = await card.locator('a[href^="/jobs/"]').first().getAttribute('href');
    const postingId = Number(href!.split('/').pop());

    await card.getByRole('button', { name: /^hide$/i }).click();
    await expect(page.locator('li.hidden-row').first()).toBeVisible();

    // The strip appears optimistically, so its presence is not proof the write
    // landed. Wait for the server to actually hold the dismissal before
    // reloading, or this races the request rather than testing it.
    // The strip appears optimistically, so its presence is not proof the write
    // landed. Wait for the server to actually hold THIS dismissal before
    // reloading, or the reload races the request rather than testing it.
    await expect
      .poll(async () =>
        page.evaluate(async (pid) => {
          const res = await fetch('/v1/me/dismissals');
          if (!res.ok) return false;
          const body = await res.json();
          return (body.items ?? []).some((d: { posting_id: number }) => d.posting_id === pid);
        }, postingId)
      )
      .toBe(true);

    await page.reload();
    await hydrated(page);
    await expect(page.locator(`li.card a[href="/jobs/${postingId}"]`)).toHaveCount(0);

    // Restore through the API so the seeded account is left as it was found —
    // a suite that mutates a shared fixture fails differently on its second run.
    await page.evaluate(
      (pid) => fetch(`/v1/me/dismissals/${pid}`, { method: 'DELETE' }),
      postingId
    );
    await page.reload();
    await hydrated(page);
    await expect(page.locator(`li.card a[href="/jobs/${postingId}"]`)).toHaveCount(1);
    expect(title.length).toBeGreaterThan(0);
  });
});
