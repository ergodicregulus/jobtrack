import { describe, expect, it } from 'vitest';
import { describeFailure } from './mutate.svelte';

function response(status: number, body?: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' }
  });
}

describe('describeFailure', () => {
  // A dropped connection is the user's and temporary; a 500 is ours. Saying
  // "you appear to be offline" when the server is down is as wrong as the
  // reverse, and sends the user to debug their wifi for no reason.
  it('separates a request that never arrived from one the server rejected', async () => {
    const noResponse = await describeFailure(null);
    expect(noResponse.retryable).toBe(true);
    expect(noResponse.message).toMatch(/reach the server|offline/i);

    const serverError = await describeFailure(response(500));
    expect(serverError.retryable).toBe(true);
    expect(serverError.message).toMatch(/our side/i);
    expect(serverError.message).not.toMatch(/offline/i);
  });

  // An expired session is recoverable and the fix is specific. "Something went
  // wrong" would send the user nowhere.
  it('treats an expired session as a sign-in prompt, not an error', async () => {
    const f = await describeFailure(response(401));
    expect(f.needsAuth).toBe(true);
    expect(f.retryable).toBe(false);
    expect(f.message).toMatch(/sign in/i);
  });

  // Offering "try again" for something that will fail identically teaches
  // people the button lies.
  it('does not offer a retry for a conflict', async () => {
    const f = await describeFailure(
      response(409, { detail: 'this application has progressed past saved' })
    );
    expect(f.retryable).toBe(false);
    expect(f.message).toContain('progressed past saved');
  });

  // The API speaks problem+json; its reason beats anything generic here.
  it('prefers the server’s own explanation when there is one', async () => {
    const f = await describeFailure(
      response(400, { detail: 'filtering by match band requires an account' })
    );
    expect(f.message).toBe('filtering by match band requires an account');
  });

  // A proxy error page is not JSON. Falling over while reporting a failure is
  // the worst possible time to fail.
  it('survives a response that is not JSON', async () => {
    const html = new Response('<html>502 Bad Gateway</html>', {
      status: 502,
      headers: { 'content-type': 'text/html' }
    });
    const f = await describeFailure(html);
    expect(f.message).toMatch(/our side/i);
    expect(f.retryable).toBe(true);
  });

  // A posting can close between the feed rendering and the click landing.
  it('explains a posting that is gone rather than blaming the user', async () => {
    const f = await describeFailure(response(404));
    expect(f.message).toMatch(/no longer live/i);
    expect(f.retryable).toBe(false);
    // The flag matters as much as the message: the detail page uses it to
    // change the page, not merely to print a sentence.
    expect(f.gone).toBe(true);
  });

  // `gone` drives a destructive UI change — disabling Save, replacing the
  // header. Anything setting it loosely would blank pages on a transient error.
  it('sets gone for nothing except a missing target', async () => {
    for (const status of [401, 409, 500, 502, 400]) {
      const f = await describeFailure(response(status, { detail: 'x' }));
      expect(f.gone, `status ${status}`).toBe(false);
    }
    expect((await describeFailure(null)).gone).toBe(false);
  });
});
