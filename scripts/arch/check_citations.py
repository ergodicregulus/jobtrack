#!/usr/bin/env python3
"""Every cited evidence ID resolves, and C-grade evidence stays out of the UI.

CLAUDE.md has claimed "CI fails on an uncited citation" since the repository
was created. No such check existed. This is that check, and it is deliberately
narrower than the sentence it replaces — detecting an *uncited* number in
arbitrary prose is a research problem, and a gate that claims to do it while
missing half the cases is worse than one that states its limits.

What is mechanically checkable, and checked here:

  1. A cited ID (A-00a, B-14, C-03) resolves to a real ledger entry.
  2. A ledger entry's ID prefix matches the grade section it sits under, and
     its HTML anchor matches its printed ID — so deep links do not rot.
  3. A C-grade ID is never referenced from web/src. Grade C means "form a
     hypothesis, never justify a decision or appear unlabelled in the UI"
     (evidence-ledger.md, Grading). That rule was prose; here it has teeth.

What is not checked, and needs a human: whether a number in the UI has a
citation at all. See docs/research/evidence-ledger.md, "Rules for using this
ledger".
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _ratchet  # noqa: E402

LEDGER = Path("docs/research/evidence-ledger.md")
SEARCH_ROOTS = [Path("docs"), Path("web/src"), Path("CLAUDE.md"), Path("README.md")]
UI_ROOTS = ("web/src",)

GRADE_SECTION = re.compile(r"^##\s+([ABC])\s+—", re.M)
ENTRY = re.compile(r'<a id="([abc]-[0-9a-z]+)"></a>\*\*([ABC]-[0-9A-Za-z]+)\s*—', re.M)
CITE = re.compile(r"\b([ABC]-[0-9]{2}[0-9a-z]?)\b")
SKIP_PARTS = {"node_modules", ".svelte-kit", "build", ".git"}


def _files() -> list[Path]:
    out = []
    for root in SEARCH_ROOTS:
        if root.is_file():
            out.append(root)
        elif root.is_dir():
            for p in sorted(root.rglob("*")):
                if p.is_file() and not (SKIP_PARTS & set(p.parts)):
                    if p.suffix in {".md", ".svelte", ".ts", ".js", ".css"}:
                        out.append(p)
    return out


def main() -> int:
    violations: dict[str, str] = {}
    if not LEDGER.exists():
        print(f"missing {LEDGER}", file=sys.stderr)
        return 1

    text = LEDGER.read_text()

    # Grade for each entry comes from the section it sits under.
    grade_of: dict[str, str] = {}
    bounds = [(m.start(), m.group(1)) for m in GRADE_SECTION.finditer(text)]
    for m in ENTRY.finditer(text):
        anchor, printed = m.group(1), m.group(2)
        section = next((g for pos, g in reversed(bounds) if pos < m.start()), None)
        if anchor != printed.lower():
            violations[f"anchor:{printed}"] = (
                f'{LEDGER}: entry {printed} has anchor id="{anchor}" — deep links to it break'
            )
        if section and printed[0] != section:
            violations[f"grade:{printed}"] = (
                f"{LEDGER}: {printed} sits under section {section} but its ID says {printed[0]}"
            )
        grade_of[printed] = section or printed[0]

    if not grade_of:
        violations["empty"] = f"{LEDGER}: no entries parsed — has the format changed?"

    for path in _files():
        if path == LEDGER:
            continue
        rel = str(path)
        for line_no, line in enumerate(path.read_text(errors="replace").splitlines(), 1):
            for cite in set(CITE.findall(line)):
                if cite not in grade_of:
                    violations[f"unknown:{rel}:{cite}"] = (
                        f"{rel}:{line_no}: cites {cite}, which is not in the ledger"
                    )
                elif grade_of[cite] == "C" and rel.startswith(UI_ROOTS):
                    violations[f"cgrade:{rel}:{cite}"] = (
                        f"{rel}:{line_no}: grade-C evidence {cite} referenced from the UI — "
                        f"C may form a hypothesis, never appear as a stated fact"
                    )

    return _ratchet.run(
        "citations", "Evidence citations", violations, "Unresolvable or misgraded citations."
    )


if __name__ == "__main__":
    sys.exit(main())
