#!/usr/bin/env python3
"""No unreachable functions.

Wraps golang.org/x/tools/cmd/deadcode, which does a real reachability analysis
from the `cmd/` entry points rather than grepping for identifiers. It found ten
here, and one of them was not a tidiness problem at all:
`SessionStore.DeleteExpired` carried the doc comment "is called by the
scheduler" and had no caller anywhere, so expired session rows accumulated
forever.

Note what this cannot see. A function reachable only from its own test is
unreachable from a binary, but the test still compiles against it, so removing
it breaks the build rather than the analysis. `Cipher.OpenString` was exactly
that: a convenience the product never called, kept alive by a test exercising
it. A test that covers a path production does not take is testing itself.

Requires network on first run to fetch the tool; skipped with a clear message
when unavailable, because a check that fails on a train is a check people
disable.
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _ratchet  # noqa: E402

CMD = [
    "docker", "compose", "run", "--rm", "--no-deps", "-T", "tools",
    "go run golang.org/x/tools/cmd/deadcode@latest ./cmd/...",
]


def main() -> int:
    try:
        proc = subprocess.run(CMD, capture_output=True, text=True, timeout=300)
    except (FileNotFoundError, subprocess.TimeoutExpired):
        print("· Dead code: skipped (docker or network unavailable)")
        return 0

    violations: dict[str, str] = {}
    for line in proc.stdout.splitlines():
        if "unreachable func:" not in line:
            continue
        location, _, name = line.partition("unreachable func:")
        key = name.strip()
        violations[key] = f"{location.strip().rstrip(':')} {key} is never reached from any binary"

    return _ratchet.run(
        "deadcode", "Dead code", violations,
        "Functions unreachable from every cmd/ entry point.",
    )


if __name__ == "__main__":
    sys.exit(main())
