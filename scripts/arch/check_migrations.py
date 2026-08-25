#!/usr/bin/env python3
"""Migrations must not lock a populated table.

CLAUDE.md and deployment-zdt both say "CREATE INDEX CONCURRENTLY, never a bare
CREATE INDEX on a populated table". Nothing checked it, and migration 0011
built a UNIQUE INDEX on `applications` — a table created five migrations
earlier — without it. A plain CREATE INDEX holds a lock that blocks every
INSERT, UPDATE and DELETE on that table until it finishes. On a small table
that is milliseconds; on a large one it is an outage during a deploy, which is
the one moment nobody is watching for it.

The rule is narrower than "always CONCURRENTLY", and the narrowness is why a
human reviewer misses it: indexing a table the SAME migration creates is
correct and CONCURRENTLY there would be *wrong*, because it cannot run inside a
transaction and the table is empty anyway. So what this checks is the actual
rule — an index on a table this migration did not create must be concurrent.

Second rule, which follows from the first: CONCURRENTLY cannot run in a
transaction, so a migration using it must carry `-- +migrate no-transaction`.
Without the directive the statement fails at deploy time rather than at review
time.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _ratchet  # noqa: E402

MIGRATIONS = Path("migrations")

CREATE_TABLE = re.compile(r"CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)", re.I)
ADD_ENUM_VALUE = re.compile(r"ALTER\s+TYPE\s+\S+\s+ADD\s+VALUE", re.I)
CREATE_INDEX = re.compile(
    r"CREATE\s+(?:UNIQUE\s+)?INDEX\s+(CONCURRENTLY\s+)?"
    r"(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)\s+ON\s+([a-z_][a-z0-9_.]*)",
    re.I,
)


def _strip_comments(sql: str) -> str:
    sql = re.sub(r"/\*.*?\*/", " ", sql, flags=re.S)
    return re.sub(r"--[^\n]*", "", sql)


def main() -> int:
    violations: dict[str, str] = {}

    for path in sorted(MIGRATIONS.glob("*.up.sql")):
        raw = path.read_text()
        sql = _strip_comments(raw)
        own_tables = {m.group(1).lower() for m in CREATE_TABLE.finditer(sql)}
        uses_concurrently = False

        for m in CREATE_INDEX.finditer(sql):
            concurrent, index, table = m.group(1), m.group(2), m.group(3).lower()
            if concurrent:
                uses_concurrently = True
                continue
            if table in own_tables:
                # Created in this migration, so it is empty. Correct as-is.
                continue
            key = f"{path.name} :: {index}"
            violations[key] = (
                f"{path}: {index} indexes `{table}`, which an earlier migration "
                f"created, without CONCURRENTLY — this blocks writes to "
                f"`{table}` for the duration of the deploy"
            )

        # ALTER TYPE ... ADD VALUE has the same constraint as CONCURRENTLY and
        # fails more subtly: Postgres accepts the statement inside a
        # transaction and then rejects the first row that uses the new label,
        # so the failure surfaces at ingest rather than at deploy.
        needs_no_tx = uses_concurrently or ADD_ENUM_VALUE.search(sql)
        if needs_no_tx and "+migrate no-transaction" not in raw:
            why = "CONCURRENTLY" if uses_concurrently else "ALTER TYPE ... ADD VALUE"
            key = f"{path.name} :: no-transaction"
            violations[key] = (
                f"{path}: uses {why} but has no `-- +migrate no-transaction` "
                f"directive; it cannot run inside a transaction"
            )

    return _ratchet.run(
        "migrations",
        "Migration safety",
        violations,
        "Migrations that lock a populated table, or use CONCURRENTLY without the directive.",
    )


if __name__ == "__main__":
    sys.exit(main())
