import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { patchJSON, problemMessage } from '$lib/server/api';
import type { Theme } from '$lib/types';

export const load: PageServerLoad = async ({ parent, fetch }) => {
  const { signedIn, theme } = await parent();
  if (!signedIn) redirect(303, '/login?next=/settings');

  // Whether the digest is on is a CONSENT, not a preference, so it is read from
  // the consent log rather than the settings blob. One account of what this
  // person agreed to, in the place the DPDP export reads from.
  let digest = false;
  try {
    const res = await fetch('/v1/me/consents');
    if (res.ok) {
      const body = await res.json();
      digest = (body.items ?? []).some(
        (c: { purpose: string; withdrawn_at: string | null }) =>
          c.purpose === 'digest_email' && !c.withdrawn_at
      );
    }
  } catch {
    // Settings must render regardless; an unread consent shows as off, which is
    // the safe direction — it never claims someone opted in when we do not know.
  }

  return { theme, digest };
};

const VALID_THEMES = new Set(['system', 'light', 'dark']);

export const actions: Actions = {
  /**
   * Turns the weekly digest on or off.
   *
   * POST grants, DELETE withdraws — the API deliberately has no "set" verb,
   * because a consent log records ACTS, and "granted then withdrawn then granted
   * again" is a true history rather than a duplicate to be collapsed.
   *
   * A plain form action, so it works with JavaScript off like every other
   * control here; use:enhance only removes the navigation.
   */
  digest: async ({ request, fetch }) => {
    const on = String((await request.formData()).get('on')) === 'true';
    const res = await fetch('/v1/me/consents?purpose=digest_email', {
      method: on ? 'POST' : 'DELETE'
    });
    if (!res.ok) {
      return fail(res.status, {
        error: await problemMessage(res, 'Could not change your email setting.')
      });
    }
    return { ok: true };
  },

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
