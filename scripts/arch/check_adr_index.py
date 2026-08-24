#!/usr/bin/env python3
"""The ADR index must list every ADR, with the status the ADR itself claims.

An index is the only entry point most readers use. When it lags — as it had:
twelve records on disk, eight in the table — a reader concludes the practice
was abandoned, and the four unlisted decisions stop binding anyone. The cost of
a stale index is not the missing rows, it is the credibility of the ones that
are there.

Numbering gaps are checked too. ADR-0010 does not exist, and a reader who
notices cannot tell whether it was withdrawn or lost. A gap is allowed, but it
has to be declared in the README with a line of the form:

    <!-- gap: 0010 — withdrawn before decision, never assigned -->
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _ratchet  # noqa: E402

ADR_DIR = Path("docs/architecture/adr")
INDEX = ADR_DIR / "README.md"

ROW = re.compile(r"^\|\s*\[(\d{4})\]\(([^)]+)\)\s*\|(.+?)\|\s*([A-Z]+)[^|]*\|", re.M)
STATUS = re.compile(r"^- \*\*Status:\*\*\s*\**([A-Z]+)\**", re.M)
GAP = re.compile(r"<!--\s*gap:\s*(\d{4})\b", re.I)
SUPERSEDED_BY = re.compile(r"SUPERSEDED\s+by\s+ADR-(\d{4})", re.I)


def main() -> int:
    violations: dict[str, str] = {}
    if not INDEX.exists():
        print(f"missing {INDEX}", file=sys.stderr)
        return 1

    index_text = INDEX.read_text()
    rows = {n: (target, status) for n, target, _, status in ROW.findall(index_text)}
    declared_gaps = set(GAP.findall(index_text))

    on_disk: dict[str, Path] = {}
    for p in sorted(ADR_DIR.glob("[0-9]*.md")):
        on_disk[p.name[:4]] = p

    for num, path in on_disk.items():
        if num not in rows:
            violations[f"unlisted:{num}"] = (
                f"ADR-{num} exists at {path} but is not in the index table"
            )
            continue
        target, index_status = rows[num]
        if target != path.name:
            violations[f"badlink:{num}"] = (
                f"index row for ADR-{num} links to {target}, file is {path.name}"
            )
        m = STATUS.search(path.read_text())
        if not m:
            violations[f"nostatus:{num}"] = f"{path} has no `- **Status:**` line"
        elif m.group(1) != index_status:
            violations[f"status:{num}"] = (
                f"ADR-{num}: index says {index_status}, file says {m.group(1)}"
            )

    for num in rows:
        if num not in on_disk:
            violations[f"missing:{num}"] = f"index lists ADR-{num}, no such file"

    # Superseded records must point forward to something real.
    for num, path in on_disk.items():
        for target in SUPERSEDED_BY.findall(path.read_text()):
            if target not in on_disk:
                violations[f"supersede:{num}"] = (
                    f"ADR-{num} says it is superseded by ADR-{target}, which does not exist"
                )

    if on_disk:
        span = range(1, max(int(n) for n in on_disk) + 1)
        for i in span:
            num = f"{i:04d}"
            if num not in on_disk and num not in declared_gaps:
                violations[f"gap:{num}"] = (
                    f"ADR-{num} is missing and the gap is not declared in the index "
                    f'(add `<!-- gap: {num} — reason -->`)'
                )

    return _ratchet.run(
        "adr-index", "ADR index", violations, "ADR index inconsistencies."
    )


if __name__ == "__main__":
    sys.exit(main())
