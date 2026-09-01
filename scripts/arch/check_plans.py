#!/usr/bin/env python3
"""A plan must be executable, and it must be able to die.

Plans are the most drift-prone documents a repository holds. They describe work
that has not happened, so nothing contradicts them; they are written once and
consulted rarely; and when the work finally lands there is no reason anyone
returns to delete them. That is how a directory of confident, stale intentions
accumulates — which is the exact failure this repository has already cleaned up
once, and the reason docs/engineering/plans/README.md says a finished plan is
DELETED rather than marked complete.

Three properties, each of which a reader depends on and none of which survives
on good intentions:

  1. A falsifiable done-condition. Without one nobody can tell whether a plan is
     finished, so it never gets deleted, so it rots.
  2. Every step tagged [opus] or [sonnet]. The split is about what a mistake
     costs, not how long the work takes, and an untagged step defaults to
     whoever picks it up.
  3. Listed in the index. check_docs.py proves a plan is REACHABLE; this proves
     it is announced, so the table stays the one place to look.

This does not check that a plan is a good plan. No script can.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _ratchet  # noqa: E402

PLANS = Path("docs/engineering/plans")
INDEX = PLANS / "README.md"

# A numbered step: "1. **[opus]** ..." — the tag comes first so it is readable
# in a diff without the surrounding prose.
STEP = re.compile(r"^\d+\.\s+(.*)$", re.MULTILINE)
TAGGED = re.compile(r"^\*\*\[(opus|sonnet)\]\*\*")


def main() -> int:
    if not PLANS.is_dir():
        return 0

    index = INDEX.read_text(errors="replace") if INDEX.exists() else ""
    violations: dict[str, str] = {}

    for plan in sorted(PLANS.glob("*.md")):
        if plan.name == "README.md":
            continue
        rel = str(plan)
        text = plan.read_text(errors="replace")

        if "**Done when:**" not in text:
            violations[f"{rel}:done-when"] = (
                f"{rel}: no '**Done when:**'. A plan without a falsifiable "
                f"done-condition can never be finished, so it is never deleted."
            )
        if "**Status:**" not in text:
            violations[f"{rel}:status"] = f"{rel}: no '**Status:**' line."

        # Only the steps section is tagged; numbered lists elsewhere (options,
        # tables of obligations) are prose and are left alone.
        steps = text.split("## Steps", 1)
        if len(steps) == 2:
            body = steps[1].split("\n## ", 1)[0]
            for step in STEP.findall(body):
                if not TAGGED.match(step.strip()):
                    label = step.strip()[:48]
                    violations[f"{rel}:untagged:{label}"] = (
                        f"{rel}: step '{label}...' is not tagged [opus] or "
                        f"[sonnet]. The split is about what a mistake costs."
                    )

        if plan.name not in index:
            violations[f"{rel}:unlisted"] = (
                f"{rel}: not in the plans index. Reachable is not the same as "
                f"announced — add a row to {INDEX}."
            )

    return _ratchet.run(
        "plans",
        "Plan files are executable",
        violations,
        "Plans missing a done-condition, a tag, or an index entry.",
    )


if __name__ == "__main__":
    sys.exit(main())
