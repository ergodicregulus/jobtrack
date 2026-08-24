#!/usr/bin/env python3
"""Only the data layer may call the database.

This replaces a blunter rule. check_layering.py used to forbid internal/api from
importing pgx at all, which was a proxy for what actually matters and caught two
honest uses along with the real ones: River's client is typed `river.Client[pgx.Tx]`,
and a store function that must enqueue inside its own transaction takes a
`func(pgx.Tx) error`. Neither touches the database from a handler.

So the rule is stated directly instead of by proxy: outside the packages whose
job is Postgres, nothing calls Query, QueryRow, Exec, SendBatch, CopyFrom or
Begin. Naming a pgx type is fine. Using one to run a statement is not.

A rule enforced by a proxy fails in both directions — it permits what it meant
to forbid and forbids what it meant to permit — and a baseline that can never
empty makes the ratchet decorative. ADR-0014 says to revisit a check when that
happens; this is that revisit.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _gosrc  # noqa: E402
import _ratchet  # noqa: E402

# Packages whose job is to talk to Postgres.
ALLOWED = (
    "internal/store/",
    "internal/migrate/",
    "internal/datamigrations/",
    "internal/seed/",
    "cmd/seed/",
    "cmd/covcheck/",
)

# The trailing group requires at least one argument. Every pgx call takes a
# context as its first, and the exclusion this buys is not incidental:
# `r.URL.Query()` from net/url takes none, and matching the bare method name
# reported it as a database call in three handlers that make none.
CALL = re.compile(r"\.(Query|QueryRow|Exec|SendBatch|CopyFrom|Begin)\(\s*[^)\s]")


def main() -> int:
    violations: dict[str, str] = {}
    for gf in _gosrc.walk():
        if gf.is_test or gf.rel.startswith(ALLOWED):
            continue
        for lineno, line in enumerate(gf.code.splitlines(), 1):
            m = CALL.search(line)
            if not m:
                continue
            key = f"{gf.rel} :: {m.group(1)} :: {line.strip()[:48].rstrip()}"
            violations[key] = (
                f"{gf.rel}:{lineno}: calls .{m.group(1)}() outside the data layer — "
                f"move the query into internal/store behind a named function"
            )
    return _ratchet.run(
        "db-access",
        "Database access",
        violations,
        "Direct database calls outside internal/store and the migration packages.",
    )


if __name__ == "__main__":
    sys.exit(main())
