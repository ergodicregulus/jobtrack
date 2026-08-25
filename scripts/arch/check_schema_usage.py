#!/usr/bin/env python3
"""Every table must be reachable from code.

A table nothing reads or writes is a table that misleads: someone reading the
schema concludes the feature exists. Two of them here carry HNSW indexes for
vector search, which made `pgvector` look like a shipped capability in a
document that listed it as a measured consequence. It has never held a row.

Two exclusions matter, and getting them wrong is what makes a naive version of
this check useless:

  - A table DROPPED by a later migration is not dead schema, it is the contract
    half of an expand/contract pair working correctly. `saved_jobs` is exactly
    that: added in 0009, superseded by `applications`, dropped in 0011.
  - A PARTITION is written through its parent and never named in code.
    `posting_observations_default` holds 202,262 rows for precisely this
    reason.

What remains is genuine: a table that exists, that no Go source mentions, and
that no migration ever removed.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _gosrc  # noqa: E402
import _ratchet  # noqa: E402

CREATE = re.compile(r"CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)", re.I)
DROP = re.compile(r"DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([a-z_][a-z0-9_]*)", re.I)
PARTITION = re.compile(r"CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)[^;]*?PARTITION\s+OF", re.I | re.S)


def main() -> int:
    sql = "\n".join(p.read_text() for p in sorted(Path("migrations").glob("*.up.sql")))

    created = {m.group(1).lower() for m in CREATE.finditer(sql)}
    dropped = {m.group(1).lower() for m in DROP.finditer(sql)}
    partitions = {m.group(1).lower() for m in PARTITION.finditer(sql)}

    live = created - dropped - partitions

    # Comments blanked, string bodies kept. Raw source let user_job_scores pass
    # this check on the strength of two explanatory comments while nothing read
    # or wrote it; `code` would go too far the other way and blank the SQL
    # literals where real usage lives.
    code = "\n".join(gf.decommented for gf in _gosrc.walk() if not gf.is_test)

    violations: dict[str, str] = {}
    for table in sorted(live):
        if re.search(rf"\b{re.escape(table)}\b", code):
            continue
        violations[table] = (
            f"table `{table}` is created by a migration, never dropped, and no "
            f"Go source mentions it — either use it, or drop it in a contract migration"
        )

    return _ratchet.run(
        "schema-usage",
        "Schema usage",
        violations,
        "Tables no code reads or writes.",
    )


if __name__ == "__main__":
    sys.exit(main())
