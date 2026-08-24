/**
 * Screenshot every page, at both widths, in both themes.
 *
 * A tool, not a test — it asserts nothing. It exists because the roadmap's
 * verification step says to LOOK at the pages, and every layout bug in this
 * project has been found that way rather than by a green suite: the 5,100px
 * tracker, the sticky bar reading through its own background, the dashboard's
 * void, the wall of red gap chips. None of those failed a test.
 *
 *   make screenshots            # everything
 *   make screenshots PAGES=jobs # one page
 *
 * Output lands in web/.screenshots/, which is git-ignored.
 */
import { chromium } from '@playwright/test';
const base = process.env.E2E_BASE_URL;
const b = await chromium.launch();
const widths = [['desk', 1440, 900], ['phone', 390, 844]];
const pages = (process.env.PAGES || 'dashboard,jobs,tracker,profile,settings').split(',');

for (const theme of ['light', 'dark']) {
  for (const [name, width, height] of widths) {
    if (theme === 'dark' && name === 'phone') continue; // one dark pass is enough to catch a palette bug
    const ctx = await b.newContext({ viewport: { width, height } });
    await ctx.addCookies([{ name: 'jt_theme', value: theme, url: base }]);
    const page = await ctx.newPage();
    await page.goto(`${base}/login`);
    await page.waitForFunction(() => document.documentElement.dataset.hydrated === 'true');
    await page.getByLabel('Email').fill('grad@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(/dashboard/, { timeout: 20000 });
    for (const p of pages) {
      await page.goto(`${base}/${p}`);
      await page.waitForTimeout(700);
      await page.screenshot({ path: `/shots/${p}-${name}-${theme}.png`, fullPage: true });
    }
    await ctx.close();
  }
}
// The landing page, signed out.
const ctx = await b.newContext({ viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
await page.goto(base);
await page.waitForTimeout(700);
await page.screenshot({ path: '/shots/landing-desk-light.png', fullPage: true });
await b.close();
console.log('ok');
