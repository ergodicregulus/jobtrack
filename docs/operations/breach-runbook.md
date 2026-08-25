# Breach runbook — CERT-In six hours

> **CERT-In's Directions (effective 27 June 2022) have no minimum company size
> or revenue threshold.** A ten-person project carries the same obligation as an
> enterprise, and non-compliance is punishable under s.70B(7) of the IT Act.
> See [phase-5 §12.2](../engineering/phase-5-production-readiness.md#122-applies-now-no-size-threshold-cert-in).

**The clock is six hours from noticing, not from confirming.** That is the
single most important sentence here. Teams miss the window because they wait to
be certain; the obligation attaches to becoming *aware* of an incident.

## 0. Before anything: the two-minute triage

Answer these, in writing, in the incident channel. Do not investigate first.

1. **What makes you think there is an incident?** One sentence.
2. **Is personal data possibly involved?** CV text, emails, session tokens,
   password hashes. If yes, the six-hour clock has started — note the time you
   became aware, not the time you finished reading this.
3. **Is it still happening?** Containment before forensics.

## 1. Contain

In this order, because each step preserves the next one's evidence.

```bash
# Revoke every session. Cheap, reversible, and the fastest way to end an
# ongoing account compromise.
make psql
> DELETE FROM sessions;

# Stop ingest if the suspicion involves a source or the parser. The corpus
# going stale for an hour is not an incident; contaminated data is.
docker compose stop ingestor scheduler
```

**Do not delete anything.** Not logs, not rows, not containers. A deleted
artefact is both lost evidence and, if the incident is later reported, a fact
you will have to explain.

## 2. Preserve

```bash
# Logs first: they are the shortest-lived thing here.
docker compose logs --no-color --timestamps > incident-$(date +%FT%H%M%S).log

# Then the database, if the incident could involve data at rest.
docker compose exec postgres pg_dump -U jobtrack -Fc jobtrack > incident-$(date +%F).dump
```

Both go somewhere the suspected compromise cannot reach. If the host itself is
suspect, copy them off it.

**Log retention is a standing obligation, not an incident task.** CERT-In
requires 180 days, within Indian jurisdiction. Logs currently go to stdout with
no retention — that is an open gap, recorded in phase-5 §12.2, and it is what
would be missing when it mattered most.

## 3. Report — within six hours

To CERT-In (`incident@cert-in.org.in`), with whatever is known. **An incomplete
report inside the window beats a complete one outside it**; you can and should
follow up.

Include:

- When you became aware, and how.
- What kind of incident (unauthorised access, data breach, ransomware…).
- What systems and what categories of data.
- Whether it is ongoing.
- What has been done so far.
- A contact who will answer the phone.

## 4. Notify the people affected

DPDP requires notifying affected data principals *and* the Data Protection
Board. Say plainly what happened, what data, and what they should do. Do not
minimise, and do not send it before §3 — the regulator should not learn of it
from your users.

## 5. Afterwards

- A written timeline: when it started, when it was noticed, the gap between.
  **That gap is the number worth reducing** and it is usually the one nobody
  measures.
- If a control failed, add the check that would have caught it. This repository
  treats an unenforced rule as one that has already drifted
  ([ADR-0014](../architecture/adr/0014-rules-are-enforced-by-scripts.md)) —
  the same applies to a control that failed silently.
- If the incident revealed data held longer than the notice claims, that is a
  second finding and it needs the retention sweep checked, not just the breach
  closed.

## What is genuinely missing today

Stated here rather than discovered mid-incident:

| Gap | Consequence |
|---|---|
| **No named responder** | Six hours is not enough time to decide who is handling it |
| **No log retention** | Logs go to stdout. 180 days in-region is required and is not happening |
| **NTP not asserted** | Inherited from the host, unmonitored. Timestamps across services are the spine of any timeline |
| **No VAPT** | Never performed. Required annually by a CERT-In empanelled auditor once there is a production deployment |
