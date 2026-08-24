import { redirect } from '@sveltejs/kit';
import type { Actions } from './$types';
import { postJSON } from '$lib/server/api';

export const actions: Actions = {
  default: async ({ fetch, cookies }) => {
    // Best effort: even if the API call fails, the local cookie is cleared so
    // the browser is signed out. A sign-out button that can fail is worse than
    // one that occasionally leaves a stale server session to expire on its own.
    try {
      await postJSON(fetch, cookies, '/v1/auth/logout', {});
    } catch {
      // fall through
    }
    cookies.delete('jt_session', { path: '/' });
    cookies.delete('__Host-jt_session', { path: '/' });
    redirect(303, '/');
  }
};
