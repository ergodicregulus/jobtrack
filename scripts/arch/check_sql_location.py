#!/usr/bin/env python3
"""SQL belongs in the packages that own the database.

A query written inline in an HTTP handler cannot be reused by the ingestor,
cannot be tested without spinning an HTTP request, and cannot be found by
anyone searching for "where do we read applications from". It also drags the
handler's length past the point where it can be read in one sitting: the five
longest handlers in this repo are long almost entirely because of embedded SQL
and positional scanning.

The rule is about location, not about style. Hand-written SQL is fine — see
ADR-0001. It just has to live somewhere a reader would think to look.
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

# A literal is SQL if it opens with a statement keyword. Anchoring at the start
# is deliberate: prose mentioning SELECT, and Go code building a fragment like
# " AND status = $1", are not statements and are not what this is looking for.
STATEMENT = re.compile(
    r"^\s*(SELECT|INSERT\s+INTO|UPDATE\s+\w|DELETE\s+FROM|WITH\s+\w)", re.I | re.S
)


def main() -> int:
    violations: dict[str, str] = {}
    for gf in _gosrc.walk():
        if gf.is_test or gf.rel.startswith(ALLOWED):
            continue
        for lit in gf.literals:
            if not STATEMENT.match(lit.text):
                continue
            first = " ".join(lit.text.split())[:64]
            key = f"{gf.rel}:{lit.line}"
            violations[key] = f'{key}: SQL in a non-database package — "{first}…"'
    return _ratchet.run(
        "sql-location",
        "SQL location",
        violations,
        "SQL statements outside internal/store and the migration packages.",
    )


if __name__ == "__main__":
    sys.exit(main())
