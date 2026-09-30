#!/usr/bin/env python3
"""A workflow that runs a service must supply everything that service requires.

The third copy of the same list. `internal/config` decides what a service needs,
`.env.example` documents it (check_env_example.py), and a CI workflow that starts
a binary directly has to set it — with nothing keeping the three in step.

That gap was not hypothetical. The e2e job ran `go run ./cmd/api` with an env
block holding DATABASE_URL, SESSION_SECRET and COOKIE_SECURE, while the loader
had grown to require RESUME_PARSER_URL and RESUME_ENCRYPTION_KEY for the api
service. The job had never once executed — it was gated behind a step that failed
every time — so the first run after that gate was fixed died with "invalid
configuration" before a single test ran.

What is checked: for each `go run ./cmd/<service>` in a workflow, every variable
the loader marks required for that service is either in the job's `env:` block,
the workflow-level `env:`, or set inline on the command itself.

What is not: whether the VALUE is usable. A URL pointing at nothing still passes
here and fails at runtime, which is the right division — this catches the absent
variable, and the service's own startup validation catches the wrong one.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import _ratchet  # noqa: E402

CONFIG = Path("internal/config/config.go")
WORKFLOWS = Path(".github/workflows")

# l.str("KEY", "", service == "api") — the third argument is the requirement.
REQUIRED = re.compile(
    r'l\.(?:str|minLen)\(\s*"([A-Z][A-Z0-9_]*)"\s*,[^,]*,(?:\s*\d+\s*,)?\s*([^)]*?)\)'
)
SERVICE_EQ = re.compile(r'service\s*==\s*"([a-z-]+)"')
RUNS = re.compile(r"go run \./cmd/([a-z-]+)")
ENV_LINE = re.compile(r"^\s*([A-Z][A-Z0-9_]*)\s*:", re.MULTILINE)
INLINE = re.compile(r"\b([A-Z][A-Z0-9_]*)=")


def required_by_service(text: str) -> dict[str, set[str]]:
    """Map service name -> variables the loader requires for it."""
    out: dict[str, set[str]] = {}
    for key, cond in REQUIRED.findall(text):
        for service in SERVICE_EQ.findall(cond):
            out.setdefault(service, set()).add(key)
    return out


def main() -> int:
    if not CONFIG.exists() or not WORKFLOWS.is_dir():
        return 0

    needs = required_by_service(CONFIG.read_text(errors="replace"))
    violations: dict[str, str] = {}

    for wf in sorted(WORKFLOWS.glob("*.yml")):
        text = wf.read_text(errors="replace")
        # Every KEY: in the file, plus every KEY= set inline on a command. Coarse
        # on purpose: a variable named anywhere in the workflow is assumed
        # reachable, so this reports only the ones nothing mentions at all.
        present = set(ENV_LINE.findall(text)) | set(INLINE.findall(text))

        for service in RUNS.findall(text):
            for key in sorted(needs.get(service, set())):
                if key not in present:
                    violations[f"{wf.name}:{service}:{key}"] = (
                        f"{wf.name} runs ./cmd/{service}, which requires {key}, "
                        f"and no env block in the file sets it. The service will "
                        f"refuse to start with 'invalid configuration'."
                    )

    return _ratchet.run(
        "workflow-env",
        "Workflows supply what the services they run require",
        violations,
        "Variables a workflow's service requires that the workflow never sets.",
    )


if __name__ == "__main__":
    sys.exit(main())
