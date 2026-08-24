import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { postJSON, problemMessage } from '$lib/server/api';

export const load: PageServerLoad = async ({ parent }) => {
  const { signedIn, profile } = await parent();
  if (signedIn) redirect(303, profile?.onboarded ? '/dashboard' : '/onboarding');
  return {};
};

const MIN_PASSWORD = 12;

export const actions: Actions = {
  default: async ({ request, fetch, cookies }) => {
    const form = await request.formData();
    const email = String(form.get('email') ?? '').trim();
    const password = String(form.get('password') ?? '');

    // Checked here as well as in the API. The API is the authority; this copy
    // exists so the user is told before a round trip, and the message matches.
    if (password.length < MIN_PASSWORD) {
      return fail(400, { email, error: `Use at least ${MIN_PASSWORD} characters.` });
    }

    const res = await postJSON(fetch, cookies, '/v1/auth/register', { email, password });
    if (!res.ok) {
      return fail(res.status, {
        email,
        error: await problemMessage(res, 'We could not create that account.')
      });
    }

    // Straight into onboarding. Asking someone to sign in again immediately
    // after signing up is the most common needless drop-off in this flow.
    redirect(303, '/onboarding');
  }
};
