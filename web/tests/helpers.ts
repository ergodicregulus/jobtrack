import type { Page } from '@playwright/test';

/**
 * Waits until the client-side app has actually taken over.
 *
 * Every page here is server-rendered, so buttons are VISIBLE and clickable long
 * before their handlers exist. Playwright happily clicks in that window, the
 * click does nothing, and the failure reads as a broken control rather than a
 * race — which cost several rounds of chasing phantom bugs in the account menu,
 * the onboarding wizard and the theme toggle.
 *
 * Two tempting signals are both worthless here:
 *
 *   - `waitForLoadState('networkidle')` reports that the network settled, which
 *     says nothing about whether Svelte has mounted.
 *   - SvelteKit's `__sveltekit_*` global is written by an inline script in the
 *     SSR'd HTML, so it is present before any hydration has happened at all.
 *
 * The root layout sets `data-hydrated` from a mount effect, which is precisely
 * the moment handlers attach.
 */
export async function hydrated(page: Page): Promise<void> {
  await page.waitForSelector('html[data-hydrated="true"]', { state: 'attached' });
}
