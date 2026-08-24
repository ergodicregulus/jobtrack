import { redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import type { Activity, Dashboard } from '$lib/types';

export const load: PageServerLoad = async ({ parent, fetch, setHeaders, url }) => {
  const { signedIn, profile } = await parent();
  if (!signedIn) redirect(303, '/login?next=/dashboard');

  // An unfinished profile means every widget would be empty. Sending the user
  // back to onboarding is more useful than showing them a hollow dashboard and
  // leaving them to work out why.
  if (!profile?.onboarded) redirect(303, '/onboarding');

  setHeaders({ 'cache-control': 'private, no-store' });

  // Both in parallel: they are independent, and serialising them would put a
  // second round trip in front of first paint for no reason.
  const [res, activityRes] = await Promise.all([
    fetch('/v1/me/dashboard'),
    fetch('/v1/me/activity')
  ]);

  if (!res.ok) {
    return {
      dashboard: null,
      activity: null,
      error: `Your dashboard is unavailable (${res.status}).`,
      welcome: false
    };
  }

  // The activity grid is the one widget that may fail on its own without
  // taking the page with it — it is context, not the reason anyone opened
  // this page. Null renders as "not available", never as an empty grid, which
  // would read as "you have done nothing".
  const activity: Activity | null = activityRes.ok ? await activityRes.json() : null;

  const dashboard: Dashboard = await res.json();
  return {
    dashboard,
    activity,
    error: null,
    welcome: url.searchParams.get('welcome') === '1'
  };
};
