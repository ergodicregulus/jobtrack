#!/usr/bin/env python3
"""Run every architecture invariant. `make arch-check`.

Each check is its own file and its own exit code, so one can be run alone while
working on it:

    python3 scripts/arch/check_layering.py
    python3 scripts/arch/check.py --update      # rewrite every baseline

Every check here follows the same rule, and it is the rule this directory
exists to make true: a project rule that no script enforces will drift, and
this repository proved it four times over — sqlc that was never adopted, a
store layer the handlers bypassed, a citation gate that was never written, and
an ADR index four records stale. Each of those was documented. None was
checked.

Adding a rule to CLAUDE.md without adding it here is how that happens again.
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).parent
# Cheapest and most-consulted first: a doc fix should not wait on a full source
# walk to be reported.
ORDER = [
    "check_adr_index.py",
    "check_citations.py",
    "check_docs.py",
    "check_layering.py",
    "check_db_access.py",
    "check_sql_location.py",
    "check_func_length.py",
]


def main() -> int:
    passthrough = [a for a in sys.argv[1:] if a.startswith("--")]
    selected = [a for a in sys.argv[1:] if not a.startswith("--")]

    checks = [c for c in ORDER if (HERE / c).exists()]
    if selected:
        checks = [c for c in checks if any(s in c for s in selected)]
        if not checks:
            print(f"no check matches {selected}; known: {ORDER}", file=sys.stderr)
            return 2

    failed = []
    for name in checks:
        rc = subprocess.run(
            [sys.executable, str(HERE / name), *passthrough],
            cwd=Path.cwd(),
        ).returncode
        if rc != 0:
            failed.append(name)

    if failed:
        print(f"\n{len(failed)} of {len(checks)} checks failed: {', '.join(failed)}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
