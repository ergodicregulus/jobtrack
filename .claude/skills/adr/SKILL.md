---
name: adr
description: Use when making or recording an architectural or technology decision for JobTrack — choosing a datastore, a library, a service boundary, a scoring constant, or overturning an existing ADR. Enforces the requirement-first framing and writes the record.
allowed-tools: Read, Grep, Glob, Bash, WebSearch, WebFetch, Edit, Write
---

# Recording an architecture decision

An ADR records **a decision, the alternatives considered, and the consequences
accepted** — as of the moment it was made. Six months from now someone will look
at a choice that seems obviously wrong; the ADR tells them whether it was a
mistake or a trade they have forgotten the other half of.

## 1. Refuse the question as asked

Almost every technology question arrives in a form that cannot be answered well.

> ❌ "Should we use Kafka or RabbitMQ?"

There is no answer. Restate it as requirements with numbers before going further:

> ✅ "We need asynchronous processing at ~500 events/sec, at-least-once delivery,
> ordering per customer, 7-day replay, three consumers, and minimal operational
> burden. Compare Kafka, RabbitMQ, SQS and NATS. Recommend one. State the failure
> modes, the operational cost, and **the workload at which the recommendation
> changes**."

If you cannot fill in the numbers, that is the finding — go and measure first.
An ADR built on an estimate that nobody checked is how ADR-0011 came to exist:
a constant was *chosen* where it should have been *measured*, and the resulting
bug put unreadable postings above readable ones in the top 100.

**The four questions every decision here must answer:**

1. What is required, in numbers? (throughput, latency, volume, consistency, ops budget)
2. What is the simplest thing that satisfies that?
3. What was rejected, and on what specific ground?
4. **At what measured value does this decision need revisiting?**

Question 4 is not optional. See `docs/architecture/caching-and-storage.md`: every
alternative infrastructure in this project has a numeric trigger, and none has
fired. That is why "no Redis" is a position rather than a prejudice.

## 2. Check what already binds you

```bash
ls docs/architecture/adr/
grep -rn "<your topic>" docs/architecture/adr/ docs/product/principles.md
```

- **An existing ADR covers this.** Do not argue against it in a PR or in chat.
  Write a superseding ADR: set the old one's status to `SUPERSEDED by ADR-NNNN`
  and state what new information changed the answer. ADR-0004 (no scraping, no
  accounts) and the anti-features in `docs/product/principles.md` are binding.
- **You are proposing new infrastructure.** Open
  `docs/architecture/caching-and-storage.md`, find the trigger, and **cite the
  metric that fired**. If none fired, that is the answer.
- **You are adding a dependency.** State what it replaces and what breaks if it
  is abandoned. CI enforces the budget.

## 3. Grade your evidence

External claims carry a grade, per `docs/research/evidence-ledger.md`:

- **A** — primary source, reproducible dataset, or first-party docs. May justify a decision.
- **B** — credible secondary source with stated methodology. Directionally trustworthy, numerically soft.
- **C** — contested, vendor-published, or anecdotal. **May never justify a decision.**

When sources disagree, **show both and say which you believe and why**. Do not
average them into a number that no source supports. A load-bearing number gets a
ledger entry; `make arch-check` fails on a citation that does not resolve.

## 4. Write it

Next free number, `docs/architecture/adr/NNNN-short-imperative-title.md`:

```markdown
# ADR-NNNN — <short imperative title>

- **Status:** PROPOSED | OPEN | DECIDED | SUPERSEDED by ADR-MMMM
- **Date:** YYYY-MM-DD
- **Decision drivers:** <the 3-5 things that actually decided it>

## Context
What is true that forces a choice. Include numbers.

## Options
Every option gets a fair hearing, including the rejected one — a strawman here
is a trap for a future reader who is re-opening the question in good faith.

## Decision
Stated so someone could implement it without asking you.

## Consequences
### Good
### Bad, and accepted
The second heading is mandatory. An ADR with no accepted downsides has not been
thought about.

## Revisit
The measured condition under which this is reopened.
```

## 5. Close the loop

```bash
make arch-check          # fails if the index is stale or a citation dangles
```

Add the row to `docs/architecture/adr/README.md`. Then ask: **is this decision
now enforced by anything?** If the ADR states a rule that code could violate
silently, it belongs in `scripts/arch/` as a check. Four rules in this repo were
documented and unenforced, and all four drifted. That is the whole reason
`scripts/arch/` exists.
