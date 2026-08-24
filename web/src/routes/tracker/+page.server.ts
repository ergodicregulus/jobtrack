import { redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import type { TrackedItem } from '$lib/types';

export const load: PageServerLoad = async ({ parent, fetch, setHeaders, url }) => {
  const { signedIn } = await parent();
  if (!signedIn) redirect(303, '/login?next=/tracker');

  setHeaders({ 'cache-control': 'private, no-store' });

  const res = await fetch('/v1/me/saved');
  if (!res.ok) {
    return {
      stage: '',
      items: [] as TrackedItem[],
      error: `Could not load your applications (${res.status}).`
    };
  }

  const body: { items: TrackedItem[] } = await res.json();
  return { stage: url.searchParams.get('stage') ?? '', items: body.items ?? [], error: null };
};
