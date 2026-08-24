import type { ProblemDetail } from '$lib/types';

/**
 * One place where a failed write becomes something a person can act on.
 *
 * Every optimistic update in the product — saving a job, advancing a tracker
 * stage — used to assume success and, when it failed, either revert silently or
 * show the raw status code. "Update failed (500)" tells a user nothing they can
 * do and quite a lot they should not have to read.
 *
 * Three distinctions this makes that a bare try/catch does not:
 *
 *  1. **Offline is not a server error.** A dropped connection is temporary and
 *     the user's own; a 500 is ours. Saying "you appear to be offline" when the
 *     server is down is as wrong as the reverse.
 *  2. **An expired session is recoverable.** A 401 mid-action means sign in
 *     again, not "something went wrong" — and the user should not lose what
 *     they were doing.
 *  3. **Retryable and not are different.** A 409 will fail again; a 503 will
 *     not. Offering "try again" for the first teaches people the button lies.
 */

export interface MutationFailure {
  message: string;
  /** True when trying again might plausibly work. */
  retryable: boolean;
  /** True when the fix is to sign in again. */
  needsAuth: boolean;
  /**
   * True when the target no longer exists — a posting closed between the page
   * rendering and the click landing. Callers use this to change the page
   * itself, not just show a message: a Save button on a role that is gone is
   * an offer that cannot be honoured.
   */
  gone: boolean;
}

export async function describeFailure(res: Response | null, err?: unknown): Promise<MutationFailure> {
  // No response at all: the request never reached the server.
  if (!res) {
    const offline = typeof navigator !== 'undefined' && navigator.onLine === false;
    return {
      message: offline
        ? 'You appear to be offline. This will work again once you reconnect.'
        : 'We could not reach the server. Check your connection and try again.',
      retryable: true,
      needsAuth: false,
      gone: false
    };
  }

  if (res.status === 401) {
    return {
      message: 'Your session expired. Sign in again and nothing will be lost.',
      retryable: false,
      needsAuth: true,
      gone: false
    };
  }

  if (res.status === 404) {
    return {
      message: 'This posting is no longer live, so it cannot be saved.',
      retryable: false,
      needsAuth: false,
      gone: true
    };
  }

  // The API speaks problem+json, so a specific reason is usually available and
  // is better than anything generic we could write here.
  let detail = '';
  try {
    const problem: ProblemDetail = await res.json();
    detail = problem.detail || problem.title || '';
  } catch {
    // Not JSON — a proxy error page, most likely. Fall through to the generic.
  }

  if (res.status === 409) {
    return {
      message: detail || 'That change conflicts with the current state.',
      retryable: false,
      needsAuth: false,
      gone: false
    };
  }

  if (res.status >= 500) {
    return {
      message: 'Something went wrong on our side. Trying again usually works.',
      retryable: true,
      needsAuth: false,
      gone: false
    };
  }

  return {
    message: detail || 'That did not work.',
    retryable: res.status !== 400,
    needsAuth: false,
    gone: false
  };
}

/**
 * Runs a write, reverting the optimistic change if it fails.
 *
 * The revert is the caller's, passed in, because only the caller knows what it
 * changed. Doing it here would mean this module knowing about every piece of
 * state in the app.
 */
export async function mutate(
  request: () => Promise<Response>,
  revert: () => void
): Promise<MutationFailure | null> {
  let res: Response | null = null;
  try {
    res = await request();
    if (res.ok) return null;
  } catch (err) {
    revert();
    return describeFailure(null, err);
  }
  revert();
  return describeFailure(res);
}
