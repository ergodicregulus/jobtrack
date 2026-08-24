# ADR-0014 — A project rule is enforced by a script or it is not a rule

- **Status:** DECIDED
- **Date:** 2026-08-25
- **Decision drivers:** four documented rules had silently become false; the repository already
  contained the cure and had not generalised it; agents fail differently from humans

## Context

An audit of this repository on 2026-08-25 compared what `CLAUDE.md` claimed against what the code
does. Four claims were false:

| Claim | Reality |
|---|---|
| "sqlc-generated queries + hand-written repository wrappers" | No `sqlc.yaml` anywhere; queries are hand-written |
| "`store/` — queries live here" | `internal/api` held **36** SQL literals to `internal/store`'s **8** |
| "CI fails on an uncited citation" | No such check existed in `Makefile` or `ci.yml` |
| ADR index lists every record | 8 listed, 12 on disk, ADR-0010 an undeclared gap |

The correlation that matters is not that four rules drifted. It is **which** four:

- `scripts/check-links.py` exists → documentation links were still correct.
- `scripts/check-make-targets.py` exists → every documented `make` target still existed.
- `scripts/drift-check.sh` exists → the live schema still matched the migrations.
- `make bench-budget` exists → the frontend budget was still met, at 75.1 KB of 100.

**Every rule with a script was true. Every rule without one had drifted.** Four for four in each
direction. That is not a coincidence to note in passing; it is a design input.

The mechanism is unremarkable once stated. A rule in prose is checked when a human happens to read
the prose and happens to remember it while reviewing the right diff. The probability of that is well
under one, and it compounds badly: each unenforced rule that drifts reduces trust in the document,
which reduces the chance the next reader takes any of it seriously.

This bites harder with coding agents than with people. An agent will produce plausible-looking code
that violates an invariant nobody wrote down as a check, and will not feel the unease a human feels
when touching an unfamiliar layer.

## Options

**A. Write the rules more prominently.** Restructure `CLAUDE.md`, add a review checklist. Costs
nothing and changes nothing: the four false claims were already in a short, well-written, prominently
placed document. Prominence was not the failing.

**B. Add reviewer agents that read the rules and check diffs.** An agent per topic — database,
scalability, security, architecture. Better than prose, because something actually reads the diff.
But it is probabilistic where a script is deterministic, it costs a model call per review, and it
cannot run in CI as a gate. Also duplicates gates that already exist: `make migrate-verify` already
checks migrations, `caching-and-storage.md` already states scalability triggers as numbers.

**C. Make each rule executable, and treat an unenforceable rule as a smell.** A rule that cannot be
checked mechanically is usually a rule stated too vaguely to follow either.

**D. Do nothing, fix the four claims.** Cheapest today. Guarantees a fifth.

## Decision

**C, with B in a reduced role.**

1. `scripts/arch/` holds one executable check per structural invariant. Stdlib Python, no toolchain
   required, so it runs before `make dev` has ever been used and finishes in under a second.
   Currently: `check_layering`, `check_sql_location`, `check_func_length`, `check_adr_index`,
   `check_citations`.
2. **Adding a rule to `CLAUDE.md` means adding its check**, or writing here why it cannot be checked.
3. Known violations are a **ratchet**, not an allowlist. `scripts/arch/baseline/*.txt` records what
   is wrong today. A new violation fails. **A baseline entry that stops violating also fails**, so
   the file must shrink as debt is paid.
4. `make arch-check` runs first in `make check` and early in CI — a layering mistake is reported in
   the first second, not after the Playwright suite.
5. **One** reviewer agent (`.claude/agents/architecture-reviewer.md`) for what a script cannot judge:
   whether a change contradicts the *reasoning* in an ADR. Not five.

The ratchet's second half — failing when a baseline entry is fixed — is the part usually omitted, and
it is the part that makes this different from an allowlist. Without it the baseline only accumulates,
and a stale entry hides the next regression in the same file behind known debt.

## Consequences

### Good

- A rule stated here can be trusted, because something ran.
- Debt is counted, visible, and can only decrease. `make arch-check` prints the number.
- The checks are the onboarding document: a newcomer runs one command to learn the boundaries.
- No new dependency in `go.mod` or `package.json`.

### Bad, and accepted

- **Some real rules are not mechanically checkable.** "Comments explain why, not what" cannot be a
  script. Those stay in prose and stay weaker, and we accept that rather than pretend otherwise.
- **The checks are heuristic where a parser would be exact.** `scripts/arch/_gosrc.py` reads gofmt'd
  Go with regexes rather than a real parser, because a real parser means a Go toolchain on the host.
  gofmt is enforced by `make fmt-check`, which is what makes the heuristic safe; if that ever stops
  being true, these checks quietly weaken.
- **A ratchet can be gamed** by running `make arch-check-update` and committing the growth. The
  defence is that the diff is visible in review, not that it is impossible.
- **Five more things can fail in CI**, including for reasons unrelated to a change — a renamed file
  moves a baseline key.

## Revisit

- If `scripts/arch/` exceeds ~10 checks or 800 lines, the regex approach has outgrown itself: move to
  a real Go analysis pass (`golang.org/x/tools/go/packages`) run inside the tools container.
- If a baseline has not shrunk in 90 days, the ratchet is decorative — either the debt is not
  actually being paid, or the rule is wrong and should be withdrawn.
