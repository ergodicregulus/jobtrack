/**
 * A rendering audit: the inconsistencies that are MEASURABLE.
 *
 * Screenshots catch what eyes catch. This catches what eyes miss — an element
 * three pixels outside its container, a 22px tap target, a heading level
 * skipped, a page that scrolls sideways on a phone. Every check below is a
 * rule this repo already states somewhere; the value is applying all of them to
 * every page at every width without anyone having to remember to look.
 *
 *   make ui-audit
 */
import { chromium } from '@playwright/test';

const base = process.env.E2E_BASE_URL;
const WIDTHS = [['phone', 390, 844], ['tablet', 834, 1112], ['desk', 1440, 900]];
const PAGES = ['', 'jobs', 'dashboard', 'tracker', 'profile', 'settings', 'profile/resume'];

const findings = [];
const note = (page, width, kind, detail) =>
  findings.push({ page: page || '/', width, kind, detail });

const browser = await chromium.launch();

for (const [wname, w, h] of WIDTHS) {
  const ctx = await browser.newContext({ viewport: { width: w, height: h } });
  const page = await ctx.newPage();

  page.on('pageerror', (e) => note('(any)', wname, 'js-error', String(e).slice(0, 160)));

  await page.goto(`${base}/login`);
  await page.waitForFunction(() => document.documentElement.dataset.hydrated === 'true');
  await page.getByLabel('Email').fill('grad@jobtrack.local');
  await page.getByLabel('Password').fill('dev-password-please');
  await page.getByRole('button', { name: /sign in/i }).click();
  await page.waitForURL(/dashboard/, { timeout: 25000 });

  for (const p of PAGES) {
    await page.goto(`${base}/${p}`);

    // Wait for a signal, not a duration. This was a flat 700ms sleep, which
    // passes on an idle machine and fails under ingest load — it reported
    // "0 h1" on two pages that render one, while curl showed the h1 present.
    // A check that fails for reasons unrelated to what it measures is a check
    // people learn to re-run instead of read.
    await page
      .waitForFunction(() => document.documentElement.dataset.hydrated === 'true', null,
        { timeout: 30000 })
      .catch(() => {});
    // A genuinely missing h1 must still be reported, so this waits and moves on
    // rather than throwing.
    await page.waitForSelector('h1', { state: 'attached', timeout: 15000 }).catch(() => {});

    const result = await page.evaluate(() => {
      const out = { overflow: null, offscreen: [], smallTargets: [], headings: [],
                    unlabelled: [], clipped: [], zeroSize: [], gutter: null };

      // 1. The page itself must never scroll sideways.
      const de = document.documentElement;
      if (de.scrollWidth > de.clientWidth + 1) {
        out.overflow = { scrollWidth: de.scrollWidth, clientWidth: de.clientWidth };
      }

      // offsetParent is null for anything display:none ANYWHERE up the tree,
      // which getComputedStyle on the element alone cannot tell you. Without
      // it the audit reported the hidden half of every responsive pair — the
      // narrow-screen filter sheet on desktop, the desktop search on a phone —
      // as zero-sized controls.
      const vis = (el) => {
        if (el.closest('details:not([open])')) return false; // browser-hidden, not CSS-hidden
        if (el.closest('.sr-only')) return false;
        const s = getComputedStyle(el);
        if (s.display === 'none' || s.visibility === 'hidden' || s.opacity === '0') return false;
        return el.offsetParent !== null || s.position === 'fixed';
      };
      const label = (el) =>
        (el.className && typeof el.className === 'string'
          ? `${el.tagName.toLowerCase()}.${el.className.split(' ').filter(Boolean).slice(0, 2).join('.')}`
          : el.tagName.toLowerCase()) + (el.id ? `#${el.id}` : '');

      // 2. Nothing may sit outside the viewport horizontally. Catches an
      //    element three pixels wide of its container, which reads as a
      //    misalignment long before it reads as a bug.
      for (const el of document.querySelectorAll('body *')) {
        if (!vis(el)) continue;
        const r = el.getBoundingClientRect();
        if (r.width === 0 && r.height === 0) continue;
        // Deliberately-scrolling containers are exempt; their children are
        // SUPPOSED to extend past the edge — that is what the scrollbar is for.
        // Detected by computed style rather than by class name, so a new
        // scroller does not have to be added to a list here to avoid a false
        // report.
        let scroller = false;
        for (let a = el.parentElement; a && a !== document.body; a = a.parentElement) {
          const as = getComputedStyle(a);
          if (as.overflowX === 'auto' || as.overflowX === 'scroll') { scroller = true; break; }
        }
        if (scroller) continue;
        if (r.right > de.clientWidth + 1 || r.left < -1) {
          out.offscreen.push({ el: label(el), left: Math.round(r.left), right: Math.round(r.right) });
        }
      }

      // 3. WCAG 2.2 SC 2.5.8: interactive targets are at least 24x24.
      //
      // Measured on the EFFECTIVE target, not the control. A native checkbox
      // renders at 13px in every browser and cannot be made bigger without
      // losing the platform's own rendering; the accepted pattern is to wrap it
      // in a label, which is itself clickable. So the label is what a finger
      // has to hit, and the label is what SC 2.5.8 is about.
      for (const el of document.querySelectorAll('a[href], button, input, select, [role="button"]')) {
        if (!vis(el)) continue;
        const wrapper =
          (el.tagName === 'INPUT' && (el.type === 'checkbox' || el.type === 'radio'))
            ? el.closest('label')
            : null;
        const r = (wrapper ?? el).getBoundingClientRect();
        if (r.width === 0 || r.height === 0) { out.zeroSize.push(label(el)); continue; }
        // Inline links inside a paragraph are exempt: SC 2.5.8 excludes targets
        // in a sentence, and padding them would break the text.
        const inSentence = el.tagName === 'A' && el.closest('p, li, .meta, .t-small, .t-micro');
        if (inSentence) continue;
        if (r.width < 24 || r.height < 24) {
          out.smallTargets.push({ el: label(el), w: Math.round(r.width), h: Math.round(r.height) });
        }
      }

      // 4. Exactly one h1, and no skipped heading levels.
      //
      // sr-only headings COUNT. A visually-hidden h1 is a real heading in the
      // accessibility tree, which is the only tree heading structure is about
      // — filtering it out made the audit demand a heading it had just been
      // given.
      const headingVisible = (el) => {
        // A closed <details> hides its content through the browser rather than
        // through CSS, and offsetParent stays non-null — so the filter sheet's
        // group labels were being counted while collapsed.
        if (el.closest('details:not([open])')) return false;
        if (el.classList.contains('sr-only')) return true;
        return getComputedStyle(el).display !== 'none' && el.offsetParent !== null;
      };
      const hs = [...document.querySelectorAll('h1,h2,h3,h4,h5,h6')].filter(headingVisible);
      out.headings = hs.map((x) => Number(x.tagName[1]));
      out.headingText = hs.map((x) => `${x.tagName}:${(x.textContent || '').trim().slice(0, 18)}`);

      // 5. Every form control needs an accessible name.
      for (const el of document.querySelectorAll('input:not([type=hidden]), select, textarea')) {
        if (!vis(el)) continue;
        const named =
          el.labels?.length ||
          el.getAttribute('aria-label') ||
          el.getAttribute('aria-labelledby') ||
          el.closest('label');
        if (!named) out.unlabelled.push(label(el));
      }

      // 6. Every page's content must begin at the same x as the wordmark.
      //
      // A gutter that moves between nav destinations reads as two different
      // apps. Settings and Profile drifted to 395px and 330px because a
      // max-width inside the already-centred .shell centred them a second time.
      const brandEl = document.querySelector('.brand');
      const col = document.querySelector('main .page') || document.querySelector('main .shell');
      if (brandEl && col) {
        const pad = parseFloat(getComputedStyle(col).paddingLeft) || 0;
        out.gutter = {
          brand: Math.round(brandEl.getBoundingClientRect().left),
          content: Math.round(col.getBoundingClientRect().left + pad)
        };
      }

      // 7. Text clipped by a fixed height — the silent one, because the words
      //    are simply gone rather than visibly broken.
      for (const el of document.querySelectorAll('body *')) {
        if (!vis(el) || !el.textContent?.trim()) continue;
        // .sr-only is a 1px clipping box on purpose — it is how text reaches a
        // screen reader without reaching the screen.
        if (el.classList.contains('sr-only')) continue;
        const s = getComputedStyle(el);
        if (s.overflow === 'visible' || s.overflowY === 'auto' || s.overflowY === 'scroll') continue;
        // A single-line ellipsis is a deliberate truncation, not a clip.
        if (s.textOverflow === 'ellipsis' || s.whiteSpace === 'nowrap') continue;
        if (el.scrollHeight > el.clientHeight + 2 && el.clientHeight > 0) {
          out.clipped.push({ el: label(el), shown: el.clientHeight, needs: el.scrollHeight });
        }
      }
      return out;
    });

    if (result.overflow) note(p, wname, 'page-scrolls-sideways', JSON.stringify(result.overflow));
    if (result.gutter && Math.abs(result.gutter.content - result.gutter.brand) > 1) {
      note(p, wname, 'content-gutter-differs', JSON.stringify(result.gutter));
    }
    for (const o of result.offscreen.slice(0, 4)) note(p, wname, 'element-outside-viewport', JSON.stringify(o));
    for (const t of result.smallTargets.slice(0, 6)) note(p, wname, 'target-below-24px', JSON.stringify(t));
    for (const z of result.zeroSize.slice(0, 4)) note(p, wname, 'zero-size-control', z);
    for (const u of result.unlabelled.slice(0, 4)) note(p, wname, 'unlabelled-control', u);
    for (const c of result.clipped.slice(0, 4)) note(p, wname, 'text-clipped', JSON.stringify(c));

    const h1s = result.headings.filter((n) => n === 1).length;
    if (h1s !== 1) note(p, wname, 'heading-h1-count', String(h1s));
    for (let i = 1; i < result.headings.length; i++) {
      if (result.headings[i] - result.headings[i - 1] > 1) {
        note(p, wname, 'heading-level-skipped',
          `h${result.headings[i - 1]} -> h${result.headings[i]} | ${result.headingText.slice(0, 6).join(' ')}`);
        break;
      }
    }
  }
  await ctx.close();
}
await browser.close();

if (!findings.length) {
  console.log('✓ ui-audit: no findings');
} else {
  console.log(`✗ ui-audit: ${findings.length} finding(s)\n`);
  const byKind = {};
  for (const f of findings) (byKind[f.kind] ??= []).push(f);
  for (const [kind, list] of Object.entries(byKind)) {
    console.log(`── ${kind} (${list.length})`);
    for (const f of list.slice(0, 10)) console.log(`   [${f.width}] /${f.page}  ${f.detail}`);
    console.log('');
  }
  process.exitCode = 1;
}
