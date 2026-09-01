# Compliance and hardening

**Status:** not started. **Done when:** the CERT-In §12.2 table has no ❌ that is
within our control, and the deferred contract migration has run.

## What is open

From [phase-5 §12.2](../phase-5-production-readiness.md), unchanged since it was
written:

| Obligation | State |
|---|---|
| Report covered incidents within 6 hours | Runbook written; **no named responder** |
| Retain logs 180 days, in Indian jurisdiction | ❌ stdout only, no retention, no residency |
| NTP synchronisation to an Indian source | ❌ not asserted |
| VAPT | ❌ never done |

The breach runbook exists and leads with the sentence teams get wrong — the six
hours run from becoming *aware*, not from confirming. It names these gaps rather
than leaving them to be found mid-incident, which is the right state for a
document but not for the product.

## Steps

1. **[opus]** Log retention. This is the largest of the four and the only one
   with an architectural cost: 180 days of structured logs, in-region, queryable
   enough to answer "what did this account do" during an incident. Postgres is
   the default answer here as everywhere else — check the numeric triggers in
   [caching-and-storage](../../architecture/caching-and-storage.md) before
   reaching for anything else, and record the volume estimate that justifies the
   choice.
2. **[sonnet]** NTP. A documented assertion in the deployment manifests plus a
   startup check that clock skew is under a threshold. Mechanical, and it is
   evidence that timestamps in an incident report mean anything.
3. **[opus]** Named responder. A person and a fallback, in the runbook, with a
   contact route that works out of hours. Not a technical task and not one to
   delegate — it is a commitment about who wakes up.
4. **[opus]** VAPT. Scope it before booking it: the API surface, the auth and
   session model, the ingest path, and the resume upload — which takes arbitrary
   files from users and runs a binary over them, and is the highest-risk surface
   in the product by some distance.
5. **[opus]** `user_job_scores` contract migration, deferred one release by
   [deployment-zdt §expand/contract](../../operations/deployment-zdt.md) when
   ADR-0016 removed the table's reason to exist. The deferral has now had its
   release; drop it, and confirm no code path references it first.

## Traps

- **A runbook that names a gap is not a control.** The gaps above have been
  documented and unfixed for a week; documenting them again is not progress.
- Log retention that holds personal data has a DPDP retention period of its own.
  180 days of logs is a 180-day retention commitment, and the sweep in
  `internal/store/privacy.go` must know about it or the two rules contradict.
- Do not run VAPT against the shared dev database. The resume fixtures are
  synthetic but the demo accounts and their consent records are not.
