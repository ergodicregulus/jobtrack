# Plans

Work that is specified but not yet done. One file per coherent unit, each with a
falsifiable done-condition.

**A plan is deleted when it is done — or when it is decided against.**
`compliance-and-hardening` was deleted on 2026-09-02 for the second reason: the
CERT-In and VAPT work was scoped out. The deferred `user_job_scores` contract
migration it also carried was completed separately. A plan nobody intends to
execute is exactly the stale document this index exists to prevent, and leaving
it would have made every future reading of this table one item less true.

**A plan is deleted when it is done.** Not marked complete, not archived —
deleted, because git remembers and a finished plan left lying about is exactly
the stale document this repository keeps having to clean up. If a plan produced
a decision worth keeping, that decision belongs in an ADR; if it produced a
number, that number belongs in the [evidence ledger](../../research/evidence-ledger.md).

Everything here is measured. No plan states a problem without the query that
found it, because a plan built on an assumption is a plan that solves the wrong
thing well.

| Plan | Problem it addresses | Blocked on a decision? |
|---|---|---|
| [corpus-relevance](corpus-relevance.md) | 67% of live postings are not engineering roles; one employer is 37% of the corpus | **Yes** — product scope |
| [corpus-completeness](corpus-completeness.md) | description coverage 79.2% while two-phase sweeps finish, country 93.9% — re-measured 2026-10-05 on the rebuilt corpus | No |
| [source-expansion](source-expansion.md) | India is 7.4% of the corpus against an India-first product | No |
| [digest-email](digest-email.md) | Built, tested end to end with a fake sender, off by default; not done until a real email is received | **Yes** — SMTP provider and credentials |

## How these get executed

Two models, split by what the work actually risks rather than by how long it
takes.

**Opus writes anything that can be wrong in a way tests will not catch**, and
reviews everything regardless of who wrote it:

- Schema, migrations, and anything touching `internal/store`
- Scoring, `internal/matching`, and any constant that changes a user's ranking
- Adapter semantics — vendor quirks, pagination caps, date handling
- ADRs, and any change that contradicts one
- Security, privacy, and compliance
- `scripts/arch/` and the ratchet baselines
- Every user-visible number, and every sentence that states one

**Sonnet subagents take work that is bounded, specified, and verified by
something other than judgement**:

- Capturing golden fixtures from an endpoint whose shape is already agreed
- Writing tests against a contract that is already written down
- Probing endpoints and recording what came back
- Mechanical refactors with a green suite either side
- Doc edits that transcribe a decision already made

**A Sonnet subagent never**: writes or edits a migration, changes a scoring
constant, edits a baseline in `scripts/arch/baseline/`, writes an ADR, or
produces a number that reaches a user. Those fail quietly and expensively, and
the whole point of this repository is that they cannot.

Every step below is tagged `[opus]` or `[sonnet]` accordingly. A `[sonnet]` step
that turns out to need judgement is escalated, not guessed at — the escalation is
cheap and the guess is not.

## The bar, once, so no plan repeats it

- `/verify` is the ladder. `make check` is the definition of done.
- A fix is not done until it is **measured on live data**, not only on a test.
  Both bugs found on 2026-09-01 passed every existing test while corrupting the
  corpus daily.
- New rule, new check. If a plan adds an invariant, it adds it to `scripts/arch/`
  or it will drift — see [ADR-0014](../../architecture/adr/0014-rules-are-enforced-by-scripts.md).
- Never fabricate a number. If a plan needs a figure it does not have, it
  measures it or says it does not know.
