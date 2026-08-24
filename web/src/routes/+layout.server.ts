import type { LayoutServerLoad } from './$types';
import type { Profile } from '$lib/types';

/**
 * Loads the viewer once, for every page.
 *
 * The theme comes from hooks.server.ts, which read it from a cookie before the
 * page rendered. Passing it down means the control starts in the right position
 * rather than flipping once the client hydrates.
 *
 * The profile is fetched here rather than per-page because the shell needs it
 * on every route — the nav shows an avatar, and the onboarding guard needs to
 * know whether the user has finished. Fetching it per-page would mean the same
 * request several times per navigation.
 */
export const load: LayoutServerLoad = async ({ locals, fetch }) => {
  let profile: Profile | null = null;

  try {
    const res = await fetch('/v1/me/profile');
    if (res.ok) profile = await res.json();
  } catch {
    // The API being unreachable must not blank the page. Each page reports its
    // own failure; the shell degrades to signed-out and stays usable.
  }

  return {
    theme: locals.theme,
    signedIn: profile !== null,
    profile
  };
};
