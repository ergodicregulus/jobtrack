import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import type { SkillSuggestion } from '$lib/types';
import { patchJSON, problemFields, problemMessage } from '$lib/server/api';

export const load: PageServerLoad = async ({ parent, fetch }) => {
  const { signedIn, profile } = await parent();
  if (!signedIn) redirect(303, '/login?next=/profile');

  // Suggestions come from the server so the vocabulary is the single source of
  // their spelling. They were hardcoded here AND in onboarding, in canonical
  // (lower-case) form, which rendered "postgresql" and "aws" as chips.
  let suggestions: SkillSuggestion[] = [];
  try {
    const res = await fetch('/v1/skills/common');
    if (res.ok) suggestions = await res.json();
  } catch {
    // Free text still works, so this costs a shortcut, not the feature.
  }

  return { profile, suggestions };
};

export const actions: Actions = {
  default: async ({ request, fetch, cookies }) => {
    const form = await request.formData();
    const str = (k: string) => String(form.get(k) ?? '').trim();

    const patch = {
      first_name: str('first_name'),
      last_name: str('last_name'),
      current_title: str('current_title'),
      target_title: str('target_title'),
      total_yoe: Number(str('total_yoe') || '0'),
      skills: str('skills').split(',').map((s) => s.trim().toLowerCase()).filter(Boolean),
      pref_countries: form.getAll('pref_countries').map(String),
      pref_modes: form.getAll('pref_modes').map(String),
      pref_comp_min: Number(str('pref_comp_min') || '0'),
      pref_currency: str('pref_currency')
    };

    const res = await patchJSON(fetch, cookies, '/v1/me/profile', patch);
    if (!res.ok) {
      return fail(res.status, {
        error: await problemMessage(res, 'We could not save your changes.'),
        fields: await problemFields(res)
      });
    }

    // Saving re-scores every posting for this user in the background, so the
    // message says so — otherwise the feed appears to change on its own a
    // moment later and that reads as a bug.
    return { saved: true };
  }
};
