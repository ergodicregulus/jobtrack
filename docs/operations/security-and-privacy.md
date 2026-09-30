# Security and privacy

> Status: **DECIDED**. Not legal advice; the regulatory reading here should be reviewed by counsel
> before public launch.

## 1. What we hold, ranked by sensitivity

| Data | Sensitivity | Why |
|---|---|---|
| **Resume file + parsed profile** | **Highest** | Name, contact details, full employment history, education, sometimes address and DOB. A dense PII package in one object |
| Application history | **High** | Reveals that a person is job-hunting. Disclosure to a current employer can cost them their job |
| Email address, password hash | High | Account takeover |
| Preferences, watchlists | Medium | Reveals target companies |
| Job postings, company data | Low | Public by construction |

**The application history deserves the emphasis it gets.** The user does not merely want it private —
a leak has a specific, severe, real-world consequence for them. That threat shapes several decisions
below that would otherwise look excessive.

## 2. Threat model

| Threat | Likelihood | Impact | Control |
|---|---|---|---|
| Credential stuffing | **High** | Account takeover | Argon2id, rate limits, breach-password check, 2FA (v2) |
| Database dump via SQLi | Low | Catastrophic | Parameterised queries only (`sqlc`); **no string-built SQL anywhere** |
| Database dump via infra compromise | Low | Catastrophic | **Resume data encrypted with per-user keys** — a dump is not a resume dump |
| Malicious upload (PDF exploit, zip bomb) | **Medium** | RCE / DoS | Isolated `resume-parser`: no egress, read-only FS, seccomp, hard memory limit, timeout |
| XSS via job description HTML | **High** | Session theft | Strict sanitisation on ingest **and** on render; CSP with no `unsafe-inline` |
| SSRF via a source URL | Medium | Internal network access | Allowlist schemes, block private IP ranges, no redirect following to private addresses |
| Prompt injection via resume/JD text | Medium | Manipulated LLM output | LLM is opt-in and never authoritative; output is treated as untrusted suggestion |
| Session hijacking | Medium | Account access | `HttpOnly`/`Secure`/`SameSite`, server-side sessions, immediate revocation |
| Enumeration of users | Medium | Privacy leak | 404 rather than 403 for non-visible resources; no "email not registered" |
| Insider access | Low | Severe | Encrypted at rest; production access audited; no bulk resume export tooling exists |

Two entries are worth expanding.

**Malicious upload.** PDF parsers are a well-documented source of crashes, unbounded memory growth,
and parser-level exploits. This is the entire reason `resume-parser` is the one network-isolated
service in the architecture — it is the only component executing over untrusted binary input, and it
runs with **no database credentials and no network egress**, so a successful exploit reaches nothing
([service-topology §3](../architecture/service-topology.md#resume-parser--the-one-true-service)).

**XSS via job descriptions.** We ingest HTML written by thousands of third parties, and Greenhouse's
`content` field is HTML that is itself HTML-escaped — a double-decode step that is exactly where
sanitisation bugs hide. We sanitise on ingest to an allowlist of tags and again on render, and store
the original in `raw` so a sanitiser fix can be reapplied without re-fetching.

## 3. Encryption

**In transit:** TLS 1.3 everywhere, including inside the cluster. mTLS on `api` → `resume-parser`.
HSTS with preload.

**At rest:** full-disk encryption on the database, plus **application-level envelope encryption for
resume data specifically**:

```
KMS master key  (never leaves the KMS)
   └─ wraps → per-user Data Encryption Key  (users.dek_wrapped)
                 └─ encrypts → parsed_text_enc, parsed_json_enc, and the object-store blob
```

Why go beyond disk encryption: disk encryption protects against a stolen disk, which is not our threat
model. Envelope encryption means **a database dump is not a resume dump** — the attacker also needs
KMS access. And it makes deletion cryptographically meaningful: destroying the user's DEK renders
every copy unreadable, including copies inside backups we cannot selectively edit.

The KMS is an interface, not a vendor. Any KMS with wrap/unwrap works; locally it is a file-backed
implementation.

## 4. Authentication

- **Argon2id** at the OWASP baseline — **m = 19 MiB, t = 2, p = 1** — which lands near 100 ms on a
  modern server core `[A-21]`. Memory cost, not time cost, is what defeats GPU and ASIC attackers, so
  raise `m` before `t` if we tune upward. Parameters are stored **alongside each hash** so they can be
  raised later and old hashes rehashed transparently on next successful login.
- Passwords checked against a breached-password corpus at registration and change.
- **Server-side sessions**, not stateless JWTs — revocation must be immediate, because a user signing
  out on a shared machine cannot be told to wait for expiry.
- Session cookie: `HttpOnly`, `Secure`, `SameSite=Lax`, 30-day expiry, rotated on privilege change.
- Password reset tokens: single-use, 1-hour expiry, and the flow **never reveals whether an email is
  registered**.
- 2FA in v2, gated on data value: once 1,000 users have parsed resumes stored, it stops being
  optional ([roadmap §v2.4](../product/roadmap.md#v24--two-factor-authentication)).

## 5. Regulatory posture

### India — DPDP Act 2023

The primary market, so this is the primary obligation.

| Requirement | Implementation |
|---|---|
| Notice and consent | Plain-language notice at collection; separate consent for optional LLM enrichment |
| Purpose limitation | Resume data used for matching and the user's own tracking. **Never for anything else** |
| Data minimisation | We store what the resume contains; we do not enrich it from third parties |
| Right to access | Full export, JSON + CSV, self-service, ≤ 60 s |
| Right to erasure | Self-service hard delete; DEK destroyed immediately |
| Right to correction | Every parsed field is user-editable, and edits always win over re-parsing |
| Breach notification | Documented procedure, Data Protection Board notification path |
| Consent withdrawal | Revocable per-purpose, without deleting the account |

### GDPR (EU users)

Same mechanics, plus: a stated **lawful basis** (contract performance for core function; consent for
optional enrichment), data portability in a machine-readable format (JSON Resume), and DPAs with any
sub-processor.

**On automated decision-making (Art. 22):** our scoring ranks *jobs for a user*, not *users for an
employer*. No decision is made about a person, so Art. 22 does not bite. This is a genuine structural
advantage of building for the candidate rather than the employer, and it is worth preserving — a
future employer-side product would inherit obligations this one does not have.

### Job posting data

Public, non-personal, first-party feeds only. **We store no recruiter or employee personal data**,
which keeps us out of processing personal data for people who never interacted with us. Some feeds
include recruiter contact details; those fields are dropped at ingest, not stored and filtered later.
Basis: [ADR-0004](../architecture/adr/0004-source-acquisition-policy.md).

## 6. LLM enrichment — the data-flow rule

Off by default, everywhere. When a user enables it:

1. **Disclosed at the point of use**, not buried in settings.
2. The configuration **records exactly which fields leave the system**, and that record is visible to
   the user in their privacy settings.
3. **No training on our data** — a contractual requirement on any provider.
4. Scoped: skill normalisation sends a *term*, never the resume. Only the explicit "rescue this parse"
   flow sends resume text, and only that resume.
5. Fully functional with it disabled. The deterministic pipeline is the product; enrichment is an
   accessory ([ADR-0007](../architecture/adr/0007-resume-parsing-local-first.md)).

## 7. Application security

**Headers:** CSP with no `unsafe-inline` (nonce-based), `X-Content-Type-Options: nosniff`,
`Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy` denying camera/mic/geo.

**Uploads:** magic-byte validation (never trust the extension or `Content-Type`), 5 MB cap, page-count
cap, stored under a random key with no user-controlled path component, served only through signed
short-lived URLs.

**Outbound fetches (SSRF):** `https` only; DNS resolved and checked against private ranges before
connecting; redirects re-validated; no redirect to a private address, ever.

**Dependencies:** `govulncheck` and `npm audit` in CI; Dependabot; a small dependency budget
([P6](../product/principles.md#p6--every-dependency-is-a-liability)) that makes this tractable
rather than a full-time job.

**Secrets:** never in code or images. Kubernetes Secrets via External Secrets Operator — a
provider-agnostic interface over any backend. Rotated quarterly, and immediately on any suspicion.

## 8. Retention and deletion

| Data | Retention |
|---|---|
| Resume files and parsed data | Until deleted by the user |
| Application history | Until deleted by the user |
| `posting_observations` | 13 months (partitioned; retention is a `DROP TABLE`) |
| Closed postings | 24 months |
| Session records | 30 days after expiry |
| Access logs | 30 days |
| Traces / metrics | 7 / 90 days |
| Backups | 30 days PITR |

**Account deletion**, on request:

1. Destroy the user's DEK. **All resume data is immediately unreadable, including in backups.**
2. Hard-delete rows: user, resumes, applications, watchlists, sessions.
3. Purge object-store blobs.
4. Complete within 24 hours; confirm by email.

Step 1 is what makes this honest. Backups cannot be selectively edited, so a system that "deletes"
data while backups retain it has not deleted anything. Destroying the key is the only deletion
guarantee that survives contact with a backup regime — and it is why per-user envelope encryption is
worth the complexity.

## 9. Incident response

| Severity | Definition | Response |
|---|---|---|
| **SEV1** | Confirmed data exposure | Immediate. Contain, assess scope, notify within 72 h (GDPR) and per DPDP timelines |
| SEV2 | Suspected compromise, no confirmed exposure | Within 1 h. Rotate credentials, invalidate sessions |
| SEV3 | Vulnerability found, not exploited | Within 24 h. Patch, assess |

Every SEV1/SEV2 gets a **blameless postmortem** with a written timeline, a root cause, and action
items with owners. Published internally, and externally where users were affected.

**Security contact:** `security@` with a published disclosure policy, a 90-day coordinated window, and
a commitment not to pursue good-faith researchers.

## 10. What we deliberately do not do

| Not doing | Why |
|---|---|
| Sell or share candidate data with recruiters | Inverts whose side we are on. This is the product thesis, not a policy ([personas](../product/personas-and-perspectives.md#perspective-map)) |
| Third-party analytics that see user content | [P8](../product/principles.md#p8--the-users-data-is-theirs-and-it-is-the-most-sensitive-thing-we-hold) |
| Store resume data unencrypted "for search" | Search operates on parsed skills, not raw resume text; no embeddings are generated ([ADR-0022](../architecture/adr/0022-retrieval-is-lexical.md)) |
| Bulk export tooling for staff | If it does not exist, it cannot be misused or compelled |
| Retain data after deletion "for analytics" | Deletion means deletion |
| Enrich profiles from third-party sources | We hold what the user gave us, and nothing more |
