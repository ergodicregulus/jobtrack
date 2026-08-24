#!/usr/bin/env python3
"""Verify that every `make <target>` the docs mention actually exists.

Onboarding docs that reference commands which no longer exist are worse than no
docs: the first thing a new contributor tries fails, and they stop trusting
everything else on the page.

Only matches `make x` inside backticks or at the start of a line in a code
block, so prose like "make it fast" is not mistaken for a target.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

# `make foo` in a code span, or a line in a fenced block starting with `make foo`
INLINE = re.compile(r"`make ([a-z][a-z0-9-]*)")
IN_BLOCK = re.compile(r"^\s*(?:\$ )?make ([a-z][a-z0-9-]*)", re.M)
FENCE = re.compile(r"```(.*?)```", re.S)

TARGET = re.compile(r"^([a-z][a-z0-9-]*(?:\s+[a-z][a-z0-9-]*)*):(?!=)", re.M)

# Directories that are not ours. Vendored READMEs have their own broken links
# and are not something we can or should fix.
IGNORED_PARTS = {"node_modules", ".svelte-kit", "build", "dist", ".git", "vendor"}


def is_ours(path) -> bool:
    return not IGNORED_PARTS.intersection(path.parts)


def defined_targets(makefile: Path) -> set[str]:
    out: set[str] = set()
    for match in TARGET.finditer(makefile.read_text(encoding="utf-8")):
        out.update(match.group(1).split())
    return out


def referenced(paths: list[Path]) -> dict[str, list[str]]:
    refs: dict[str, list[str]] = {}
    for path in paths:
        text = path.read_text(encoding="utf-8")
        found = set(INLINE.findall(text))
        for block in FENCE.findall(text):
            found.update(IN_BLOCK.findall(block))
        for name in found:
            refs.setdefault(name, []).append(str(path))
    return refs


def main() -> int:
    root = Path(".")
    makefile = root / "Makefile"
    if not makefile.exists():
        print("Makefile not found", file=sys.stderr)
        return 1

    defined = defined_targets(makefile)
    docs = [p for p in sorted(root.rglob("*.md")) if is_ours(p)]
    refs = referenced(docs)

    missing = {name: where for name, where in refs.items() if name not in defined}
    if missing:
        print(f"{len(missing)} make target(s) referenced in docs but not defined:",
              file=sys.stderr)
        for name, where in sorted(missing.items()):
            print(f"  make {name}   (in {', '.join(sorted(set(where)))})", file=sys.stderr)
        print("\nEither define the target or fix the documentation.", file=sys.stderr)
        return 1

    print(f"make targets ok ({len(refs)} referenced, {len(defined)} defined)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
