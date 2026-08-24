import { describe, it, expect, vi, afterEach } from 'vitest';
import {
  freshnessOf, relativeAge, formatComp, formatYoE, formatMode, formatVendor
} from './format';

/**
 * Every function here encodes a product decision, so every test asserts a
 * decision rather than a string. The ones that matter most are the refusals:
 * what we decline to display, and why.
 */

const now = new Date('2026-08-15T12:00:00Z');

function at(hoursAgo: number): string {
  return new Date(now.getTime() - hoursAgo * 36e5).toISOString();
}

afterEach(() => vi.useRealTimers());

function freeze() {
  vi.useFakeTimers();
  vi.setSystemTime(now);
}

describe('freshnessOf', () => {
  it('bands by age, with 24h as the boundary that matters', () => {
    freeze();
    // The first 24-48 hours is the entire premise of the product, so that
    // boundary is the one the visual encoding has to get right.
    expect(freshnessOf(at(1), at(1))).toBe('fresh');
    expect(freshnessOf(at(23), at(23))).toBe('fresh');
    expect(freshnessOf(at(25), at(25))).toBe('recent');
    expect(freshnessOf(at(100), at(100))).toBe('ageing');
    expect(freshnessOf(at(24 * 20), at(24 * 20))).toBe('stale');
  });

  it('falls back to first_seen_at when the vendor published no date', () => {
    freeze();
    expect(freshnessOf(null, at(2))).toBe('fresh');
  });
});

describe('relativeAge', () => {
  it('answers "am I early?" rather than "what date was this?"', () => {
    freeze();
    expect(relativeAge(at(0.5), at(0.5))).toBe('30m ago');
    expect(relativeAge(at(4), at(4))).toBe('4h ago');
    expect(relativeAge(at(48), at(48))).toBe('2d ago');
    expect(relativeAge(at(24 * 45), at(24 * 45))).toBe('2mo ago');
  });
});

describe('formatComp', () => {
  it('says "Not disclosed" rather than hiding the row or showing zero', () => {
    // Only ~80% of postings disclose salary. Collapsing "not published" into
    // "below your floor" would silently drop a fifth of the market, and an
    // omitted row reads as "we did not check".
    const got = formatComp(null, null, null, 'year');
    expect(got.disclosed).toBe(false);
    expect(got.text).toBe('Not disclosed');
  });

  it('renders INR in lakh and crore, which is how the market reads it', () => {
    expect(formatComp(2_800_000, 4_200_000, 'INR', 'year').text).toBe('₹28L–₹42L');
    expect(formatComp(12_000_000, 18_000_000, 'INR', 'year').text).toBe('₹1.2Cr–₹1.8Cr');
  });

  it('renders western currencies in k and M', () => {
    expect(formatComp(180_000, 220_000, 'USD', 'year').text).toBe('$180k–$220k');
    expect(formatComp(90_000, 120_000, 'GBP', 'year').text).toBe('£90k–£120k');
  });

  it('handles an open-ended range without inventing an upper bound', () => {
    expect(formatComp(2_800_000, null, 'INR', 'year').text).toBe('₹28L+');
    expect(formatComp(null, 220_000, 'USD', 'year').text).toBe('up to $220k');
  });

  it('marks non-annual periods', () => {
    expect(formatComp(65, 85, 'USD', 'hour').text).toBe('$65–$85/hr');
    // Ashby publishes daily and weekly rates too. Without a suffix these read
    // as annual salaries, which is the same class of defect as the interval
    // that used to default to yearly.
    expect(formatComp(400, 600, 'USD', 'day').text).toBe('$400–$600/day');
    expect(formatComp(2_000, 3_000, 'USD', 'week').text).toBe('$2k–$3k/wk');
  });
});

describe('formatYoE', () => {
  it('returns null below the confidence threshold', () => {
    // A guessed band shown as fact would make the user self-filter on OUR
    // parse error. Unknown must render as absent, never as a number.
    expect(formatYoE(5, 10, 0.3)).toBeNull();
    expect(formatYoE(5, 10, 0.5)).toBe('5–10 yrs');
  });

  it('returns null when there is no band at all', () => {
    expect(formatYoE(null, null, 1)).toBeNull();
  });

  it('renders an open-ended minimum without inventing a ceiling', () => {
    expect(formatYoE(5, null, 0.9)).toBe('5+ yrs');
  });

  it('collapses an equal band', () => {
    expect(formatYoE(3, 3, 0.9)).toBe('3 yrs');
  });
});

describe('formatMode', () => {
  it('renders unknown as absence, not as a claim', () => {
    // Printing "Unknown" on a card would imply we checked and could not tell.
    // Omitting it is the honest rendering of "the posting did not say".
    expect(formatMode('unknown')).toBeNull();
    expect(formatMode('remote')).toBe('Remote');
    expect(formatMode('hybrid')).toBe('Hybrid');
    expect(formatMode('onsite')).toBe('On-site');
  });
});

describe('formatVendor', () => {
  it('names the ATS so the user knows the apply link is real', () => {
    expect(formatVendor('greenhouse')).toBe('Greenhouse');
    expect(formatVendor('ashby')).toBe('Ashby');
    // JSON-LD postings come from the company's own careers page, and saying
    // "jsonld" to a user would be meaningless.
    expect(formatVendor('jsonld')).toBe('company site');
  });

  it('passes through an unknown vendor rather than dropping it', () => {
    expect(formatVendor('newats')).toBe('newats');
  });
});
