#!/usr/bin/env python3
"""Functions short enough to hold in your head at once.

The threshold is measured, not borrowed. Across 528 top-level functions the
median is 15 lines and p75 is 30, so this codebase is already mostly fine — the
problem is a tail of 23 functions over 80 lines that carry most of the
confusion, and eleven of them are in two packages.

LIMIT is set at the measured p95 so the gate is green on the day it lands
except for that recorded tail. A gate that is red on arrival gets switched off
within a week, and then it protects nothing. TARGET is where this should end
up: roughly the Linux kernel's "one or two screens" rule, and roughly this
codebase's own p90. Lower LIMIT toward TARGET as the baseline empties.

Length is a symptom, not the disease. A 160-line handler is long because it is
doing four jobs; splitting it into four 40-line functions that still do four
jobs in sequence is how you satisfy a linter without fixing anything.
"""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _gosrc  # noqa: E402
import _ratchet  # noqa: E402

LIMIT = 80
TARGET = 50


def main() -> int:
    violations: dict[str, str] = {}
    worst = 0
    for gf in _gosrc.walk():
        if gf.is_test:
            continue
        for fn in gf.funcs:
            worst = max(worst, fn.lines)
            if fn.lines <= LIMIT:
                continue
            key = f"{gf.rel}:{fn.name}"
            violations[key] = f"{gf.rel}:{fn.start}: {fn.name} is {fn.lines} lines (limit {LIMIT})"
    code = _ratchet.run(
        "func-length",
        f"Function length (limit {LIMIT}, target {TARGET})",
        violations,
        f"Functions longer than {LIMIT} lines. Target is {TARGET}; lower LIMIT as this empties.",
    )
    if code == 0 and not violations and LIMIT > TARGET:
        print(f"    baseline is empty — lower LIMIT toward {TARGET} in this file")
    return code


if __name__ == "__main__":
    sys.exit(main())
