/**
 * Formatting helpers.
 *
 * These live in one place because every one of them encodes a product decision,
 * not just a display choice — and a decision duplicated across components is a
 * decision that will drift.
 */

/** Freshness bands. The whole product is built on the first 24-48 hours. */
export type Freshness = 'fresh' | 'recent' | 'ageing' | 'stale';

export function freshnessOf(postedAt: string | null, firstSeenAt: string): Freshness {
  const when = new Date(postedAt ?? firstSeenAt).getTime();
  const hours = (Date.now() - when) / 36e5;
  if (hours <= 24) return 'fresh';
  if (hours <= 72) return 'recent';
  if (hours <= 14 * 24) return 'ageing';
  return 'stale';
}

/**
 * Relative age, never an absolute date.
 *
 * "4h ago" answers the question the user is actually asking — am I early? — in
 * a way "12 Aug" does not.
 */
export function relativeAge(postedAt: string | null, firstSeenAt: string): string {
  const when = new Date(postedAt ?? firstSeenAt).getTime();
  const mins = Math.max(0, (Date.now() - when) / 6e4);

  if (mins < 60) return `${Math.round(mins)}m ago`;
  const hours = mins / 60;
  if (hours < 24) return `${Math.round(hours)}h ago`;
  const days = hours / 24;
  if (days < 30) return `${Math.round(days)}d ago`;
  return `${Math.round(days / 30)}mo ago`;
}

/**
 * Compensation.
 *
 * `null` means NOT DISCLOSED and must never render as "0" or be omitted
 * silently — the distinction between "below your floor" and "not published" is
 * load-bearing, because only ~80% of postings disclose salary at all.
 */
export function formatComp(
  min: number | null,
  max: number | null,
  currency: string | null,
  period: string | null
): { text: string; disclosed: boolean } {
  if (min == null && max == null) {
    return { text: 'Not disclosed', disclosed: false };
  }

  const cur = currency ?? '';
  const fmt = (v: number) => compactMoney(v, cur);

  let text: string;
  if (min != null && max != null) text = `${fmt(min)}–${fmt(max)}`;
  else if (min != null) text = `${fmt(min)}+`;
  else text = `up to ${fmt(max!)}`;

  // A period we did not recognise gets no suffix, and so reads as annual — so
  // every period the adapters can emit needs a spelling here. `year` is the one
  // deliberate blank: nobody writes "$180k–$220k per year" on a job card.
  const suffix: Record<string, string> = { hour: '/hr', day: '/day', week: '/wk', month: '/mo' };
  if (period && suffix[period]) text += suffix[period];

  return { text, disclosed: true };
}

/** INR reads in lakh/crore; everything else in k/M. Anything else is unreadable. */
function compactMoney(value: number, currency: string): string {
  if (currency === 'INR') {
    if (value >= 1e7) return `₹${trim(value / 1e7)}Cr`;
    if (value >= 1e5) return `₹${trim(value / 1e5)}L`;
    return `₹${Math.round(value).toLocaleString('en-IN')}`;
  }

  const symbol = { USD: '$', EUR: '€', GBP: '£', SGD: 'S$', CAD: 'C$', AUD: 'A$' }[currency] ?? '';
  if (value >= 1e6) return `${symbol}${trim(value / 1e6)}M`;
  if (value >= 1e3) return `${symbol}${trim(value / 1e3)}k`;
  return `${symbol}${Math.round(value)}`;
}

function trim(n: number): string {
  return n % 1 === 0 ? String(n) : n.toFixed(1);
}

/**
 * Years of experience.
 *
 * Below the confidence threshold the band is treated as UNKNOWN rather than
 * shown. Displaying a guess would make the user self-filter on our parse error.
 */
export function formatYoE(
  min: number | null,
  max: number | null,
  confidence: number
): string | null {
  if (confidence < 0.5 || min == null) return null;
  if (max == null) return `${min}+ yrs`;
  if (min === max) return `${min} yrs`;
  return `${min}–${max} yrs`;
}

/** Work mode, with unknown rendered as absence rather than as a claim. */
export function formatMode(mode: string): string | null {
  switch (mode) {
    case 'remote': return 'Remote';
    case 'hybrid': return 'Hybrid';
    case 'onsite': return 'On-site';
    default: return null;
  }
}

/** ATS vendor, shown so the user knows the apply link lands in a real pipeline. */
export function formatVendor(vendor: string): string {
  return {
    greenhouse: 'Greenhouse',
    lever: 'Lever',
    ashby: 'Ashby',
    smartrecruiters: 'SmartRecruiters',
    recruitee: 'Recruitee',
    workable: 'Workable',
    personio: 'Personio',
    jsonld: 'company site'
  }[vendor] ?? vendor;
}

/**
 * Compact figures for dashboard tiles: 6672 -> "6.7k".
 *
 * Rounded on purpose. A dashboard number is read for its magnitude, and an
 * exact 6,672 invites the reader to treat a figure that changes every few
 * minutes as precise.
 */
export function compactNumber(n: number): string {
  if (!Number.isFinite(n)) return '0';
  if (n < 1000) return String(n);
  if (n < 10_000) return `${(n / 1000).toFixed(1).replace(/\.0$/, '')}k`;
  if (n < 1_000_000) return `${Math.round(n / 1000)}k`;
  return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}m`;
}

/**
 * Day-level relative time: "today", "yesterday", "5d ago", then a date.
 *
 * Switches to an absolute date after four weeks because "43d ago" requires the
 * reader to do arithmetic to place it, while a date does not.
 */
export function relativeDay(iso: string | null): string {
  if (!iso) return 'date unknown';
  const then = new Date(iso);
  if (Number.isNaN(then.getTime())) return 'date unknown';

  const days = Math.floor((Date.now() - then.getTime()) / 86_400_000);
  if (days <= 0) return 'today';
  if (days === 1) return 'yesterday';
  if (days < 28) return `${days}d ago`;
  return then.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}
