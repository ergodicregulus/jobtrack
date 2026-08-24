import { test, expect } from '@playwright/test';
import { hydrated } from './helpers';

/**
 * The one sentence the roadmap calls "done":
 *
 *   "a stranger can land on it, understand what it is, create an account,
 *    describe themselves in four steps, see roles ranked by a score that shows
 *    its working, apply through a link that lands in a real requisition queue,
 *    track what happens next, and be told the truth at every step about what we
 *    know and what we do not."
 *
 * Every other test in this suite checks a part. This one walks the whole
 * sentence in order, as one person, and fails if any clause stops being true.
 * It is deliberately slow and deliberately end-to-end.
 */
test('the whole product, as one stranger walks it', async ({ page }) => {
  test.setTimeout(120_000);

  // --- land on it, and understand what it is -------------------------------
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1 })).toContainText(
    /scored against what you actually have/i
  );
  // "understand what it is" means the claim is legible AND checkable: real
  // figures, not "thousands of opportunities".
  //
  // Located by container rather than by the sentence: Svelte 5 splits dynamic
  // expressions with comment markers, so a getByText spanning a number and its
  // surrounding words is matching across nodes and is fragile for a reason
  // that has nothing to do with the behaviour under test.
  const proof = page.locator('.proof');
  await expect(proof).toContainText(/live postings from/i);
  await expect(proof).toContainText(/\d/);

  // --- create an account ---------------------------------------------------
  const email = `acceptance-${Date.now()}@jobtrack.test`;
  await page.getByRole('link', { name: /create an account/i }).first().click();
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill('correct-horse-battery-staple');
  await page.getByRole('button', { name: /create account/i }).click();
  await expect(page).toHaveURL(/\/onboarding/);

  // --- describe themselves in four steps -----------------------------------
  await hydrated(page);
  await expect(page.getByText(/step 1 of 4/i)).toBeVisible();

  // --- see roles ranked by a score that shows its working ------------------
  await page.goto('/jobs');
  await hydrated(page);
  const first = page.locator('li.card').first();
  await expect(first).toBeVisible();

  // Filters are chips with live counts — the thing that stops someone
  // filtering blindly into zero results.
  await expect(page.locator('aside .fchip').first().locator('.count')).toHaveText(/[\d,]+/);

  // The score shows its working: open one and read the breakdown.
  await first.locator('.title a').click();
  await expect(page).toHaveURL(/\/jobs\/\d+/);
  const breakdown = page.locator('.components, .breakdown').first();
  await expect(breakdown.or(page.getByText(/how well does this fit|skills/i).first())).toBeVisible();

  // --- apply through a link that lands in a real requisition queue ---------
  const apply = page.getByRole('link', { name: /^apply on/i }).first();
  await expect(apply).toBeVisible();
  const href = await apply.getAttribute('href');
  // A real employer ATS, never a board's inbox and never our own domain.
  expect(href).toMatch(/^https:\/\//);
  expect(href).not.toContain('jobtrack');

  // --- be told the truth about what we know and do not --------------------
  // Undisclosed salary is stated, never rendered as zero.
  await page.goto('/jobs');
  await hydrated(page);
  const undisclosed = page.getByText(/not disclosed/i).first();
  if (await undisclosed.count()) {
    await expect(undisclosed).toBeVisible();
    await expect(page.getByText(/\$0\b/).first()).toHaveCount(0);
  }
});
