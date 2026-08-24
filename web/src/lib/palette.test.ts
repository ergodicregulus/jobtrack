import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// @ts-expect-error — plain ESM, no types, and one source of truth is the point.
import { PALETTE } from '../../scripts/palette.mjs';

/**
 * The shipped palette must be the one the ramp produces.
 *
 * app.css carries hex because tokens.test.ts computes WCAG ratios from sRGB and
 * because hex has no browser-support question. The cost of that choice is that
 * a hex is hand-editable, and a hand-edited hex silently leaves the ramp — at
 * which point "the lightness step is the contrast guarantee" stops being true
 * and the contrast suite goes back to being a discovery mechanism rather than a
 * regression check.
 *
 * So: change scripts/palette.mjs, run `node scripts/palette.mjs`, paste. Never
 * type a colour into app.css.
 */
const css = readFileSync(fileURLToPath(new URL('../app.css', import.meta.url)), 'utf8');

function block(theme: 'light' | 'dark'): string {
  return theme === 'light'
    ? css.slice(css.indexOf(':root {'), css.indexOf('@media (prefers-color-scheme: dark)'))
    : css.slice(css.indexOf(":root[data-theme='dark']"));
}

describe('app.css matches the OKLCH ramp', () => {
  for (const theme of ['light', 'dark'] as const) {
    describe(theme, () => {
      const source = block(theme);
      for (const [name, expected] of Object.entries(PALETTE[theme] as Record<string, string>)) {
        it(`${name} is ${expected}`, () => {
          const found = source.match(new RegExp(`${name}:\\s*([^;]+);`));
          expect(found, `${name} missing from the ${theme} block`).not.toBeNull();
          expect(found![1].trim().toLowerCase()).toBe(expected.toLowerCase());
        });
      }
    });
  }
});

describe('the accent is not the default', () => {
  /**
   * Tailwind's indigo-600. Named here so the thing being avoided is stated
   * rather than implied: it is the most-used accent on the web, and half of
   * what made this interface read as machine-generated.
   */
  const INDIGO_600 = '#4f46e5';

  it('is nowhere in the stylesheet', () => {
    expect(css.toLowerCase()).not.toContain(INDIGO_600);
  });

  it('is far enough from indigo-600 to read as a different colour', () => {
    const rgb = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
    const [ar, ag, ab] = rgb(PALETTE.light['--accent']);
    const [br, bg, bb] = rgb(INDIGO_600);
    const distance = Math.hypot(ar - br, ag - bg, ab - bb);
    // Well beyond "a slightly different indigo".
    expect(distance).toBeGreaterThan(100);
  });
});
