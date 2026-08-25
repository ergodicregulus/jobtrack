#!/usr/bin/env python3
"""Actions are pinned by digest, not by tag.

A tag is a mutable pointer. `actions/checkout@v4` means "whatever the owner of
that repository decides v4 means today", and CI runs with a token and access to
the build. Retagging is not hypothetical: it is the mechanism behind several
real supply-chain incidents, and the compromise is invisible in the diff because
the workflow file does not change.

A 40-character SHA is immutable. Dependabot still bumps it, and the trailing
`# v4` comment keeps the file readable — the version is still visible, it is
just no longer what is resolved.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _ratchet  # noqa: E402

USES = re.compile(r"^\s*-?\s*uses:\s*(\S+)", re.M)
PINNED = re.compile(r"^[^@]+@[0-9a-f]{40}$")


def main() -> int:
    violations: dict[str, str] = {}
    workflows = sorted(Path(".github/workflows").glob("*.y*ml"))

    for wf in workflows:
        for lineno, line in enumerate(wf.read_text().splitlines(), 1):
            m = USES.match(line)
            if not m:
                continue
            ref = m.group(1)
            # A local action (./path) or a docker:// image is not a tag pin.
            if ref.startswith(("./", "docker://")):
                continue
            if PINNED.match(ref):
                continue
            key = f"{wf.name} :: {ref}"
            violations[key] = (
                f"{wf}:{lineno}: {ref} is pinned by tag, which is mutable. "
                f"Resolve it to a commit SHA and keep the tag as a comment."
            )

    if not workflows:
        violations["missing"] = ".github/workflows contains no workflows"

    return _ratchet.run(
        "workflows", "Workflow action pinning", violations,
        "GitHub Actions referenced by mutable tag.",
    )


if __name__ == "__main__":
    sys.exit(main())
