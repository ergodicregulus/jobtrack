import { env } from '$env/dynamic/private';
import type { Handle, HandleFetch } from '@sveltejs/kit';

/**
 * The theme is stamped into the HTML shell before it is sent.
 *
 * The alternative — resolving on the client — cannot avoid a flash, because the
 * page has already painted by the time any script runs. A cookie is the only
 * signal available to the server at render time, so the API mirrors the stored
 * preference into one on every write.
 *
 * Three states, not two:
 *
 *   data-theme="dark"   explicit choice   -> :root[data-theme="dark"] wins
 *   data-theme="light"  explicit choice   -> :root[data-theme="light"] wins
 *   data-theme=""       follow the system -> prefers-color-scheme decides
 *
 * Resolving "system" server-side would be wrong: we would have to guess, and a
 * guess pins the user to whatever we assumed.
 */

import type { Theme } from '$lib/types';

// Typed as tuples so the guards below narrow to the union rather than to
// `string`. A Set<string> would validate correctly and still lose the type.
const VALID_THEMES = ['light', 'dark'] as const;

const isTheme = (v: string): v is 'light' | 'dark' =>
  (VALID_THEMES as readonly string[]).includes(v);

export const handle: Handle = async ({ event, resolve }) => {
  const rawTheme = event.cookies.get('jt_theme') ?? 'system';

  // Validate rather than trust. The cookie is not HttpOnly — the client toggle
  // writes it for an instant flip — so its value is attacker-controllable.
  // Unvalidated, it would be injected straight into an HTML attribute.
  const theme = isTheme(rawTheme) ? rawTheme : '';

  const resolved: Theme = theme === '' ? 'system' : theme;
  event.locals.theme = resolved;

  return resolve(event, {
    transformPageChunk: ({ html }) =>
      html.replace('%jt.theme%', theme)
  });
};

/**
 * Forward the originating client's address on server-side fetches.
 *
 * SSR calls the API on behalf of every visitor, so from the API's point of view
 * every request arrives from ONE address — this container. Without forwarding,
 * all users share a single rate-limit bucket and a busy page 429s everyone else
 * on the site. A load test found exactly that.
 *
 * SvelteKit forwards cookies on same-origin `event.fetch`, but not arbitrary
 * headers, so the client address has to be attached explicitly.
 *
 * The API honours these headers ONLY from peers in its TRUSTED_PROXIES
 * allowlist, so a client cannot spoof its way out of a rate limit by sending
 * the header itself.
 */
export const handleFetch: HandleFetch = async ({ event, request, fetch }) => {
  const url = new URL(request.url);
  if (!url.pathname.startsWith('/v1')) return fetch(request);

  // Straight to the API over the internal network, never back out through the
  // public entry. adapter-node has no /v1 route, so an unrewritten server-side
  // fetch would leave the process, reach the proxy at the public hostname, and
  // come back in — or, with no route for it there either, 404. In development
  // Vite's proxy hid this: it answered /v1 itself.
  //
  // A different origin means SvelteKit stops forwarding the session cookie, so
  // it is copied across explicitly (the documented handleFetch pattern). The
  // response's Set-Cookie is unaffected: lib/server/api.ts already forwards it
  // by hand, without relying on same-origin behaviour.
  if (env.API_URL) {
    request = new Request(new URL(url.pathname + url.search, env.API_URL), request);
    const cookie = event.request.headers.get('cookie');
    if (cookie) request.headers.set('cookie', cookie);
  }

  const clientAddress = clientAddressOf(event);
  if (clientAddress) {
    // Append rather than overwrite: if we sit behind another proxy, the chain
    // must be preserved so the leftmost entry stays the true origin.
    const existing = event.request.headers.get('x-forwarded-for');
    request.headers.set(
      'x-forwarded-for',
      existing ? `${existing}, ${clientAddress}` : clientAddress
    );
  }
  return fetch(request);
};

function clientAddressOf(event: Parameters<HandleFetch>[0]['event']): string | null {
  // getClientAddress throws when the adapter cannot determine it (for example
  // during prerendering), which must not take the page down.
  try {
    return event.getClientAddress();
  } catch {
    return null;
  }
}
