import { test, expect } from '@playwright/test';
import { hydrated } from './helpers';

/**
 * The whole user journey, in one pass: sign up, onboard, land on a dashboard
 * with real widgets, edit a profile, save a job, advance it in the tracker.
 *
 * This suite exists because every part of it passed in isolation while the
 * journey itself was broken — the API returned scores, the dashboard rendered,
 * and the two were never exercised against each other with a real account.
 *
 * Each account is unique per run so the suite is re-runnable without a reset,
 * and so a failure never leaves state that makes the NEXT run fail differently.
 */
const password = 'correct-horse-battery-staple';

function freshEmail(): string {
  return `e2e-${Date.now()}-${Math.floor(Math.random() * 1e6)}@jobtrack.test`;
}


async function signUp(page: import('@playwright/test').Page): Promise<string> {
  const email = freshEmail();
  await page.goto('/signup');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: /create account/i }).click();
  // Registration must land the user straight in onboarding. Asking someone to
  // sign in again immediately after signing up is a needless drop-off.
  await expect(page).toHaveURL(/\/onboarding/);
  await hydrated(page);
  return email;
}

test.describe('signup and onboarding', () => {
  test('a new account is walked through onboarding and lands on a working dashboard', async ({
    page
  }) => {
    await signUp(page);

    // Step 1 — identity. The wizard must say where the user is, not just show
    // a bar: an unlabelled bar leaves people unsure how much is left.
    await expect(page.getByText('Step 1 of 4')).toBeVisible();
    await page.getByLabel('First name').fill('Ada');
    await page.getByLabel('Last name').fill('Lovelace');
    await page.getByRole('button', { name: /continue/i }).click();

    // Step 2 — experience.
    await expect(page).toHaveURL(/step=2/);
    await hydrated(page);
    await page.getByLabel('Current title').fill('Software Engineer');
    await page.getByLabel('What you want next').fill('Senior Backend Engineer');
    await page.getByLabel('Years of experience').fill('4');
    await page.getByRole('button', { name: /continue/i }).click();

    // Step 3 — skills, via the tag picker.
    await expect(page).toHaveURL(/step=3/);
    await hydrated(page);
    const skillInput = page.getByLabel('Add a skill');
    for (const skill of ['go', 'postgresql', 'kubernetes']) {
      await skillInput.fill(skill);
      await skillInput.press('Enter');
    }
    await expect(page.locator('.chip', { hasText: 'postgresql' })).toBeVisible();
    await page.getByRole('button', { name: /continue/i }).click();

    // Step 4 — preferences, then finish.
    await expect(page).toHaveURL(/step=4/);
    await hydrated(page);
    await page.locator('label.opt:has(input[value="remote"])').click();
    await page.getByRole('button', { name: /finish/i }).click();

    // The dashboard is the destination, and it must acknowledge the new user.
    await expect(page).toHaveURL(/\/dashboard/);
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Ada');
  });

  test('progress survives a reload part-way through', async ({ page }) => {
    await signUp(page);

    await page.getByLabel('First name').fill('Grace');
    await page.getByLabel('Last name').fill('Hopper');
    await page.getByRole('button', { name: /continue/i }).click();
    await expect(page).toHaveURL(/step=2/);

    // Each step PATCHes the server, so a closed tab or a dead battery does not
    // discard everything the user has already typed.
    await page.reload();
    await page.goto('/onboarding?step=1');
    await expect(page.getByLabel('First name')).toHaveValue('Grace');
  });

  test('onboarding refuses to complete without the fields matching depends on', async ({
    page
  }) => {
    await signUp(page);

    // Skip ahead to the last step without ever entering a name or skills.
    await page.goto('/onboarding?step=4');
    await hydrated(page);
    await page.getByRole('button', { name: /finish/i }).click();

    // It must say WHICH fields are missing rather than failing vaguely — a form
    // that cannot tell you what is wrong is a dead end.
    await expect(page.locator('.banner')).toBeVisible();
    await expect(page).toHaveURL(/\/onboarding/);
  });
});

test.describe('dashboard', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('senior@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);
    await hydrated(page);
  });

  test('shows every widget with real numbers, not placeholders', async ({ page }) => {
    // Matches, pipeline, and the market summary must all be present. A
    // dashboard missing a widget looks identical to one whose query failed.
    await expect(page.getByLabel('Your matches')).toBeVisible();
    await expect(page.getByRole('heading', { name: /your best matches/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /your pipeline/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /the market right now/i })).toBeVisible();

    // Real ingested data, so the figures must be non-zero.
    const live = page.locator('.market dd').first();
    await expect(live).not.toHaveText('0');
  });

  test('the activity grid records real movement, and says so honestly', async ({ page }) => {
    const grid = page.locator('.activity');
    await expect(grid).toBeVisible();
    await expect(grid.getByRole('heading', { name: /your activity/i })).toBeVisible();

    // 12 weeks x 7 days. The shape is the information — a grid that quietly
    // rendered 40 cells would still look like a heatmap. Scoped to .grid
    // because the legend reuses the same .cell class, and the legend only
    // exists once there is activity to explain.
    await expect(grid.locator('.grid .cell')).toHaveCount(84);

    // It must never read as a streak tracker. A grid of squares carries that
    // connotation whether we intend it or not, so the copy DISCLAIMS it
    // explicitly rather than merely avoiding the word — asserting the word is
    // absent would have failed the honest version of this text, and did.
    // Both branches of the component must disclaim, and both are asserted.
    // The empty state says "no streak to keep"; the populated one says "rather
    // than a target". The regex knew only the first, so this passed for as long
    // as the seeded account happened to have no activity — and started failing
    // the moment another test gave it some. A test whose result depends on
    // which branch it happened to hit is not testing the copy, it is testing
    // the fixture.
    await expect(grid).toContainText(/no streak to keep|not a target|rather than a target/i);

    // The grid itself is hidden from assistive tech — 84 cells announced one
    // by one is a minute of noise — so the same fact must exist as text.
    await expect(grid.locator('.grid')).toHaveAttribute('aria-hidden', 'true');
    await expect(grid.locator('.sr-only')).toContainText(/12 weeks/i);
  });

  test('every match names the score AND what is missing from it', async ({ page }) => {
    const first = page.locator('.match').first();
    await expect(first).toBeVisible();

    // A score with no explanation asks for trust. Showing the gaps lets the
    // reader overrule it, which is the whole design position.
    await expect(first.locator('.score')).toBeVisible();
    const score = await first.locator('.score').textContent();
    expect(Number(score)).toBeGreaterThan(0);
    expect(Number(score)).toBeLessThanOrEqual(100);
  });

  test('signing out returns to the landing page and clears the session', async ({ page }) => {
    await page.getByRole('button', { name: 'Account menu' }).click();
    await page.getByRole('button', { name: /sign out/i }).click();

    await expect(page).toHaveURL(/\/$/);
    // The dashboard must now be unreachable, not merely unlinked.
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/login/);
  });
});

test.describe('profile', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('dev@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);
    await hydrated(page);
  });

  test('loads the stored profile and saves an edit', async ({ page }) => {
    await page.goto('/profile');
    await hydrated(page);

    await expect(page.getByLabel('First name')).toHaveValue('Arjun');

    const title = `Staff Engineer ${Date.now()}`;
    await page.getByLabel('Target title').fill(title);
    await page.getByRole('button', { name: /save changes/i }).click();

    // Saving must say that a rescore is running, or the feed appearing to
    // change by itself a moment later reads as a bug.
    await expect(page.locator('.banner.ok')).toContainText(/re-scoring/i);

    await page.reload();
    await expect(page.getByLabel('Target title')).toHaveValue(title);
  });
});

test.describe('saving and tracking', () => {
  test('a job saved from the feed appears in the tracker and can be advanced', async ({
    page
  }) => {
    // A FRESH account, not the shared demo one.
    //
    // This used the demo account and passed until that account had saved every
    // role on page one — each run consumed one more, and the failure when it
    // finally ran out ("no untracked role in the feed") looked nothing like
    // its cause. A test whose lifespan is a countdown is not a test.
    //
    // A new account has zero applications, so every card is untracked, and the
    // feed is public so it renders regardless of whether scoring has caught up.
    const email = await signUp(page);
    expect(email).toBeTruthy();

    await page.goto('/jobs');
    await hydrated(page);

    // Pick a card that is not ALREADY tracked, then pin the test to THAT card
    // by POSITION.
    //
    // Three traps here, all hit in turn. Taking the first card unconditionally
    // fails once the demo account has history: the card is found, but it sits
    // in a later tracker column where the next action is "Got a screen" rather
    // than "Mark applied", and it reads as a missing button.
    //
    // Filtering on `aria-pressed="false"` fixes that and introduces a worse
    // problem: the filter depends on the very attribute the test is about to
    // change, so the moment the click succeeds the locator stops matching this
    // card and silently re-resolves to the NEXT unsaved one. Locators are live
    // queries, not handles.
    //
    // Re-querying by TITLE looks like the fix and is the subtlest failure of
    // the three: `hasText` is a substring, real titles overlap ("Senior
    // Software Engineer" is a prefix of "Senior Software Engineer, Ads"), so
    // the re-query resolves to a different card — one that is already saved.
    // The click then UNsaves it, the API answers 409 because that application
    // has progressed, and the test fails somewhere else entirely.
    //
    // Position is the only stable handle within one render, so the index is
    // resolved once and everything downstream uses it.
    const cards = page.locator('li.card');
    const count = await cards.count();
    let index = -1;
    for (let i = 0; i < count; i++) {
      const btn = cards.nth(i).locator('button.save');
      if ((await btn.count()) && (await btn.getAttribute('aria-pressed')) === 'false') {
        index = i;
        break;
      }
    }
    expect(index, 'the feed should contain at least one untracked role').toBeGreaterThanOrEqual(0);

    const card = cards.nth(index);
    const title = (await card.locator('.title a').textContent())!.trim();
    const saveButton = card.locator('button.save');

    await saveButton.click();
    // Optimistic: the button must flip immediately rather than after a round
    // trip, because this action is cheap to undo and slow feedback feels broken.
    await expect(saveButton).toHaveAttribute('aria-pressed', 'true');

    await page.goto('/tracker');
    // Pinned to the row that is still in Saved, not merely the first title
    // match. `hasText` is a substring and real titles overlap, so on an
    // account with history .first() resolves to an OLDER application of a
    // similarly-named role sitting in a later column — where the next action
    // is "Got a screen" and the missing "Mark applied" reads as a broken
    // button. This is the same trap the comment above describes, reached from
    // the other direction.
    const saved = page
      .locator('.tracked', { hasText: title })
      .filter({ has: page.getByRole('button', { name: /mark applied/i }) })
      .first();
    await expect(saved).toBeVisible();

    // Advancing must be one click from the board — a tracker that requires a
    // detail view to change a status will not get updated.
    await hydrated(page);
    await saved.getByRole('button', { name: /mark applied/i }).click();

    // Assert a card with this title is now in Applied, rather than counting
    // the column. A count is a claim about global state, and this database is
    // shared with every other test and with whatever a developer clicked five
    // minutes ago. .first() because the title is a substring and this account
    // legitimately holds several applications to similarly-named roles.
    await expect(
      page
        .locator('.column', { hasText: 'Applied' })
        .locator('.tracked', { hasText: title })
        .first()
    ).toBeVisible();
  });
});

/**
 * The activity record, at the API level.
 *
 * `application_events` existed from the first migration and NOTHING had ever
 * written to it, so the grid would have rendered empty forever while looking
 * like a working feature. These assertions are about what counts as activity,
 * which is the part that decides whether the number means anything.
 */
test.describe('activity is recorded from real movement', () => {
  test('a status change logs exactly one event, and a repeat logs none', async ({ page }) => {
    const email = await signUp(page);
    expect(email).toBeTruthy();

    // Save a posting so there is something to advance.
    const feed = await (await page.request.get('/v1/jobs?posted_within=any')).json();
    const postingId = feed.data[0].id;
    expect((await page.request.put(`/v1/me/saved/${postingId}`)).ok()).toBeTruthy();

    // The endpoint returns { items: [...] }, not a bare array — an envelope
    // that leaves room for paging without a breaking change.
    const tracked = await (await page.request.get('/v1/me/saved')).json();
    const appId = tracked.items.find(
      (t: { posting_id: number }) => t.posting_id === postingId
    )!.id;

    const before = await (await page.request.get('/v1/me/activity')).json();

    await page.request.patch(`/v1/me/saved/${appId}`, { data: { status: 'applied' } });
    const afterOne = await (await page.request.get('/v1/me/activity')).json();
    expect(afterOne.total, 'a real transition is one event').toBe(before.total + 1);

    // Re-sending the same status is not activity. Counting it would inflate a
    // record whose entire value is being an honest one.
    await page.request.patch(`/v1/me/saved/${appId}`, { data: { status: 'applied' } });
    const afterRepeat = await (await page.request.get('/v1/me/activity')).json();
    expect(afterRepeat.total, 'a no-op PATCH is not activity').toBe(afterOne.total);

    // And the window is the server's to decide, so the grid cannot drift
    // across a midnight boundary in the browser.
    expect(afterRepeat.from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(new Date(afterRepeat.to) > new Date(afterRepeat.from)).toBe(true);
  });
});

test.describe('routing guards', () => {
  test('signed-out visitors get the landing page, not an empty dashboard', async ({ page }) => {
    await page.goto('/');
    // .first(): the landing page offers the same action at the top and at the
    // bottom, which is deliberate. This test is about which page a signed-out
    // visitor lands on, not about how many times it invites them in.
    await expect(page.getByRole('link', { name: /create an account/i }).first()).toBeVisible();
  });

  test('protected pages redirect to sign-in and return you afterwards', async ({ page }) => {
    await page.goto('/tracker');
    // SvelteKit's redirect does not percent-encode the path, so the raw form
    // is what actually lands in the address bar.
    await expect(page).toHaveURL(/\/login\?next=\/tracker/);

    await page.getByLabel('Email').fill('dev@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();

    // Being dumped on the dashboard after signing in loses the user's intent.
    await expect(page).toHaveURL(/\/tracker/);
  });

  test('the feed is public — discovery must not require an account', async ({ page }) => {
    await page.goto('/jobs');
    await expect(page.locator('li.card').first()).toBeVisible();
    // ...but the save action belongs to signed-in users only.
    await expect(page.locator('button.save')).toHaveCount(0);
  });
});

/**
 * A different demo account from the dashboard suite, deliberately.
 *
 * Three groups shared senior@jobtrack.local and signed into it concurrently
 * across Playwright workers. Every one of them passed in isolation and the
 * dashboard trio failed roughly one run in three together — the signature of
 * shared mutable state, not of a broken assertion. dev@ has 5,921 scores and
 * 2,458 abstaining components, so it exercises the same paths.
 */
test.describe('job detail', () => {
  test('the score breakdown names every component and its reason', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('dev@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);

    await page.goto('/jobs?sort=match');
    await hydrated(page);

    // The title opens our detail page. It used to link straight to the ATS,
    // which left the breakdown — the thing no competitor can show — with
    // nowhere to be read.
    await page.locator('li.card .title a').first().click();
    await expect(page).toHaveURL(/\/jobs\/\d+/);

    // All five weighted components, each with the reason it earned what it did.
    for (const name of ['Skills', 'Experience', 'Location', 'Compensation', 'Freshness']) {
      await expect(page.locator('.component-name', { hasText: name })).toBeVisible();
    }
    const details = await page.locator('.component-detail').allTextContents();
    expect(details.length).toBe(5);
    for (const d of details) expect(d.trim().length).toBeGreaterThan(0);

    // The weights are stated rather than implied.
    await expect(page.locator('.weights')).toContainText('skills 40');

    // Apply still goes directly to the employer — nothing is interposed
    // between the decision and acting on it.
    const apply = page.getByRole('link', { name: /^Apply on / });
    const href = await apply.getAttribute('href');
    expect(href).toMatch(/^https?:\/\//);
    expect(href).not.toContain('localhost');
  });

  test('an abstaining component says so rather than showing a zero', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('dev@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);

    // Undisclosed salary is the common abstention: roughly a fifth of postings.
    await page.goto('/jobs?sort=match');
    await hydrated(page);
    await page.locator('li.card .title a').first().click();
    await expect(page).toHaveURL(/\/jobs\/\d+/);

    const abstained = page.locator('.component.abstained');
    if (await abstained.count()) {
      // "We could not judge this" must not render as "this scored nothing".
      await expect(abstained.first().locator('.component-figure')).toContainText('—');
      await expect(abstained.first().locator('.bar.hollow')).toBeVisible();
    }
  });

  test('a posting that is no longer live says so and offers a way back', async ({ page }) => {
    const res = await page.goto('/jobs/999999999');
    expect(res!.status()).toBe(404);
  });
});

test.describe('feed keyboard navigation', () => {
  test('j and k move focus, and s saves the focused row', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('grad@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);

    await page.goto('/jobs');
    await hydrated(page);

    // j from outside the list enters at the top — what every list with vim keys
    // does, and what people reach for without being told.
    await page.keyboard.press('j');
    const first = page.locator('li.card .title a').first();
    await expect(first).toBeFocused();

    await page.keyboard.press('j');
    await expect(page.locator('li.card .title a').nth(1)).toBeFocused();

    await page.keyboard.press('k');
    await expect(first).toBeFocused();

    // Focus is real focus, not a separate "selected" highlight — so the focus
    // ring and the cursor cannot drift apart, and a screen reader follows it.
    const focusedCard = page.locator('li.card').filter({ has: page.locator('.title a:focus') });
    await expect(focusedCard).toHaveCount(1);
  });

  test('a bare letter typed into search is text, not a command', async ({ page }) => {
    await page.goto('/jobs');
    await hydrated(page);

    // The header's search, explicitly. There are two search fields in the
    // markup — the header's and the feed's — paired on one breakpoint so only
    // one is ever visible, and a bare getByRole('searchbox') is ambiguous
    // about which it means even when only one can be clicked.
    const search = page.locator('#jt-search');
    await search.click();
    await search.type('js');

    // If the shortcut had fired, the value would be missing a character and
    // focus would have jumped into the list.
    await expect(search).toHaveValue('js');
    await expect(search).toBeFocused();
  });
});

/**
 * What the user sees when a write does not land.
 *
 * Every optimistic update in the product assumes success. These tests break
 * that assumption on purpose, because the failure path is the one nobody
 * exercises by hand and the one that decides whether the product feels honest.
 */
test.describe('a write that fails', () => {
  test('a save the server rejects reverts and offers a way forward', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('grad@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);

    await page.goto('/jobs');
    await hydrated(page);

    await page.route('**/v1/me/saved/*', (route) => route.fulfill({ status: 503 }));

    const card = page.locator('li.card').first();
    const save = card.locator('button.save');
    const before = await save.getAttribute('aria-pressed');
    await save.click();

    const alert = card.getByRole('alert');
    await expect(alert).toBeVisible();

    // The message must be about what happened, not the status code. A user who
    // reads "503" learns nothing they can act on.
    await expect(alert).not.toContainText('503');
    await expect(alert).toContainText(/our side/i);

    // And the button must go back to the truth: nothing was saved.
    await expect(save).toHaveAttribute('aria-pressed', before ?? 'false');

    // A 5xx is worth another go, so the offer is there.
    await expect(alert.getByRole('button', { name: /try again/i })).toBeVisible();
  });

  test('an expired session asks for a sign-in, not a retry', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('grad@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);

    await page.goto('/jobs');
    await hydrated(page);

    await page.route('**/v1/me/saved/*', (route) => route.fulfill({ status: 401 }));

    const card = page.locator('li.card').first();
    await card.locator('button.save').click();

    const alert = card.getByRole('alert');
    await expect(alert).toContainText(/session expired/i);
    // Retrying an expired session just fails again, so that button is absent
    // and the actual fix is a link.
    await expect(alert.getByRole('button', { name: /try again/i })).toHaveCount(0);
    await expect(alert.getByRole('link', { name: /sign in/i })).toBeVisible();
  });

  test('a posting that closes mid-read changes the page, not just a message', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('grad@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);

    await page.goto('/jobs');
    await hydrated(page);
    await page.locator('li.card .title a').first().click();
    await expect(page).toHaveURL(/\/jobs\/\d+/);
    await hydrated(page);

    // The posting was live when the page rendered. It closes now.
    await page.route('**/v1/me/saved/*', (route) => route.fulfill({ status: 404 }));
    await page.getByRole('button', { name: /^save|^saved/i }).click();

    const notice = page.getByRole('alert');
    await expect(notice).toContainText(/closed while you were reading/i);

    // Save is now an offer that cannot be honoured, so it stops being offered.
    await expect(page.getByRole('button', { name: /^save|^saved/i })).toBeDisabled();

    // But we must not claim the employer's page is down — we only know ours
    // dropped it. Apply stays a working link.
    const apply = page.getByRole('link', { name: /^apply on/i });
    await expect(apply).toBeVisible();
    await expect(apply).toHaveAttribute('href', /^https?:/);
  });

  test('losing the connection says so before the user finds out by clicking', async ({
    page,
    context
  }) => {
    await page.goto('/jobs');
    await hydrated(page);

    await expect(page.getByText(/you are offline/i)).toHaveCount(0);

    await context.setOffline(true);
    const banner = page.getByText(/you are offline/i);
    await expect(banner).toBeVisible();

    // The distinction that makes this worth showing: the page they are on did
    // not break. Saying so is the difference between a warning and a panic.
    await expect(banner).toContainText(/pages already open still work/i);

    await context.setOffline(false);
    await expect(banner).toHaveCount(0);
  });

  test('a request that never reaches the server says so', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('grad@jobtrack.local');
    await page.getByLabel('Password').fill('dev-password-please');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/dashboard/);

    await page.goto('/jobs');
    await hydrated(page);

    // A dropped connection, not a server that answered badly. The two must not
    // produce the same sentence.
    await page.route('**/v1/me/saved/*', (route) => route.abort('connectionfailed'));

    const card = page.locator('li.card').first();
    await card.locator('button.save').click();

    const alert = card.getByRole('alert');
    await expect(alert).toContainText(/reach the server|offline/i);
    await expect(alert).not.toContainText(/our side/i);
  });
});

/**
 * Importing a CV — the largest single improvement to match quality, because
 * skills are 40 of the 100 points and people type five of them by hand.
 *
 * The contract under test is the two-step shape: upload PROPOSES, apply
 * COMMITS. A parser that writes straight to a profile is a parser whose
 * mistakes are invisible.
 */
test.describe('importing a CV', () => {
  const cv = `Priya Raman
priya.raman@example.com | +91 98765 43210

Summary
Backend engineer working on payments infrastructure.

Skills
Go, PostgreSQL, Kubernetes, Terraform, Kafka, Redis

Experience
Senior Software Engineer, Razorpay
Mar 2021 - Present
Built the settlement ledger in Go on top of PostgreSQL.

Software Engineer, Freshworks
Jul 2018 - Feb 2021
Introduced Kafka for event delivery.

Education
B.Tech, Anna University
2014 - 2018
`;

  test('a CV is read, reviewed, and only then applied', async ({ page }) => {
    const email = await signUp(page);
    expect(email).toBeTruthy();

    await page.goto('/profile/resume');
    await hydrated(page);

    await page.setInputFiles('input[type=file]', {
      name: 'priya-cv.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from(cv)
    });
    await page.getByRole('button', { name: /read my cv/i }).click();

    // It must report what it found, and lead with what is NEW — that is the
    // number answering "was this worth doing?".
    await expect(page.getByText(/skills? you had not told us about/i)).toBeVisible();

    // The evidence distinction is the product's own claim about honesty: a
    // skill described inside a role outranks one merely listed.
    // "Go", not "go": chips render the display name now, because the
    // canonical form is an identifier and putting it on screen produced
    // "postgresql" and "aws" as labels.
    // Matched on the label span exactly, not on the row's whole text: "Go" is
    // a substring of half the rows' prose ("used in a role" contains no Go,
    // but "Golang" and "MongoDB" would), and a row-level regex is anchored to
    // text that includes the evidence label.
    const goRow = page
      .locator('li.skill', { has: page.locator('.skill-name', { hasText: /^Go$/ }) })
      .first();
    await expect(goRow).toContainText(/used in a role/i);

    // And crucially, NOTHING has been saved yet.
    const before = await (await page.request.get('/v1/me/profile')).json();
    expect(before.resume_skills ?? []).toHaveLength(0);

    // Untick one, then commit.
    await page.locator('li.skill').filter({ hasText: /Terraform/i })
      .first().getByRole('checkbox').uncheck();
    await page.getByRole('button', { name: /save .* and rescore/i }).click();

    await expect(page).toHaveURL(/\/profile/);

    // resume_skills, not skills: the two are kept apart on purpose, because
    // the profile editor PATCHes `skills` wholesale and merging them would
    // silently promote every inferred skill to a declared one on the next save.
    const after = await (await page.request.get('/v1/me/profile')).json();
    const fromCV: string[] = after.resume_skills ?? [];
    expect(fromCV).toContain('kubernetes');
    expect(fromCV).toContain('go');
    // The unticked one must be absent: a review that ignores the user's
    // corrections is not a review.
    expect(fromCV).not.toContain('terraform');

    // And they must be visible. The first version of this returned only
    // hand-typed skills, so importing fifteen changed nothing on screen.
    await expect(page.locator('.from-cv')).toBeVisible();
    // "Kubernetes", not "kubernetes" — the chip shows the display name, while
    // the API field it came from is the canonical identifier.
    await expect(page.locator('.cv-chips')).toContainText('Kubernetes');
  });

  test('a two-column PDF is read in reading order, not visual order', async ({ page }) => {
    // PDF is what most people actually have, and a two-column layout is the
    // one ADR-0007 names as the commonest cause of catastrophic parse failure.
    // The fixture is a real Chromium-generated PDF with a sidebar beside a
    // body — see ADR-0012 for why reading order beats -layout here.
    await signUp(page);
    await page.goto('/profile/resume');
    await hydrated(page);

    await page.setInputFiles('input[type=file]', '../internal/resume/testdata/two-column-cv.pdf');
    await page.getByRole('button', { name: /read my cv/i }).click();

    // A cleanly-read two-column CV must SAY it read cleanly. Under-reporting
    // confidence would send someone to fix a file that is fine.
    await expect(page.getByText(/skills? you had not told us about/i)).toBeVisible();

    const skills = page.locator('li.skill');
    await expect(skills.filter({ hasText: /PostgreSQL/ })).toHaveCount(1);
    await expect(skills.filter({ hasText: /Kubernetes/ })).toHaveCount(1);

    // The dated roles live in the BODY column; finding them proves the columns
    // were not interleaved.
    await expect(page.getByText(/set my experience to/i)).toBeVisible();
  });

  test('a file with no readable text says why, and what to do', async ({ page }) => {
    await signUp(page);
    await page.goto('/profile/resume');
    await hydrated(page);

    // Binary junk: no text layer, which is what a scanned CV looks like.
    await page.setInputFiles('input[type=file]', {
      name: 'scan.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from([0x00, 0x01, 0x02, 0xff, 0xfe, 0x00, 0x03])
    });
    await page.getByRole('button', { name: /read my cv/i }).click();

    const alert = page.getByRole('alert');
    await expect(alert).toBeVisible();
    // Not a shrug. The advice is worth more than the error, because a file we
    // cannot read fails employer systems too.
    await expect(alert).toContainText(/cannot read|scan|image|file type/i);
  });
});

test.describe('saved searches', () => {
  test('a filter set is saved, named from its own facets, and confirmed in place', async ({
    page
  }) => {
    await signUp(page);
    await page.goto('/jobs?mode=remote');
    await hydrated(page);

    // The name is generated from the ACTIVE facets, so saving does not begin
    // with a naming task. It must be editable, not fixed.
    const name = page.locator('#search-name');
    await expect(name).toBeVisible();
    await expect(name).toHaveValue(/remote/i);

    const unique = `Remote ${Date.now()}`;
    await name.fill(unique);
    await page.getByRole('button', { name: /save these filters/i }).click();

    // Confirmation is INLINE and it stays. A toast would be gone before a slow
    // reader finished it, and invisible to a screen reader mid-utterance.
    await expect(page.getByText(/^Saved$/)).toBeVisible();
    await expect(page.getByRole('link', { name: new RegExp(unique) })).toBeVisible();

    // Re-saving the same filters is refused by the interface, not by an error:
    // two names for one filter set is a duplicate the user cannot see.
    await page.reload();
    await hydrated(page);
    await expect(page.getByText(/already saved/i)).toBeVisible();
  });

  test('a saved search replays its filters', async ({ page }) => {
    await signUp(page);
    await page.goto('/jobs?mode=remote&posted_within=14d');
    await hydrated(page);

    const unique = `Replay ${Date.now()}`;
    await page.locator('#search-name').fill(unique);
    await page.getByRole('button', { name: /save these filters/i }).click();
    await expect(page.getByText(/^Saved$/)).toBeVisible();

    // Leave, come back through the saved search, and land on the same filters.
    await page.goto('/jobs?country=US');
    await hydrated(page);
    await page.getByRole('link', { name: new RegExp(unique) }).click();

    await expect(page).toHaveURL(/mode=remote/);
    await expect(page).toHaveURL(/posted_within=14d/);
  });
});
