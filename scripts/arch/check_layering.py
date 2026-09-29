#!/usr/bin/env python3
"""Enforce the dependency direction between packages.

The layout in CLAUDE.md is a claim about which package may know about which.
Nothing was checking it, and it stopped being true: seven files under
internal/api import pgx directly, which makes the HTTP layer also the data
layer. That is the single change that made this codebase feel tangled, and it
happened one honest-looking commit at a time.

Each rule below names the package, what it may not reach for, and why. The why
is the part that survives: a reader who disagrees with a rule needs to know
what it was protecting before deciding to break it.
"""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _gosrc  # noqa: E402
import _ratchet  # noqa: E402

# Read rather than hardcoded. Every layering rule below is a string prefix
# built from this, so a module path this file disagreed with would not fail —
# it would silently match nothing and pass everything.
MOD = next(
    ln.split(None, 1)[1].strip()
    for ln in Path("go.mod").read_text().splitlines()
    if ln.startswith("module ")
)

# (package prefix, forbidden import prefixes, exempt prefixes, why)
RULES: list[tuple[str, tuple[str, ...], tuple[str, ...], str]] = [
    (
        "internal/domain/",
        (f"{MOD}/internal/",),
        (f"{MOD}/internal/domain/",),
        "domain holds entities and business rules; everything may import it and "
        "it may import nothing, which is what keeps it testable in isolation",
    ),
    # internal/api is covered by check_db_access.py instead: naming pgx.Tx for
    # River's client or for a transactional-enqueue callback is legitimate, and
    # an import ban cannot tell that apart from running a query.
    (
        "internal/store/",
        ("net/http", f"{MOD}/internal/api"),
        (),
        "the store must not know it is being called over HTTP, or it cannot be "
        "reused by the ingestor and the matcher",
    ),
    (
        "internal/source/",
        (f"{MOD}/internal/store",),
        (),
        "an adapter returns RawPosting; whether that gets persisted is the "
        "ingest job's decision, not the adapter's",
    ),
    (
        "internal/matching/",
        (f"{MOD}/internal/store", "github.com/jackc/pgx"),
        (),
        "the scorer is a pure function of profile and posting; keeping the "
        "database out of it is why scoring can be tested against a fixed corpus",
    ),
]


def main() -> int:
    violations: dict[str, str] = {}
    for gf in _gosrc.walk():
        if gf.is_test:
            continue
        for prefix, forbidden, exempt, why in RULES:
            if not gf.rel.startswith(prefix):
                continue
            for imp in gf.imports:
                if not imp.startswith(forbidden) or imp.startswith(exempt):
                    continue
                key = f"{gf.rel} -> {imp}"
                violations[key] = f"{gf.rel}:1 imports {imp}\n      {why}"
    return _ratchet.run(
        "layering",
        "Package layering",
        violations,
        "Import-direction violations. See scripts/arch/check_layering.py for the rules.",
    )


if __name__ == "__main__":
    sys.exit(main())
