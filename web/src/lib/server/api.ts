import type { Cookies } from '@sveltejs/kit';
import type { ProblemDetail } from '$lib/types';

/**
 * Helpers for talking to the API from SvelteKit server code.
 *
 * These exist so that error handling is identical on every route. The failure
 * mode they prevent is specific: a form action that forgets to forward the
 * Set-Cookie header authenticates the user in the database and then loses the
 * session, which presents to the user as "my password is wrong".
 */

type Fetch = typeof globalThis.fetch;

/** Reads a problem+json body, falling back to something sayable. */
export async function problemMessage(res: Response, fallback: string): Promise<string> {
  try {
    const problem: ProblemDetail = await res.json();
    return problem.detail || problem.title || fallback;
  } catch {
    return fallback;
  }
}

export async function problemFields(res: Response): Promise<Record<string, string>> {
  try {
    const problem: ProblemDetail = await res.json();
    const out: Record<string, string> = {};
    for (const f of problem.fields ?? []) out[f.field] = f.message;
    return out;
  } catch {
    return {};
  }
}

/**
 * Copies session cookies from an API response onto the browser response.
 *
 * SvelteKit does not forward Set-Cookie from `event.fetch` automatically — it
 * cannot, since it has no way to know which of them are meant for the browser.
 * Parsing is deliberately minimal: the API sets the attributes it wants, and
 * re-deriving them here would be a second source of truth for cookie security
 * that could drift from the first.
 */
export function forwardCookies(res: Response, cookies: Cookies): void {
  const raw = res.headers.getSetCookie?.() ?? [];
  for (const line of raw) {
    const [pair, ...attrs] = line.split(';');
    const eq = pair.indexOf('=');
    if (eq < 0) continue;

    const name = pair.slice(0, eq).trim();
    const value = pair.slice(eq + 1).trim();

    // `secure` MUST be set explicitly, and defaults to false here.
    //
    // SvelteKit's cookies.set() defaults `secure` to true for every host except
    // localhost. Leaving it unset therefore ADDED Secure to a cookie the API
    // deliberately sent without it, and a browser on plain HTTP silently drops
    // a Secure cookie. The symptom was a sign-in that returned 200, set a
    // cookie, and left the user signed out — on any non-localhost HTTP origin,
    // which is exactly what the E2E suite uses (http://web:5173) and what a
    // staging box behind a TLS-terminating proxy looks like.
    //
    // The API computes this from its own CookieSecure configuration and is the
    // authority on it. Mirroring what it actually sent is the only correct
    // behaviour; re-deriving it here would be a second source of truth for
    // cookie security that can disagree with the first.
    const opts: Parameters<Cookies['set']>[2] = { path: '/', secure: false };
    for (const attr of attrs) {
      const [k, v] = attr.split('=');
      const key = k.trim().toLowerCase();
      if (key === 'path') opts.path = v?.trim() || '/';
      else if (key === 'max-age') opts.maxAge = Number(v);
      else if (key === 'httponly') opts.httpOnly = true;
      else if (key === 'secure') opts.secure = true;
      else if (key === 'samesite') {
        const s = v?.trim().toLowerCase();
        opts.sameSite = s === 'strict' ? 'strict' : s === 'none' ? 'none' : 'lax';
      }
    }

    // An expiring cookie arrives as max-age=0. Deleting rather than setting it
    // keeps sign-out working on browsers that ignore a zero max-age.
    if (opts.maxAge === 0) cookies.delete(name, { path: opts.path ?? '/' });
    else cookies.set(name, value, opts);
  }
}

/** POSTs JSON to the API and forwards any session cookies it sets. */
export async function postJSON(
  fetch: Fetch,
  cookies: Cookies,
  path: string,
  body: unknown
): Promise<Response> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body)
  });
  forwardCookies(res, cookies);
  return res;
}

export async function patchJSON(
  fetch: Fetch,
  cookies: Cookies,
  path: string,
  body: unknown
): Promise<Response> {
  const res = await fetch(path, {
    method: 'PATCH',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body)
  });
  forwardCookies(res, cookies);
  return res;
}
