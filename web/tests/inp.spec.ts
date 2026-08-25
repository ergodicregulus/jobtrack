import { test, expect, type Page, type CDPSession } from '@playwright/test';
import { hydrated } from './helpers';

/**
 * The Event Timing API, which TypeScript's DOM lib does not yet describe.
 *
 * `interactionId` and `durationThreshold` are both in the published spec and
 * both shipped in Chromium years ago; lib.dom.d.ts simply lags. Declared here
 * rather than cast to `any` at the call sites, so the two fields this file
 * depends on are named once and checked everywhere they are used.
 */
declare global {
  interface PerformanceEventTiming extends PerformanceEntry {
    readonly interactionId?: number;
  }
  interface PerformanceObserverInit {
    durationThreshold?: number;
  }
}

/**
 * INP against the 200 ms budget, measured rather than proxied.
 *
 * The obvious tool here is Lighthouse CI, and it was rejected. Lighthouse
 * cannot measure INP: INP is a field metric that requires real interactions, so
 * a lab run reports Total Blocking Time INSTEAD and leaves you to treat it as a
 * stand-in. TBT is main-thread busyness during load; INP is the delay a person
 * feels when they click something. They are correlated and they are not the
 * same number, and the budget in CLAUDE.md says INP.
 *
 * Playwright is already here and can drive the actual interactions, so this
 * observes the real `event` timings the browser reports. It measures the metric
 * the budget names, and it adds nothing to package.json — Lighthouse CI and its
 * Chrome tooling would have been the largest dependency in the repo, added to
 * approximate a number we can take directly.
 */

// 4× matches the budget line and Lighthouse's mid-tier mobile default. Real
// low-end Android is worse; this is the stated bar, not the worst case.
const CPU_THROTTLE = 4;
const BUDGET_MS = 200;

// p75 needs a sample. Eight visits is enough for the 6th-worst to be a real
// quantile and short enough to run in about a minute under 4× throttling.
const VISITS = 8;

/**
 * Installs the observer before any page script runs.
 *
 * `buffered: true` is not enough on its own — it replays entries from before
 * the observer existed, but only for the current document, so this must be an
 * init script rather than an evaluate() after navigation.
 *
 * durationThreshold is 16: the spec's floor, and below the default of 104 which
 * would silently discard every interaction that is merely mediocre and report a
 * page with no measurements as passing.
 */
async function observeInteractions(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const w = window as unknown as { __inp: Map<number, number> };
    w.__inp = new Map();
    const record = (entries: PerformanceEntryList) => {
      for (const e of entries) {
        const id = (e as PerformanceEventTiming).interactionId ?? 0;
        // interactionId is 0 for events that are not part of a discrete
        // interaction (scroll, pointermove). Those are not INP candidates.
        if (id === 0) continue;
        // One interaction fires several events — pointerdown, pointerup, click.
        // INP is the longest of them, not their sum.
        w.__inp.set(id, Math.max(w.__inp.get(id) ?? 0, e.duration));
      }
    };
    new PerformanceObserver((l) => record(l.getEntries())).observe({
      type: 'event',
      buffered: true,
      durationThreshold: 16
    });
    // first-input is reported even below the threshold, and the first
    // interaction after load is usually the slowest one on the page.
    new PerformanceObserver((l) => record(l.getEntries())).observe({
      type: 'first-input',
      buffered: true
    });
  });
}

/**
 * This visit's INP.
 *
 * For fewer than 50 interactions the web-vitals definition is the worst one,
 * which is what a handful of scripted clicks always is. Returns null when
 * nothing crossed 16 ms — that is a fast page, not a failed measurement, and
 * scoring it as 0 would drag the p75 down with a value we did not observe.
 */
async function readINP(page: Page): Promise<number | null> {
  const durations = await page.evaluate(() =>
    Array.from((window as unknown as { __inp: Map<number, number> }).__inp.values())
  );
  return durations.length === 0 ? null : Math.max(...durations);
}

/** The interactions a person actually performs on the feed, in order. */
async function exercise(page: Page): Promise<void> {
  await page.locator('aside .fchip').filter({ hasText: 'Remote' }).first().click();
  await expect(page.locator('ul.results')).toBeVisible();

  // Typing is the interaction most likely to blow the budget: every keystroke
  // is its own INP candidate and each one re-renders the list.
  const search = page.locator('#jt-search');
  if (await search.count()) {
    await search.click();
    await search.pressSequentially('engineer', { delay: 60 });
  }

  const sort = page.locator('aside select').first();
  if (await sort.count()) await sort.selectOption({ index: 1 }).catch(() => {});
}

function p75(values: number[]): number {
  const s = [...values].sort((a, b) => a - b);
  // Nearest-rank: the smallest value with at least 75% of the sample at or
  // below it. No interpolation — inventing a value between two measurements is
  // exactly the kind of fabricated number this repo forbids.
  return s[Math.min(s.length - 1, Math.ceil(0.75 * s.length) - 1)];
}

test.describe('INP budget', () => {
  // Under 4× throttling each visit takes several seconds, and the suite's
  // default 45 s covers one visit, not eight.
  test.setTimeout(180_000);

  test(`p75 INP at ${CPU_THROTTLE}x CPU throttle is within ${BUDGET_MS} ms`, async ({
    page,
    browserName
  }) => {
    test.skip(browserName !== 'chromium', 'Event Timing API is Chromium-only.');

    let cdp: CDPSession | null = null;
    const samples: number[] = [];

    await observeInteractions(page);
    for (let i = 0; i < VISITS; i++) {
      await page.goto('/jobs');
      // Throttle AFTER navigation so the measurement is of interaction cost,
      // not of a load made artificially slow. INP is what happens once the page
      // is there.
      cdp ??= await page.context().newCDPSession(page);
      await cdp.send('Emulation.setCPUThrottlingRate', { rate: CPU_THROTTLE });
      await hydrated(page);

      await exercise(page);
      const inp = await readINP(page);
      if (inp !== null) samples.push(inp);
    }
    await cdp?.send('Emulation.setCPUThrottlingRate', { rate: 1 });

    // No sample at all means every interaction was under 16 ms across eight
    // visits. Report it rather than passing silently, so a broken exercise()
    // that clicks nothing cannot masquerade as a fast page.
    if (samples.length === 0) {
      console.log(`INP: no interaction exceeded 16 ms in ${VISITS} visits.`);
      return;
    }

    const worst = Math.max(...samples);
    const result = p75(samples);
    console.log(
      `INP p75 = ${result.toFixed(0)} ms (worst ${worst.toFixed(0)} ms, ` +
        `n=${samples.length}/${VISITS}, ${CPU_THROTTLE}x CPU) — budget ${BUDGET_MS} ms`
    );
    expect(result, `INP p75 over budget at ${CPU_THROTTLE}x CPU throttle`).toBeLessThanOrEqual(
      BUDGET_MS
    );
  });
});
