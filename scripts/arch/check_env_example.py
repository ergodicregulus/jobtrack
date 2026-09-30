#!/usr/bin/env python3
"""Every variable the config loader reads is documented in .env.example.

This check exists because of the failure it would have caught. `internal/config`
grew RESUME_PARSER_URL and RESUME_ENCRYPTION_KEY as required values for the api
service; docker-compose.yml set both from its shared anchor; .env.example never
mentioned either. So the only readers who ever saw them were the containers,
and the file whose opening line promises that "every variable is validated at
startup" was missing sixteen of forty-three.

The direction matters. A variable in the loader but not the file is a value a
reader cannot discover without grepping Go — the drift this catches. The
reverse, a documented variable nothing reads, is caught too, because a stale
line is a reader following an instruction that does nothing. LOG_SQL was one:
documented as "logs every query with timing", read by no code at all.

The two directions deliberately use different sources. What MUST be documented
is what the loader reads, and only that — REGISTRY and VERSION are build knobs
in the Makefile, not runtime configuration, and listing them here would make
the file longer and less true. What COUNTS as read is wider: PG_PORT is never
seen by the loader — it moves the host-side Postgres port when 5432 is taken —
but it is a real setting and belongs in the file all the same.

Commented lines count as documented: most of these variables SHOULD be shown
commented out, since compose already supplies a development default and an
uncommented value in .env would override it.

It also checks the two deployment configurations, in one direction: every key
the Kubernetes ConfigMap or the production compose file's shared environment sets
must be read by the loader. Both said ENV: production — read by nothing — so
APP_ENV fell back to dev and every production guard was off in production. Both
also set DRAIN_DELAY, read by nothing. Nobody noticed, because an unread variable
fails silently in exactly the way this check makes loud.

What this cannot check: whether the description next to the variable is true.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _ratchet  # noqa: E402

CONFIG = Path("internal/config")
ENV_EXAMPLE = Path(".env.example")
SHELL_READERS = [Path("docker-compose.yml"), Path("docker-compose.prod.yml"), Path("Makefile")]
K8S_CONFIG = Path("deploy/k8s/config.yaml")
PROD_COMPOSE = Path("docker-compose.prod.yml")

# l.str("KEY", ...), l.boolVal("KEY", ...), os.Getenv("KEY") — every accessor
# takes the variable name as its first literal argument.
READ = re.compile(r'(?:l\.(?:str|minLen|intVal|floatVal|boolVal|dur|list|oneOf)|os\.Getenv)\(\s*"([A-Z][A-Z0-9_]*)"')
# ${KEY}, ${KEY:-default}, ${KEY:?message} — compose and make interpolation.
INTERPOLATED = re.compile(r"\$\{([A-Z][A-Z0-9_]*)[:}-]")
# KEY=value, with or without a leading comment marker.
DOCUMENTED = re.compile(r"^#?\s*([A-Z][A-Z0-9_]*)=", re.MULTILINE)


def _block_keys(text: str, start: str, indent: int) -> set[str]:
    """Keys of the YAML mapping that begins on the line matching `start`, read
    by indentation. Deliberately narrow: stdlib only, like every check here."""
    keys: set[str] = set()
    lines = text.splitlines()
    for i, line in enumerate(lines):
        if line.strip() != start:
            continue
        for body in lines[i + 1:]:
            if not body.strip() or body.lstrip().startswith("#"):
                continue
            if len(body) - len(body.lstrip()) < indent:
                break
            m = re.match(rf"^ {{{indent}}}([A-Z][A-Z0-9_]*):", body)
            if m:
                keys.add(m.group(1))
        break
    return keys


def deployment_keys() -> dict[str, set[str]]:
    out: dict[str, set[str]] = {}
    if K8S_CONFIG.exists():
        out[str(K8S_CONFIG)] = _block_keys(K8S_CONFIG.read_text(errors="replace"), "data:", 2)
    if PROD_COMPOSE.exists():
        out[f"{PROD_COMPOSE} (&env)"] = _block_keys(
            PROD_COMPOSE.read_text(errors="replace"), "environment: &env", 4)
    return out


def main() -> int:
    if not CONFIG.is_dir() or not ENV_EXAMPLE.exists():
        return 0

    loader: set[str] = set()
    for go in sorted(CONFIG.glob("*.go")):
        if go.name.endswith("_test.go"):
            continue
        loader |= set(READ.findall(go.read_text(errors="replace")))

    elsewhere: set[str] = set()
    for other in SHELL_READERS:
        if other.exists():
            elsewhere |= set(INTERPOLATED.findall(other.read_text(errors="replace")))

    documented = set(DOCUMENTED.findall(ENV_EXAMPLE.read_text(errors="replace")))
    violations: dict[str, str] = {}

    for key in sorted(loader - documented):
        violations[f"undocumented:{key}"] = (
            f"{key} is read by internal/config but absent from {ENV_EXAMPLE}. "
            f"A value only the compose file knows about is a value nobody "
            f"running the service another way can discover."
        )
    for key in sorted(documented - loader - elsewhere):
        violations[f"unread:{key}"] = (
            f"{key} is documented in {ENV_EXAMPLE} but read by nothing — not "
            f"internal/config, not compose, not the Makefile. Delete the line: "
            f"an instruction that does nothing is worse than no instruction."
        )

    for where, keys in deployment_keys().items():
        for key in sorted(keys - loader):
            violations[f"deploy-unread:{where}:{key}"] = (
                f"{where} sets {key}, which internal/config does not read. It does "
                f"nothing — and if it was meant to be APP_ENV, production is "
                f"running in dev mode."
            )

    return _ratchet.run(
        "env-example",
        ".env.example matches the config loader",
        violations,
        "Variables the loader reads that .env.example does not document, and vice versa.",
    )


if __name__ == "__main__":
    sys.exit(main())
