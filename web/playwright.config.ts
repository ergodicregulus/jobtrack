import { defineConfig, devices } from '@playwright/test';

/**
 * E2E tests are few and cover only the core loop. They are slow and
 * flaky-prone, so they earn their place by proving the things unit tests
 * cannot: that the server, the API and the browser agree.
 */
export default defineConfig({
  testDir: 'tests',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',

  // Four workers, not one per core.
  //
  // The target is a Vite DEV server doing server-side rendering, and saturating
  // it produces failures that look exactly like real bugs: `a.apply` not found,
  // a dashboard with no matches, 30-second timeouts waiting for a page that
  // renders correctly the moment it is requested on its own. Every one of those
  // was chased down and turned out to be load, not behaviour.
  //
  // A suite that cries wolf is worse than a slower one.
  //
  // Reduced from 4 to 3 on 2026-08-17. The corpus grew from 6,677 postings to
  // 8,985 (a SmartRecruiters adapter and four new boards) and scores from
  // ~96,000 to 578,769, so every dashboard and feed render costs more than it
  // did when 4 was chosen. The symptom was the failure this comment already
  // describes: three dashboard tests failing together, each passing alone.
  workers: process.env.E2E_WORKERS ? Number(process.env.E2E_WORKERS) : 3,

  // The dev server compiles routes on first request, so the first hit to a page
  // can be genuinely slow without anything being wrong.
  timeout: 45_000,
  expect: { timeout: 10_000 },

  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:5173',
    trace: 'on-first-retry',
    actionTimeout: 15_000,
    navigationTimeout: 30_000
  },

  // The INP probe throttles the CPU 4x and visits the feed eight times, so it
  // is minutes rather than seconds. It runs on demand (`make inp`), not on every
  // `make test-e2e`, which is why it is excluded here by name rather than by a
  // tag someone has to remember to add.
  testIgnore: process.env.E2E_INP ? [] : ['inp.spec.ts'],

  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    {
      // The reference low-end profile from the performance budget. Testing
      // only on a fast desktop proves nothing about the machines this product
      // is meant to run well on.
      name: 'low-end',
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 1366, height: 768 }
      }
    }
  ]
});
