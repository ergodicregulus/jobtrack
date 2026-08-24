import { error } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import type { Posting } from '$lib/types';

export const load: PageServerLoad = async ({ params, fetch, setHeaders, parent }) => {
  const { signedIn } = await parent();

  const res = await fetch(`/v1/jobs/${params.id}`);

  if (res.status === 404) {
    // A posting that closed while someone was reading the feed. Saying so is
    // truer than showing a role nobody can apply to, and the page below turns
    // this into a route back rather than a dead end.
    error(404, 'This posting is no longer live.');
  }
  if (!res.ok) {
    error(res.status, 'We could not load this posting.');
  }

  setHeaders({ 'cache-control': 'private, no-store' });

  const posting: Posting = await res.json();
  return { posting, signedIn };
};
