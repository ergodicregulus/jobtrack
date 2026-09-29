#!/usr/bin/env python3
"""Verify that every relative Markdown link and #anchor resolves.

Documentation that lies is worse than documentation that is missing: it destroys
trust in everything else on the page. This runs in pre-commit because it takes
milliseconds and catches the single most common docs defect.

Usage:
    scripts/check-links.py                # every .md file
    scripts/check-links.py a.md b.md      # only these (pre-commit passes staged files)
"""

from __future__ import annotations

import os
import re
import sys
from pathlib import Path

LINK = re.compile(r"\[([^\]]*)\]\((?!https?:|mailto:|#!)([^)#]*)(#[^)]*)?\)")
HEADING = re.compile(r"^#{1,6}\s+(.*?)\s*$", re.M)
ANCHOR = re.compile(r'<a id="([^"]+)"')
FENCE = re.compile(r"```.*?```", re.S)

# Directories that are not ours. Vendored READMEs have their own broken links
# and are not something we can or should fix.
IGNORED_PARTS = {"node_modules", ".svelte-kit", "build", "dist", ".git", "vendor"}


def is_ours(path) -> bool:
    return not IGNORED_PARTS.intersection(path.parts)


def slug(heading: str) -> str:
    """Reproduce GitHub's heading-to-anchor rule.

    Punctuation is dropped and spaces become hyphens *individually* — so
    "P6 — Every dependency" yields "p6--every-dependency" with two hyphens,
    because the em dash disappears and leaves two spaces behind. Collapsing
    them is the mistake that makes a naive checker report false positives.
    """
    s = re.sub(r"`([^`]*)`", r"\1", heading)
    s = re.sub(r"~~([^~]*)~~", r"\1", s)
    s = re.sub(r"\*\*?([^*]*)\*\*?", r"\1", s)
    s = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", s)
    s = s.strip().lower()
    s = "".join(c for c in s if c.isalnum() or c in " -_")
    return s.replace(" ", "-")


def anchors_of(path: Path) -> set[str]:
    raw = path.read_text(encoding="utf-8")
    body = FENCE.sub("", raw)  # headings inside code fences are not anchors
    return {slug(h) for h in HEADING.findall(body)} | set(ANCHOR.findall(raw))


def main(argv: list[str]) -> int:
    root = Path(".")
    targets = [Path(a) for a in argv[1:] if a.endswith(".md")] or sorted(root.rglob("*.md"))
    targets = [t for t in targets if t.exists() and is_ours(t)]
    if not targets:
        return 0

    all_md = {str(p) for p in root.rglob("*.md") if is_ours(p)}
    anchor_cache: dict[str, set[str]] = {}
    problems: list[str] = []

    for path in targets:
        body = FENCE.sub("", path.read_text(encoding="utf-8"))
        for match in LINK.finditer(body):
            target, frag = match.group(2), (match.group(3) or "")[1:]

            resolved = str(path) if not target else os.path.normpath(
                os.path.join(os.path.dirname(str(path)), target)
            )
            if os.path.isdir(resolved):
                continue
            if resolved not in all_md:
                # Not Markdown. A link to source, a licence or a fixture is a
                # normal thing for a README to have, and until this branch
                # existed every one of them was reported as broken — which is
                # the failure mode that teaches people to ignore the checker.
                # Existence is all that can be verified; a non-Markdown file
                # has no headings, so #fragments on it are not checked.
                if not os.path.isfile(resolved):
                    problems.append(f"{path}: missing file -> {match.group(0)[:80]}")
                continue
            if not frag:
                continue
            if resolved not in anchor_cache:
                anchor_cache[resolved] = anchors_of(Path(resolved))
            if frag not in anchor_cache[resolved]:
                problems.append(f"{path}: missing anchor -> {match.group(0)[:80]}")

    if problems:
        print(f"{len(problems)} broken documentation link(s):", file=sys.stderr)
        for p in problems:
            print(f"  {p}", file=sys.stderr)
        return 1

    print(f"documentation links ok ({len(targets)} file(s))")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
