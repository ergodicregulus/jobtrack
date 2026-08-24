import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

/**
 * Contrast is computed, never eyeballed.
 *
 * This test exists because the same defect appeared three times: amber failed
 * AA while carrying the word "estimated", `--fg-subtle` failed while carrying
 * every timestamp and vendor attribution, and a teal score chip survived a
 * review that was explicitly looking for contrast problems. Every one of them
 * looked fine.
 *
 * A colour that carries text is a correctness concern with a number attached,
 * so it gets a test rather than a review. The pairings below are a CONTRACT:
 * adding a new text colour means adding a row here, which is the point — it
 * forces the question "what does this sit on?" at the moment the colour is
 * introduced rather than months later.
 */

const srcDir = fileURLToPath(new URL('..', import.meta.url));
const css = readFileSync(join(srcDir, 'app.css'), 'utf8');

/**
 * Every stylesheet in the app, concatenated.
 *
 * Tokens are declared in app.css but consumed from Svelte components too, so a
 * usage check that reads only app.css reports live tokens as dead — which is
 * exactly what the first version of this test did.
 */
function allSource(): string {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir)) {
      const full = join(dir, entry);
      if (statSync(full).isDirectory()) walk(full);
      else if (/\.(css|svelte|ts)$/.test(entry) && !entry.endsWith('.test.ts')) {
        out.push(readFileSync(full, 'utf8'));
      }
    }
  };
  walk(srcDir);
  return out.join('\n');
}

/** WCAG 2.2 — 1.4.3 Contrast (Minimum) for text below 18.66px / not bold. */
const AA_TEXT = 4.5;
/** WCAG 2.2 — 1.4.11 Non-text Contrast, for icons and meaningful boundaries. */
const AA_NON_TEXT = 3;

type Theme = 'light' | 'dark';

/**
 * Reads a token out of the relevant block.
 *
 * The dark palette is declared twice — once under `prefers-color-scheme` and
 * once under `[data-theme='dark']` — because the toggle has to beat the OS in
 * both directions. Both copies must agree, and `readsIdenticalInBothDarkBlocks`
 * below is what proves it: a palette that drifts between them produces a theme
 * that changes depending on how you arrived at it.
 */
function token(name: string, theme: Theme): string {
  const block =
    theme === 'light'
      ? css.slice(css.indexOf(':root {'), css.indexOf('@media (prefers-color-scheme: dark)'))
      : css.slice(css.indexOf(":root[data-theme='dark']"));

  const match = block.match(new RegExp(`${name}:\\s*([^;]+);`));
  if (!match) throw new Error(`token ${name} not found in the ${theme} palette`);
  return match[1].trim();
}

function relativeLuminance(hex: string): number {
  const h = hex.replace('#', '');
  const channel = (v: number) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

/**
 * Flattens a translucent token over the surface it is painted on.
 *
 * The chip backgrounds are `rgb(r g b / a)` rather than opaque hexes, because
 * one alpha tint reads correctly on the page ground, on a card and inside a
 * sunken panel, where three hand-picked hexes would drift apart. But a
 * contrast ratio is only defined between two OPAQUE colours: measuring the
 * ink against the tint as if it were solid answers a question nobody is
 * looking at. So the tint is composited first, which is what the eye actually
 * receives.
 */
function flatten(colour: string, over: string): string {
  const m = colour.match(
    /^rgb\(\s*(\d+)\s+(\d+)\s+(\d+)\s*\/\s*([\d.]+)\s*\)$/
  );
  if (!m) return colour;

  const [, r, g, b, a] = m;
  const alpha = Number(a);
  const base = over.replace('#', '');
  const mix = (fgChannel: number, i: number) => {
    const bgChannel = parseInt(base.slice(i, i + 2), 16);
    return Math.round(fgChannel * alpha + bgChannel * (1 - alpha));
  };
  const out = [Number(r), Number(g), Number(b)]
    .map((v, i) => mix(v, i * 2).toString(16).padStart(2, '0'))
    .join('');
  return `#${out}`;
}

function contrast(fg: string, bg: string): number {
  const [a, b] = [relativeLuminance(fg), relativeLuminance(bg)];
  const [hi, lo] = a > b ? [a, b] : [b, a];
  return (hi + 0.05) / (lo + 0.05);
}

/**
 * Every pairing where one token is rendered as TEXT over another.
 *
 * `where` is not decoration — it is what lets whoever breaks this test find the
 * thing on screen without hunting for it.
 */
/**
 * `under` names the OPAQUE surface a translucent pairing is painted on.
 *
 * A chip tinted `rgb(13 148 136 / 0.10)` is a different colour on a card than
 * on the page ground, so the pairing has to say which. Defaults to
 * `--bg-raised`, since a chip almost always sits on a card.
 */
const TEXT_PAIRINGS: { fg: string; bg: string; where: string; under?: string }[] = [
  { fg: '--fg', bg: '--bg', where: 'body copy on the page ground' },
  { fg: '--fg', bg: '--bg-raised', where: 'body copy on a card' },
  { fg: '--fg-muted', bg: '--bg-raised', where: 'card metadata, help text' },
  { fg: '--fg-muted', bg: '--bg-sunken', where: 'plain chips, sunken panels' },
  { fg: '--fg-subtle', bg: '--bg-raised', where: 'timestamps, "via Greenhouse", micro labels' },
  { fg: '--fg-subtle', bg: '--bg', where: 'footer, eyebrow labels' },
  { fg: '--accent', bg: '--bg-raised', where: 'links' },
  { fg: '--accent-ink', bg: '--accent-bg', where: 'accent chips, the avatar' },
  { fg: '--grow-ink', bg: '--grow-bg', where: 'matched-skill chip, strong score chip' },
  { fg: '--grow-ink', bg: '--bg-raised', where: 'fresh timestamps' },
  { fg: '--shrink-ink', bg: '--shrink-bg', where: 'missing-skill chip' },
  { fg: '--shrink-ink', bg: '--bg-raised', where: 'error text, the gaps line' },
  { fg: '--uncertain-ink', bg: '--uncertain-bg', where: 'estimate and gone-quiet chips' },
  { fg: '--uncertain-ink', bg: '--bg-raised', where: 'ageing timestamps' }
];

/**
 * Tokens that may only be used for non-text, and therefore only need 3:1.
 *
 * `--fg-faint` is the whole reason this list exists. It was carrying
 * placeholders and facet counts at 2.06:1 — both of which are text, and the
 * facet count is the thing that stops someone filtering blindly into zero
 * results. It is now restricted to the search icon and the "·" separator.
 */
const NON_TEXT_PAIRINGS: { fg: string; bg: string; where: string }[] = [
  { fg: '--fg-faint', bg: '--bg-raised', where: 'search icon, the "·" meta separator' }
];

const THEMES: Theme[] = ['light', 'dark'];

describe('colour tokens meet WCAG 2.2 AA', () => {
  for (const theme of THEMES) {
    describe(theme, () => {
      for (const { fg, bg, where, under } of TEXT_PAIRINGS) {
        it(`${fg} on ${bg} — ${where}`, () => {
          const surface = token(under ?? '--bg-raised', theme);
          const solidBg = flatten(token(bg, theme), surface);
          const solidFg = flatten(token(fg, theme), solidBg);
          const ratio = contrast(solidFg, solidBg);
          expect(
            ratio,
            `${fg} (${solidFg}) on ${bg} (${solidBg}) is ` +
              `${ratio.toFixed(2)}:1 and needs ${AA_TEXT}:1. ` +
              `Darken the ink token rather than lightening the ground — the ` +
              `ground is shared and moving it changes every other pairing.`
          ).toBeGreaterThanOrEqual(AA_TEXT);
        });
      }

      for (const { fg, bg, where } of NON_TEXT_PAIRINGS) {
        it(`${fg} on ${bg} (non-text) — ${where}`, () => {
          const solidBg = flatten(token(bg, theme), token('--bg-raised', theme));
          const ratio = contrast(flatten(token(fg, theme), solidBg), solidBg);
          expect(ratio).toBeGreaterThanOrEqual(AA_NON_TEXT);
        });
      }
    });
  }
});

describe('the palette is internally consistent', () => {
  // The dark palette is written twice so the toggle beats the OS in both
  // directions. If the copies drift, the theme depends on how you arrived at
  // it, which is close to impossible to diagnose from a bug report.
  it('reads identically in both dark blocks', () => {
    const mediaBlock = css.slice(
      css.indexOf('@media (prefers-color-scheme: dark)'),
      css.indexOf(":root[data-theme='dark']")
    );
    const explicitBlock = css.slice(css.indexOf(":root[data-theme='dark']"));

    const colourTokens = [
      ...new Set([...TEXT_PAIRINGS, ...NON_TEXT_PAIRINGS].flatMap((p) => [p.fg, p.bg]))
    ];

    for (const name of colourTokens) {
      const inMedia = mediaBlock.match(new RegExp(`${name}:\\s*([^;]+);`))?.[1].trim();
      const inExplicit = explicitBlock.match(new RegExp(`${name}:\\s*([^;]+);`))?.[1].trim();
      expect(
        inMedia,
        `${name} differs between the prefers-color-scheme block and the ` +
          `[data-theme="dark"] block — the theme would depend on how the user got there`
      ).toBe(inExplicit);
    }
  });

  // --fg-faint clears 3:1, not 4.5:1, so using it for text is a WCAG failure
  // that the pairing table above cannot see — the table checks the tokens we
  // declared, not where they were actually applied. The tracker's empty-column
  // placeholder was doing exactly this.
  it('never uses the non-text token for text', () => {
    const source = allSource();
    const offenders: string[] = [];

    for (const line of source.split('\n')) {
      if (!line.includes('color: var(--fg-faint)')) continue;
      // Legitimate non-text uses opt in explicitly with a `/* non-text */`
      // marker. Inferring it from the selector name was tried first and was
      // wrong in both directions: it missed a multi-line rule whose selector
      // was on an earlier line, and it would have silently blessed anything
      // that happened to contain the word "icon".
      if (line.includes('/* non-text */')) continue;
      offenders.push(line.trim());
    }

    expect(
      offenders,
      `--fg-faint only clears 3:1 and may not carry text. Use --fg-subtle:\n` +
        offenders.map((o) => `  ${o}`).join('\n')
    ).toEqual([]);
  });

  // A token nobody references is a decision nobody made. This catches the
  // leftovers of a refactor, which is how palettes quietly double in size.
  it('defines no colour token that nothing uses', () => {
    const declared = [...css.matchAll(/^\s*(--(?:fg|bg|grow|shrink|uncertain|accent)[a-z-]*):/gm)]
      .map((m) => m[1]);

    const source = allSource();
    const unused = [...new Set(declared)].filter(
      (name) => source.split(`var(${name})`).length - 1 === 0
    );

    expect(
      unused,
      `declared but referenced nowhere in web/src: ${unused.join(', ')}`
    ).toEqual([]);
  });
});
