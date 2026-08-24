import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import type { SkillSuggestion } from '$lib/types';
import { patchJSON, postJSON, problemFields, problemMessage } from '$lib/server/api';

/**
 * Onboarding is server-rendered and posts one step at a time.
 *
 * Each step PATCHes the profile, so progress survives a closed tab, a dead
 * battery or a refresh. The alternative — accumulating answers in client state
 * and submitting once at the end — loses everything at the exact moment a user
 * is least willing to start again.
 *
 * The step lives in the URL (?step=2) rather than in component state, so Back
 * works, the page is linkable, and a reload does not restart the wizard.
 */
export const load: PageServerLoad = async ({ parent, url, fetch }) => {
  const { signedIn, profile } = await parent();
  if (!signedIn) redirect(303, '/login?next=/onboarding');

  const step = clampStep(Number(url.searchParams.get('step') ?? '1'));

  // Suggestions come from the server so the vocabulary is the single source of
  // their spelling. They were hardcoded here AND on the profile page, in
  // canonical (lower-case) form, which rendered "postgresql" and "aws" as
  // chips and had already drifted between the two copies.
  let suggestions: SkillSuggestion[] = [];
  try {
    const res = await fetch('/v1/skills/common');
    if (res.ok) suggestions = await res.json();
  } catch {
    // The picker still accepts free text, so an empty suggestion list costs a
    // shortcut rather than the feature.
  }

  return { step, profile, suggestions };
};

const TOTAL_STEPS = 4;

function clampStep(n: number): number {
  if (!Number.isFinite(n)) return 1;
  return Math.min(TOTAL_STEPS, Math.max(1, Math.trunc(n)));
}

export const actions: Actions = {
  /** Saves one step and advances. */
  save: async ({ request, fetch, cookies }) => {
    const form = await request.formData();
    const step = clampStep(Number(form.get('step') ?? '1'));

    const patch = buildPatch(step, form);
    const res = await patchJSON(fetch, cookies, '/v1/me/profile', patch);

    if (!res.ok) {
      return fail(res.status, {
        step,
        error: await problemMessage(res, 'We could not save that.'),
        fields: await problemFields(res)
      });
    }

    if (step < TOTAL_STEPS) redirect(303, `/onboarding?step=${step + 1}`);

    // Final step: completing is a separate call because it applies the
    // "is this profile actually usable" check that per-step saves skip.
    const done = await postJSON(fetch, cookies, '/v1/me/onboarding/complete', {});
    if (!done.ok) {
      return fail(done.status, {
        step,
        error: await problemMessage(done, 'Something is still missing.'),
        fields: await problemFields(done)
      });
    }
    redirect(303, '/dashboard?welcome=1');
  },

  /**
   * Skips the rest and finishes with whatever is on file.
   *
   * Offered from step 2 onward, once name and skills exist. A wizard with no
   * exit is a wizard people abandon by closing the tab, which costs the account
   * rather than the answers.
   */
  finish: async ({ fetch, cookies }) => {
    const res = await postJSON(fetch, cookies, '/v1/me/onboarding/complete', {});
    if (!res.ok) {
      return fail(res.status, {
        step: 1,
        error: await problemMessage(res, 'A little more is needed first.'),
        fields: await problemFields(res)
      });
    }
    redirect(303, '/dashboard?welcome=1');
  }
};

function buildPatch(step: number, form: FormData): Record<string, unknown> {
  const str = (k: string) => String(form.get(k) ?? '').trim();
  const list = (k: string) => form.getAll(k).map(String).filter(Boolean);

  switch (step) {
    case 1:
      return {
        first_name: str('first_name'),
        last_name: str('last_name')
      };
    case 2:
      return {
        current_title: str('current_title'),
        target_title: str('target_title'),
        total_yoe: Number(str('total_yoe') || '0')
      };
    case 3:
      // Skills arrive as a comma-separated string from the tag input, which
      // degrades to a plain text field without JavaScript.
      return { skills: splitSkills(str('skills')) };
    case 4:
      return {
        pref_countries: list('pref_countries'),
        pref_modes: list('pref_modes'),
        pref_comp_min: Number(str('pref_comp_min') || '0'),
        pref_currency: str('pref_currency')
      };
    default:
      return {};
  }
}

function splitSkills(raw: string): string[] {
  return raw
    .split(',')
    .map((s) => s.trim().toLowerCase())
    .filter(Boolean)
    .slice(0, 60);
}
