import { redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import type { IngestSeries, Coverage } from '$lib/types';

/**
 * The root is a decision, not a page, for anyone with an account.
 *
 * A signed-in user landing on a marketing page has to click again to reach the
 * thing they came for. Visitors get the landing page; everyone else is sent
 * where they were going.
 */
export const load: PageServerLoad = async ({ parent, fetch, url }) => {
  const { signedIn, profile } = await parent();
  if (signedIn) redirect(303, profile?.onboarded ? '/dashboard' : '/onboarding');

  // Real figures, not claims. "6,804 live postings" is checkable; "thousands
  // of opportunities" is the sentence every job board writes.
  //
  // This used to read the facets endpoint and derive `companies` from
  // `Object.keys(facets.countries).length` — a count of COUNTRIES presented
  // under a company label. It happened not to be rendered, which is the only
  // reason it was never a lie on screen. /v1/market shares its SQL with the
  // dashboard, so the landing page and the signed-in view cannot disagree.
  // Three independent reads, fetched together. They were sequential, which cost
  // two extra round trips on the most-served page in the product for no reason
  // — none of them depends on another.
  //
  // Each failure is absorbed on its own: the landing page must render whatever
  // happens, and a page that 500s because a widget could not load has its
  // priorities backwards. A figure we could not read is simply not shown, never
  // shown as zero.
  const [statsRes, ingestRes, coverageRes] = await Promise.allSettled([
    fetch('/v1/market'),
    fetch('/v1/market/ingest?days=30'),
    fetch('/v1/market/coverage')
  ]);

  async function unwrap<T>(r: PromiseSettledResult<Response>, fallback: T): Promise<T> {
    if (r.status !== 'fulfilled' || !r.value.ok) return fallback;
    try {
      return (await r.value.json()) as T;
    } catch {
      return fallback;
    }
  }

  // Real figures, not claims. "14,036 live postings" is checkable; "thousands of
  // opportunities" is the sentence every job board writes. /v1/market shares its
  // SQL with the dashboard, so the landing page and the signed-in view cannot
  // disagree.
  const stats = await unwrap(statsRes, {
    live_postings: 0,
    companies: 0,
    added_this_week: 0,
    remote_share: 0
  });

  // Both widgets read the source_daily rollup (ADR-0017), so they cost the same
  // whether the corpus holds twelve thousand postings or twelve million.
  const ingest = await unwrap<IngestSeries | null>(ingestRes, null);
  const coverage = await unwrap<Coverage | null>(coverageRes, null);

  // Which dimension the absence field opens on. In the URL rather than in
  // component state so a link to a particular reading is shareable, and so the
  // control works with JavaScript off.
  const shows = url.searchParams.get('shows') ?? 'pay';

  return { stats, ingest, coverage, shows };
};
