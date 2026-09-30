import { expect } from '@playwright/test';
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

/**
 * Signs in and lands on the dashboard.
 *
 * Hydration comes FIRST, before a single keystroke, and that ordering is the
 * whole reason this helper exists. A server-rendered input accepts `fill()`
 * immediately, and when Svelte mounts a moment later it re-initialises the field
 * from component state — silently discarding what was typed. The form then
 * submits with an empty email, the page stays on /login, and the failure reads
 * as broken authentication.
 *
 * It cost a CI run to find, from a snapshot showing the password box full and
 * the email box empty: the password had been typed late enough to survive.
 *
 * This existed as twelve copies of the same five lines, each racing
 * independently.
 */
export async function signIn(page: Page, email: string): Promise<void> {
  await page.goto('/login');
  await hydrated(page);
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill('dev-password-please');
  await page.getByRole('button', { name: /sign in/i }).click();
  await expect(page).toHaveURL(/\/dashboard/);
  await hydrated(page);
}
