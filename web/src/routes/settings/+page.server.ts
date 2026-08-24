import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { patchJSON, problemMessage } from '$lib/server/api';
import type { Theme } from '$lib/types';

export const load: PageServerLoad = async ({ parent }) => {
  const { signedIn, theme } = await parent();
  if (!signedIn) redirect(303, '/login?next=/settings');
  return { theme };
};

const VALID_THEMES = new Set(['system', 'light', 'dark']);

export const actions: Actions = {
  /**
   * Saves the theme to the account AND mirrors it into a cookie.
   *
   * Both are needed and they do different jobs. The account is the source of
   * truth and follows the user to another machine; the cookie is the only thing
   * the server can read at render time, which is what prevents a flash of the
   * wrong colours before hydration.
   */
  appearance: async ({ request, fetch, cookies }) => {
    const form = await request.formData();
    const theme = String(form.get('theme') ?? 'system');

    if (!VALID_THEMES.has(theme)) {
      return fail(400, { error: 'Unknown theme.' });
    }

    const res = await patchJSON(fetch, cookies, '/v1/me/preferences', { theme });
    if (!res.ok) {
      return fail(res.status, {
        error: await problemMessage(res, 'We could not save that.')
      });
    }

    // Not HttpOnly: the client flips it optimistically so the change is
    // instant rather than arriving a round trip later. It carries no secret —
    // the worst anyone can do by setting it is choose their own colour scheme.
    cookies.set('jt_theme', theme, {
      path: '/',
      httpOnly: false,
      secure: false,
      sameSite: 'lax',
      maxAge: 365 * 24 * 60 * 60
    });

    return { saved: true, theme: theme as Theme };
  }
};
