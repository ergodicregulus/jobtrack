"""A baseline that is only allowed to shrink.

Every invariant in this directory describes a rule the codebase does not fully
obey yet. A check that simply fails is a check somebody deletes on a deadline;
a check with an open-ended allowlist is a rule that quietly stops meaning
anything. The ratchet is the third option: today's violations are recorded, new
ones fail, and a violation that gets fixed must be struck from the record or
the check fails for that too.

That last part is the half people leave out, and it is the half that matters.
Without it the baseline only ever grows stale, and a stale baseline hides the
next regression in the same file.
"""

from __future__ import annotations

import sys
from pathlib import Path

BASELINE_DIR = Path(__file__).parent / "baseline"

RED = "\033[31m" if sys.stderr.isatty() else ""
GREEN = "\033[32m" if sys.stderr.isatty() else ""
DIM = "\033[2m" if sys.stderr.isatty() else ""
OFF = "\033[0m" if sys.stderr.isatty() else ""


def _load(name: str) -> set[str]:
    path = BASELINE_DIR / f"{name}.txt"
    if not path.exists():
        return set()
    return {
        ln.strip()
        for ln in path.read_text().splitlines()
        if ln.strip() and not ln.startswith("#")
    }


def _save(name: str, keys: set[str], header: str) -> None:
    path = BASELINE_DIR / f"{name}.txt"
    body = "\n".join(sorted(keys))
    path.write_text(f"# {header}\n#\n# Managed by `make arch-check-update`. Entries may be removed, never added\n# by hand — a new entry means a new violation, and that is what this file\n# exists to prevent.\n\n{body}\n" if keys else f"# {header}\n#\n# Empty. The rule holds with no exceptions.\n")


def run(name: str, title: str, violations: dict[str, str], header: str) -> int:
    """Compare violations against the baseline. Returns a process exit code."""
    update = "--update" in sys.argv
    baseline = _load(name)
    found = set(violations)

    if update:
        added, removed = found - baseline, baseline - found
        _save(name, found, header)
        for k in sorted(removed):
            print(f"  {GREEN}-{OFF} {k}")
        for k in sorted(added):
            print(f"  {RED}+{OFF} {k}  {DIM}(new violation recorded){OFF}")
        print(f"{title}: baseline now {len(found)} (was {len(baseline)})")
        return 0

    new = sorted(found - baseline)
    fixed = sorted(baseline - found)

    if new:
        print(f"{RED}✗ {title}{OFF}: {len(new)} new violation(s)", file=sys.stderr)
        for k in new:
            print(f"    {violations[k]}", file=sys.stderr)
    if fixed:
        print(
            f"{RED}✗ {title}{OFF}: {len(fixed)} baseline entr(y/ies) no longer "
            f"violate — the ratchet must be tightened",
            file=sys.stderr,
        )
        for k in fixed:
            print(f"    fixed, remove from baseline: {k}", file=sys.stderr)
    if new or fixed:
        print(
            f"\n  Run {DIM}make arch-check-update{OFF} to rewrite the baseline, "
            f"then read the diff.\n"
            f"  Removals are progress. Additions need a reason in the commit message.",
            file=sys.stderr,
        )
        return 1

    if baseline:
        print(f"{GREEN}✓{OFF} {title} {DIM}({len(baseline)} known, none new){OFF}")
    else:
        print(f"{GREEN}✓{OFF} {title}")
    return 0
