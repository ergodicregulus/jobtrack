import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { postJSON, problemMessage } from '$lib/server/api';

export const load: PageServerLoad = async ({ parent, url }) => {
  const { signedIn, profile } = await parent();
  if (signedIn) {
    redirect(303, profile?.onboarded ? '/dashboard' : '/onboarding');
  }
  // Preserve where the user was headed so sign-in returns them there rather
  // than dumping everyone on the dashboard.
  return { next: safeNext(url.searchParams.get('next')) };
};

export const actions: Actions = {
  default: async ({ request, fetch, cookies }) => {
    const form = await request.formData();
    const email = String(form.get('email') ?? '').trim();
    const password = String(form.get('password') ?? '');
    const next = safeNext(String(form.get('next') ?? ''));

    if (!email || !password) {
      return fail(400, { email, error: 'Enter your email and password.' });
    }

    const res = await postJSON(fetch, cookies, '/v1/auth/login', { email, password });

    if (!res.ok) {
      // The email is echoed back so the user does not retype it; the password
      // never is. Returning the address also means a wrong-password retry does
      // not look like the form forgot who you are.
      return fail(res.status === 401 ? 401 : res.status, {
        email,
        error: await problemMessage(res, 'That email and password did not match.')
      });
    }

    const me = await fetch('/v1/me/profile');
    const onboarded = me.ok ? ((await me.json()).onboarded ?? false) : false;

    redirect(303, onboarded ? next : '/onboarding');
  }
};

/**
 * Only same-origin paths are honoured as a redirect target. Without this check
 * `?next=https://evil.example` turns the sign-in page into an open redirect,
 * which is a credible phishing primitive precisely because the link genuinely
 * starts on our domain.
 */
function safeNext(value: string | null): string {
  if (!value || !value.startsWith('/') || value.startsWith('//')) return '/dashboard';
  return value;
}
