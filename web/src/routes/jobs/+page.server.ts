import { redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import type { FeedPage, Facets, SavedSearch, Preferences } from '$lib/types';

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
    'comp_disclosed_only', 'posted_within', 'skills', 'vendor', 'field', 'sort', 'cursor'
  ];
  for (const key of allowed) {
    const values = url.searchParams.getAll(key);
    if (values.length) params.set(key, values.join(','));
  }

  // Saved searches are fetched before the feed, because the redirect below
  // depends on them. That costs one serial round trip for a signed-in reader
  // and saves fetching a feed that is about to be replaced.
  //
  // Preferences ride along in the same round trip rather than after it. They
  // are needed before the feed request is built — they decide its sort and page
  // size — so fetching them serially would add a second full round trip to the
  // most-requested page in the product. In parallel they add none.
  let searches: SavedSearch[] = [];
  let prefs: Preferences | null = null;
  if (signedIn) {
    const [searchRes, prefRes] = await Promise.all([
      fetch('/v1/me/searches'),
      fetch('/v1/me/preferences')
    ]);
    if (searchRes.ok) searches = (await searchRes.json()).items ?? [];
    if (prefRes.ok) prefs = await prefRes.json();
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

  // The URL wins, then the user's stored default, then the product's opinion.
  //
  // That order is the whole design. A stored preference must never override a
  // sort the user just clicked — it is a default, not a policy — and the
  // product's own guess must never override a preference the user set
  // deliberately.
  //
  // The fallback stays: a signed-in user with a finished profile gets their
  // best matches first, because sorting a personalised feed by date buries the
  // scoring work behind whatever happened to be posted most recently.
  if (!params.has('sort')) {
    if (prefs?.sort) params.set('sort', prefs.sort);
    else if (signedIn && profile?.onboarded) params.set('sort', 'match');
  }

  // Page size has no URL control today, so this is the only thing that sets it.
  // Sent only when it differs from the API's own default, so a reader who never
  // touched the setting produces the same request they always did.
  if (prefs?.per_page && prefs.per_page !== 25) params.set('limit', String(prefs.per_page));

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
      canMatch: Boolean(signedIn && profile?.onboarded),
      searches
    };
  }

  const feed: FeedPage = await feedRes.json();
  const facets: Facets | null = facetsRes.ok ? await facetsRes.json() : null;

  return {
    feed, facets, error: null, params: params.toString(), signedIn, searches,
    // Best match needs something to match against.
    canMatch: Boolean(signedIn && profile?.onboarded)
  };
};
