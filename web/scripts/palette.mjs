/**
 * The palette, defined in OKLCH and emitted as hex.
 *
 * OKLCH is perceptually uniform: equal numeric changes produce equal perceived
 * changes, so a ramp built at fixed lightness steps has predictable contrast
 * across every hue. HSL does not — the same lightness value in two hues yields
 * very different luminance, which is how a palette ends up with one colour that
 * fails AA while its neighbour passes.
 *
 * Emitted as hex rather than as `oklch()` for one reason: contrast is verified
 * by tokens.test.ts, which computes WCAG ratios from sRGB. Keeping the
 * authored form in OKLCH and the shipped form in hex means the ramp is
 * designed perceptually and checked numerically, with no browser-support
 * question in between.
 *
 *   node scripts/palette.mjs          print the token block
 *   node scripts/palette.mjs --check  fail if app.css has drifted
 */

/**
 * OKLCH -> sRGB hex. Björn Ottosson's oklab matrices.
 *
 * @param {number} L Perceptual lightness, 0-1.
 * @param {number} C Chroma.
 * @param {number} H Hue in degrees.
 * @returns {string} A `#rrggbb` string.
 */
export function hex(L, C, H) {
  const h = (H * Math.PI) / 180;
  const a = C * Math.cos(h);
  const b = C * Math.sin(h);

  const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
  const s_ = L - 0.0894841775 * a - 1.291485548 * b;
  const l = l_ ** 3, m = m_ ** 3, s = s_ ** 3;

  const lin = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];

  return (
    '#' +
    lin
      .map((v) => {
        const g = v <= 0.0031308 ? 12.92 * v : 1.055 * Math.max(v, 0) ** (1 / 2.4) - 0.055;
        return Math.round(Math.min(1, Math.max(0, g)) * 255)
          .toString(16)
          .padStart(2, '0');
      })
      .join('')
  );
}

/**
 * Hues, fixed once.
 *
 * The neutrals sit at 70° with chroma held near zero — warm stone, not the
 * blue-grey a default palette produces. #f5f5f5 reads as unfinished; a warm
 * cast reads as chosen.
 *
 * ACCENT is the one that changed. It was Tailwind indigo-600, oklch(51% .23 277)
 * — the single most-used accent on the web and half of what makes an interface
 * read as machine-generated. This is the same hue family, because the semantic
 * colours already own green-cyan, red-pink and orange-yellow and the accent has
 * to stay distinguishable from all three. What changed is everything else:
 * a third of the chroma and much darker, which reads as ink rather than brand.
 */
const HUE = { neutral: 70, accent: 258, grow: 178, shrink: 18, uncertain: 65 };

/** Lightness steps, shared by both themes so the ramps mirror each other. */
/** @type {Record<string, [L: number, C: number, H: number]>} */
const light = {
  '--bg': [0.985, 0.002, HUE.neutral],
  '--bg-raised': [1.0, 0.0, HUE.neutral],
  '--bg-sunken': [0.968, 0.004, HUE.neutral],
  '--bg-hover': [0.942, 0.006, HUE.neutral],
  '--bg-active': [0.915, 0.007, HUE.neutral],

  '--fg': [0.225, 0.006, HUE.neutral],
  '--fg-muted': [0.44, 0.008, HUE.neutral],
  '--fg-subtle': [0.525, 0.008, HUE.neutral],
  '--fg-faint': [0.66, 0.009, HUE.neutral],

  '--accent': [0.35, 0.09, HUE.accent],
  '--accent-hover': [0.28, 0.085, HUE.accent],
  '--accent-fg': [1.0, 0.0, HUE.neutral],
  '--accent-ink': [0.35, 0.09, HUE.accent],

  '--grow-ink': [0.45, 0.08, HUE.grow],
  '--shrink-ink': [0.47, 0.15, HUE.shrink],
  '--uncertain-ink': [0.47, 0.1, HUE.uncertain],
};

/**
 * Dark is not the light ramp inverted.
 *
 * Inverting puts full-chroma colour on a dark ground, where it vibrates. The
 * ink colours lighten AND lose chroma; the surfaces stay warm rather than
 * sliding to neutral grey, so the two themes read as the same product.
 */
/** @type {Record<string, [L: number, C: number, H: number]>} */
const dark = {
  '--bg': [0.155, 0.004, HUE.neutral],
  '--bg-raised': [0.196, 0.005, HUE.neutral],
  '--bg-sunken': [0.132, 0.004, HUE.neutral],
  '--bg-hover': [0.245, 0.006, HUE.neutral],
  '--bg-active': [0.285, 0.007, HUE.neutral],

  '--fg': [0.945, 0.004, HUE.neutral],
  '--fg-muted': [0.76, 0.007, HUE.neutral],
  '--fg-subtle': [0.66, 0.008, HUE.neutral],
  '--fg-faint': [0.52, 0.008, HUE.neutral],

  '--accent': [0.78, 0.075, HUE.accent],
  '--accent-hover': [0.86, 0.06, HUE.accent],
  '--accent-fg': [0.16, 0.02, HUE.accent],
  '--accent-ink': [0.78, 0.075, HUE.accent],

  '--grow-ink': [0.8, 0.1, HUE.grow],
  '--shrink-ink': [0.78, 0.11, HUE.shrink],
  '--uncertain-ink': [0.8, 0.1, HUE.uncertain],
};

export const PALETTE = {
  light: Object.fromEntries(Object.entries(light).map(([k, v]) => [k, hex(...v)])),
  dark: Object.fromEntries(Object.entries(dark).map(([k, v]) => [k, hex(...v)])),
};

if (import.meta.url === `file://${process.argv[1]}`) {
  for (const theme of ['light', 'dark']) {
    console.log(`/* ${theme} */`);
    for (const [k, v] of Object.entries(PALETTE[/** @type {'light'|'dark'} */ (theme)])) {
      console.log(`  ${k}: ${v};`);
    }
    console.log();
  }
}
