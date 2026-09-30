# Digest email

**Status:** built 2026-09-02 (ADR-0019). Disabled by default; the opt-in control exists (the "Weekly digest" toggle in `web/src/routes/settings/+page.svelte`, posting to `?/digest`), so only SMTP settings remain before a real send. Step 6, the integration test, has not been written; the repository has zero digest tests. **Done when:** a user with a saved
search receives one email containing only roles they have not seen, and can stop
it from the email itself in one click.

## Why it is last

It is the only remaining Block C item that needs infrastructure the product does
not have, and [phase-5 §8.4](../phase-5-production-readiness.md) calls it "the
one new dependency" for that reason. Everything else in the product is Postgres
and Go; this adds a third party that can be down, can be slow, and holds user
email addresses.

## The decision this is blocked on

A transactional provider (Resend, Postmark, SES) or SMTP against something the
operator already runs. The dependency rule in
[CLAUDE.md](../../../CLAUDE.md) applies: what does it replace, and what breaks if
it is abandoned. An SMTP client is in the standard library; a provider SDK is
not, and the difference is a vendor lock-in decision rather than a technical one.

**Recommendation: `net/smtp` with the provider behind configuration.** It keeps
the dependency count at zero, works against any provider including a
self-hosted one, and makes switching a config change rather than a rewrite. The
cost is losing provider-specific delivery telemetry, which we have no way to act
on yet.

## Steps

1. **[opus]** The decision above, as an ADR. It is a new external dependency and
   a new class of user data leaving the system.
2. **[opus]** Sending is a River job on the maintenance queue, never on a request
   path, with the same idempotency the rest of the queue has: a digest is keyed
   by (user, saved_search, window) so a retry cannot send twice. A duplicate
   digest is worse than a late one.
3. **[opus]** Content comes from the saved search's own query string replayed
   against the feed, with `last_seen_max_posted_at` as the boundary — the mark
   that already exists and already means this. No second definition of "new".
4. **[sonnet]** Plain-text and HTML bodies from one template source, with the
   unsubscribe link in both. Both parts must say the same thing; a text part that
   drops the opt-out is a compliance problem, not a rendering one.
5. **[opus]** Unsubscribe is a signed token in the URL, honoured without a login,
   and a `List-Unsubscribe` header. It is also a DPDP withdrawal of consent for
   this purpose — it writes to `user_consents` like every other withdrawal rather
   than setting a flag somewhere new.
6. **[sonnet]** Integration test: a user with a saved search and one new posting
   gets exactly one digest; running the job twice sends one email.

## Traps

- **An empty digest must not be sent.** "Nothing new this week" is a message
  people unsubscribe from, and it is also the most likely output most weeks.
- Consent for `marketing` is not consent for `account`. This needs its own
  purpose in `user_consents`, recorded when the user asks for the digest and not
  at signup — the same reasoning that put `resume_parsing` at CV-apply.
- Email addresses in logs are personal data. The DPDP work already established
  this; a new sending path is the easiest place to undo it.
