#!/usr/bin/env python3
"""Every document must be reachable from an entry point.

A document nobody can navigate to is a document nobody reads, and an unread
document is one nobody notices going stale — which is how this repository ended
up with a 1,293-line design brief that nothing linked to and a "LIVING DOCUMENT"
that had stopped living two hundred commits earlier.

Reachability is checked from the two places a person actually starts: CLAUDE.md,
which agents and contributors both load first, and docs/README.md, the index.
A doc reachable only from a doc that is itself unreachable does not count, which
is why this walks the graph rather than counting inbound links.

This does not check whether a document is CORRECT or CURRENT — no script can.
It checks the weaker property that someone could find it, which is the
precondition for anyone noticing that it is neither.
"""

from __future__ import annotations

import re
import sys
from collections import deque
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _ratchet  # noqa: E402

ROOTS = [Path("CLAUDE.md"), Path("docs/README.md"), Path("README.md")]
LINK = re.compile(r"\]\(([^)#]+)(?:#[^)]*)?\)")


def _targets(path: Path) -> list[Path]:
    out = []
    for raw in LINK.findall(path.read_text(errors="replace")):
        if raw.startswith(("http://", "https://", "mailto:")):
            continue
        target = (path.parent / raw).resolve()
        # A link to a directory means its README.
        if target.is_dir():
            target = target / "README.md"
        if target.suffix == ".md":
            out.append(target)
    return out


def main() -> int:
    repo = Path.cwd().resolve()
    all_docs = {p.resolve() for p in Path("docs").rglob("*.md")}

    seen: set[Path] = set()
    queue = deque(r.resolve() for r in ROOTS if r.exists())
    seen.update(queue)

    while queue:
        current = queue.popleft()
        if not current.exists():
            continue
        for target in _targets(current):
            if target not in seen:
                seen.add(target)
                queue.append(target)

    violations: dict[str, str] = {}
    for doc in sorted(all_docs - seen):
        rel = doc.relative_to(repo)
        violations[str(rel)] = (
            f"{rel}: nothing reachable from CLAUDE.md or docs/README.md links to "
            f"this. Link it from the index, or delete it."
        )

    return _ratchet.run(
        "docs-reachable",
        "Documentation reachability",
        violations,
        "Docs unreachable from CLAUDE.md and docs/README.md.",
    )


if __name__ == "__main__":
    sys.exit(main())
