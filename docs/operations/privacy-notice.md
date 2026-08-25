# Privacy notice

> **Version 2026-08-25.** This string is `store.NoticeVersion` in the code, and
> every consent record names the version that was shown. Change this document
> materially and you must bump that constant — a consent record that cannot name
> what the person actually read is not a record of anything.

This is the notice India's DPDP Act requires, written to be read rather than to
be defensible. It says what is collected, why, how long it is kept, and what you
can do about it.

## What we hold, and why

| Data | Why | How long |
|---|---|---|
| Email and password hash | To have an account at all | Until you delete the account |
| Name, current and target title, years of experience, location and pay preferences | To score postings against you. Every one is a filter or a scoring input — nothing here is collected "in case" | Until you delete the account, or change it |
| Skills you declare | The largest single input to a match score | Same |
| **Text extracted from a CV you upload** | To propose skills and years of experience, which you then review before anything is applied | **24 months from upload**, then automatically erased |
| Applications you track, and their status changes | The tracker, and the activity grid | Until you delete the account |
| Saved searches | So the feed remembers your filters | Same |
| Consent records | Evidence that consent was given, for what, and when | Retained after account deletion, without identifiers |
| Session records | To keep you signed in | 30 days after expiry |

## What we do NOT hold

- **The CV file itself is never stored.** It is sent to an isolated parser,
  read, and discarded. Only the extracted text is kept, encrypted
  ([ADR-0007](../architecture/adr/0007-resume-parsing-local-first.md)).
- **No tracking, no advertising identifiers, no third-party analytics.** The
  pages load nothing from another origin; the Content-Security-Policy forbids
  it, so this is enforced rather than promised.
- **No recruiter or employer personal data.** Job postings come from employers'
  own public feeds and contain roles, not people.

## How the CV text is protected

Encrypted at rest with AES-256-GCM under a key held separately from the session
secret, so a leaked session secret does not also decrypt CVs. The service that
reads CVs holds no database credentials and has no network egress at all — it
cannot send your CV anywhere, because it cannot reach anywhere.

## Your rights, as endpoints

Not an email address and a promise. Each of these is a request the product
answers directly:

| Right | How |
|---|---|
| **Access and portability** | `GET /v1/me/export` — everything held about you, as JSON, with CV text decrypted |
| **Correction** | `PATCH /v1/me/profile`, or the profile page |
| **Erasure** | `POST /v1/me/erasure`. Takes effect after **7 days**; signing in cancels it |
| **Withdraw consent** | `DELETE /v1/me/consents?purpose=…`. Purposes are separately withdrawable |

**Why erasure waits seven days.** It is irreversible, and a stolen session could
request it. Every session is revoked the moment it is requested, so the same
theft cannot repeat the request, and signing in yourself cancels it.

## Consent

Recorded per purpose, with a timestamp and the version of this notice:

- **account** — holding an account. Withdrawing this *is* deletion.
- **matching** — scoring postings against your profile.
- **resume_parsing** — recorded when you apply a parsed CV, not at sign-up,
  because that is the moment you are actually deciding it.
- **digest_email** — not yet built. Nothing is sent without it.

Records are append-only. Withdrawal stamps a row; it never deletes one, because
deleting the record of consent destroys the evidence that it was obtained.

## Contact and complaints

Data fiduciary contact: the address in the repository's README. A data principal
who is unsatisfied may complain to the Data Protection Board of India.

## What changes this document

A material change bumps the version at the top and `store.NoticeVersion`, and
consent given against an older version is recorded as such — visible to you at
`GET /v1/me/consents`.
