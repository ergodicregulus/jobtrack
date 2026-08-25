import { redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import type { FeedPage, Facets, SavedSearch } from '$lib/types';

/**
 * The feed is loaded on the server.
 *
 * Filtering, sorting and pagination all happen in Postgres; the browser
 * receives ~25 already-decided results and renders them. Shipping a query
 * engine to the client is what blows the interaction budget on a low-end
 * machine, and it is the single decision this page exists to avoid.
 */
export const load: PageServerLoad = async ({ url, fetch, setHeaders, parent }) => {
  const { signedIn, profile } = await parent();
  const params = new URLSearchParams();

  // Only forward parameters the API accepts. It rejects unknown ones with a
  // 400 — a typo'd filter that silently returns unfiltered results is the worst
  // failure here, because the user believes they are looking at a filtered list.
  const allowed = [
    'q', 'mode', 'country', 'yoe', 'yoe_stretch', 'comp_min',
    'comp_disclosed_only', 'posted_within', 'skills', 'vendor', 'sort', 'cursor'
  ];
  for (const key of allowed) {
    const values = url.searchParams.getAll(key);
    if (values.length) params.set(key, values.join(','));
  }

  // Saved searches are fetched before the feed, because the redirect below
  // depends on them. That costs one serial round trip for a signed-in reader
  // and saves fetching a feed that is about to be replaced.
  let searches: SavedSearch[] = [];
  if (signedIn) {
    const res = await fetch('/v1/me/searches');
    if (res.ok) searches = (await res.json()).items ?? [];
  }

  // A default saved search is applied ONLY to a bare /jobs.
  //
  // Any query parameter at all means the user has expressed an intent, and
  // silently replacing it with a stored one would make the back button lie and
  // a shared link resolve differently for its author. This is the
  // "self-sufficient environment" phase-5 §6.5 asks for: open the app, see
  // your feed — but never at the cost of the URL meaning what it says.
  const bare = [...url.searchParams.keys()].length === 0;
  if (bare && signedIn) {
    const def = searches.find((s) => s.is_default);
    if (def?.query) {
      redirect(303, `/jobs?${def.query}`);
    }
  }

  // A signed-in user with a finished profile gets their best matches first.
  // Sorting a personalised feed by date would bury the scoring work behind
  // whatever happened to be posted most recently.
  if (!params.has('sort') && signedIn && profile?.onboarded) params.set('sort', 'match');

  // Default to the last 7 days. "Any time" would bury the fresh postings this
  // product exists to surface, so the default encodes the product's view and
  // the control lets the user override it.
  if (!params.has('posted_within')) params.set('posted_within', '7d');

  // Personalised and freshness-critical: never cached at the edge.
  setHeaders({ 'cache-control': 'private, no-store' });

  const [feedRes, facetsRes] = await Promise.all([
    fetch(`/v1/jobs?${params}`),
    fetch('/v1/jobs/facets')
  ]);

  if (!feedRes.ok) {
    // Degrade visibly. A page that silently shows zero results is
    // indistinguishable from "there are no jobs", which is a lie.
    return {
      // Annotated rather than `satisfies`: the literal would narrow the type
      // to its own shape, and every optional field FeedPage gains later would
      // then be missing from this branch only.
      feed: { data: [], has_more: false } as FeedPage,
      facets: null,
      error: `The job feed is unavailable (${feedRes.status}).`,
      params: params.toString(),
      signedIn,
      searches
    };
  }

  const feed: FeedPage = await feedRes.json();
  const facets: Facets | null = facetsRes.ok ? await facetsRes.json() : null;

  return { feed, facets, error: null, params: params.toString(), signedIn, searches };
};
