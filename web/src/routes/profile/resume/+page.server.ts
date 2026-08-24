import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { forwardCookies, postJSON, problemMessage } from '$lib/server/api';

/**
 * Upload proposes; apply commits.
 *
 * Two actions rather than one because the review step in between is the whole
 * point. A parser that writes straight to a profile is a parser whose mistakes
 * are invisible — and a silently wrong skill list corrupts every score
 * afterwards while looking like it worked.
 *
 * The parse result is returned to the page rather than stashed in a session:
 * it is already stored server-side against the resume row, and re-reading it on
 * apply means the "skills you confirmed" list is validated against what we
 * actually found rather than against whatever the browser posts back.
 */
export const load: PageServerLoad = async ({ parent }) => {
  const { signedIn, profile } = await parent();
  if (!signedIn) redirect(303, '/login?next=/profile/resume');
  return { profile };
};

export const actions: Actions = {
  upload: async ({ request, fetch, cookies }) => {
    const form = await request.formData();
    const file = form.get('file');

    if (!(file instanceof File) || file.size === 0) {
      return fail(400, { message: 'Choose a file to upload.' });
    }
    // Checked here as well as server-side so the user is told before waiting
    // for a 5 MB upload to complete and be rejected.
    if (file.size > 5 * 1024 * 1024) {
      return fail(400, {
        message:
          'That file is larger than 5 MB. A CV that size is usually an image ' +
          'export, which applicant tracking systems cannot read either.'
      });
    }

    const label = String(form.get('label') ?? '').trim() || file.name;

    const res = await fetch(`/v1/me/resume?label=${encodeURIComponent(label)}`, {
      method: 'POST',
      headers: { 'content-type': 'application/octet-stream' },
      body: await file.arrayBuffer()
    });
    forwardCookies(res, cookies);

    if (!res.ok) {
      return fail(res.status === 413 ? 413 : 422, {
        message: await problemMessage(res, 'We could not read that file.')
      });
    }

    // Returned, not redirected: a redirect would lose the parse result, and
    // re-fetching it would be a second round trip for data we are holding.
    return { parsed: await res.json() };
  },

  apply: async ({ request, fetch, cookies }) => {
    const form = await request.formData();
    const id = String(form.get('id') ?? '');
    if (!id) return fail(400, { message: 'Missing resume.' });

    // getAll, so an unchecked box simply is not here. The server accepts only
    // skills the parse proposed, so this list cannot smuggle anything in.
    const skills = form.getAll('skill').map(String);

    const res = await postJSON(fetch, cookies, `/v1/me/resume/${id}/apply`, {
      skills,
      set_years: form.get('set_years') === 'on'
    });

    if (!res.ok) {
      return fail(422, {
        message: await problemMessage(res, 'We could not save those skills.')
      });
    }

    // Straight to the profile, where the user can see what landed. Ending on
    // a success banner with no visible change is how people lose trust in a
    // save button.
    redirect(303, '/profile?resume=applied');
  }
};
