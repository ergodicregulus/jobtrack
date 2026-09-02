# ADR-0019 — Digest email goes over SMTP, not a provider SDK

- **Status:** DECIDED
- **Date:** 2026-09-02
- **Decision drivers:** this is the first outbound channel and the first new
  external dependency the product has needed; the dependency rule in CLAUDE.md
  asks what it replaces and what breaks if it is abandoned

## Context

The digest email is the last item in phase-5 Block C and the only one requiring
infrastructure the product does not have. Everything else is Go and Postgres.
Email adds a third party that can be down, can be slow, and holds user addresses.

## Decision

**`net/smtp`, with the provider behind configuration.**

- Zero new modules in `go.mod`; `net/smtp`, `mime/quotedprintable` and
  `crypto/hmac` are all standard library.
- Any provider that speaks SMTP works — Resend, Postmark, SES, or something the
  operator already runs — and switching is a config change, not a rewrite.
- Disabled by default (`EMAIL_ENABLED=false`). A deployment that has not
  configured a host sends nothing and says so once per run, rather than failing
  per message.

## What this gives up

Provider-specific delivery telemetry: bounces, complaints, opens. We have no way
to act on any of it today and nowhere to put it. **If bounce handling becomes
necessary, that is the trigger to revisit this** — not a general preference for
a richer client.

## Consequences and the rules that follow

**Consent is a record, not a flag.** `digest_email` was already in the
`user_consents` CHECK constraint before there was anything to send, and the send
query requires a live, un-withdrawn grant. Unsubscribing writes a withdrawal to
the same table rather than setting a second, parallel notion of "unsubscribed"
that would eventually disagree with the consent log.

**Unsubscribe needs no session.** Someone who wants out is usually signed out,
on a phone, months later; asking for a password first is how an unsubscribe link
becomes a spam report. The link carries an HMAC of the user id — without it the
link is a guessable integer and anyone could unsubscribe anyone. It has no
expiry on purpose: a link in a two-year-old email must still work.

It answers GET as well as POST. Mail clients follow links with GET, and
`List-Unsubscribe-Post` sends an empty POST; refusing GET means the button built
into the client does nothing. The token is the authorisation and the action is
idempotent.

**An empty digest is never sent.** "Nothing new this week" is the message people
unsubscribe from, and on most weeks for most searches it is the honest content.
That is decided in the query rather than the template.

**"New" has one definition.** The boundary is `last_seen_max_posted_at`, the
same mark the sidebar badge uses, so the email and the interface cannot disagree
about what a reader has already seen.

**The mark is written after the send.** A crash between the two resends a digest,
which is a nuisance; the other order drops one silently, which is a promise
broken with no trace.

**Weekly, and never `RunOnStart`.** A deploy loop would otherwise email every
reader on every restart, and that is the one failure here that cannot be taken
back.
